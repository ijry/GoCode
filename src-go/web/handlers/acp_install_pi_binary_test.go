package handlers_test

// acp_install_pi_binary / acp_uninstall_pi_binary port coverage.
//
// Both routes shell out to `npm install -g` / `npm uninstall -g` of the pi
// coding-agent package and stream npm output as agent-task progress events.
// Rather than hit the network (or require a real npm), the tests prepend a fake
// `npm` onto PATH so the run is deterministic: exit 0 → 200, exit 1 → non-200.

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"yoyocode/src-go/web/handlers"
)

func writeFakeNpm(t *testing.T, dir string, exitCode int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		script := "@echo off\r\necho npm-fake: %*\r\nexit /b " + strconv.Itoa(exitCode) + "\r\n"
		if err := os.WriteFile(filepath.Join(dir, "npm.bat"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		script := "#!/bin/sh\necho \"npm-fake: $@\"\nexit " + strconv.Itoa(exitCode) + "\n"
		if err := os.WriteFile(filepath.Join(dir, "npm"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// withFakeNpm points handlers.NpmBinary at a fake `npm` (exiting exitCode) in a
// temp dir, so the install routes run it deterministically instead of any real
// npm on PATH (no network, no dependency on a real Node install).
func withFakeNpm(t *testing.T, exitCode int) {
	t.Helper()
	dir := t.TempDir()
	writeFakeNpm(t, dir, exitCode)
	var p string
	if runtime.GOOS == "windows" {
		p = filepath.Join(dir, "npm.bat")
	} else {
		p = filepath.Join(dir, "npm")
	}
	orig := handlers.NpmBinary
	handlers.NpmBinary = p
	t.Cleanup(func() { handlers.NpmBinary = orig })
}

func TestAcpInstallPiBinaryRunsNpm(t *testing.T) {
	withFakeNpm(t, 0)
	srv := newTestServer(t)
	code, out := call(t, srv, "acp_install_pi_binary", map[string]any{"taskId": "t1"})
	if code != 200 {
		t.Fatalf("install status %d: %v", code, out)
	}
}

func TestAcpUninstallPiBinaryRunsNpm(t *testing.T) {
	withFakeNpm(t, 0)
	srv := newTestServer(t)
	code, out := call(t, srv, "acp_uninstall_pi_binary", map[string]any{"taskId": "t2"})
	if code != 200 {
		t.Fatalf("uninstall status %d: %v", code, out)
	}
}

func TestAcpInstallPiBinaryFailsOnNpmError(t *testing.T) {
	withFakeNpm(t, 1)
	srv := newTestServer(t)
	code, _ := call(t, srv, "acp_install_pi_binary", map[string]any{"taskId": "t3"})
	if code == 200 {
		t.Fatalf("expected non-200 when npm exits non-zero, got 200")
	}
}
