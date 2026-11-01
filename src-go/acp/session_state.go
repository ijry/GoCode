package acp

import (
	"encoding/json"
	"sync"
	"time"
)

// ConnectionStatus mirrors upstream `acp::types::ConnectionStatus` with
// serde(rename_all="snake_case").
type ConnectionStatus string

const (
	StatusConnecting   ConnectionStatus = "connecting"
	StatusConnected    ConnectionStatus = "connected"
	StatusPrompting    ConnectionStatus = "prompting"
	StatusDisconnected ConnectionStatus = "disconnected"
	StatusError        ConnectionStatus = "error"
)

// ToolCallStatus mirrors upstream ToolCallStatus (snake_case).
type ToolCallStatus string

const (
	ToolCallPending  ToolCallStatus = "pending"
	ToolCallRunning  ToolCallStatus = "running"
	ToolCallSuccess  ToolCallStatus = "success"
	ToolCallFailed   ToolCallStatus = "failed"
	ToolCallRejected ToolCallStatus = "rejected"
)

// ─── Snapshot sub-structs ─────────────────────────────────────────────────

// LiveContentBlock mirrors upstream `LiveContentBlock`, a serde
// tag="kind" enum with snake_case variants.
type LiveContentBlock struct {
	Kind            string `json:"kind"` // text | thinking | tool_call_ref | plan
	Text            string `json:"text,omitempty"`
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`
	ToolCallID      string `json:"tool_call_id,omitempty"`
	Entries         any    `json:"entries,omitempty"`
}

// LiveMessage is the in-flight streaming turn.
type LiveMessage struct {
	ID        string             `json:"id"`
	Role      string             `json:"role"`
	Content   []LiveContentBlock `json:"content"`
	StartedAt time.Time          `json:"started_at"`
}

// ToolCallState is one live tool call. Optional fields use omitempty to match
// upstream's skip_serializing_if, keeping the common case byte-identical.
type ToolCallState struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Label     string          `json:"label"`
	Status    ToolCallStatus  `json:"status"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	Content   string          `json:"content,omitempty"`
	Locations json.RawMessage `json:"locations,omitempty"`
	Meta      json.RawMessage `json:"meta,omitempty"`
}

