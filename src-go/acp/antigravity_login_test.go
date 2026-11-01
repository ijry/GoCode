package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// resetAgLoginSlot clears the package-level sign-in slot after a test so one
// test cannot refuse the next one's start.
func resetAgLoginSlot(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		agLoginMu.Lock()
		pending := agLoginPendingState
		agLoginPendingState = nil
		agLoginFinishingUntil = time.Time{}
		agLoginMu.Unlock()
		if pending != nil {
			pending.kill()
		}
	})
}

// fakeAgLoginSpawn wires an in-memory agent for the sign-in flow. When authURL
// is set, the agent prints it from its `authenticate` handler (as the real one
// does) and then blocks until release is closed. authResult, when set, is the
// handler's refusal.
func fakeAgLoginSpawn(t *testing.T, authURL string, authResult error, release <-chan struct{}) func(SpawnConfig) (*AgentProcess, error) {
	t.Helper()
	return func(cfg SpawnConfig) (*AgentProcess, error) {
		cr, aw := io.Pipe() // agent -> client
		ar, cw := io.Pipe() // client -> agent
		clientPeer := NewPeer(cr, cw)
		agentPeer := NewPeer(ar, aw)

		agentPeer.HandleRequest("initialize", func(json.RawMessage) (any, error) {
			return map[string]any{
				"protocolVersion":   ACPProtocolVersion,
				"agentCapabilities": map[string]any{},
			}, nil
		})
		agentPeer.HandleRequest("authenticate", func(json.RawMessage) (any, error) {
			if authURL != "" && cfg.OnOutput != nil {
				cfg.OnOutput("stdout", agAuthPromptMarker+authURL)
			}
			if release != nil {
				select {
				case <-release:
				case <-time.After(5 * time.Second):
				}
			}
			if authResult != nil {
				return nil, authResult
			}
			return map[string]any{}, nil
		})

		go clientPeer.Run()
		go agentPeer.Run()
		return &AgentProcess{Peer: clientPeer, CloseFn: func() {
			cw.Close()
			aw.Close()
			cr.Close()
			ar.Close()
		}}, nil
	}
}

// loopbackListener starts a one-shot redirect listener and reports the query it
// received. Closing gotRedirect releases the fake agent.
func loopbackListener(t *testing.T) (*httptest.Server, <-chan url.Values, chan struct{}) {
	t.Helper()
	got := make(chan url.Values, 1)
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case got <- r.URL.Query():
		default:
		}
		close(released)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("signed in"))
	}))
	t.Cleanup(srv.Close)
	return srv, got, released
}

func authURLFor(srv *httptest.Server, state string) string {
	redirect := srv.URL + "/"
	return "https://accounts.google.com/o/oauth2/auth?client_id=test&scope=openid&state=" +
		url.QueryEscape(state) + "&redirect_uri=" + url.QueryEscape(redirect)
}

func TestAntigravityLoginStartAndFinish(t *testing.T) {
	resetAgLoginSlot(t)
	srv, got, released := loopbackListener(t)
	authURL := authURLFor(srv, "st-123")
	withFakeAntigravitySpawn(t, true, fakeAgLoginSpawn(t, authURL, nil, released))

	env, _ := geminiEnv(t, nil)
	start, err := AntigravityLoginStart(env, "oauth-personal")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if start.AlreadySignedIn || start.Handle == nil || start.AuthURL == nil || start.RedirectURI == nil {
		t.Fatalf("start = %+v, want a pending link", start)
	}
	if *start.AuthURL != authURL {
		t.Fatalf("authUrl = %q", *start.AuthURL)
	}
	if *start.RedirectURI != srv.URL+"/" {
		t.Fatalf("redirectUri = %q", *start.RedirectURI)
	}
	if start.MethodID != "oauth-personal" || start.ExpiresInSecs != 300 {
		t.Fatalf("start = %+v", start)
	}

	outcome, err := AntigravityLoginFinish(*start.Handle, "http://127.0.0.1:1/?state=st-123&code=4%2Fabc")
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !outcome.SignedIn || outcome.Message != nil || outcome.Retryable {
		t.Fatalf("outcome = %+v, want a successful sign-in", outcome)
	}
	query := <-got
	if query.Get("state") != "st-123" || query.Get("code") != "4/abc" {
		t.Fatalf("listener query = %v", query)
	}
	wantPath := filepath.Join(env["GEMINI_HOME"], "antigravity-acp", "acp_token.json")
	if outcome.CredentialPath == nil || *outcome.CredentialPath != wantPath {
		t.Fatalf("credentialPath = %v, want %q", outcome.CredentialPath, wantPath)
	}
}

