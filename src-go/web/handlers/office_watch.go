package handlers

// Office watch process pool — the Go port of upstream
// src-tauri/src/office_watch (start_office_watch_core / stop_office_watch_core).
//
// A watch is a long-lived `officecli watch <file> --port <n>` child serving the
// live document preview over loopback. Watches are shared by ref-count per
// canonical file: the first open spawns, later opens adopt the same process, and
// the last close reaps it. Each watch mints a per-watch capability the
// server-mode reverse proxy (office_watch_proxy.go) authenticates against.
//
// This slice ports start/stop + the ref-counted pool + the registry the proxy
// reads. The idle sweep is not ported (watches are reaped on stop); a watch that
// crashes leaves a stale pool entry until its next stop — noted as a gap.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	apperror "gocode/src-go/app_error"
	"gocode/src-go/officewatch"
	"gocode/src-go/web"
)

const (
	// watchReadyTimeout bounds how long we wait for a fresh watch to announce
	// its `Watch: http://…:<port>` readiness line.
	watchReadyTimeout = 8 * time.Second
	// watchReadyConnectTimeout is the post-announce reachability confirm.
	watchReadyConnectTimeout = 500 * time.Millisecond
	// maxConcurrentWatches is a backstop against unbounded officecli spawns.
	maxConcurrentWatches = 32
)

// Injectable seams (tests override to avoid a real officecli binary).
var (
	officeWatchResolveBinary = resolveOfficecli
	officeWatchSpawn         = spawnOfficeWatchProcess
	officeWatchAwaitReady    = awaitOfficeWatchReady
	officeWatchAllocatePort  = allocateFreeWatchPort
)

// errWatchReadyTimeout marks a readiness wait that expired (vs. a start failure)
// so the caller can report the PORT_TIMEOUT code.
var errWatchReadyTimeout = errors.New("watch ready timeout")

// watchProcess is a spawned officecli watch child plus the pipes we read.
type watchProcess struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr *bytes.Buffer
}

// officeWatchProc is one live watch in the pool.
type officeWatchProc struct {
	key      string
	port     int
	cap      string
	refCount int
	proc     *watchProcess
}

var officeWatchPool = struct {
	mu    sync.Mutex
	byKey map[string]*officeWatchProc
}{byKey: map[string]*officeWatchProc{}}

// ── handlers ────────────────────────────────────────────────────────────────

func startOfficeWatchHandler(c *web.Context) (any, error) {
	root := GetString(c, "rootPath", "root_path")
	path := GetString(c, "path")
	return startOfficeWatchCore(root, path)
}

func stopOfficeWatchHandler(c *web.Context) (any, error) {
	root := GetString(c, "rootPath", "root_path")
	path := GetString(c, "path")
	if err := stopOfficeWatchCore(root, path); err != nil {
		return nil, err
	}
	return nil, nil
}

// ── core ────────────────────────────────────────────────────────────────────