// PendingPermissionState is an unresolved permission request.
type PendingPermissionState struct {
	RequestID  string          `json:"request_id"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCall   json.RawMessage `json:"tool_call"`
	Options    json.RawMessage `json:"options"`
	CreatedAt  time.Time       `json:"created_at"`
	Queued     uint32          `json:"queued"`
}

// ContextUsage is the agent's context-window reading, folded from ACP
// `usage_update` notifications ({"used": tokens, "size": window}).
type ContextUsage struct {
	Used uint64 `json:"used"`
	Size uint64 `json:"size"`
}

// PendingQuestionState is an in-flight `ask_user_question`.
type PendingQuestionState struct {
	RequestID string          `json:"request_id"`
	Questions json.RawMessage `json:"questions"`
	CreatedAt time.Time       `json:"created_at,omitempty"`
}

// PendingUserMessage is the in-flight user prompt for the current turn.
type PendingUserMessage struct {
	MessageID string          `json:"message_id"`
	Blocks    json.RawMessage `json:"blocks"`
}

// LiveSessionSnapshot mirrors upstream `LiveSessionSnapshot`. It is the payload
// of the WS `snapshot` frame — the cold-start / large-gap catch-up shape.
//
// Optional fields carry omitempty to match upstream skip_serializing_if so a
// connection with no pending question / no delegations stays byte-identical to
// the pre-feature shape.
type LiveSessionSnapshot struct {
	ConnectionID        string                  `json:"connection_id"`
	ConversationID      *int64                  `json:"conversation_id,omitempty"`
	FolderID            *int64                  `json:"folder_id,omitempty"`
	Status              ConnectionStatus        `json:"status"`
	ExternalID          string                  `json:"external_id,omitempty"`
	LiveMessage         *LiveMessage            `json:"live_message,omitempty"`
	ActiveToolCalls     []ToolCallState         `json:"active_tool_calls"`
	PendingPermission   *PendingPermissionState `json:"pending_permission,omitempty"`
	PendingQuestion     *PendingQuestionState   `json:"pending_question,omitempty"`
	PendingPlanApproval json.RawMessage         `json:"pending_plan_approval,omitempty"`
	PendingUserMessage  *PendingUserMessage     `json:"pending_user_message,omitempty"`
	ActiveDelegations   json.RawMessage         `json:"active_delegations,omitempty"`
	Feedback            json.RawMessage         `json:"feedback,omitempty"`
	// FeedbackToolAvailable reports whether the agent can read live-feedback
	// notes through the companion `check_user_feedback` tool (the pull
	// channel). NativeSteeringAvailable reports the ACP `_session/steering`
	// push channel, which this build does not implement (always false).
	FeedbackToolAvailable   bool `json:"feedback_tool_available"`
	NativeSteeringAvailable bool `json:"native_steering_available"`
	// Modes and ConfigOptions are the agent's advertised selectors (from
	// `session_modes` / `session_config_options` events) plus their current
	// values. The snapshot endpoint exposes them so the UI can render mode
	// and model pickers without a separate probe call.
	Modes         *SessionModeStateInfo     `json:"modes,omitempty"`
	ConfigOptions []SessionConfigOptionInfo `json:"config_options"`
	// ContextUsage is the agent's last reported context-window reading (from
	// `usage_update`); nil until the agent reports one.
	ContextUsage *ContextUsage `json:"context_usage,omitempty"`
	// Plan is the agent's latest plan (ACP `plan` update: an array of
	// {content, status, priority}), so a cold attach can show it without
	// waiting for the next update.
	Plan json.RawMessage `json:"plan,omitempty"`
	// QueuedPrompts are the sends waiting behind the running turn, oldest
	// first, so the composer can list them.
	QueuedPrompts []string `json:"queued_prompts"`
	// AsyncTasks are the agent's live background tasks (ACP
	// async_task_* updates), so a cold attach can render the strip too.
	AsyncTasks []AsyncTaskState `json:"async_tasks"`
	// ApiRetry is the agent's in-flight API retry (claude's `api_retry` SDK
	// message); nil when the turn is not retrying.
	ApiRetry *ApiRetryState `json:"api_retry,omitempty"`
}

// ─── SessionState ─────────────────────────────────────────────────────────

// SessionState is the authoritative per-connection live state (upstream
// `SessionState`, held as Arc<RwLock<..>> per AgentConnection). Only the
// current turn's in-flight data lives here; completed turns are read back from
// the agent's JSONL transcript by the parsers.
type SessionState struct {
	mu sync.RWMutex

	ConnectionID   string
	ConversationID *int64
	FolderID       *int64
	ExternalID     string
	Status         ConnectionStatus

	LiveMessage        *LiveMessage
	ActiveToolCalls    []ToolCallState
	PendingPermission  *PendingPermissionState
	PendingQuestion    *PendingQuestionState
	PendingUserMessage *PendingUserMessage

	// Selector catalog populated by session/update events during a probe or a
	// live session's handshake. Mirrors upstream SessionState's
	// modes/config_options/prompt_capabilities/available_commands/selectors_ready/last_error.
	ContextUsage  *ContextUsage
	Plan          json.RawMessage
	QueuedPrompts []string
	AsyncTasks    []AsyncTaskState
	Feedback      []FeedbackItem
	ApiRetry      *ApiRetryState
	// FeedbackToolAvailable is set at spawn when the companion (which provides
	// check_user_feedback) is offered to the agent.
	FeedbackToolAvailable bool
	Modes                 *SessionModeStateInfo
	ConfigOptions         []SessionConfigOptionInfo
	AvailableCommands     []AvailableCommandInfo
	PromptCapabilities    *PromptCapabilitiesInfo
	SelectorsReady        bool
	LastError             *SessionLastError

	// EventSeq is the high-water mark; the next emitted envelope takes
	// EventSeq+1 and every `event` frame delivered after a snapshot/replay
	// carries seq > the value the client was given.
	EventSeq     uint64
	recentEvents RecentEvents
	stream       *ConnectionEventStream
}

func NewSessionState(connectionID string) *SessionState {
	return &SessionState{
		ConnectionID:    connectionID,
		Status:          StatusConnecting,
		ActiveToolCalls: []ToolCallState{},
		stream:          NewConnectionEventStream(),
	}
}

// Snapshot produces the LiveSessionSnapshot under the read lock.
func (s *SessionState) Snapshot() *LiveSessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	toolCalls := make([]ToolCallState, len(s.ActiveToolCalls))
	copy(toolCalls, s.ActiveToolCalls)
	opts := s.ConfigOptions
	if opts == nil {
		opts = []SessionConfigOptionInfo{}
	}
	queued := s.QueuedPrompts
	if queued == nil {
		queued = []string{}
	}
	tasks := s.AsyncTasks
	if tasks == nil {
		tasks = []AsyncTaskState{}
	}
	var feedbackRaw json.RawMessage
	if len(s.Feedback) > 0 {
		feedbackRaw, _ = json.Marshal(s.Feedback)
	}
	return &LiveSessionSnapshot{
		ConnectionID:            s.ConnectionID,
		ConversationID:          s.ConversationID,
		FolderID:                s.FolderID,
		Status:                  s.Status,
		ExternalID:              s.ExternalID,
		LiveMessage:             s.LiveMessage,
		ActiveToolCalls:         toolCalls,
		PendingPermission:       s.PendingPermission,
		PendingQuestion:         s.PendingQuestion,
		PendingUserMessage:      s.PendingUserMessage,
		Modes:                   s.Modes,
		ConfigOptions:           opts,
		ContextUsage:            s.ContextUsage,
		Plan:                    s.Plan,
		QueuedPrompts:           queued,
		AsyncTasks:              tasks,
		ApiRetry:                s.ApiRetry,
		Feedback:                feedbackRaw,
		FeedbackToolAvailable:   s.FeedbackToolAvailable,
		NativeSteeringAvailable: false,
	}
}

// SetQueuedPrompts records the sends waiting behind the running turn.
func (s *SessionState) SetQueuedPrompts(prompts []string) {
	s.mu.Lock()
	s.QueuedPrompts = prompts
	s.mu.Unlock()
}

// SetAsyncTasks replaces the tracked background tasks.
func (s *SessionState) SetAsyncTasks(tasks []AsyncTaskState) {
	s.mu.Lock()
	s.AsyncTasks = tasks
	s.mu.Unlock()
}

// AppendFeedback adds a live-feedback note for the current turn.
func (s *SessionState) AppendFeedback(item FeedbackItem) {
	s.mu.Lock()
	s.Feedback = append(s.Feedback, item)
	s.mu.Unlock()
}

// DeliverFeedback flips every pending note to delivered and returns the ones it
// delivered (oldest first), so the caller can broadcast which ids the agent read.
func (s *SessionState) DeliverFeedback() []FeedbackItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := nowRFC3339()
	var out []FeedbackItem
	for i := range s.Feedback {
		if s.Feedback[i].Status == "pending" {
			s.Feedback[i].Status = "delivered"
			s.Feedback[i].DeliveredAt = now
			out = append(out, s.Feedback[i])
		}
	}
	return out
}

// ClearFeedback drops the turn's notes (a new prompt starts a fresh turn).
func (s *SessionState) ClearFeedback() {
	s.mu.Lock()
	s.Feedback = nil
	s.mu.Unlock()
}

// SetApiRetry records the agent's in-flight API retry.
func (s *SessionState) SetApiRetry(r *ApiRetryState) {
	s.mu.Lock()
	s.ApiRetry = r
	s.mu.Unlock()
}

// ClearApiRetry drops the retry reading once the turn makes progress or ends.
func (s *SessionState) ClearApiRetry() {
	s.mu.Lock()
	s.ApiRetry = nil
	s.mu.Unlock()
}

// SetPendingQuestion parks an in-flight ask_user_question so the snapshot and
// the event stream can show it.
func (s *SessionState) SetPendingQuestion(q *PendingQuestionState) {
	s.mu.Lock()
	s.PendingQuestion = q
	s.mu.Unlock()
}

// ClearPendingQuestion drops the pending question when it is the one with the
// given id (a stale clear must not wipe a newer question).
func (s *SessionState) ClearPendingQuestion(requestID string) {
	s.mu.Lock()
	if s.PendingQuestion != nil && s.PendingQuestion.RequestID == requestID {
		s.PendingQuestion = nil
	}
	s.mu.Unlock()
}

// SetPlan records the agent's latest plan entries (ACP `plan` update).
func (s *SessionState) SetPlan(entries json.RawMessage) {
	s.mu.Lock()
	s.Plan = entries
	s.mu.Unlock()
}

// SetContextUsage records the agent's context-window reading.
func (s *SessionState) SetContextUsage(used, size uint64) {
	s.mu.Lock()
	s.ContextUsage = &ContextUsage{Used: used, Size: size}
	s.mu.Unlock()
}

// EventSeqNow returns the current high-water mark.
func (s *SessionState) EventSeqNow() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.EventSeq
}

// Emit records an envelope into the ring buffer and broadcasts it. Must be the
// only emission path: holding the write lock across "increment seq → push to
// buffer → broadcast" is what guarantees a subscriber registered under the read
// lock cannot miss an event that follows its snapshot.
func (s *SessionState) Emit(payload json.RawMessage) *EventEnvelope {
	s.mu.Lock()
	s.EventSeq++
	env := &EventEnvelope{
		Seq:          s.EventSeq,
		ConnectionID: s.ConnectionID,
		Payload:      payload,
	}
	s.recentEvents.Push(env)
	stream := s.stream
	s.mu.Unlock()

	stream.Broadcast(env)
	return env
}

// Subscribe registers a live receiver. Callers must hold the read lock (see
// Attach) so registration is ordered against Emit's write lock.
func (s *SessionState) Subscribe() *StreamReceiver { return s.stream.Subscribe() }

// RecentEventsAfter returns events with seq > sinceSeq for a `replay`. The bool
// reports whether the ring buffer still covers the cursor — false means the
// client fell too far behind and must take a snapshot instead.
func (s *SessionState) RecentEventsAfter(sinceSeq uint64) ([]*EventEnvelope, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.recentEvents.RangeAfter(sinceSeq)
}

// SetStatus updates the connection status under the write lock.
func (s *SessionState) SetStatus(st ConnectionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = st
}

// SetConversation binds the session to a persisted conversation.
func (s *SessionState) SetConversation(conversationID, folderID *int64, externalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ConversationID = conversationID
	s.FolderID = folderID
	if externalID != "" {
		s.ExternalID = externalID
	}
}

// ─── Selector catalog (probe + live handshake) ──────────────────────────────

// SetModes records the agent's mode catalog (session_modes event).
func (s *SessionState) SetModes(m *SessionModeStateInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Modes = m
}

// SetConfigOptions records the agent's config option catalog
// (session_config_options event).
func (s *SessionState) SetConfigOptions(opts []SessionConfigOptionInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ConfigOptions = opts
}

// SetAvailableCommands records the agent's advertised slash commands
// (available_commands event).
func (s *SessionState) SetAvailableCommands(cmds []AvailableCommandInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AvailableCommands = cmds
}

// SetPromptCapabilities records what the agent accepts in a prompt
// (prompt_capabilities event).
func (s *SessionState) SetPromptCapabilities(p *PromptCapabilitiesInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PromptCapabilities = p
}

// MarkSelectorsReady latches once when the agent signals its initial selector
// set is complete (selectors_ready event). The probe waits on this before
// snapshotting.
func (s *SessionState) MarkSelectorsReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SelectorsReady = true
}

// SetLastError captures the agent's own error text (error event), surfaced by
// the probe path when the handshake fails.
func (s *SessionState) SetLastError(e *SessionLastError) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastError = e
}

// IsSelectorsReady reports whether the agent has published its selectors.
func (s *SessionState) IsSelectorsReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SelectorsReady
}

// LastErrorSnapshot returns the captured agent error, if any.
func (s *SessionState) LastErrorSnapshot() *SessionLastError {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastError
}

// AgentOptions returns a read-only snapshot of the modes + config options the
// agent advertised — mirroring upstream AgentOptionsSnapshot /
// SessionState::to_snapshot. Slices are always non-nil so the wire shape is a
// JSON array (never null), matching upstream's Vec contract.
func (s *SessionState) AgentOptions() *AgentOptionsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var modes *SessionModeStateInfo
	if s.Modes != nil {
		cp := *s.Modes
		modes = &cp
	}
	var pc *PromptCapabilitiesInfo
	if s.PromptCapabilities != nil {
		cp := *s.PromptCapabilities
		pc = &cp
	}
	opts := s.ConfigOptions
	if opts == nil {
		opts = []SessionConfigOptionInfo{}
	}
	cmds := s.AvailableCommands
	if cmds == nil {
		cmds = []AvailableCommandInfo{}
	}
	return &AgentOptionsSnapshot{
		Modes:              modes,
		ConfigOptions:      opts,
		AvailableCommands:  cmds,
		PromptCapabilities: pc,
	}
}

// ─── Session registry ─────────────────────────────────────────────────────

// SessionRegistry maps connection_id → live SessionState. Upstream holds these
// on the ConnectionManager (`manager.get_state(&connection_id)`).
type SessionRegistry struct {
	mu      sync.RWMutex
	byID    map[string]*SessionState
	metrics *EventBusMetrics
}

func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{byID: map[string]*SessionState{}, metrics: NewEventBusMetrics()}
}

// GetState returns the live session state, or nil when the connection is
// unknown (already GC'd, or never existed).
func (r *SessionRegistry) GetState(connectionID string) *SessionState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[connectionID]
}

// Insert registers a new session state.
func (r *SessionRegistry) Insert(s *SessionState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[s.ConnectionID] = s
}

// Remove drops a session state (connection gone).
func (r *SessionRegistry) Remove(connectionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, connectionID)
}

// Metrics exposes the event-bus counters for the debug/event_metrics endpoint.
func (r *SessionRegistry) Metrics() *EventBusMetrics { return r.metrics }