func TestAntigravityLoginStartAlreadySignedIn(t *testing.T) {
	resetAgLoginSlot(t)
	withFakeAntigravitySpawn(t, true, fakeAgLoginSpawn(t, "", nil, nil))

	env, _ := geminiEnv(t, nil)
	start, err := AntigravityLoginStart(env, "oauth-personal")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !start.AlreadySignedIn {
		t.Fatalf("start = %+v, want alreadySignedIn", start)
	}
	if start.Handle != nil || start.AuthURL != nil || start.RedirectURI != nil {
		t.Fatalf("start = %+v, want no link fields", start)
	}
}

func TestAntigravityLoginStartRefused(t *testing.T) {
	resetAgLoginSlot(t)
	withFakeAntigravitySpawn(t, true, fakeAgLoginSpawn(t, "", errors.New("bad auth method"), nil))

	env, _ := geminiEnv(t, nil)
	_, err := AntigravityLoginStart(env, "oauth-personal")
	if err == nil || !strings.Contains(err.Error(), "refused to start a sign-in") {
		t.Fatalf("err = %v, want a refusal", err)
	}
}

func TestAntigravityLoginStartValidatesMethod(t *testing.T) {
	resetAgLoginSlot(t)
	env, _ := geminiEnv(t, nil)
	if _, err := AntigravityLoginStart(env, "gemini-api-key"); err == nil ||
		!strings.Contains(err.Error(), "does not use a browser sign-in") {
		t.Fatalf("api-key method err = %v", err)
	}
	if _, err := AntigravityLoginStart(env, "nope"); err == nil ||
		!strings.Contains(err.Error(), "unknown Antigravity auth method") {
		t.Fatalf("unknown method err = %v", err)
	}
}

func TestAntigravityLoginBinaryMissing(t *testing.T) {
	resetAgLoginSlot(t)
	withFakeAntigravitySpawn(t, false, nil)
	env, _ := geminiEnv(t, nil)
	_, err := AntigravityLoginStart(env, "oauth-personal")
	if err == nil {
		t.Fatalf("expected an error when the binary cannot be resolved")
	}
	if _, ok := err.(*AgentUnavailableError); !ok {
		t.Fatalf("err = %T, want *AgentUnavailableError", err)
	}
}

func TestAntigravityLoginFinishRejectsBadPasteWithoutConsuming(t *testing.T) {
	resetAgLoginSlot(t)
	srv, got, released := loopbackListener(t)
	withFakeAntigravitySpawn(t, true, fakeAgLoginSpawn(t, authURLFor(srv, "st-123"), nil, released))

	env, _ := geminiEnv(t, nil)
	start, err := AntigravityLoginStart(env, "oauth-personal")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	// No code at all: rejected locally, the attempt stays usable.
	outcome, err := AntigravityLoginFinish(*start.Handle, "http://127.0.0.1:1/?state=st-123")
	if err != nil {
		t.Fatalf("finish (no code): %v", err)
	}
	if outcome.SignedIn || !outcome.Retryable || outcome.Message == nil {
		t.Fatalf("outcome = %+v, want a retryable local reject", outcome)
	}

	// A paste from an earlier attempt: also retryable.
	outcome, err = AntigravityLoginFinish(*start.Handle, "http://127.0.0.1:1/?state=other&code=4%2Fabc")
	if err != nil {
		t.Fatalf("finish (wrong state): %v", err)
	}
	if outcome.SignedIn || !outcome.Retryable || !strings.Contains(*outcome.Message, "different sign-in attempt") {
		t.Fatalf("outcome = %+v, want a wrong-state reject", outcome)
	}

	// The attempt is still waiting, so a correct paste completes it. A lone
	// code carries no state; the request uses the captured one.
	outcome, err = AntigravityLoginFinish(*start.Handle, "4/abc")
	if err != nil {
		t.Fatalf("finish (lone code): %v", err)
	}
	if !outcome.SignedIn {
		t.Fatalf("outcome = %+v, want signed in", outcome)
	}
	query := <-got
	if query.Get("state") != "st-123" || query.Get("code") != "4/abc" {
		t.Fatalf("listener query = %v", query)
	}
}

