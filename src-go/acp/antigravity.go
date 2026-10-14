package acp

// Antigravity settings sync — the Go port of the settings-file half of upstream
// acp::connection (sync_antigravity_settings_file / antigravity_effective_auth_type
// / merge_antigravity_settings / antigravity_acp_dir_for_env) plus the
// `acp_sync_antigravity_settings` command.
//
// Antigravity's ACP server authenticates from `auth.type` in
// `<GEMINI_HOME>/antigravity-acp/settings.json` — NOT from the stored per-agent
// row. The panel saves the row, so on save it also projects the chosen method
// into that file and reports what happened. The sync FAILS CLOSED: a file codeg
// cannot parse (the server reads Hjson, codeg only strict JSON) is left alone
// rather than flattened, and the report names the path so the user can fix it.
//
// The interactive login/sign-out protocol (acp::antigravity_login, ~2.2k lines,
// needs the real Antigravity binary) is NOT ported; only this settings half is.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// antigravityLogoutTimeout bounds the spawn+handshake+logout round-trip.
const antigravityLogoutTimeout = 30 * time.Second

// Injectable seams so the sign-out protocol is testable without the real
// Antigravity binary (tests inject an in-memory agent).
var (
	antigravityResolveSpawn = ResolveSpawn
	antigravitySpawnAgent   = SpawnAgent
)

// AntigravityAuthMethods are the four auth.type values Antigravity's ACP server
// accepts, canonical spellings only. `vertex-ai` is the pre-rebrand alias for
// `agent-platform`; the server still accepts it, but codeg never writes it.
var AntigravityAuthMethods = []string{
	"oauth-personal", "oauth-business", "gemini-api-key", "agent-platform",
}

const (
	antigravityACPSubdir     = "antigravity-acp"
	antigravityAuthMethodEnv = "AGY_AUTH_METHOD"
)

// AntigravitySyncStatus is the outcome of one sync pass.
type AntigravitySyncStatus string

const (
	// AntigravitySyncWritten means the file now declares the chosen method.
	AntigravitySyncWritten AntigravitySyncStatus = "written"
	// AntigravitySyncAlreadyCurrent means it already did; nothing to write.
	AntigravitySyncAlreadyCurrent AntigravitySyncStatus = "already_current"
	// AntigravitySyncSkipped means left untouched — the agent's auth is NOT what
	// the panel shows.
	AntigravitySyncSkipped AntigravitySyncStatus = "skipped"
)

// AntigravitySyncReport mirrors upstream AntigravitySyncReport.
type AntigravitySyncReport struct {
	Path   string                `json:"path"`
	Status AntigravitySyncStatus `json:"status"`
	// Reason is present only for skipped.
	Reason *string `json:"reason,omitempty"`
}

func antigravityReportWritten(path string) AntigravitySyncReport {
	return AntigravitySyncReport{Path: path, Status: AntigravitySyncWritten}
}
func antigravityReportCurrent(path string) AntigravitySyncReport {
	return AntigravitySyncReport{Path: path, Status: AntigravitySyncAlreadyCurrent}
}
func antigravityReportSkipped(path, reason string) AntigravitySyncReport {
	return AntigravitySyncReport{Path: path, Status: AntigravitySyncSkipped, Reason: &reason}
}

// AntigravityAuthTypeState is what codeg can say about the method the ACP server
// will authenticate with. Kind is "declared" | "absent" | "unreadable";
// Unreadable is emphatically not a flavor of Absent (the server parses Hjson,
// codeg only strict JSON, so a file codeg cannot read is one the server can).
type AntigravityAuthTypeState struct {
	Kind     string
	Declared string
}

