package acp

// feedback.go is the live-feedback ("steering") side channel.
//
// The user sends a short note to the running agent from the composer; the agent
// picks it up the next time it calls the companion's `check_user_feedback`
// tool. This is the PULL channel — this build does not implement ACP's native
// `_session/steering` push, so `native_steering_available` stays false and the
// companion tool is the only delivery path. Notes are turn-scoped: a new prompt
// clears them, and a note the agent never read surfaces in the UI's "expired"
// form (resend as a message / dismiss) rather than vanishing.
//
// Upstream keeps this bridge inside codeg-mcp; this build owns both ends
// (cmd/gocode-mcp + src-go/mcp/companion.go), so the tool calls a local route
// (`companion_check_user_feedback`) rather than an upstream contract.

import (
	"encoding/json"
	"time"
)

// FeedbackItem is one live-feedback note (upstream `FeedbackItem`).
type FeedbackItem struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	CreatedAt   string `json:"created_at"`
	Status      string `json:"status"` // "pending" | "delivered"
	DeliveredAt string `json:"delivered_at,omitempty"`
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// SubmitFeedback records a note for the connection's current turn and emits
// `feedback_submitted`. A note needs a live session (the agent is the one that
// will read it); otherwise it returns ErrNoLiveSession so the caller can fall
// back to sending the note as an ordinary prompt.
func (m *Manager) SubmitFeedback(connID, text string) (*FeedbackItem, error) {
	m.mu.Lock()
	ls := m.live[connID]
	m.mu.Unlock()
	if ls == nil {
		return nil, ErrNoLiveSession
	}
	item := FeedbackItem{
		ID:        uuidV4(),
		Text:      text,
		CreatedAt: nowRFC3339(),
		Status:    "pending",
	}
	ls.state.AppendFeedback(item)
	raw, _ := json.Marshal(map[string]any{"type": "feedback_submitted", "item": item})
	ls.state.Emit(raw)
	return &item, nil
}

// CheckUserFeedback delivers every pending note to the agent (the companion's
// `check_user_feedback` tool calls this) and marks them delivered. It returns
// the notes that were waiting, oldest first.
func (m *Manager) CheckUserFeedback(connID string) []FeedbackItem {
	m.mu.Lock()
	ls := m.live[connID]
	m.mu.Unlock()
	if ls == nil {
		return nil
	}
	delivered := ls.state.DeliverFeedback()
	if len(delivered) == 0 {
		return nil
	}
	ids := make([]string, len(delivered))
	for i, it := range delivered {
		ids[i] = it.ID
	}
	raw, _ := json.Marshal(map[string]any{
		"type":         "feedback_consumed",
		"ids":          ids,
		"delivered_at": nowRFC3339(),
	})
	ls.state.Emit(raw)
	return delivered
}