func TestAntigravityLoginExpires(t *testing.T) {
	resetAgLoginSlot(t)
	srv, _, released := loopbackListener(t)
	withFakeAntigravitySpawn(t, true, fakeAgLoginSpawn(t, authURLFor(srv, "st-1"), nil, released))

	env, _ := geminiEnv(t, nil)
	start, err := AntigravityLoginStart(env, "oauth-personal")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	agLoginMu.Lock()
	agLoginPendingState.started = time.Now().Add(-6 * time.Minute)
	agLoginMu.Unlock()

	outcome, err := AntigravityLoginFinish(*start.Handle, "http://127.0.0.1:1/?state=st-1&code=x")
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if outcome.SignedIn || outcome.Retryable || outcome.Message == nil ||
		!strings.Contains(*outcome.Message, "expired") {
		t.Fatalf("outcome = %+v, want an expiry outcome", outcome)
	}
	if _, err := AntigravityLoginFinish(*start.Handle, "http://127.0.0.1:1/?state=st-1&code=x"); err == nil {
		t.Fatalf("second finish should report that nothing is waiting")
	}
}

func TestAntigravityLoginCancel(t *testing.T) {
	resetAgLoginSlot(t)
	srv, _, released := loopbackListener(t)
	withFakeAntigravitySpawn(t, true, fakeAgLoginSpawn(t, authURLFor(srv, "st-1"), nil, released))

	env, _ := geminiEnv(t, nil)
	start, err := AntigravityLoginStart(env, "oauth-personal")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := AntigravityLoginCancel(*start.Handle); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := AntigravityLoginCancel(*start.Handle); err == nil ||
		!strings.Contains(err.Error(), "no sign-in is waiting") {
		t.Fatalf("second cancel err = %v", err)
	}
	if _, err := AntigravityLoginFinish(*start.Handle, "http://127.0.0.1:1/?state=st-1&code=x"); err == nil {
		t.Fatalf("finish after cancel should fail")
	}
}

func TestAntigravityLinkScraping(t *testing.T) {
	link := "https://accounts.google.com/o/oauth2/auth?client_id=x&state=s"
	for _, line := range []string{
		agAuthPromptMarker + link,
		"INFO some log header " + agAuthPromptMarker + link,
		`{"jsonrpc":"2.0","method":"x"} ` + agAuthPromptMarker + link + " trailing",
	} {
		got, ok := agLinkFromLine(line)
		if !ok || got != link {
			t.Errorf("agLinkFromLine(%q) = %q, %v", line, got, ok)
		}
	}
	for _, line := range []string{
		"",
		"no marker here",
		agAuthPromptMarker,
		agAuthPromptMarker + "http://insecure.example/x",
	} {
		if got, ok := agLinkFromLine(line); ok {
			t.Errorf("agLinkFromLine(%q) = %q, want false", line, got)
		}
	}
}

func TestAntigravityRedirectParsing(t *testing.T) {
	parsed := parseAgRedirect("http://127.0.0.1:8080/?state=st%2D1&code=4%2Fabc&error=")
	if parsed.code != "4/abc" || parsed.state != "st-1" || parsed.error != "" {
		t.Fatalf("parsed = %+v", parsed)
	}
	if got := parseAgRedirect("code=4%2Fabc&state=a+b"); got.code != "4/abc" || got.state != "a b" {
		t.Fatalf("bare query = %+v", got)
	}
	if got := parseAgRedirect("4/0Axyz"); got.code != "4/0Axyz" {
		t.Fatalf("lone code = %+v", got)
	}
	if got := parseAgRedirect("  "); got != (agParsedRedirect{}) {
		t.Fatalf("blank = %+v", got)
	}

	if agRejectLocally("st", parseAgRedirect("code=x")) != "" {
		t.Fatalf("no state should not be rejected")
	}
	if agRejectLocally("st", parseAgRedirect("code=x&state=st")) != "" {
		t.Fatalf("matching state should pass")
	}
	if got := agRejectLocally("st", parseAgRedirect("code=x&state=other")); !strings.Contains(got, "different sign-in") {
		t.Fatalf("state reject = %q", got)
	}
	if got := agRejectLocally("st", parseAgRedirect("error=access_denied")); got != "" {
		t.Fatalf("error should be forwarded, got %q", got)
	}
	if got := agRejectLocally("st", parseAgRedirect("")); !strings.Contains(got, "No authorization code") {
		t.Fatalf("empty reject = %q", got)
	}
}

