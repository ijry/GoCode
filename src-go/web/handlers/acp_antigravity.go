package handlers

// Antigravity settings-sync route (upstream commands/acp.rs
// acp_sync_antigravity_settings_core → acp::connection::sync_antigravity_settings_for_env).
//
// The interactive login/sign-out protocol (acp::antigravity_login) is not ported;
// this is the settings-file half only.

import (
	"encoding/json"
	"strings"

	"yoyocode/src-go/acp"
	"yoyocode/src-go/web"
)

func registerACPAntigravity(reg *Registry) {
	reg.Register("acp_sync_antigravity_settings", acpSyncAntigravitySettings)
}

// acpSyncAntigravitySettings projects the saved Antigravity auth choice into the
// server's settings.json and reports whether it landed. Reads the stored row
// rather than taking the env from the caller so it reports on what was actually
// persisted, not on what the request claimed.
func acpSyncAntigravitySettings(c *web.Context) (any, error) {
	return acp.SyncAntigravitySettingsForEnv(antigravityRuntimeEnv(c)), nil
}

// antigravityRuntimeEnv composes the Antigravity runtime env from the stored
// agent_setting row (upstream antigravity_runtime_env →
// build_runtime_env_from_setting). A read failure is not fatal: an empty env
// makes the sync fall back to the file's existing auth.type or the
// oauth-personal default.
func antigravityRuntimeEnv(c *web.Context) map[string]string {
	out := map[string]string{}
	st, ok := appState(c)
	if !ok || st == nil || st.DB == nil {
		return out
	}
	var raw *string
	if err := st.DB.Conn.QueryRow(
		`SELECT env_json FROM agent_setting WHERE agent_type = ?`, "antigravity").Scan(&raw); err != nil {
		return out
	}
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(*raw), &out)
	return out
}
