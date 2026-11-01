package acp

// Live connection lifecycle: spawn a real agent, run the ACP handshake, register
// the session with the event-stream registry (so WS attach streams it), and
// drive prompt/permission/cancel. Mirrors the manager side of upstream
// acp::connection + acp::manager.

import (
	"context"
	"database/sql"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"
)

type liveSession struct {
	conn    *Connection
	proc    *AgentProcess
	session *AgentSession
	state   *SessionState
	cancel  context.CancelFunc
	// Prompt turns are serialized: an ACP session handles one prompt at a time,
	// so a send that lands mid-turn is queued here and started when the turn
	// ends. Without this a second send would overwrite cancel and interleave
	// two turns on one session.
	promptMu  sync.Mutex
	prompting bool
	queue     [][]any
}

// SetSessionRegistry wires the shared registry the WS attach protocol reads.
func (m *Manager) SetSessionRegistry(r *SessionRegistry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = r
}

// SetSpawner overrides the agent spawner (tests inject a fake agent).
func (m *Manager) SetSpawner(fn func(SpawnConfig) (*AgentProcess, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawner = fn
}

// SetResolver overrides the agent-type → SpawnConfig resolver.
func (m *Manager) SetResolver(fn func(agentType, cwd string, env map[string]string) (SpawnConfig, bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolver = fn
}

// handshakeTimeout bounds initialize + session/new; a hung agent must not wedge
// the connect call forever. Raised to 120s over upstream's 60s because a cold
// start of a large binary agent (opencode measured ~32s here) sits too close to
// the old ceiling. Override with GOCODE_ACP_HANDSHAKE_TIMEOUT_MS.
var handshakeTimeout = envDurationMs("GOCODE_ACP_HANDSHAKE_TIMEOUT_MS", 120*time.Second)

func envDurationMs(name string, def time.Duration) time.Duration {
	if raw := os.Getenv(name); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return def
}

// ConnectOptions is what acp_connect sends beyond the agent type and working
// directory, mirroring upstream AcpConnectParams.
type ConnectOptions struct {
	// SessionID names an existing external session to resume. Upstream also uses
	// it to dedup re-connects to a live session; that layer is not ported yet.
	SessionID string
	// PreferredModeID is applied right after the session is established.
	PreferredModeID string
	// PreferredConfigValues are applied after PreferredModeID, keyed by config id.
	PreferredConfigValues map[string]string
}

// ConnectLive spawns the agent, runs the handshake, registers the session, and
// returns the connection. On any failure it tears the child down and returns a
// typed error (dependency_missing when the agent can't be launched at all).
func (m *Manager) ConnectLive(agentType, folderPath string, conversationID int64, opts ConnectOptions) (*Connection, error) {
	// Hold the connection gate shared for the whole connect: LockOutNewConnections
	// (Antigravity sign-out) takes it exclusively so no session is spawned into
	// the credential-erasing window.
	m.connGate.RLock()
	defer m.connGate.RUnlock()

	m.mu.Lock()
	resolver := m.resolver
	spawner := m.spawner
	sessions := m.sessions
	bc := m.bc
	m.mu.Unlock()

	cwd := folderPath
	if cwd == "" {
		cwd = os.TempDir()
	}
	cfg, ok := resolver(agentType, cwd, nil)
	if !ok {
		return nil, &AgentUnavailableError{AgentType: agentType}
	}

	proc, err := spawner(cfg)
	if err != nil {
		return nil, &SpawnError{AgentType: agentType, Err: err}
	}

	id := uuidV4()
	state := NewSessionState(id)
	if conversationID > 0 {
		cid := conversationID
		state.SetConversation(&cid, nil, "")
	}
	sess := NewAgentSession(proc.Peer, state, agentType)

	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	if err := sess.Initialize(ctx); err != nil {
		cancel()
		_ = proc.Close()
		return nil, &SpawnError{AgentType: agentType, Err: err, Stderr: proc.StderrTail()}
	}
	// Offer the gocode-mcp workbench companion to the agent. When the
	// backend is configured and the companion binary is present, it spawns
	// with the session context injected as env vars; otherwise we connect
	// without it (the agent stays fully functional minus the workbench tools).
	servers := []any{}
	if spec := m.companionSpec(agentType, id); spec != nil {
		servers = append(servers, spec)
		// The companion provides `check_user_feedback`, the pull channel for
		// live-feedback notes. Advertise it so the composer offers the entry.
		state.FeedbackToolAvailable = true
	}
	// Resume the conversation's agent session when we know its id and the agent
	// supports session/load — that keeps the agent's context across app restarts
	// instead of starting a blank session. A load failure falls back to a new
	// session rather than failing the connect.
	resumeID := opts.SessionID
	if resumeID == "" && conversationID > 0 {
		resumeID = m.conversationSessionID(conversationID)
	}
	loaded := false
	if resumeID != "" && sess.SupportsLoadSession() {
		if _, err := sess.LoadSession(ctx, resumeID, cwd, servers); err == nil {
			loaded = true
		}
	}
	if !loaded {
		if _, err := sess.NewSession(ctx, cwd, servers); err != nil {
			cancel()
			_ = proc.Close()
			return nil, &SpawnError{AgentType: agentType, Err: err, Stderr: proc.StderrTail()}
		}
	}
	cancel()

	// Remember which agent session backs this conversation. The history
	// endpoint reads the agent's on-disk transcript by this id, so without it a
	// reopened conversation has no history at all. First session wins: later
	// reconnects start a new agent session, and keeping the original id keeps
	// the conversation's transcript stable (ACP session/load resume is not
	// ported yet, so a reconnect cannot continue the same session).
	if conversationID > 0 {
		m.rememberSessionID(conversationID, sess.SessionID())
	}

	state.SetStatus(StatusConnected)
	// Apply the launch preferences where upstream does: after the session is
	// established, mode first then the config values. A failure here is logged
	// against the connection and otherwise ignored — the session is already up,
	// and a mode the agent no longer offers must not fail the connect.
	if err := m.applyConnectPreferences(sess, opts); err != nil {
		_ = err // surfaced on the event stream by the session itself
	}
	conn := &Connection{
		ID:             id,
		AgentType:      agentType,
		Status:         "connected",
		FolderPath:     folderPath,
		ConversationID: conversationID,
		CreatedAt:      time.Now(),
	}
	ls := &liveSession{conn: conn, proc: proc, session: sess, state: state}

	m.mu.Lock()
	m.conn[id] = conn
	m.live[id] = ls
	m.mu.Unlock()
	if sessions != nil {
		sessions.Insert(state)
	}
	if bc != nil {
		bc.Send("acp://connection", map[string]any{"operation": "connected", "connection": conn})
	}
	return conn, nil
}

// applyConnectPreferences replays the launch preferences the user picked —
// the mode and the config values — onto a freshly established session, the way
// upstream's apply_preferred_session_options does. Errors are returned so the
// caller can decide; connect treats them as non-fatal.
func (m *Manager) applyConnectPreferences(sess *AgentSession, opts ConnectOptions) error {
	if opts.PreferredModeID == "" && len(opts.PreferredConfigValues) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), acpControlTimeout)
	defer cancel()
	// Upstream skips the round-trip when the session already holds the mode; this
	// build does not track modes, so the request always goes out. A mode the agent
	// no longer offers fails here and is ignored, which is where upstream ends up
	// too once its `needs_apply` check passes.
	if opts.PreferredModeID != "" {
		if err := sess.SetMode(ctx, opts.PreferredModeID); err != nil {
			return err
		}
	}
	// Deterministic order: a map's iteration order is random, and two options
	// that depend on each other must be applied in a stable sequence.
	for _, id := range sortedKeys(opts.PreferredConfigValues) {
		if err := sess.SetConfigOption(ctx, id, opts.PreferredConfigValues[id],
			opts.PreferredConfigValues[id] == "true" || opts.PreferredConfigValues[id] == "false"); err != nil {
			return err
		}
	}
	return nil
}