// startOfficeWatchCore ensures a watch serves `path` (relative to root_path) and
// returns {port, cap}. Shares a live process by ref-count; only spawns when none
// is live. Mirrors upstream start_office_watch_core.
func startOfficeWatchCore(rootPath, path string) (map[string]any, error) {
	canonical, err := resolveOfficeTarget(rootPath, path)
	if err != nil {
		return nil, err
	}
	key := watchKey(canonical)

	// Fast path: a live process already exists → share it (no binary needed).
	if port, cap, ok := reuseLiveWatch(key); ok {
		return map[string]any{"port": port, "cap": cap}, nil
	}

	binary := officeWatchResolveBinary()
	if binary == "" {
		return nil, watchErr("NOT_INSTALLED", web.ErrDependencyMissing("officecli is not installed"))
	}

	officeWatchPool.mu.Lock()
	tooMany := len(officeWatchPool.byKey) >= maxConcurrentWatches
	officeWatchPool.mu.Unlock()
	if tooMany {
		return nil, watchErr("TOO_MANY", web.ErrTaskExecution("too many office preview servers are already running"))
	}

	port, perr := officeWatchAllocatePort()
	if perr != nil {
		return nil, watchErr("NO_PORT", web.ErrTaskExecution("no free port available for the preview server"))
	}
	cap := officewatch.NewCap()
	proc, serr := officeWatchSpawn(binary, canonical, port)
	if serr != nil {
		return nil, watchErr("START_FAILED", web.ErrTaskExecution("failed to start officecli watch: "+serr.Error()))
	}
	if rerr := officeWatchAwaitReady(port, proc); rerr != nil {
		detail := rerr.Error()
		if proc != nil && proc.stderr != nil {
			if s := strings.TrimSpace(proc.stderr.String()); s != "" {
				detail += " — " + s
			}
		}
		reapWatch(proc)
		if errors.Is(rerr, errWatchReadyTimeout) {
			return nil, watchErr("PORT_TIMEOUT", web.ErrTaskExecution("officecli watch did not become ready in time: "+detail))
		}
		return nil, watchErr("START_FAILED", web.ErrTaskExecution("failed to start officecli watch: "+detail))
	}

	// Register under the pool lock; re-check for a race winner while we waited.
	officeWatchPool.mu.Lock()
	if existing, ok := officeWatchPool.byKey[key]; ok {
		existing.refCount++
		port, cap = existing.port, existing.cap
		officeWatchPool.mu.Unlock()
		reapWatch(proc)
		return map[string]any{"port": port, "cap": cap}, nil
	}
	officeWatchPool.byKey[key] = &officeWatchProc{key: key, port: port, cap: cap, refCount: 1, proc: proc}
	officeWatchPool.mu.Unlock()
	officewatch.Register(key, port, cap)
	return map[string]any{"port": port, "cap": cap}, nil
}

// stopOfficeWatchCore releases one reference; kills the process when the last
// reference goes away. Idempotent. Mirrors upstream stop_office_watch_core.
func stopOfficeWatchCore(rootPath, path string) error {
	key, ok := looseWatchKey(rootPath, path)
	if !ok {
		return nil
	}
	officeWatchPool.mu.Lock()
	e, ok := officeWatchPool.byKey[key]
	if !ok {
		officeWatchPool.mu.Unlock()
		return nil
	}
	if e.refCount > 1 {
		e.refCount--
		officeWatchPool.mu.Unlock()
		return nil
	}
	delete(officeWatchPool.byKey, key)
	officeWatchPool.mu.Unlock()
	// Removing the registry entry instantly closes the proxy gate.
	officewatch.Unregister(key)
	reapWatch(e.proc)
	return nil
}

// reuseLiveWatch bumps the ref-count of a live watch and returns its (port, cap).
func reuseLiveWatch(key string) (int, string, bool) {
	officeWatchPool.mu.Lock()
	defer officeWatchPool.mu.Unlock()
	e, ok := officeWatchPool.byKey[key]
	if !ok {
		return 0, "", false
	}
	e.refCount++
	return e.port, e.cap, true
}

// reapWatch kills a watch child and reaps it without blocking the caller.
func reapWatch(p *watchProcess) {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Kill()
	go func() { _ = p.cmd.Wait() }()
}

// ── target resolution / keys ─────────────────────────────────────────────────

// resolveOfficeTarget resolves rel against root, confines it to the root, and
// requires an office extension — the same defense-in-depth as
// officecli_render_html. Returns the symlink-resolved absolute path.
func resolveOfficeTarget(rootPath, rel string) (string, error) {
	if err := requireFolderDir(rootPath); err != nil {
		return "", watchErr("IO", web.ErrIO("io error: workspace root does not exist"))
	}
	target, terr := resolveTreePath(rootPath, rel)
	if terr != nil {
		return "", watchErr("IO", web.ErrIO("io error: "+terr.Error()))
	}
	if e := ensureNavigable(rootPath, target); e != nil {
		return "", watchErr("OUTSIDE_ROOT", web.ErrInvalidInput("path is outside the workspace root"))
	}
	if fi, serr := os.Stat(target); serr != nil || fi.IsDir() {
		return "", watchErr("IO", web.ErrIO("io error: path is not a file"))
	}
	if !isOfficePath(target) {
		return "", watchErr("NOT_OFFICE", web.ErrInvalidInput("not a supported office file (.docx/.xlsx/.pptx)"))
	}
	if canon, e := filepath.EvalSymlinks(target); e == nil {
		target = canon
	}
	return target, nil
}

