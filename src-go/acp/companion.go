package acp

// Companion wiring: the gocode-mcp workbench MCP server is the Go companion
// of upstream codeg-mcp (src-tauri/src/bin/codeg_mcp.rs). Unlike upstream — which
// talks to codeg over a delegation socket — our port is a stdio MCP server that
// proxies the authoring tools to the loopback backend and reads the live session
// context from environment variables. The app injects that server into every
// real ACP session's `session/new` mcpServers list so the agent runtime spawns
// it per session with the right context.
//
// Mirrors upstream locate_codeg_mcp_binary / inject_codeg_mcp: the binary is
// located, and when it is missing we skip injection (never register a phantom
// path, which stricter agents would reject and take the whole session down).

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// ResolveCompanionBinary locates the gocode-mcp companion binary. Search order
// mirrors upstream: GOCODE_MCP_PATH override, then the running executable's
// directory (the sidecar lives next to the app in a packaged build), then PATH.
// Returns ("", false) when no candidate is an executable file.
func ResolveCompanionBinary() (string, bool) {
	filename := "gocode-mcp"
	if runtime.GOOS == "windows" {
		filename = "gocode-mcp.exe"
	}
	if raw := os.Getenv("GOCODE_MCP_PATH"); raw != "" {
		if isExecutableFile(raw) {
			return raw, true
		}
	}
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			if cand := filepath.Join(dir, filename); isExecutableFile(cand) {
				return cand, true
			}
		}
	}
	if p, err := exec.LookPath(filename); err == nil {
		return p, true
	}
	return "", false
}

// isExecutableFile reports whether path is a regular, runnable file. On Unix at
// least one execute bit must be set; on Windows any regular file counts.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return false
	}
	return true
}

// CompanionMCPServer builds the stdio MCP server entry the agent runtime should
// spawn for the gocode-mcp workbench companion. It returns (nil, false) when
// the binary cannot be located so the caller can skip injection. The live
// session context — backend API/token, agent type, session id, folder id — is
// injected as environment variables the companion reads (see Companion.env).
//
// The wire shape is the de-facto MCP stdio server spec agent runtimes such as
// opencode expect under session/new's mcpServers (type/command/args/env).
func CompanionMCPServer(apiBase, token, folderID, agentType, sessionID string) (map[string]any, bool) {
	bin, ok := ResolveCompanionBinary()
	if !ok {
		return nil, false
	}
	env := map[string]any{}
	if apiBase != "" {
		env["GOCODE_API"] = apiBase
	}
	if token != "" {
		env["GOCODE_TOKEN"] = token
	}
	if folderID != "" {
		env["GOCODE_FOLDER_ID"] = folderID
	}
	if agentType != "" {
		env["GOCODE_AGENT_TYPE"] = agentType
	}
	if sessionID != "" {
		env["GOCODE_SESSION_ID"] = sessionID
	}
	return map[string]any{
		"name":    "gocode-mcp",
		"type":    "stdio",
		"command": bin,
		"args":    []any{},
		"env":     env,
	}, true
}
