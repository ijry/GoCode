package handlers

// Antigravity settings-sync route (upstream commands/acp.rs
// acp_sync_antigravity_settings_core → acp::connection::sync_antigravity_settings_for_env).
//
// The interactive login/sign-out protocol (acp::antigravity_login) is not ported;
// this is the settings-file half only.

import (
	"encoding/json"
	"errors"
	"strings"

	"yoyocode/src-go/acp"
	"yoyocode/src-go/web"
)

// antigravitySignOutFn is injectable so tests can drive the route without the
// real Antigravity binary.
var antigravitySignOutFn = acp.AntigravitySignOut

func registerACPAntigravity(reg *Registry) {
	reg.Register("acp_sync_antigravity_settings", acpSyncAntigravitySettings)
	reg.Register("acp_antigravity_sign_out", acpAntigravitySignOut)
}

// acpAntigravitySignOut clears the Antigravity credential so another account
// can be signed in (upstream acp_antigravity_sign_out_core). Three steps around
// the logout: refuse unless the file names a Google-account method, quiesce the
// agent (lock out new connections + disconnect live ones), then write the saved
// method back via the settings sync.
func acpAntigravitySignOut(c *web.Context) (any, error) {
	env := antigravityRuntimeEnv(c)
	if err := antigravitySignOutGuards(env); err != nil {
		return nil, err
	}
	if st, ok := appState(c); ok && st != nil && st.ACP != nil {
		// The lockout is held across the logout so a session cannot be spawned
		// into the window and inherit the credential being erased.
		unlock := st.ACP.LockOutNewConnections()
		st.ACP.DisconnectByAgentType("antigravity")
		err := antigravitySignOutFn(env)
		unlock()
		if err != nil {
			return nil, antigravitySignOutErr(err)
		}
	} else if err := antigravitySignOutFn(env); err != nil {
		return nil, antigravitySignOutErr(err)
	}
	// logout removes auth.type on its way out, and a session whose auth.type is
	// missing fails outright — so write the saved method back.
	return acp.SyncAntigravitySettingsForEnv(env), nil
}

// antigravitySignOutGuards refuses a sign-out that would look like it worked
// when it did not.
func antigravitySignOutGuards(env map[string]string) error {
	switch st := acp.AntigravityEffectiveAuthType(env); st.Kind {
	case "declared":
		if st.Declared != "oauth-personal" && st.Declared != "oauth-business" {
			return web.ErrTaskExecution("Antigravity's settings.json says it authenticates with " +
				st.Declared + ", not with a Google account, so there is no signed-in account to leave. " +
				"Choose a Google sign-in method above and save first, then sign out.")
		}
	case "unreadable":
		return web.ErrTaskExecution("codeg cannot read the authentication method out of Antigravity's " +
			"settings.json, so it cannot tell which account would be signed out — or whether anything " +
			"would be. Make that file strict JSON (the server also accepts comments and trailing commas; " +
			"codeg does not), or move it aside, then try again.")
	}
	return nil
}

// antigravitySignOutErr maps a failed logout: a missing binary is
// dependency_missing, everything else is a task failure (upstream AcpError →
// task_execution_failed).
func antigravitySignOutErr(err error) error {
	var unavailable *acp.AgentUnavailableError
	if errors.As(err, &unavailable) {
		return web.ErrDependencyMissing("Antigravity is not installed")
	}
	return web.ErrTaskExecution(err.Error())
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
