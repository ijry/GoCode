package app

// Stream is the native UI's client for the backend's /ws/events socket — the
// same multiplexed firehose + attach protocol mcode-app speaks. It keeps the
// workspace live: firehose frames (conversation://changed, acp://*) trigger a
// repaint, and per-connection ACP event envelopes (attach → snapshot/replay/
// event) accumulate so the chat detail can render streaming agent output.

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
)

type connStream struct {
	subID   string
	lastSeq uint64
	events  []map[string]any
}

// Stream owns the websocket connection and the per-connection event buffers.
type Stream struct {
	b        *Backend
	onChange func(connID, channel string, payload json.RawMessage) // repaint request (goroutine-safe); connID "" for firehose, channel set for firehose frames

	mu       sync.Mutex
	conn     *websocket.Conn
	ready    bool
	byConn   map[string]*connStream
	attached map[string]bool
	// usage is each connection's last context-window reading, folded from the
	// stream's usage_update events so the composer meter moves mid-turn.
	usage map[string]contextUsage
	// plan is each connection's latest plan entries (raw JSON array), folded
	// from plan_update events so the plan card tracks a running turn.
	plan map[string]json.RawMessage
	// last keeps the payload of the most recent frame of each firehose channel,
	// for the app-level channels whose body the hook needs (pet://oneshot's
	// kind, pet://state's state).
	last map[string]json.RawMessage
}

// Payload returns the payload of the most recent frame on channel.
func (s *Stream) Payload(channel string) json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last[channel]
}

// Plan returns a connection's latest plan entries as raw JSON.
func (s *Stream) Plan(connID string) json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.plan[connID]
}

// contextUsage is an agent's context-window reading (ACP `usage_update`).
type contextUsage struct {
	Used uint64
	Size uint64
}

// Usage returns a connection's last context-window reading.
func (s *Stream) Usage(connID string) (contextUsage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.usage[connID]
	return u, ok
}

func newStream(b *Backend, onChange func(connID, channel string, payload json.RawMessage)) *Stream {
	s := &Stream{b: b, onChange: onChange, byConn: map[string]*connStream{}, attached: map[string]bool{}, usage: map[string]contextUsage{}, last: map[string]json.RawMessage{}}
	go s.run()
	return s
}

// wsURL turns the backend's http base into a ws:// /ws/events URL.
func (s *Stream) wsURL() string {
	u := s.b.URL
	u = strings.Replace(u, "http://", "ws://", 1)
	u = strings.Replace(u, "https://", "wss://", 1)
	return u + "/ws/events"
}

func (s *Stream) run() {
	hdr := http.Header{}
	hdr.Set("authorization", "Bearer "+s.b.Token)
	conn, _, err := websocket.DefaultDialer.Dial(s.wsURL(), hdr)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.conn = conn
	s.ready = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.ready = false
		s.conn = nil
		s.mu.Unlock()
		conn.Close()
	}()

	for {
		var f wsFrame
		if err := conn.ReadJSON(&f); err != nil {
			return
		}
		s.handle(&f)
	}
}

// wsFrame is the union of a firehose WebEvent ({channel,payload}) and an
// attach-protocol ServerMsg ({type,…}); only one family's keys are set.
type wsFrame struct {
	Channel      string           `json:"channel"`
	Payload      json.RawMessage  `json:"payload"`
	Type         string           `json:"type"`
	ConnectionID string           `json:"connection_id"`
	EventSeq     uint64           `json:"event_seq"`
	HighWaterSeq uint64           `json:"high_water_seq"`
	Envelope     map[string]any   `json:"envelope"`
	Events       []map[string]any `json:"events"`
	Snapshot     map[string]any   `json:"snapshot"`
}

func (s *Stream) handle(f *wsFrame) {
	changed := false
	changedConn := ""
	channel := ""
	if f.Channel != "" {
		// Firehose: any change is worth a repaint; the view refetches as needed.
		channel = f.Channel
		s.mu.Lock()
		if len(f.Payload) > 0 {
			s.last[f.Channel] = f.Payload
		}
		s.mu.Unlock()
		changed = true
	} else {
		switch f.Type {
		case "snapshot":
			s.mu.Lock()
			cs := s.ensureConn(f.ConnectionID)
			cs.lastSeq = f.EventSeq
			s.mu.Unlock()
			changedConn = f.ConnectionID
			changed = true
		case "replay":
			changedConn = f.ConnectionID
			s.mu.Lock()
			cs := s.ensureConn(f.ConnectionID)
			for _, ev := range f.Events {
				cs.events = append(cs.events, ev)
				if seq, ok := ev["seq"].(float64); ok {
					cs.lastSeq = uint64(seq)
				}
			}
			if f.HighWaterSeq > cs.lastSeq {
				cs.lastSeq = f.HighWaterSeq
			}
			s.mu.Unlock()
			changed = true
		case "event":
			if f.Envelope != nil {
				connID, _ := f.Envelope["connection_id"].(string)
				s.mu.Lock()
				cs := s.ensureConn(connID)
				cs.events = append(cs.events, f.Envelope)
				if seq, ok := f.Envelope["seq"].(float64); ok {
					cs.lastSeq = uint64(seq)
				}
				if f.Envelope["type"] == "usage_update" {
					if s.usage == nil {
						s.usage = map[string]contextUsage{}
					}
					u := contextUsage{}
					if v, ok := f.Envelope["used"].(float64); ok && v > 0 {
						u.Used = uint64(v)
					}
					if v, ok := f.Envelope["size"].(float64); ok && v > 0 {
						u.Size = uint64(v)
					}
					s.usage[connID] = u
				}
				if f.Envelope["type"] == "plan_update" {
					if s.plan == nil {
						s.plan = map[string]json.RawMessage{}
					}
					if entries, ok := f.Envelope["entries"]; ok && entries != nil {
						if b, err := json.Marshal(entries); err == nil {
							s.plan[connID] = b
						}
					}
				}
				s.mu.Unlock()
				changedConn = connID
				changed = true
			}
		}
	}
	if changed && s.onChange != nil {
		var payload json.RawMessage
		if channel != "" {
			payload = s.Payload(channel)
		}
		s.onChange(changedConn, channel, payload)
	}
}

