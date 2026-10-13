package handlers

// opencode plugin install / uninstall (upstream acp/opencode_plugins.rs:
// install_missing_plugins / uninstall_plugin). Built on the discovery helpers
// in opencode_plugins.go (checkOpencodePlugins, classifyPlugin, pluginPackageDir,
// effectiveSpec, …). Both operations serialize on pluginOpLock and stream
// progress on the same `app://opencode-plugin-install` channel upstream uses,
// with {task_id, kind, payload} where kind ∈ started|log|completed|failed.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"yoyocode/src-go/web"
)

// pluginOpLock serializes install/uninstall, mirroring upstream PLUGIN_OP_LOCK.
var pluginOpLock sync.Mutex

// BunBinary is the bun launcher install/uninstall shell out to. Empty = auto
// resolve (~/.cache/opencode/bin/bun, then system bun). Overridable for tests.
var BunBinary string

const pluginInstallEvent = "app://opencode-plugin-install"

// bunBinary resolves the bun launcher, honoring the BunBinary test hook.
func bunBinary() (string, error) {
	if BunBinary != "" {
		return BunBinary, nil
	}
	if cacheDir := ocCacheDir(); cacheDir != "" {
		for _, cand := range []string{
			filepath.Join(cacheDir, "bin", "bun"),
			filepath.Join(cacheDir, "bin", "bun.exe"),
		} {
			if _, err := os.Stat(cand); err == nil {
				return cand, nil
			}
		}
	}
	if p, err := exec.LookPath("bun"); err == nil {
		return p, nil
	}
	return "", errors.New("bun binary not found (neither ~/.cache/opencode/bin/bun nor system bun)")
}

func emitPluginEvent(c *web.Context, taskID, kind, payload string) {
	if taskID == "" {
		return
	}
	Emit(c, pluginInstallEvent, map[string]any{
		"task_id": taskID,
		"kind":    kind,
		"payload": payload,
	})
}

func isProtectedPackage(name string) bool {
	return strings.HasPrefix(name, "@opencode-ai/")
}