// watchKey is the canonical registry key for a file (case-folded on Windows so
// the same file referenced with different casing collapses to one watch).
func watchKey(canonical string) string {
	s := strings.ReplaceAll(canonical, "\\", "/")
	if runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}
	return s
}

// looseWatchKey is a best-effort key for stop — falls back to a loose
// canonicalization so a since-deleted file can still be released by key.
func looseWatchKey(rootPath, rel string) (string, bool) {
	if canonical, err := resolveOfficeTarget(rootPath, rel); err == nil {
		return watchKey(canonical), true
	}
	target, err := resolveTreePath(rootPath, rel)
	if err != nil {
		return "", false
	}
	if canon, e := filepath.EvalSymlinks(target); e == nil {
		target = canon
	}
	return watchKey(target), true
}

// watchErr attaches the stable watchCode i18n param the frontend branches on.
func watchErr(code string, base *apperror.AppCommandError) *apperror.AppCommandError {
	return base.WithI18n("Folder.fileWorkspacePanel.officeWatchError", map[string]string{"watchCode": code})
}

// ── spawn / readiness ────────────────────────────────────────────────────────

// allocateFreeWatchPort asks the OS for a free loopback port (bind :0, release).
func allocateFreeWatchPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// spawnOfficeWatchProcess launches `officecli watch <target> --port <n>`.
func spawnOfficeWatchProcess(binary, target string, port int) (*watchProcess, error) {
	cmd := exec.Command(binary, "watch", target, "--port", strconv.Itoa(port))
	cmd.Stdin = nil
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &watchProcess{cmd: cmd, stdout: stdout, stderr: stderr}, nil
}

type watchLine struct {
	line string
	err  error
}

// awaitOfficeWatchReady waits for the child to announce `Watch: …:<port>` on
// stdout and confirms the port accepts a connection. Reads *our child's* stdout
// so the listener is positively identified as this officecli.
func awaitOfficeWatchReady(port int, p *watchProcess) error {
	if p == nil || p.stdout == nil {
		return errors.New("no stdout pipe")
	}
	reader := bufio.NewReader(p.stdout)
	ch := make(chan watchLine, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			select {
			case ch <- watchLine{line: strings.TrimRight(line, "\r\n"), err: err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	deadline := time.Now().Add(watchReadyTimeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("%w: officecli did not announce readiness in time", errWatchReadyTimeout)
		}
		select {
		case res := <-ch:
			if res.line != "" {
				if bound, ok := parseWatchPort(res.line); ok {
					if bound != port {
						return fmt.Errorf("officecli bound port %d, expected %d", bound, port)
					}
					conn, cerr := net.DialTimeout("tcp",
						net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), watchReadyConnectTimeout)
					if cerr != nil {
						return errors.New("officecli announced ready but the port did not accept")
					}
					conn.Close()
					// Keep the pipe drained for the child's lifetime.
					go func() { for { if _, err := reader.ReadString('\n'); err != nil { return } } }()
					return nil
				}
			}
			if res.err != nil {
				if res.err == io.EOF {
					return errors.New("officecli exited before announcing readiness")
				}
				return fmt.Errorf("officecli exited before announcing readiness: %v", res.err)
			}
		case <-time.After(remaining):
			return fmt.Errorf("%w: officecli did not announce readiness in time", errWatchReadyTimeout)
		}
	}
}

// parseWatchPort parses officecli's readiness line, e.g.
// `Watch: http://localhost:26411`, into the port it bound.
func parseWatchPort(line string) (int, bool) {
	line = strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(line, "Watch:") {
		return 0, false
	}
	idx := strings.LastIndex(line, ":")
	if idx < 0 {
		return 0, false
	}
	digits := ""
	for _, c := range line[idx+1:] {
		if c >= '0' && c <= '9' {
			digits += string(c)
			continue
		}
		break
	}
	if digits == "" {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return n, true
}
