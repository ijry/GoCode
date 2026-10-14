// Package acp mirrors upstream src-tauri/src/acp: the Agent Client Protocol
// engine (agent listing, connections, session state, event stream).
package acp

import (
	"encoding/json"
	"sync"
	"time"

	"yoyocode/src-go/db"
)

// EventSink is the narrow contract the ACP engine needs to publish onto the
// legacy web firehose. It exists to keep this package independent of
// `yoyocode/src-go/web` — upstream's acp module is likewise unaware of the web
// layer, and the dependency would otherwise form an import cycle (web needs acp
// for the attach protocol).
//
// *web.Broadcaster satisfies this interface; appstate wires the two together.
type EventSink interface {
	Send(channel string, payload any)
	SendRaw(channel string, payload json.RawMessage)
}

// Manager is the Go analog of upstream ConnectionManager.
type Manager struct {
	mu   sync.RWMutex
	conn map[string]*Connection
	// agents registered (mirrors upstream agent registry)
	agents []*AgentInfo
	db     *db.Database
	bc     EventSink
	seq    uint64

	// companionBackend is the loopback address + bearer token of the running
	// YoYoCode backend, injected so ConnectLive can offer the yoyocode-mcp
	// companion to the agent with a working YOYOCODE_API / YOYOCODE_TOKEN. Set
	// once at startup via SetCompanionBackend; empty means "not configured",
	// and ConnectLive skips companion injection.
	companionAPI   string
	companionToken string

	// Live-session machinery (wired by connection.go). sessions is the shared
	// registry the WS attach protocol reads; live holds the running agent
	// process + driver per connection. spawner/resolver are injectable so tests
	// can point at a fake agent instead of a real npx/uvx launch.
	sessions *SessionRegistry
	live     map[string]*liveSession
	spawner  func(SpawnConfig) (*AgentProcess, error)
	resolver func(agentType, cwd string, env map[string]string) (SpawnConfig, bool)
	// probeLocks serializes concurrent probes per agent type (upstream's
	// probe_locks). Different agent types still probe in parallel; the same
	// agent type is bounded to one in-flight probe so rapid settings-UI clicks
	// fan out to a single real CLI process instead of one per click.
	probeLocks sync.Map

	// connGate is the global connection gate (upstream external_restore_lock):
	// ConnectLive holds it shared for the whole connect, and LockOutNewConnections
	// takes it exclusively across a credential-erasing operation (Antigravity
	// sign-out) so no session can be spawned into that window.
	connGate sync.RWMutex
}

// Connection is one live agent session.
type Connection struct {
	ID             string    `json:"id"`
	AgentType      string    `json:"agentType"`
	Status         string    `json:"status"` // connected | idle | disconnected | error
	FolderPath     string    `json:"folderPath"`
	ConversationID int64     `json:"conversationId"`
	CreatedAt      time.Time `json:"createdAt"`
}

// AgentInfo describes a supported agent (mirrors upstream registry entries).
type AgentInfo struct {
	AgentType string            `json:"agentType"`
	Name      string            `json:"name"`
	Command   string            `json:"command"`
	Env       map[string]string `json:"env,omitempty"`
}

func NewManager(database *db.Database, bc EventSink) *Manager {
	return &Manager{
		conn:     map[string]*Connection{},
		agents:   []*AgentInfo{},
		db:       database,
		bc:       bc,
		live:     map[string]*liveSession{},
		spawner:  SpawnAgent,
		resolver: ResolveSpawn,
	}
}

// LockOutNewConnections takes the global connection gate exclusively and returns
// the unlock. Held across a credential-erasing operation so no session can be
// spawned into the window and inherit the credential being erased. Mirrors
// upstream ConnectionManager::lock_out_new_connections.
func (m *Manager) LockOutNewConnections() func() {
	m.connGate.Lock()
	return m.connGate.Unlock
}

// DisconnectByAgentType tears down every live connection for agentType and
// returns how many it ended. Mirrors upstream
// ConnectionManager::disconnect_by_agent_type.
func (m *Manager) DisconnectByAgentType(agentType string) int {
	m.mu.RLock()
	ids := make([]string, 0, 4)
	for id, c := range m.conn {
		if c.AgentType == agentType {
			ids = append(ids, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range ids {
		m.DisconnectLive(id)
	}
	// Drop the (now disconnected) rows so the sweep is idempotent — upstream
	// removes them from its connections map too.
	m.mu.Lock()
	for _, id := range ids {
		if c, ok := m.conn[id]; ok && c.AgentType == agentType {
			delete(m.conn, id)
		}
	}
	m.mu.Unlock()
	return len(ids)
}

// SetCompanionBackend records the loopback backend address + bearer token the
// yoyocode-mcp companion should talk to. Called once at startup after the web
// server is listening (app_state creates the Manager before the port is known).
func (m *Manager) SetCompanionBackend(apiBase, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.companionAPI = apiBase
	m.companionToken = token
}

// companionSpec builds the yoyocode-mcp MCP server entry for a real session, or
// nil when injection should be skipped (backend not configured, or the
// companion binary is missing on disk). agentType/sessionID seed the companion's
// environment so the workbench tools report the right context back to YoYoCode.
func (m *Manager) companionSpec(agentType, sessionID string) map[string]any {
	m.mu.RLock()
	api, token := m.companionAPI, m.companionToken
	m.mu.RUnlock()
	if api == "" {
		return nil
	}
	spec, ok := CompanionMCPServer(api, token, "", agentType, sessionID)
	if !ok {
		return nil
	}
	return spec
}

func (m *Manager) RegisterAgent(a *AgentInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.agents {
		if existing.AgentType == a.AgentType {
			*existing = *a
			return
		}
	}
	m.agents = append(m.agents, a)
}

func (m *Manager) ListAgents() []*AgentInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*AgentInfo, len(m.agents))
	copy(out, m.agents)
	return out
}

func (m *Manager) ListConnections() []*Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Connection, 0, len(m.conn))
	for _, c := range m.conn {
		out = append(out, c)
	}
	return out
}

func (m *Manager) Connect(agentType, folderPath string, conversationID int64) (*Connection, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uuidV4()
	c := &Connection{
		ID:             id,
		AgentType:      agentType,
		Status:         "connected",
		FolderPath:     folderPath,
		ConversationID: conversationID,
		CreatedAt:      time.Now(),
	}
	m.conn[id] = c
	m.bc.Send("acp://connection", map[string]any{"operation": "connected", "connection": c})
	return c, nil
}

func (m *Manager) Disconnect(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.conn[id]; ok {
		c.Status = "disconnected"
		m.bc.Send("acp://event", map[string]any{"connectionID": id, "status": "disconnected"})
	}
}

func (m *Manager) Get(id string) *Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conn[id]
}

// Touch is the keepalive probe behind acp_touch_connection: it reports whether
// the connection is still known (live) so a client can refresh its idle timer.
func (m *Manager) Touch(id string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.conn[id]
	return ok
}

func (m *Manager) NextSeq() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	return m.seq
}