// opencodeInstallPlugins re-checks state, installs every Missing / NeedsMigration
// plugin via `bun add` in its own cache package dir, pins @latest specs to the
// installed version, and streams progress events.
func opencodeInstallPlugins(c *web.Context) (any, error) {
	taskID := strVal(c.Body["taskId"], c.Body["task_id"])
	var names []string
	if arr, ok := c.Body["names"].([]any); ok {
		for _, x := range arr {
			if s, ok := x.(string); ok {
				names = append(names, s)
			}
		}
	}

	pluginOpLock.Lock()
	defer pluginOpLock.Unlock()
	emitPluginEvent(c, taskID, "started", "")

	summary, errMsg := checkOpencodePlugins("")
	if errMsg != "" {
		emitPluginEvent(c, taskID, "failed", errMsg)
		return nil, web.ErrTaskExecution(errMsg)
	}

	// Filter to Missing / NeedsMigration, optionally narrowed by names.
	var targets [][2]string
	var display []string
	for _, p := range summary["plugins"].([]any) {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		st := strVal(pm["status"])
		if st != "missing" && st != "needs_migration" {
			continue
		}
		name := strVal(pm["name"])
		if len(names) > 0 && !containsStr(names, name) {
			continue
		}
		targets = append(targets, [2]string{name, strVal(pm["declared_spec"])})
		display = append(display, name)
	}

	if len(targets) == 0 {
		// Nothing to install; still pin any @latest specs already on disk.
		pinned, perr := pinLatestSpecs(c, strVal(summary["config_path"]), strVal(summary["cache_dir"]), opencodeAllSpecPairs(summary))
		if perr != nil {
			emitPluginEvent(c, taskID, "failed", perr.Error())
			return nil, web.ErrTaskExecution(perr.Error())
		}
		if pinned > 0 {
			emitPluginEvent(c, taskID, "log", fmt.Sprintf("Pinned %d @latest plugin(s) to installed versions in opencode.json", pinned))
			emitPluginEvent(c, taskID, "completed", fmt.Sprintf("Pinned %d @latest plugin(s) — no missing plugins to install", pinned))
		} else {
			emitPluginEvent(c, taskID, "completed", "Nothing to install — all plugins are already present")
		}
		return nil, nil
	}

	bin, berr := bunBinary()
	if berr != nil {
		emitPluginEvent(c, taskID, "failed", berr.Error())
		return nil, web.ErrDependencyMissing(berr.Error())
	}

	emitPluginEvent(c, taskID, "log", "Installing: "+strings.Join(display, ", "))
	cacheDir := strVal(summary["cache_dir"])
	ctx, cancel := context.WithTimeout(context.Background(), agentInstallTimeout)
	defer cancel()

	installed := 0
	for _, t := range targets {
		name, declared := t[0], t[1]
		if isPathSpec(declared) {
			emitPluginEvent(c, taskID, "log", "Skipping "+name+": path plugins are loaded from disk, not installed")
			continue
		}
		dir := pluginPackageDir(cacheDir, effectiveSpec(declared, name))
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			msg := "Failed to create " + dir + ": " + mkErr.Error()
			emitPluginEvent(c, taskID, "failed", msg)
			return nil, web.ErrTaskExecution(msg)
		}
		if err := runBunAdd(ctx, c, bin, dir, declared, taskID); err != nil {
			return nil, web.ErrTaskExecution(err.Error())
		}
		installed++
	}

	// Pin @latest specs (all plugins, not just the ones just installed) so
	// opencode does not hit the npm registry on every startup.
	if pinned, perr := pinLatestSpecs(c, strVal(summary["config_path"]), cacheDir, opencodeAllSpecPairs(summary)); perr != nil {
		emitPluginEvent(c, taskID, "log", "Warning: could not pin @latest versions: "+perr.Error())
	} else if pinned > 0 {
		emitPluginEvent(c, taskID, "log", fmt.Sprintf("Pinned %d @latest plugin(s) to installed versions in opencode.json", pinned))
	}

	if installed == 0 {
		emitPluginEvent(c, taskID, "completed", "Nothing to install — opencode loads these plugins itself")
	} else {
		emitPluginEvent(c, taskID, "completed", fmt.Sprintf("%d plugin(s) installed successfully", installed))
	}
	return nil, nil
}

// opencodeUninstallPlugin removes a plugin from opencode.json, deletes its
// cache directories, runs a legacy `bun remove`, and returns a fresh summary.
func opencodeUninstallPlugin(c *web.Context) (any, error) {
	name := strVal(c.Body["name"])
	if name == "" {
		return nil, web.ErrInvalidInput("name required")
	}
	pluginOpLock.Lock()
	defer pluginOpLock.Unlock()

	if isProtectedPackage(name) {
		return nil, web.ErrInvalidInput("Cannot uninstall " + name + ": it is an internal opencode package")
	}
	configPath := ocConfigPath()
	if configPath == "" {
		return nil, web.ErrTaskExecution("Cannot determine opencode config directory")
	}
	cacheDir := ocCacheDir()
	if cacheDir == "" {
		return nil, web.ErrTaskExecution("Cannot determine opencode cache directory")
	}

	// Step 1: drop the declaration from opencode.json.
	if msg := atomicRewriteOpencodeJSON(configPath, func(doc map[string]any) error {
		arr, ok := doc["plugin"].([]any)
		if !ok {
			return nil
		}
		kept := make([]any, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				if pn, _, ok := parsePluginSpec(s); ok && pn == name {
					continue
				}
			}
			kept = append(kept, item)
		}
		doc["plugin"] = kept
		return nil
	}); msg != "" {
		return nil, web.ErrTaskExecution(msg)
	}

	// A path plugin has no package on disk; removing the declaration is the
	// uninstall. (Mirrors upstream's `is_path_spec` short-circuit.)
	if isPathSpec(name) {
		summary, em := checkOpencodePlugins("")
		if em != "" {
			return nil, web.ErrTaskExecution(em)
		}
		return summary, nil
	}

	// Step 2: remove the modern per-package directories (deleting the dir IS
	// the uninstall there — opencode's hit test is a bare existence check).
	for _, dir := range packageDirsFor(cacheDir, name) {
		if err := os.RemoveAll(dir); err != nil {
			return nil, web.ErrTaskExecution("Failed to remove " + dir + ": " + err.Error())
		}
	}

	// Step 3: legacy flat-layout cleanup via `bun remove` (best-effort; ignore
	// "not found" so an already-clean cache is not an error).
	if bin, berr := bunBinary(); berr == nil {
		cmd := exec.CommandContext(context.Background(), bin, "remove", name)
		cmd.Dir = cacheDir
		out, rerr := cmd.CombinedOutput()
		if rerr != nil && !strings.Contains(string(out), "not found") {
			// Non-fatal: a missing legacy copy does not undo the declaration
			// removal above. Surface as a log line via the summary instead.
			_ = rerr
		}
	}

	summary, em := checkOpencodePlugins("")
	if em != "" {
		return nil, web.ErrTaskExecution(em)
	}
	return summary, nil
}