// SyncAntigravitySettingsForEnv runs the settings-file sync for an agent's
// STORED environment. Mirrors upstream sync_antigravity_settings_for_env.
func SyncAntigravitySettingsForEnv(runtimeEnv map[string]string) AntigravitySyncReport {
	recorded := antigravityRecordedMethod(runtimeEnv)

	acpDir, err := antigravityACPdirForEnv(runtimeEnv)
	if err != nil {
		return antigravityReportSkipped("<unknown>", err.Error())
	}
	path := filepath.Join(acpDir, "settings.json")

	existing, rerr := readAntigravitySettings(path)
	if rerr != nil {
		return antigravityReportSkipped(path, rerr.Error())
	}

	existingAuthType := antigravityExistingAuthType(existing)

	// Fall back to the method the panel DISPLAYS as selected, but only when the
	// file names none: the agent cannot start a session without an auth.type, and
	// erroring out on a user who never opened the panel would be a dead end.
	// oauth-personal is the free-tier default. An auth.type already in the file
	// is NEVER overridden.
	var method string
	switch {
	case recorded != "":
		method = recorded
	case existingAuthType != "":
		return antigravityReportCurrent(path)
	default:
		method = "oauth-personal"
	}

	gcpProject := antigravityGcpField(runtimeEnv, recorded, "GOOGLE_CLOUD_PROJECT")
	gcpLocation := antigravityGcpField(runtimeEnv, recorded, "GOOGLE_CLOUD_LOCATION")

	updated, changed, merr := mergeAntigravitySettings(existing, method, gcpProject, gcpLocation)
	if merr != nil {
		return antigravityReportSkipped(path, merr.Error())
	}
	if !changed {
		return antigravityReportCurrent(path)
	}
	if werr := writeAntigravitySettings(acpDir, path, updated); werr != nil {
		return antigravityReportSkipped(path, "codeg could not write it ("+werr.Error()+")")
	}
	return antigravityReportWritten(path)
}

// AntigravityEffectiveAuthType reports the auth.type the ACP server will
// authenticate with, read from the file the server reads it from.
func AntigravityEffectiveAuthType(runtimeEnv map[string]string) AntigravityAuthTypeState {
	acpDir, err := antigravityACPdirForEnv(runtimeEnv)
	if err != nil {
		return AntigravityAuthTypeState{Kind: "unreadable"}
	}
	parsed, rerr := readAntigravitySettings(filepath.Join(acpDir, "settings.json"))
	if rerr != nil {
		return AntigravityAuthTypeState{Kind: "unreadable"}
	}
	if parsed == nil {
		// No such file — positive knowledge that there is no method to find.
		return AntigravityAuthTypeState{Kind: "absent"}
	}
	if auth, ok := parsed["auth"].(map[string]any); ok {
		if v, ok := auth["type"].(string); ok {
			if t := strings.TrimSpace(v); t != "" {
				return AntigravityAuthTypeState{Kind: "declared", Declared: canonicalAntigravityAuthMethod(t)}
			}
		}
	}
	return AntigravityAuthTypeState{Kind: "absent"}
}

// AntigravitySignOut spawns the Antigravity agent and asks it to log out: ACP
// `initialize`, check the agent advertises `auth.logout`, then send `logout`.
// Mirrors upstream acp::antigravity_login::sign_out (minus the login-slot
// bookkeeping, which this build does not have).
//
// Returns *AgentUnavailableError when the Antigravity binary cannot be resolved
// (the caller maps that to dependency_missing).
func AntigravitySignOut(runtimeEnv map[string]string) error {
	cfg, ok := antigravityResolveSpawn("antigravity", os.TempDir(), runtimeEnv)
	if !ok {
		return &AgentUnavailableError{AgentType: "antigravity"}
	}
	proc, err := antigravitySpawnAgent(cfg)
	if err != nil {
		return err
	}
	defer proc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), antigravityLogoutTimeout)
	defer cancel()
	res, err := proc.Peer.Call(ctx, "initialize", map[string]any{
		"protocolVersion":    ACPProtocolVersion,
		"clientCapabilities": map[string]any{},
	})
	if err != nil {
		return err
	}
	if !antigravityAdvertisesLogout(res) {
		return errors.New("this version of Google Antigravity cannot sign out. Update it in " +
			"Agent Settings, or delete its stored credential yourself.")
	}
	if _, err := proc.Peer.Call(ctx, "logout", map[string]any{}); err != nil {
		return fmt.Errorf("Antigravity refused to sign out: %v", err)
	}
	return nil
}

