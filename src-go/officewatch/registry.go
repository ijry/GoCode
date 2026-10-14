// Package officewatch ports upstream src-tauri/src/office_watch: the pool of
// `officecli watch` preview processes plus the per-watch capability / SSE-lease
// registry the server-mode reverse proxy authenticates against.
//
// This slice ports the registry + the capability/SSRF gate the proxy needs
// (validate_watch_cap / note_proxy_request / acquire_sse_lease /
// release_sse_lease). Spawning and reaping the watch child lives in the caller
// (start/stop_office_watch); here a watch is "live" while it has a registry
// entry, matching upstream's rule that a stopped/crashed watch's port stops
// being forwardable on the very next request.
package officewatch

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// Instance is one live watch process's proxy-facing state. Mirrors upstream
// WatchInstance's proxy fields (the child process + file bookkeeping live in the
// caller). Keyed by canonical file path upstream; any stable key works here.
type Instance struct {
	Key          string
	Port         int
	Cap          string
	Proxied      bool
	SSELeases    int
	LastActivity time.Time
	Live         bool
}

var (
	mu      sync.Mutex
	watches = map[string]*Instance{}
)

// NewCap mints a high-entropy per-watch capability. Upstream uses a v4 UUID
// string (~122 bits); we emit 32 hex chars from crypto/rand (128 bits).
func NewCap() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never fails on supported platforms; fall back to a
		// time-seeded value rather than returning an empty (guessable) cap.
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}

// Register seeds or refreshes a live watch entry keyed by key (canonical file
// path). Called by start_office_watch (and by tests).
func Register(key string, port int, cap string) {
	mu.Lock()
	defer mu.Unlock()
	watches[key] = &Instance{
		Key: key, Port: port, Cap: cap,
		LastActivity: time.Now(), Live: true,
	}
}

// Unregister removes a watch (stop_office_watch / folder removal).
func Unregister(key string) {
	mu.Lock()
	defer mu.Unlock()
	delete(watches, key)
}

// NoteProxyRequest marks a watch as proxy-served (web/remote, not desktop) and
// bumps its activity clock. Called by the proxy on every forwarded request.
func NoteProxyRequest(port int) {
	mu.Lock()
	defer mu.Unlock()
	if e := liveByPort(port); e != nil {
		e.Proxied = true
		e.LastActivity = time.Now()
	}
}

// AcquireSSELease accounts a newly-opened SSE (`/events`) connection.
func AcquireSSELease(port int) {
	mu.Lock()
	defer mu.Unlock()
	if e := liveByPort(port); e != nil {
		e.Proxied = true
		e.SSELeases++
		e.LastActivity = time.Now()
	}
}

// ReleaseSSELease releases an SSE connection accounted by AcquireSSELease and
// bumps the activity clock, starting the grace window before reaping.
func ReleaseSSELease(port int) {
	mu.Lock()
	defer mu.Unlock()
	if e := liveByPort(port); e != nil {
		if e.SSELeases > 0 {
			e.SSELeases--
		}
		e.LastActivity = time.Now()
	}
}

// IsKnownWatchPort is the SSRF gate: true only for a port bound to a live watch.
func IsKnownWatchPort(port int) bool {
	mu.Lock()
	defer mu.Unlock()
	return liveByPort(port) != nil
}

// ValidateWatchCap is the proxy's auth + SSRF gate: the request's port must
// belong to a live watch AND carry that watch's exact capability. Constant-time
// compare so a mismatched cap can't be probed by timing.
func ValidateWatchCap(port int, cap string) bool {
	mu.Lock()
	defer mu.Unlock()
	e := liveByPort(port)
	if e == nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(e.Cap), []byte(cap)) == 1
}

// Snapshot returns a copy of every live watch, sorted by port, for status /
// idle-sweep consumers.
func Snapshot() []Instance {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Instance, 0, len(watches))
	for _, e := range watches {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// liveByPort returns the live entry bound to port (callers hold mu).
func liveByPort(port int) *Instance {
	for _, e := range watches {
		if e.Port == port && e.Live {
			return e
		}
	}
	return nil
}