// runBunAdd runs `bun add <spec>` in dir, streaming both streams as plugin "log"
// events. Uses the same os.Pipe + goroutine-scan + explicit-close pattern as
// runNpmGlobal so the reader always sees EOF (Go does not close a directly
// assigned *os.File on Wait).
func runBunAdd(ctx context.Context, c *web.Context, bin, dir, spec, taskID string) error {
	cmd := exec.CommandContext(ctx, bin, "add", spec)
	cmd.Dir = dir
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		pw.Close()
		msg := "Failed to spawn bun: " + err.Error()
		emitPluginEvent(c, taskID, "failed", msg)
		return errors.New(msg)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			if line := strings.TrimRight(scanner.Text(), "\r"); line != "" {
				emitPluginEvent(c, taskID, "log", line)
			}
		}
	}()
	waitErr := cmd.Wait()
	pw.Close()
	<-done
	if waitErr != nil {
		msg := fmt.Sprintf("bun add %s exited with error", spec)
		emitPluginEvent(c, taskID, "failed", msg)
	}
	return waitErr
}

// opencodeAllSpecPairs returns (name, declared_spec) for every plugin in summary.
func opencodeAllSpecPairs(summary map[string]any) [][2]string {
	out := [][2]string{}
	for _, p := range summary["plugins"].([]any) {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, [2]string{strVal(pm["name"]), strVal(pm["declared_spec"])})
	}
	return out
}

// pinLatestSpecs rewrites @latest specs in opencode.json to the version actually
// on disk, then realigns the package directory (config first, dir move is
// best-effort) so opencode's `packages/<spec>` key matches. Returns the count
// pinned.
func pinLatestSpecs(c *web.Context, configPath, cacheDir string, specs [][2]string) (int, error) {
	type pin struct{ name, version string }
	var pinMap []pin
	for _, sp := range specs {
		name, declared := sp[0], sp[1]
		if !strings.HasSuffix(declared, "@latest") {
			continue
		}
		eff := effectiveSpec(declared, name)
		ver := readPkgVersion(modernPkgJSON(cacheDir, eff, name))
		if ver == nil {
			ver = readPkgVersion(legacyPkgJSON(cacheDir, name))
		}
		if vs, ok := ver.(string); ok && vs != "" {
			pinMap = append(pinMap, pin{name, vs})
		}
	}
	if len(pinMap) == 0 {
		return 0, nil
	}
	err := atomicRewriteOpencodeJSON(configPath, func(doc map[string]any) error {
		arr, ok := doc["plugin"].([]any)
		if !ok {
			return nil
		}
		for i, item := range arr {
			s, ok := item.(string)
			if !ok {
				continue
			}
			pn, _, ok := parsePluginSpec(s)
			if !ok {
				continue
			}
			for _, p := range pinMap {
				if p.name == pn {
					arr[i] = p.name + "@" + p.version
				}
			}
		}
		doc["plugin"] = arr
		return nil
	})
	if err != "" {
		return 0, errors.New(err)
	}
	// Realign directory: config is committed first, so a failed rename just
	// means opencode reinstalls once into the pinned directory.
	for _, p := range pinMap {
		from := pluginPackageDir(cacheDir, p.name+"@latest")
		to := pluginPackageDir(cacheDir, p.name+"@"+p.version)
		if from == to || !fileExists(from) {
			continue
		}
		if fileExists(to) {
			_ = os.RemoveAll(from)
			continue
		}
		_ = os.Rename(from, to)
	}
	return len(pinMap), nil
}