// antigravityAdvertisesLogout reports whether the agent's `initialize` answer
// offers the ACP `logout` method. Peer.Call returns the response's `result`
// value (not the full JSON-RPC envelope), so this reads
// agentCapabilities.auth.logout at the top level.
func antigravityAdvertisesLogout(result json.RawMessage) bool {
	var h struct {
		AgentCapabilities struct {
			Auth struct {
				Logout json.RawMessage `json:"logout"`
			} `json:"auth"`
		} `json:"agentCapabilities"`
	}
	if json.Unmarshal(result, &h) != nil {
		return false
	}
	v := strings.TrimSpace(string(h.AgentCapabilities.Auth.Logout))
	return v != "" && v != "null"
}

// ── helpers ──────────────────────────────────────────────────────────────────

// antigravityRecordedMethod is the method the panel stored, if it is one the
// server accepts.
func antigravityRecordedMethod(env map[string]string) string {
	m := strings.TrimSpace(env[antigravityAuthMethodEnv])
	for _, ok := range AntigravityAuthMethods {
		if m == ok {
			return m
		}
	}
	return ""
}

// canonicalAntigravityAuthMethod resolves the pre-rebrand `vertex-ai` spelling
// to the id codeg uses.
func canonicalAntigravityAuthMethod(method string) string {
	if method == "vertex-ai" {
		return "agent-platform"
	}
	return method
}

// antigravityExistingAuthType pulls auth.type out of a parsed settings object.
func antigravityExistingAuthType(existing map[string]any) string {
	if existing == nil {
		return ""
	}
	auth, ok := existing["auth"].(map[string]any)
	if !ok {
		return ""
	}
	v, _ := auth["type"].(string)
	return strings.TrimSpace(v)
}

// gcpFieldKind is the tri-state the two-state Option could not carry.
type gcpFieldKind int

const (
	gcpKeep gcpFieldKind = iota
	gcpSet
	gcpClear
)

type gcpField struct {
	kind  gcpFieldKind
	value string
}

// antigravityGcpField decides what the panel is saying about one `gcp` field:
// a stored value → Set; empty while the panel owns it (oauth-business /
// agent-platform) → Clear; otherwise → Keep (leave the file's value alone).
func antigravityGcpField(env map[string]string, recorded, key string) gcpField {
	if v := strings.TrimSpace(env[key]); v != "" {
		return gcpField{kind: gcpSet, value: v}
	}
	if recorded == "oauth-business" || recorded == "agent-platform" {
		return gcpField{kind: gcpClear}
	}
	return gcpField{kind: gcpKeep}
}

// mergeAntigravitySettings merges the panel's choice into a parsed settings.json.
// changed=false means the file already says exactly this (skip the write). An
// error means a block codeg would have to edit is not the shape it expects — the
// same fail-closed rule the read side applies (replacing a non-object `auth`
// would delete whatever the user meant by it).
func mergeAntigravitySettings(existing map[string]any, method string, project, location gcpField) (map[string]any, bool, error) {
	root := map[string]any{}
	for k, v := range existing {
		root[k] = v
	}
	before, _ := json.Marshal(root)

	// auth: absent/null → create {}; object → keep; anything else → refuse.
	if authVal, ok := root["auth"]; !ok || authVal == nil {
		root["auth"] = map[string]any{}
	} else if _, isObj := authVal.(map[string]any); !isObj {
		return nil, false, fmt.Errorf("`auth` is not an object")
	}
	root["auth"].(map[string]any)["type"] = method

	// gcp: only touch it when the panel has something to say. A Clear on a block
	// that is not an object has nothing to remove, so it must not be the thing
	// that refuses the write — only a Set earns the refusal.
	writes := project.kind == gcpSet || location.kind == gcpSet
	clears := project.kind == gcpClear || location.kind == gcpClear
	_, gcpIsObject := root["gcp"].(map[string]any)
	if writes || (clears && gcpIsObject) {
		if gcpVal, ok := root["gcp"]; !ok || gcpVal == nil {
			root["gcp"] = map[string]any{}
		} else if _, isObj := gcpVal.(map[string]any); !isObj {
			return nil, false, fmt.Errorf("`gcp` is not an object")
		}
		gcp := root["gcp"].(map[string]any)
		for name, f := range map[string]gcpField{"project": project, "location": location} {
			switch f.kind {
			case gcpSet:
				gcp[name] = f.value
			case gcpClear:
				delete(gcp, name)
			}
		}
		if len(gcp) == 0 {
			delete(root, "gcp")
		}
	}

	after, _ := json.Marshal(root)
	return root, string(before) != string(after), nil
}

