package handlers_test

// opencode_install_plugins / opencode_uninstall_plugin coverage.
//
// The routes operate on ~/.config/opencode/opencode.json + ~/.cache/opencode
// (XDG). Tests redirect XDG_CONFIG_HOME / XDG_CACHE_HOME to temp dirs and point
// handlers.BunBinary at a fake bun, so everything is isolated and offline.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gocode/src-go/web/handlers"
)

func writeFakeBun(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		// add → create <cwd>/node_modules/<name>/package.json (version 1.2.3);
		// remove → no-op.
		bat := "@echo off\r\n" +
			"set \"spec=%~2\"\r\n" +
			"for /f \"tokens=1 delims=@\" %%a in (\"%spec%\") do set \"name=%%a\"\r\n" +
			"if \"%~1\"==\"add\" (\r\n" +
			"  if not exist \"%CD%\\node_modules\\%name%\" mkdir \"%CD%\\node_modules\\%name%\"\r\n" +
			"  echo {\"version\":\"1.2.3\"}>\"%CD%\\node_modules\\%name%\\package.json\"\r\n" +
			")\r\n" +
			"exit /b 0\r\n"
		if err := os.WriteFile(filepath.Join(dir, "bun.bat"), []byte(bat), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		sh := "#!/bin/sh\n" +
			"spec=\"$2\"\n" +
			"name=\"${spec%@*}\"\n" +
			"if [ \"$1\" = \"add\" ]; then\n" +
			"  mkdir -p \"$PWD/node_modules/$name\"\n" +
			"  echo '{\"version\":\"1.2.3\"}' > \"$PWD/node_modules/$name/package.json\"\n" +
			"fi\n" +
			"exit 0\n"
		if err := os.WriteFile(filepath.Join(dir, "bun"), []byte(sh), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func setFakeBun(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFakeBun(t, dir)
	var p string
	if runtime.GOOS == "windows" {
		p = filepath.Join(dir, "bun.bat")
	} else {
		p = filepath.Join(dir, "bun")
	}
	orig := handlers.BunBinary
	handlers.BunBinary = p
	t.Cleanup(func() { handlers.BunBinary = orig })
	return p
}

func writeOpencodeConfig(t *testing.T, pluginArray string) string {
	t.Helper()
	cfg := t.TempDir()
	cache := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_CACHE_HOME", cache)
	if err := os.MkdirAll(filepath.Join(cfg, "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `{"plugin":` + pluginArray + `}`
	if err := os.WriteFile(filepath.Join(cfg, "opencode", "opencode.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestOpencodeInstallNothingToInstall(t *testing.T) {
	writeOpencodeConfig(t, "[]")
	srv := newTestServer(t)
	code, out := call(t, srv, "opencode_install_plugins", map[string]any{"taskId": "t1"})
	if code != 200 {
		t.Fatalf("install status %d: %v", code, out)
	}
}

func TestOpencodeInstallPinsLatest(t *testing.T) {
	cfg := writeOpencodeConfig(t, `["foo@latest"]`)
	setFakeBun(t)
	srv := newTestServer(t)
	code, out := call(t, srv, "opencode_install_plugins", map[string]any{"taskId": "t2"})
	if code != 200 {
		t.Fatalf("install status %d: %v", code, out)
	}
	raw, err := os.ReadFile(filepath.Join(cfg, "opencode", "opencode.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var doc struct {
		Plugin []string `json:"plugin"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse: %v (%s)", err, raw)
	}
	if len(doc.Plugin) != 1 || doc.Plugin[0] != "foo@1.2.3" {
		t.Fatalf("expected @latest pinned to foo@1.2.3, got %s", raw)
	}
}

func TestOpencodeUninstallRemovesFromConfig(t *testing.T) {
	cfg := writeOpencodeConfig(t, `["foo","@keep/bar@1.0.0"]`)
	setFakeBun(t) // remove is a no-op
	srv := newTestServer(t)
	code, out := call(t, srv, "opencode_uninstall_plugin", map[string]any{"name": "foo"})
	if code != 200 {
		t.Fatalf("uninstall status %d: %v", code, out)
	}
	raw, _ := os.ReadFile(filepath.Join(cfg, "opencode", "opencode.json"))
	if !strings.Contains(string(raw), "@keep/bar@1.0.0") || strings.Contains(string(raw), `"foo"`) {
		t.Fatalf("expected foo removed and @keep/bar retained, got %s", raw)
	}
}

func TestOpencodeUninstallRejectsProtected(t *testing.T) {
	writeOpencodeConfig(t, `["@opencode-ai/core"]`)
	setFakeBun(t)
	srv := newTestServer(t)
	code, _ := call(t, srv, "opencode_uninstall_plugin", map[string]any{"name": "@opencode-ai/core"})
	if code == 200 {
		t.Fatalf("protected package @opencode-ai/ should be rejected")
	}
}
