package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gocode/src-go/officewatch"
	"gocode/src-go/web"
)

// ── pure helpers ────────────────────────────────────────────────────────────

func TestQueryParam(t *testing.T) {
	if v, ok := queryParam("cap=abc", "cap"); !ok || v != "abc" {
		t.Fatalf("got %q,%v", v, ok)
	}
	if v, ok := queryParam("foo=1&cap=ab%2Bc&bar=2", "cap"); !ok || v != "ab+c" {
		t.Fatalf("percent-decode failed: %q,%v", v, ok)
	}
	if _, ok := queryParam("foo=1", "cap"); ok {
		t.Fatalf("absent key should report not-found")
	}
	if v, ok := queryParam("cap=", "cap"); !ok || v != "" {
		t.Fatalf("empty value should be present-but-empty: %q,%v", v, ok)
	}
}

func TestQueryWithout(t *testing.T) {
	cases := map[string]string{
		"cap=secret":       "",
		"foo=1&cap=secret": "?foo=1",
		"cap=secret&a=1&b=2": "?a=1&b=2",
		"":                 "",
		"x=cap":            "?x=cap", // key merely appearing in a value is untouched
	}
	for in, want := range cases {
		if got := queryWithout(in, "cap"); got != want {
			t.Fatalf("queryWithout(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInjectShim(t *testing.T) {
	html := "<html><head><title>x</title></head><body>b</body></html>"
	want := "<html><head><SHIM><title>x</title></head><body>b</body></html>"
	if got := injectShim(html, "<SHIM>"); got != want {
		t.Fatalf("injectShim = %q", got)
	}
	if got := injectShim("<HTML><HEAD><meta></HEAD>", "<S>"); !strings.HasPrefix(got, "<HTML><HEAD><S><meta>") {
		t.Fatalf("case-insensitive head not handled: %q", got)
	}
	if got := injectShim("<body>x</body>", "<S>"); got != "<S><body>x</body>" {
		t.Fatalf("no-head fallback failed: %q", got)
	}
}

func TestShimTemplateSubstitution(t *testing.T) {
	shim := strings.ReplaceAll(shimTemplate, "__PFX__", "/api/office-watch-proxy/26315")
	shim = strings.ReplaceAll(shim, "__CAP__", "deadbeef")
	if !strings.Contains(shim, `var P="/api/office-watch-proxy/26315"`) {
		t.Fatalf("prefix not substituted")
	}
	if !strings.Contains(shim, `C="deadbeef"`) {
		t.Fatalf("cap not substituted")
	}
	if strings.Contains(shim, "__PFX__") || strings.Contains(shim, "__CAP__") {
		t.Fatalf("placeholder left behind")
	}
}

func TestHeaderWhitelists(t *testing.T) {
	for _, h := range []string{"accept", "content-type", "range", "if-none-match"} {
		if !forwardRequestHeader(h) {
			t.Fatalf("request header %q should forward", h)
		}
	}
	for _, h := range []string{"authorization", "cookie", "host", "connection"} {
		if forwardRequestHeader(h) {
			t.Fatalf("request header %q must NOT forward", h)
		}
	}
	for _, h := range []string{"content-type", "cache-control", "etag", "last-modified", "expires"} {
		if !forwardResponseHeader(h) {
			t.Fatalf("response header %q should forward", h)
		}
	}
	if forwardResponseHeader("content-length") {
		t.Fatalf("content-length must be dropped (streamed/rewritten)")
	}
}

// ── integration (httptest upstream stands in for officecli watch) ────────────

type upstreamCapture struct {
	mu    sync.Mutex
	path  string
	query string
	auth  string
	ctype string
}

func (c *upstreamCapture) record(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.path, c.query = r.URL.Path, r.URL.RawQuery
	c.auth, c.ctype = r.Header.Get("authorization"), r.Header.Get("content-type")
}

func (c *upstreamCapture) get() (path, query, auth, ctype string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.path, c.query, c.auth, c.ctype
}

func newWatchUpstream(t *testing.T) (*httptest.Server, *upstreamCapture, int) {
	t.Helper()
	cap := &upstreamCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.record(r)
		switch r.URL.Path {
		case "/":
			w.Header().Set("content-type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<html><head><title>t</title></head><body>x</body></html>")
		case "/events":
			w.Header().Set("content-type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "data: hello\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		default:
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"ok":true}`)
		}
	}))
	u := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	port, err := strconv.Atoi(u)
	if err != nil {
		t.Fatalf("bad upstream port: %v", err)
	}
	return srv, cap, port
}

func newProxyFront(t *testing.T) *httptest.Server {
	t.Helper()
	router := web.NewRouter(nil)
	RegisterOfficeWatchProxy(NewRegistry(router))
	return httptest.NewServer(router)
}

func TestOfficeWatchProxy_Integration(t *testing.T) {
	upstream, capture, port := newWatchUpstream(t)
	defer upstream.Close()
	officewatch.Register("test", port, "secret-cap")
	defer officewatch.Unregister("test")

	front := newProxyFront(t)
	defer front.Close()

	base := front.URL + "/api/office-watch-proxy/" + strconv.Itoa(port)

	// 1. Missing cap → 401.
	if code := statusOf(t, base+"/"); code != http.StatusUnauthorized {
		t.Fatalf("missing cap: code = %d, want 401", code)
	}
	// 2. Wrong cap → 401.
	if code := statusOf(t, base+"/?cap=nope"); code != http.StatusUnauthorized {
		t.Fatalf("wrong cap: code = %d, want 401", code)
	}
	// 3. Unknown port (SSRF) with a valid-looking cap → 401.
	ssrf := front.URL + "/api/office-watch-proxy/" + strconv.Itoa(port+1) + "/?cap=secret-cap"
	if code := statusOf(t, ssrf); code != http.StatusUnauthorized {
		t.Fatalf("SSRF unknown port: code = %d, want 401", code)
	}

	// 4. HTML root → shim injected, cap stripped upstream, no-store.
	resp, err := http.Get(base + "/?cap=secret-cap")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("html code = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("content-type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	if resp.Header.Get("cache-control") != "no-store" {
		t.Fatalf("cache-control = %q, want no-store", resp.Header.Get("cache-control"))
	}
	shimPrefix := "/api/office-watch-proxy/" + strconv.Itoa(port)
	if !strings.Contains(string(body), shimPrefix) || !strings.Contains(string(body), "secret-cap") {
		t.Fatalf("shim not injected with prefix+cap")
	}
	if !strings.HasPrefix(string(body), "<html><head><script>") {
		t.Fatalf("shim should land right after <head>: %q", string(body))
	}
	if p, q, _, _ := capture.get(); p != "/" || q != "" {
		t.Fatalf("upstream got path=%q query=%q, want / and empty (cap stripped)", p, q)
	}

	// 5. POST JSON → pass-through; cap + auth stripped, content-type forwarded.
	req, _ := http.NewRequest(http.MethodPost, base+"/api/edit?cap=secret-cap&foo=1", strings.NewReader(`{"a":1}`))
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer SHOULD-NOT-FORWARD")
	presp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	pbody, _ := io.ReadAll(presp.Body)
	presp.Body.Close()
	if presp.StatusCode != http.StatusOK || string(pbody) != `{"ok":true}` {
		t.Fatalf("post passthrough: code=%d body=%q", presp.StatusCode, pbody)
	}
	p, q, auth, ctype := capture.get()
	if p != "/api/edit" || q != "foo=1" { // RawQuery has no leading '?'
		t.Fatalf("upstream got path=%q query=%q", p, q)
	}
	if ctype != "application/json" {
		t.Fatalf("content-type not forwarded: %q", ctype)
	}
	if auth != "" {
		t.Fatalf("authorization header leaked upstream: %q", auth)
	}

	// 6. SSE → streamed, x-accel-buffering: no, lease released after drain.
	sresp, err := http.Get(base + "/events?cap=secret-cap")
	if err != nil {
		t.Fatalf("sse: %v", err)
	}
	sbody, _ := io.ReadAll(sresp.Body)
	sresp.Body.Close()
	if sresp.StatusCode != http.StatusOK {
		t.Fatalf("sse code = %d", sresp.StatusCode)
	}
	if ct := sresp.Header.Get("content-type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("sse content-type = %q", ct)
	}
	if sresp.Header.Get("x-accel-buffering") != "no" {
		t.Fatalf("x-accel-buffering = %q, want no", sresp.Header.Get("x-accel-buffering"))
	}
	if !strings.Contains(string(sbody), "data: hello") {
		t.Fatalf("sse body = %q", sbody)
	}
	waitLeasesReleased(t, port, 2*time.Second)
}

func statusOf(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func waitLeasesReleased(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, e := range officewatch.Snapshot() {
			if e.Port == port && e.SSELeases == 0 {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("SSE lease for port %d not released within %s", port, timeout)
}