// PromptLive drives a prompt turn in the background; session/update events
// stream to the connection's event buffer as they arrive. Returns immediately
// with the connection unknown error if there is no live session.
func (m *Manager) PromptLive(connID string, blocks []any) error {
	m.mu.Lock()
	ls := m.live[connID]
	m.mu.Unlock()
	if ls == nil {
		return &ConnectionUnknownError{ID: connID}
	}
	ls.promptMu.Lock()
	if ls.prompting {
		// A turn is in flight: queue this prompt and let the running turn start
		// it when it ends.
		ls.queue = append(ls.queue, blocks)
		queued := ls.queueTextsLocked()
		ls.promptMu.Unlock()
		ls.state.SetQueuedPrompts(queued)
		return nil
	}
	ls.prompting = true
	ls.promptMu.Unlock()
	go ls.runPrompts(blocks)
	return nil
}

// conversationSessionID returns the agent session id stored on a conversation
// row (conversation.external_id), or "" when there is none.
func (m *Manager) conversationSessionID(convID int64) string {
	m.mu.RLock()
	d := m.db
	m.mu.RUnlock()
	if d == nil || d.Conn == nil {
		return ""
	}
	var ext sql.NullString
	if err := d.Conn.QueryRow(
		`SELECT external_id FROM conversation WHERE id = ? AND deleted_at IS NULL`, convID).Scan(&ext); err != nil {
		return ""
	}
	if !ext.Valid {
		return ""
	}
	return ext.String
}

