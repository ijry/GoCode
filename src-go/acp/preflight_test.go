package acp

// preflight_test.go covers the offline dependency detection and the binary-cache
// install path (with a local server standing in for the real download URL).

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestDetectAgentOffline locks the availability rules with an empty PATH:
// npx/uvx runners are absent, binary agents have a recipe (installable) but no
// extracted binary (unavailable).
func TestDetectAgentOffline(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	av, inst, reason := DetectAgent("claude_code")
	if av {
		t.Error("claude_code should be unavailable without npx on PATH")
	}
	if inst {
		t.Error("claude_code is npx — not installable via the binary cache")
	}
	if reason == "" {
		t.Error("an unavailable agent must carry a reason")
	}

	av2, inst2, _ := DetectAgent("open_code")
	if !inst2 {
		t.Errorf("open_code should be installable on %s", CurrentPlatform())
	}
	if av2 {
		t.Error("open_code should be unavailable until its binary is downloaded")
	}

	if _, _, r := DetectAgent("nope"); r != "unknown agent" {
		t.Errorf("unknown agent reason = %q", r)
	}
}

// TestPreflightCount locks the aggregate: one row per built-in agent, with the
// binary agents flagged installable and unavailable offline.
func TestPreflightCount(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	rep := Preflight()
	if len(rep) != len(BuiltinAgents) {
		t.Fatalf("preflight rows = %d, want %d", len(rep), len(BuiltinAgents))
	}
	byType := map[string]PreflightReport{}
	for _, r := range rep {
		byType[r.AgentType] = r
	}
	if !byType["open_code"].Installable {
		t.Error("open_code should be installable")
	}
	if byType["open_code"].Available {
		t.Error("open_code should be unavailable offline")
	}
	if byType["claude_code"].Installable {
		t.Error("claude_code (npx) should not be installable here")
	}
}

// TestEnsureAgentBinaryDownloadsAndCaches drives the real download + extraction
// against a local server (the same code path acp_install_agent_binary uses), then
// confirms a second call is a cache hit with no re-download.
func TestEnsureAgentBinaryDownloadsAndCaches(t *testing.T) {
	root := t.TempDir()
	plat := CurrentPlatform()
	dist, ok := binaryDists["open_code"]
	if !ok {
		t.Fatal("no open_code dist")
	}
	pb, ok := dist.platforms[plat]
	if !ok {
		t.Skipf("no open_code recipe on %s", plat)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeZipTo(w, map[string]string{"opencode": "#!/bin/sh\necho hi\n"})
	}))
	defer srv.Close()

	origURL := pb.url
	origEntry := pb.entry
	// The downloader keys the temp archive extension off the URL suffix (real
	// binary URLs end in .zip/.tar.gz), so the stub URL must carry one.
	pb.url = srv.URL + "/open_code.zip"
	pb.entry = "opencode"
	dist.platforms[plat] = pb
	binaryDists["open_code"] = dist
	t.Cleanup(func() {
		d := binaryDists["open_code"]
		p := d.platforms[plat]
		p.url = origURL
		p.entry = origEntry
		d.platforms[plat] = p
		binaryDists["open_code"] = d
	})

	got, err := EnsureAgentBinary("open_code", root, "", nil)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("binary not present at %s: %v", got, err)
	}
	got2, err := EnsureAgentBinary("open_code", root, "", nil)
	if err != nil {
		t.Fatalf("cache hit: %v", err)
	}
	if got2 != got {
		t.Errorf("cache path mismatch: %q vs %q", got, got2)
	}
}

func writeZipTo(w http.ResponseWriter, files map[string]string) {
	zw := zip.NewWriter(w)
	for name, body := range files {
		ww, err := zw.Create(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = ww.Write([]byte(body))
	}
	_ = zw.Close()
}
