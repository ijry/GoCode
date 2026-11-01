// Command gocode-mcp is the GoCode workbench companion MCP server — the Go
// counterpart of upstream `codeg-mcp` (src-tauri/src/bin/codeg_mcp.rs). It
// speaks MCP over stdio (JSON-RPC 2.0), so any MCP-capable agent (Claude Code,
// Cursor, opencode, …) can be pointed at it to report progress and author
// automations / work tasks back into GoCode.
//
// Configuration (injected by the app when it launches a session, or set by hand):
//
//	GOCODE_API        base URL of the running backend, e.g. http://127.0.0.1:PORT
//	GOCODE_TOKEN      loopback auth token
//	GOCODE_FOLDER_ID  active folder (authors work tasks / scopes automations)
//	GOCODE_AGENT_TYPE agent type in use
//	GOCODE_SESSION_ID conversation id (get_session_info)
//
// run: gocode-mcp   (reads the env, serves on stdin/stdout)
package main

import (
	"log"
	"os"

	"gocode/src-go/mcp"
)

func main() {
	c := mcp.NewCompanion(
		os.Getenv("GOCODE_API"),
		os.Getenv("GOCODE_TOKEN"),
		os.Getenv("GOCODE_FOLDER_ID"),
		os.Getenv("GOCODE_AGENT_TYPE"),
		os.Getenv("GOCODE_SESSION_ID"),
	)
	if c.APIBase == "" {
		log.Printf("[gocode-mcp] warning: GOCODE_API unset; authoring tools will fail")
	}
	if err := c.Serve(os.Stdin, os.Stdout); err != nil {
		log.Printf("[gocode-mcp] serve: %v", err)
	}
}