// rememberSessionID stores the agent session id on the conversation row when it
// has none yet (see ConnectLive). Best-effort: a failure here costs history, not
// the session.
func (m *Manager) rememberSessionID(convID int64, sessionID string) {
	if sessionID == "" {
		return
	}
	m.mu.RLock()
	d := m.db
	m.mu.RUnlock()
	if d == nil || d.Conn == nil {
		return
	}
	_, _ = d.Conn.Exec(
		`UPDATE conversation SET external_id = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL AND (external_id IS NULL OR external_id = '')`,
		sessionID, time.Now().UTC().Format(time.RFC3339), convID)
}

// queueTextsLocked renders the queued prompts' text, oldest first. Callers hold
// promptMu.
func (ls *liveSession) queueTextsLocked() []string {
	out := make([]string, 0, len(ls.queue))
	for _, blocks := range ls.queue {
		text := ""
		for _, raw := range blocks {
			b, _ := raw.(map[string]any)
			if t, _ := b["text"].(string); t != "" {
				text = t
				break
			}
		}
		out = append(out, text)
	}
	return out
}

// DropQueuedPrompt removes one waiting send (0-based, oldest first). It reports
// whether a prompt was there to drop.
func (m *Manager) DropQueuedPrompt(connID string, index int) bool {
	m.mu.Lock()
	ls := m.live[connID]
	m.mu.Unlock()
	if ls == nil {
		return false
	}
	ls.promptMu.Lock()
	if index < 0 || index >= len(ls.queue) {
		ls.promptMu.Unlock()
		return false
	}
	ls.queue = append(ls.queue[:index], ls.queue[index+1:]...)
	queued := ls.queueTextsLocked()
	ls.promptMu.Unlock()
	ls.state.SetQueuedPrompts(queued)
	return true
}

// runPrompts drains the session's prompt queue: it runs one turn at a time,
// then starts the next queued prompt, until the queue empties.
func (ls *liveSession) runPrompts(blocks []any) {
	for {
		ctx, cancel := context.WithCancel(context.Background())
		ls.promptMu.Lock()
		ls.cancel = cancel
		ls.promptMu.Unlock()
		_, _ = ls.session.Prompt(ctx, blocks)
		cancel()

		ls.promptMu.Lock()
		if len(ls.queue) == 0 {
			ls.prompting = false
			ls.cancel = nil
			ls.promptMu.Unlock()
			ls.state.SetQueuedPrompts(nil)
			return
		}
		blocks = ls.queue[0]
		ls.queue = ls.queue[1:]
		queued := ls.queueTextsLocked()
		ls.promptMu.Unlock()
		ls.state.SetQueuedPrompts(queued)
	}
}

// acpControlTimeout bounds a control round-trip (set mode / set config option)
// so a wedged agent cannot hold an HTTP request open forever.
const acpControlTimeout = 30 * time.Second

// SetMode selects the live session's mode (acp_set_mode / the preferredMode the
// user picked at connect time).
func (m *Manager) SetMode(connID, modeID string) error {
	m.mu.Lock()
	ls := m.live[connID]
	m.mu.Unlock()
	if ls == nil {
		return &ConnectionUnknownError{ID: connID}
	}
	ctx, cancel := context.WithTimeout(context.Background(), acpControlTimeout)
	defer cancel()
	return ls.session.SetMode(ctx, modeID)
}

// SetConfigOption sets one config option on the live session
// (acp_set_config_option).
func (m *Manager) SetConfigOption(connID, configID, valueID string) error {
	m.mu.Lock()
	ls := m.live[connID]
	m.mu.Unlock()
	if ls == nil {
		return &ConnectionUnknownError{ID: connID}
	}
	ctx, cancel := context.WithTimeout(context.Background(), acpControlTimeout)
	defer cancel()
	return ls.session.SetConfigOption(ctx, configID, valueID, valueID == "true" || valueID == "false")
}

// RespondPermission answers a pending permission request on a live session.
func (m *Manager) RespondPermission(connID, requestID, optionID string) bool {
	m.mu.Lock()
	ls := m.live[connID]
	m.mu.Unlock()
	if ls == nil {
		return false
	}
	return ls.session.ResolvePermission(requestID, optionID)
}