// readAntigravitySettings reads settings.json for editing. (nil, nil) means "no
// file, safe to create"; an error means "do not touch it".
func readAntigravitySettings(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read it (%v)", err)
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("it is not strict JSON codeg can rewrite without losing content (%v)", err)
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("it does not hold a JSON object")
	}
	return obj, nil
}

// writeAntigravitySettings serializes over path through a temp file in the same
// directory, mirroring the server's own writer: symlinks are resolved first so
// dotfile managers keep their link, and the temp file lands on the same
// filesystem so the rename is atomic.
func writeAntigravitySettings(acpDir, path string, value map[string]any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	target := path
	if resolved, rerr := filepath.EvalSymlinks(path); rerr == nil {
		target = resolved
	}
	parent := filepath.Dir(target)
	if parent == "" || parent == "." {
		parent = acpDir
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	temp := filepath.Join(parent, fmt.Sprintf(".settings.json.codeg-%d.tmp", os.Getpid()))
	if err := os.WriteFile(temp, append(body, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(temp, target); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

// antigravityACPdirForEnv resolves `<GEMINI_HOME>/antigravity-acp` for a launch
// carrying runtimeEnv. (Named with a typo guard-free lower-case 'dir' to keep it
// unexported.) The three-state GEMINI_HOME resolution matches upstream: an
// explicitly empty value means "removed" (fall back to ~/.gemini), an absent
// value inherits codeg's own GEMINI_HOME, and a `~`-prefixed value expands
// against the CHILD's home.
func antigravityACPdirForEnv(runtimeEnv map[string]string) (string, error) {
	configured, hasConfigured := "", false
	if v, ok := runtimeEnv["GEMINI_HOME"]; ok {
		if v != "" { // explicitly blank ⇒ removed ⇒ treated as absent
			configured, hasConfigured = v, true
		}
	} else if inherited := os.Getenv("GEMINI_HOME"); inherited != "" {
		configured, hasConfigured = inherited, true
	}

	needsHome := !hasConfigured || configured == "~" ||
		strings.HasPrefix(configured, "~/") || strings.HasPrefix(configured, "~\\")
	home, homeOK := childHomeDir(runtimeEnv)
	if needsHome && !homeOK {
		return "", fmt.Errorf("codeg cannot tell which home directory the agent will use (this " +
			"launch removes HOME, or points it somewhere relative), so it cannot tell where the file is")
	}

	if hasConfigured {
		return filepath.Join(expandHomePrefix(configured, home), antigravityACPSubdir), nil
	}
	return filepath.Join(home, ".gemini", antigravityACPSubdir), nil
}

// expandHomePrefix expands a leading `~` / `~/` against home.
func expandHomePrefix(value, home string) string {
	if value == "~" {
		return home
	}
	if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, "~\\") {
		return filepath.Join(home, value[2:])
	}
	return value
}

// childHomeDir is the home the agent child will run with: HOME from the launch
// env when it is set and absolute, otherwise codeg's own home. ok=false when the
// home is unknowable (HOME removed or relative).
func childHomeDir(env map[string]string) (string, bool) {
	if v, ok := env["HOME"]; ok {
		if v == "" || !filepath.IsAbs(v) {
			return "", false
		}
		return v, true
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h, true
	}
	return "", false
}
