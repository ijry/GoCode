package acp

// ask_user.go is the companion bridge for "the agent asks the user a question".
//
// The tool itself lives in the companion MCP server (cmd/gocode-mcp), which
// calls the backend's `companion_ask_user_question` route. That route parks the
// call here: the question lands in the session snapshot (and on the event
// stream) so the chat can render a card, and the call returns once
// `acp_answer_question` supplies the answer, the session goes away, or the
// timeout fires. Upstream keeps this bridge inside codeg-mcp; this build owns
// both ends, so the route is a local one rather than an upstream contract.

import (
	"encoding/json"
	"errors"
	"time"
)

// AskUserTimeout bounds how long a parked question waits for an answer.
const AskUserTimeout = 10 * time.Minute

// ErrNoLiveSession is returned when a question arrives for a connection that is
// not live (the agent exited between the tool call and the park).
var ErrNoLiveSession = errors.New("no live session for this conversation")

// AskUser parks an ask_user_question call and waits for its answer.
// questions is the raw array the UI renders ([]QuestionInfo).
func (m *Manager) AskUser(connID string, questions json.RawMessage) (string, error) {
	m.mu.Lock()
	ls := m.live[connID]
	if m.questions == nil {
		m.questions = map[string]chan string{}
	}
	m.mu.Unlock()
	if ls == nil {
		return "", ErrNoLiveSession
	}

	requestID := uuidV4()
	ch := make(chan string, 1)
	m.mu.Lock()
	m.questions[requestID] = ch
	m.mu.Unlock()

	ls.state.SetPendingQuestion(&PendingQuestionState{
		RequestID: requestID,
		Questions: questions,
		CreatedAt: time.Now().UTC(),
	})
	raw, _ := json.Marshal(map[string]any{
		"type":       "question_requested",
		"request_id": requestID,
		"questions":  json.RawMessage(questions),
	})
	ls.state.Emit(raw)

	defer func() {
		m.mu.Lock()
		delete(m.questions, requestID)
		m.mu.Unlock()
		ls.state.ClearPendingQuestion(requestID)
		done, _ := json.Marshal(map[string]any{
			"type":       "question_resolved",
			"request_id": requestID,
		})
		ls.state.Emit(done)
	}()

	select {
	case answer := <-ch:
		return answer, nil
	case <-time.After(AskUserTimeout):
		return "", errors.New("no answer before the timeout")
	}
}

// AnswerQuestion delivers an answer to a parked question. It reports whether a
// question with that id was waiting on this connection.
func (m *Manager) AnswerQuestion(connID, requestID, answer string) bool {
	m.mu.Lock()
	ls := m.live[connID]
	ch := m.questions[requestID]
	m.mu.Unlock()
	if ls == nil || ch == nil {
		return false
	}
	select {
	case ch <- answer:
		return true
	default:
		return false // already answered
	}
}

// dropQuestions unparks every question of a connection that is going away, so a
// waiting tool call returns instead of hanging until the timeout.
func (m *Manager) dropQuestions(connID string) {
	m.mu.Lock()
	ls := m.live[connID]
	pending := make([]chan string, 0, len(m.questions))
	for id, ch := range m.questions {
		if ls != nil && ls.state != nil {
			if q := ls.state.Snapshot().PendingQuestion; q != nil && q.RequestID == id {
				pending = append(pending, ch)
				delete(m.questions, id)
			}
		}
	}
	m.mu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- "":
		default:
		}
	}
}