func TestAntigravityLoopbackChecks(t *testing.T) {
	valid := []string{"http://127.0.0.1:8080/", "http://127.0.0.1:1", "http://127.0.0.1:65535/cb?x=1"}
	for _, uri := range valid {
		if !agIsLoopbackRedirect(uri) {
			t.Errorf("agIsLoopbackRedirect(%q) = false", uri)
		}
	}
	invalid := []string{
		"https://127.0.0.1:8080/", "http://localhost:8080/", "http://127.0.0.1/",
		"http://127.0.0.1:8080@evil.example/", "http://127.0.0.1:8a/",
	}
	for _, uri := range invalid {
		if agIsLoopbackRedirect(uri) {
			t.Errorf("agIsLoopbackRedirect(%q) = true", uri)
		}
	}

	if _, err := agBuildRedirectRequest("http://evil.example/", "st", agParsedRedirect{code: "x"}); err == nil {
		t.Fatalf("non-loopback redirect should be refused")
	}
	built, err := agBuildRedirectRequest("http://127.0.0.1:9/cb", "st 1", agParsedRedirect{code: "a/b", error: "e"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !strings.HasPrefix(built, "http://127.0.0.1:9/cb?") ||
		!strings.Contains(built, "code=a%2Fb") || !strings.Contains(built, "state=st+1") {
		t.Fatalf("built = %q", built)
	}
}

func TestAntigravityCredentialPath(t *testing.T) {
	env, acpDir := geminiEnv(t, nil)
	path := antigravityCredentialPath(env, "oauth-personal")
	if path == nil || *path != filepath.Join(acpDir, "acp_token.json") {
		t.Fatalf("personal path = %v", path)
	}
	path = antigravityCredentialPath(env, "oauth-business")
	if path == nil || *path != filepath.Join(acpDir, "acp_business_token.json") {
		t.Fatalf("business path = %v", path)
	}
}

func TestAntigravityLoginURLQueryHelpers(t *testing.T) {
	u := "https://x.test/auth?a=1&state=st%2D9&redirect_uri=" + url.QueryEscape("http://127.0.0.1:42/")
	if got := agQueryParam(u, "state"); got != "st-9" {
		t.Fatalf("state = %q", got)
	}
	if got := agQueryParam(u, "redirect_uri"); got != "http://127.0.0.1:42/" {
		t.Fatalf("redirect = %q", got)
	}
	if got := agQueryParam(u, "missing"); got != "" {
		t.Fatalf("missing = %q", got)
	}
	if _, err := agExtractRedirectURI("https://x.test/auth?state=1"); err == nil {
		t.Fatalf("link without redirect_uri should fail")
	}
	if _, err := agExtractRedirectURI("https://x.test/auth?redirect_uri=" + url.QueryEscape("http://evil.example/cb")); err == nil {
		t.Fatalf("off-loopback redirect_uri should fail")
	}
}

func TestAntigravityLoginStartDisplacesPrevious(t *testing.T) {
	resetAgLoginSlot(t)
	srv, _, released := loopbackListener(t)
	withFakeAntigravitySpawn(t, true, fakeAgLoginSpawn(t, authURLFor(srv, "st-1"), nil, released))

	env, _ := geminiEnv(t, nil)
	first, err := AntigravityLoginStart(env, "oauth-personal")
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	second, err := AntigravityLoginStart(env, "oauth-personal")
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if *first.Handle == *second.Handle {
		t.Fatalf("handles should differ")
	}
	if err := AntigravityLoginCancel(*first.Handle); err == nil {
		t.Fatalf("the displaced handle must not cancel the current attempt")
	}
	if _, err := AntigravityLoginFinish(*first.Handle, fmt.Sprintf("http://127.0.0.1:1/?state=st-1&code=x")); err == nil {
		t.Fatalf("the displaced handle must not finish the current attempt")
	}
	if err := AntigravityLoginCancel(*second.Handle); err != nil {
		t.Fatalf("cancel second: %v", err)
	}
}
