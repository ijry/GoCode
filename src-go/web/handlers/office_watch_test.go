package handlers

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	apperror "gocode/src-go/app_error"
	"gocode/src-go/officewatch"
)

func TestParseWatchPort(t *testing.T) {
	cases := []struct {
		line string
		port int
		ok   bool
	}{
		{"Watch: http://localhost:26411", 26411, true},
		{"  Watch: http://127.0.0.1:1234/", 1234, true},
		{"Watching: stuff", 0, false},
		{"Press Ctrl+C to stop", 0, false},
		{"Watch: no-colon-digits", 0, false},
	}
	for _, c := range cases {
		got, ok := parseWatchPort(c.line)
		if got != c.port || ok != c.ok {
			t.Fatalf("parseWatchPort(%q) = %d,%v; want %d,%v", c.line, got, ok, c.port, c.ok)
		}
	}
}

func TestWatchKeyCaseFoldsOnWindows(t *testing.T) {
	got := watchKey(`C:\Dir\File.docx`)
	want := "C:/Dir/File.docx"
	if runtime.GOOS == "windows" {
		want = "c:/dir/file.docx"
	}
	if got != want {
		t.Fatalf("watchKey = %q, want %q", got, want)
	}
}

func TestResolveOfficeTarget(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.docx"))
	writeTestFile(t, filepath.Join(root, "b.txt"))

	if _, err := resolveOfficeTarget(root, "a.docx"); err != nil {
		t.Fatalf("docx inside root should resolve: %v", err)
	}
	if _, err := resolveOfficeTarget(root, "b.txt"); err == nil {
		t.Fatalf("non-office file must be rejected")
	}
	if _, err := resolveOfficeTarget(root, "../escape.docx"); err == nil {
		t.Fatalf("traversal must be rejected")
	}
	if _, err := resolveOfficeTarget(filepath.Join(root, "nope"), "a.docx"); err == nil {
		t.Fatalf("missing root must be rejected")
	}
}

func TestStartOfficeWatchDependencyMissing(t *testing.T) {
	withFakeWatch(t, func() string { return "" }, nil, nil, nil)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.docx"))

	_, err := startOfficeWatchCore(root, "a.docx")
	if err == nil {
		t.Fatalf("expected dependency_missing")
	}
	ae, ok := err.(*apperror.AppCommandError)
	if !ok {
		t.Fatalf("err type = %T, want *AppCommandError", err)
	}
	if ae.Code != apperror.CodeDependencyMissing {
		t.Fatalf("code = %q, want dependency_missing", ae.Code)
	}
	if ae.I18nParams["watchCode"] != "NOT_INSTALLED" {
		t.Fatalf("watchCode = %q, want NOT_INSTALLED", ae.I18nParams["watchCode"])
	}
}

func TestStartStopOfficeWatch_PoolAndRegistry(t *testing.T) {
	withFakeWatch(t,
		func() string { return "officecli-fake" },
		func(binary, target string, port int) (*watchProcess, error) { return &watchProcess{}, nil },
		func(port int, p *watchProcess) error { return nil },
		func() (int, error) { return 26500, nil },
	)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "a.docx"))

	started, err := startOfficeWatchCore(root, "a.docx")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	port, _ := started["port"].(int)
	cap, _ := started["cap"].(string)
	if port != 26500 || cap == "" {
		t.Fatalf("started = %v", started)
	}
	if !officewatch.ValidateWatchCap(port, cap) {
		t.Fatalf("registry not populated after start")
	}

	// Second open reuses the same process (same port + cap).
	again, err := startOfficeWatchCore(root, "a.docx")
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if again["port"] != port || again["cap"] != cap {
		t.Fatalf("reuse should share port+cap: %v vs %d/%s", again, port, cap)
	}

	// First stop decrements the ref-count; the watch stays live.
	if err := stopOfficeWatchCore(root, "a.docx"); err != nil {
		t.Fatalf("stop1: %v", err)
	}
	if !officewatch.ValidateWatchCap(port, cap) {
		t.Fatalf("watch still referenced — should stay registered")
	}
	// Last stop reaps + unregisters (closes the proxy gate).
	if err := stopOfficeWatchCore(root, "a.docx"); err != nil {
		t.Fatalf("stop2: %v", err)
	}
	if officewatch.ValidateWatchCap(port, cap) {
		t.Fatalf("last stop must unregister")
	}
	// Idempotent.
	if err := stopOfficeWatchCore(root, "a.docx"); err != nil {
		t.Fatalf("stop3: %v", err)
	}
}

// withFakeWatch swaps the injectable seams for the duration of a test and
// isolates the shared pool, restoring everything on cleanup.
func withFakeWatch(
	t *testing.T,
	resolve func() string,
	spawn func(binary, target string, port int) (*watchProcess, error),
	ready func(port int, p *watchProcess) error,
	allocPort func() (int, error),
) {
	t.Helper()
	officeWatchPool.mu.Lock()
	officeWatchPool.byKey = map[string]*officeWatchProc{}
	officeWatchPool.mu.Unlock()

	origResolve, origSpawn, origReady, origPort :=
		officeWatchResolveBinary, officeWatchSpawn, officeWatchAwaitReady, officeWatchAllocatePort
	t.Cleanup(func() {
		officeWatchResolveBinary, officeWatchSpawn, officeWatchAwaitReady, officeWatchAllocatePort =
			origResolve, origSpawn, origReady, origPort
		officeWatchPool.mu.Lock()
		officeWatchPool.byKey = map[string]*officeWatchProc{}
		officeWatchPool.mu.Unlock()
	})

	officeWatchResolveBinary = resolve
	if spawn != nil {
		officeWatchSpawn = spawn
	}
	if ready != nil {
		officeWatchAwaitReady = ready
	}
	if allocPort != nil {
		officeWatchAllocatePort = allocPort
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
