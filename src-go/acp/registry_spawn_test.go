package acp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestResolveSpawnBinaryFallsBackToPath pins the usability fallback: a binary
// agent (e.g. open_code → `opencode`) must resolve from PATH when the app's
// download cache is empty, so a machine that already has the CLI can launch it.
func TestResolveSpawnBinaryFallsBackToPath(t *testing.T) {
	orig := binaryCacheRoot
	binaryCacheRoot = "" // force the cache lookup to miss
	t.Cleanup(func() { binaryCacheRoot = orig })

	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(dir, "opencode.bat"), []byte("@echo off\r\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg, ok := ResolveSpawn("open_code", t.TempDir(), nil)
	if !ok {
		t.Fatalf("open_code should resolve via the PATH fallback")
	}
	if !strings.Contains(cfg.Command, "opencode") {
		t.Fatalf("command = %q, want the on-PATH opencode", cfg.Command)
	}
	if len(cfg.Args) == 0 || cfg.Args[0] != "acp" {
		t.Fatalf("args = %v, want [acp ...]", cfg.Args)
	}
}

// TestResolveSpawnUnknownAgent: an unknown wire type never resolves.
func TestResolveSpawnUnknownAgent(t *testing.T) {
	if _, ok := ResolveSpawn("no_such_agent", t.TempDir(), nil); ok {
		t.Fatalf("unknown agent must not resolve")
	}
}