// packageDirsFor returns every `packages/<name>@*` directory for a plugin
// (scoped names nest under packages/@scope). Used by uninstall to clean copies
// left by earlier specs.
func packageDirsFor(cacheDir, name string) []string {
	packages := filepath.Join(cacheDir, "packages")
	var parent, prefix string
	if strings.HasPrefix(name, "@") {
		if i := strings.Index(name, "/"); i >= 0 {
			parent = filepath.Join(packages, name[:i])
			prefix = sanitizeSpec(name[i+1:] + "@")
		} else {
			parent = packages
			prefix = sanitizeSpec(name + "@")
		}
	} else {
		parent = packages
		prefix = sanitizeSpec(name + "@")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(sanitizeSpec(e.Name()), prefix) {
			out = append(out, filepath.Join(parent, e.Name()))
		}
	}
	return out
}

// atomicRewriteOpencodeJSON reads → (refuses JSONC) → backs up (keep 3) →
// mutate → write temp → rename. Mirrors upstream atomic_rewrite_opencode_json.
// Returns "" on success or an error message string.
func atomicRewriteOpencodeJSON(path string, mutate func(doc map[string]any) error) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "Failed to read " + path + ": " + err.Error()
	}
	// Only refuse when a JSON parse fails AND the raw text looks like JSONC
	// (real // or /* comments). A successful parse means any such sequences are
	// inside string values, not comments.
	if json.Unmarshal(raw, &map[string]any{}) != nil &&
		(strings.Contains(string(raw), "//") || strings.Contains(string(raw), "/*")) {
		return "opencode.json appears to be JSONC (contains comments). Refusing to rewrite to avoid data loss."
	}
	writeOpencodeJSONBackup(path, string(raw), 3)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "Failed to parse " + path + ": " + err.Error()
	}
	if err := mutate(doc); err != nil {
		return err.Error()
	}
	newRaw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "Failed to serialize JSON: " + err.Error()
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, newRaw, 0o644); err != nil {
		return "Failed to write temp file: " + err.Error()
	}
	if err := os.Rename(tmp, path); err != nil {
		return "Failed to rename temp file: " + err.Error()
	}
	return ""
}

// writeOpencodeJSONBackup writes a timestamped .bak and prunes to keep copies.
func writeOpencodeJSONBackup(path, content string, keep int) {
	now := time.Now().Format("2006-01-02T15-04-05")
	backupPath := path + ".bak." + now
	_ = os.WriteFile(backupPath, []byte(content), 0o644)
	parent := filepath.Dir(path)
	base := filepath.Base(path)
	prefix := base + ".bak."
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	var backups []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			backups = append(backups, filepath.Join(parent, e.Name()))
		}
	}
	// Timestamped names → ascending sort puts oldest first; drop the oldest
	// beyond `keep`.
	sort.Strings(backups)
	for i := 0; i < len(backups)-keep; i++ {
		_ = os.Remove(backups[i])
	}
}