// CancelLive cancels the in-flight turn (ACP session/cancel + ctx cancel).
func (m *Manager) CancelLive(connID string) bool {
	m.mu.Lock()
	ls := m.live[connID]
	m.mu.Unlock()
	if ls == nil {
		return false
	}
	// A stop ends the turn AND the queued sends behind it: the user asked for
	// this conversation to go quiet, not to start the next prompt immediately.
	ls.promptMu.Lock()
	ls.queue = nil
	cancel := ls.cancel
	ls.promptMu.Unlock()
	ls.state.SetQueuedPrompts(nil)
	_ = ls.proc.Peer.Notify("session/cancel", map[string]any{"sessionId": ls.session.SessionID()})
	if cancel != nil {
		cancel()
	}
	return true
}

// GoalControlLive forwards a goal-control request (codex-acp
// `_codex/session/goal_control`, action "pause"|"clear") to the live adapter.
// Returns false when there is no live session for connID. Mirrors the manager
// side of upstream acp::manager::goal_control; the out-of-band interrupt reply
// is adapter-specific and not awaited here.
func (m *Manager) GoalControlLive(connID, action string) bool {
	m.mu.Lock()
	ls := m.live[connID]
	m.mu.Unlock()
	if ls == nil {
		return false
	}
	_ = ls.proc.Peer.Notify("_codex/session/goal_control", map[string]any{
		"sessionId": ls.session.SessionID(),
		"action":    action,
	})
	return true
}

// DisconnectLive tears down a live session (child + registry entry).
func (m *Manager) DisconnectLive(connID string) {
	// A parked ask_user_question must not outlive its session.
	m.dropQuestions(connID)
	m.mu.Lock()
	ls := m.live[connID]
	delete(m.live, connID)
	sessions := m.sessions
	if c, ok := m.conn[connID]; ok {
		c.Status = "disconnected"
	}
	m.mu.Unlock()
	if ls == nil {
		return
	}
	if ls.cancel != nil {
		ls.cancel()
	}
	_ = ls.proc.Close()
	if sessions != nil {
		sessions.Remove(connID)
	}
	if m.bc != nil {
		m.bc.Send("acp://event", map[string]any{"connectionID": connID, "status": "disconnected"})
	}
}

// LiveSession returns the agent session driver for a connection (nil if none).
func (m *Manager) LiveSession(connID string) *AgentSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ls := m.live[connID]; ls != nil {
		return ls.session
	}
	return nil
}

// ─── typed errors ────────────────────────────────────────────────────────────

// AgentUnavailableError: the agent can't be launched here (binary agent with no
// cached binary, or unknown type). Maps to dependency_missing (422).
type AgentUnavailableError struct{ AgentType string }

func (e *AgentUnavailableError) Error() string {
	return "agent not available for launch: " + e.AgentType
}

// SpawnError: the agent process failed to start or complete the handshake.
type SpawnError struct {
	AgentType string
	Err       error
	Stderr    []string
}

func (e *SpawnError) Error() string {
	msg := "failed to start agent " + e.AgentType + ": " + e.Err.Error()
	return msg
}

// ConnectionUnknownError: no such live connection.
type ConnectionUnknownError struct{ ID string }

func (e *ConnectionUnknownError) Error() string { return "connection not found: " + e.ID }

// FindConnectionForConversation returns the live connection bound to a
// conversation (by conversation id), with its current event seq. The optional
// session id is a pre-first-prompt fallback matched against external_id.
func (m *Manager) FindConnectionForConversation(conversationID int64, sessionID string) (connID string, eventSeq uint64, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, ls := range m.live {
		if ls.state == nil {
			continue
		}
		snap := ls.state.Snapshot()
		if snap.ConversationID != nil && *snap.ConversationID == conversationID {
			return id, ls.state.EventSeqNow(), true
		}
		if sessionID != "" && snap.ExternalID == sessionID {
			return id, ls.state.EventSeqNow(), true
		}
	}
	return "", 0, false
}

// SnapshotByConversation returns the live snapshot for a conversation's
// connection, or nil when none is live.
func (m *Manager) SnapshotByConversation(conversationID int64) *LiveSessionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ls := range m.live {
		if ls.state == nil {
			continue
		}
		snap := ls.state.Snapshot()
		if snap.ConversationID != nil && *snap.ConversationID == conversationID {
			return snap
		}
	}
	return nil
}

// sortedKeys returns a map's keys in ascending order, so a caller iterating a
// preference map applies it deterministically.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
