// Command yoyocode-mcp is the YoYoCode workbench companion MCP server — the Go
// counterpart of upstream `codeg-mcp` (src-tauri/src/bin/codeg_mcp.rs). It
// speaks MCP over stdio (JSON-RPC 2.0), so any MCP-capable agent (Claude Code,
// Cursor, opencode, …) can be pointed at it to report progress and author
// automations / work tasks back into YoYoCode.
//
// Configuration (injected by the app when it launches a session, or set by hand):
//
//	YOYOCODE_API        base URL of the running backend, e.g. http://127.0.0.1:PORT
//	YOYOCODE_TOKEN      loopback auth token
//	YOYOCODE_FOLDER_ID  active folder (authors work tasks / scopes automations)
//	YOYOCODE_AGENT_TYPE agent type in use
//	YOYOCODE_SESSION_ID conversation id (get_session_info)
//
// run: yoyocode-mcp   (reads the env, serves on stdin/stdout)
package main

import (
	"log"
	"os"

	"yoyocode/src-go/mcp"
)

func main() {
	c := mcp.NewCompanion(
		os.Getenv("YOYOCODE_API"),
		os.Getenv("YOYOCODE_TOKEN"),
		os.Getenv("YOYOCODE_FOLDER_ID"),
		os.Getenv("YOYOCODE_AGENT_TYPE"),
		os.Getenv("YOYOCODE_SESSION_ID"),
	)
	if c.APIBase == "" {
		log.Printf("[yoyocode-mcp] warning: YOYOCODE_API unset; authoring tools will fail")
	}
	if err := c.Serve(os.Stdin, os.Stdout); err != nil {
		log.Printf("[yoyocode-mcp] serve: %v", err)
	}
}