// ensureConn must be called with s.mu held.
func (s *Stream) ensureConn(connID string) *connStream {
	cs := s.byConn[connID]
	if cs == nil {
		cs = &connStream{subID: "sub-" + connID}
		s.byConn[connID] = cs
	}
	return cs
}

// Attach subscribes to a connection's live event stream (idempotent).
func (s *Stream) Attach(connID string) {
	if connID == "" {
		return
	}
	s.mu.Lock()
	if s.attached[connID] || !s.ready || s.conn == nil {
		s.mu.Unlock()
		return
	}
	cs := s.ensureConn(connID)
	s.attached[connID] = true
	conn := s.conn
	since := cs.lastSeq
	subID := cs.subID
	s.mu.Unlock()

	msg := map[string]any{"action": "attach", "subscription_id": subID, "connection_id": connID}
	if since > 0 {
		msg["since_seq"] = since
	}
	_ = conn.WriteJSON(msg)
}

// Events returns the accumulated event envelopes for a connection.
func (s *Stream) Events(connID string) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs := s.byConn[connID]
	if cs == nil {
		return nil
	}
	out := make([]map[string]any, len(cs.events))
	copy(out, cs.events)
	return out
}

// AppendLocal injects a synthetic event into a connection's buffer — used to
// echo the user's own turn (text + attachments) into the transcript so it
// renders the way the agent's reply does. It is a client-side convenience only;
// the real server echo (if any) is de-duplicated by buildTranscript's flush.
func (s *Stream) AppendLocal(connID string, ev map[string]any) {
	if connID == "" || ev == nil {
		return
	}
	s.mu.Lock()
	cs := s.ensureConn(connID)
	cs.events = append(cs.events, ev)
	s.mu.Unlock()
	if s.onChange != nil {
		s.onChange(connID, "", nil)
	}
}

// Close drops the live websocket so a server switch can replace this stream
// with one bound to the new backend. The read loop notices the closed conn and
// returns; ready flips false so the status bar shows "connecting" again.
func (s *Stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = false
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	s.byConn = map[string]*connStream{}
	s.attached = map[string]bool{}
}

// isReady reports whether the websocket is connected.
func (s *Stream) isReady() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// ensureStream lazily starts the websocket client once a Services handle is
// available to repaint through.
func (s *Shell) ensureStream() {
	s.mu.Lock()
	if s.stream != nil {
		s.mu.Unlock()
		return
	}
	svc := s.svc
	sh := s
	s.stream = newStream(s.B, func(connID, channel string, payload json.RawMessage) {
		if connID != "" {
			sh.invalidate("snapshot:" + connID)
		}
		// Live terminal output (terminal://output/{id} / terminal://exit/{id})
		// carries no body on the firehose; drop that terminal's snapshot so its
		// buffered text refetches, whether or not its tab is active.
		if strings.HasPrefix(channel, "terminal://") {
			if i := strings.LastIndexByte(channel, '/'); i >= 0 && i+1 < len(channel) {
				sh.invalidate("term:" + channel[i+1:])
			}
		}
		// A server-side conversation mutation (create/delete/title/status, incl.
		// the agent's auto-title) refreshes the sidebar lists — and the tab
		// strip's decoration, which reads the all-conversations list.
		if channel == "conversation://changed" {
			sh.invalidate("folders")
			sh.invalidate("all-convs")
			sh.mu.Lock()
			folder := sh.selectedFolder
			sh.mu.Unlock()
			if folder != "" {
				sh.invalidate("convs:" + folder)
			}
		}
		// A live-feedback settings save (this window or another) re-reads the
		// flag so the composer's feedback entry converges.
		if strings.HasPrefix(channel, "feedback-settings://") {
			sh.invalidate("feedback-settings")
		}
		// The pet channels drive the floating sprite window: a one-shot plays its
		// animation, a state change moves the ambient animation, and a new active
		// pet swaps the sheet in place (upstream's usePetOneShot / usePetState /
		// PetWindow subscriptions).
		if strings.HasPrefix(channel, "pet://") {
			sh.invalidate("pet-config")
			if w := sh.petWindow(); w != nil {
				switch channel {
				case "pet://oneshot":
					w.playOneShot(petEventString(payload))
				case "pet://state":
					w.setAmbient(petEventString(payload))
				case "pet://active-changed":
					w.reload()
				}
			}
		}
		svc.Invalidate()
	})
	s.mu.Unlock()
}
