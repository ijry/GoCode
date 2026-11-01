package acp

// Antigravity browser-free sign-in — the Go port of upstream
// acp::antigravity_login (start / finish / cancel).
//
// The agent runs its OAuth flow through a one-shot loopback listener and opens
// a browser itself. On a headless machine that fails silently: the browser
// cannot be opened, so nothing completes and the agent blocks until its own
// timeout. This flow drives a short-lived agent instead: ACP `initialize` +
// `authenticate`, scrape the authorization link the agent prints (stdout or
// stderr, depending on its version), show it to the user, then relay the
// unreachable loopback redirect from here — codeg runs on the machine that can
// reach it.
//
// Only one sign-in may be in flight: the credential is per-GEMINI_HOME, so two
// concurrent flows race to the same token file.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// agAuthPromptMarker is the exact line the agent's credential manager
	// prints before it starts waiting for the redirect. Matched as a substring:
	// a protocol frame can share the stdout line, and stderr may carry a log
	// prefix.
	agAuthPromptMarker = "Open the following link to authenticate the ACP server: "
	// agLoopbackPrefix is the only redirect host the agent ever uses.
	agLoopbackPrefix = "http://127.0.0.1:"

	agInitWait         = 60 * time.Second
	agURLWait          = 90 * time.Second
	agAuthenticateWait = 180 * time.Second
	agRedirectWait     = 20 * time.Second
	agPendingTTL       = 300 * time.Second
	agFinishBudget     = agRedirectWait + agAuthenticateWait + 30*time.Second
)

// AntigravityLoginStartInfo is what the panel needs to render step one. The
// three link fields are nil together, exactly when AlreadySignedIn is true.
type AntigravityLoginStartInfo struct {
	AlreadySignedIn bool    `json:"alreadySignedIn"`
	Handle          *string `json:"handle"`
	AuthURL         *string `json:"authUrl"`
	RedirectURI     *string `json:"redirectUri"`
	MethodID        string  `json:"methodId"`
	ExpiresInSecs   int64   `json:"expiresInSecs"`
}

// AntigravityLoginOutcome is the result of step two.
type AntigravityLoginOutcome struct {
	SignedIn bool `json:"signedIn"`
	// Message carries the agent's own refusal, already redacted; nil on success.
	Message *string `json:"message"`
	// Retryable is true only for a paste codeg rejected on its own: the
	// consent is still good and only the paste needs fixing.
	Retryable bool `json:"retryable"`
	// CredentialPath names the file this sign-in produced, when codeg can.
	CredentialPath *string `json:"credentialPath"`
}

// agLoginPending is one published sign-in waiting for its redirect.
type agLoginPending struct {
	handle         string
	methodID       string
	redirectURI    string
	state          string
	credentialPath *string
	proc           *AgentProcess
	authCh         chan error
	authCancel     context.CancelFunc
	started        time.Time
}

func (p *agLoginPending) kill() {
	if p.authCancel != nil {
		p.authCancel()
	}
	if p.proc != nil {
		_ = p.proc.Close()
	}
}

var (
	agLoginMu             sync.Mutex
	agLoginGeneration     uint64
	agLoginPendingState   *agLoginPending
	agLoginFinishingUntil time.Time
)

// agLoginHTTPClient is used only for the loopback redirect: no proxy (a
// loopback address must never leave the machine), no redirect following (the
// response must not choose the next target).
var agLoginHTTPClient = &http.Client{
	Timeout: agRedirectWait,
	Transport: &http.Transport{
		Proxy: nil,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// AntigravityLoginStart begins a browser-free sign-in: spawn the agent, ask it
// to authenticate, and return the URL it wants a human to open.
func AntigravityLoginStart(runtimeEnv map[string]string, methodID string) (*AntigravityLoginStartInfo, error) {
	methodID = strings.TrimSpace(methodID)
	if !isAntigravityAuthMethod(methodID) {
		return nil, fmt.Errorf("unknown Antigravity auth method %q", methodID)
	}
	if methodID != "oauth-personal" && methodID != "oauth-business" {
		return nil, fmt.Errorf("%s does not use a browser sign-in; set its credential in the panel instead", methodID)
	}

	generation, displaced, err := claimAgLogin()
	if err != nil {
		return nil, err
	}
	if displaced != nil {
		displaced.kill()
	}

	info, pending, err := agStartClaimed(runtimeEnv, methodID)
	if err != nil {
		return nil, err
	}
	if pending == nil {
		return info, nil // already signed in; no link to complete
	}
	if !installAgLogin(generation, pending) {
		pending.kill()
		return nil, errors.New("a newer sign-in replaced this one; use the link it produced")
	}
	return info, nil
}

// claimAgLogin takes the single sign-in slot, displacing any published attempt.
// A sign-in that is mid-completion is refused: it is about to write the
// credential, and overlapping it could leave the wrong account signed in.
func claimAgLogin() (uint64, *agLoginPending, error) {
	agLoginMu.Lock()
	if !agLoginFinishingUntil.IsZero() && time.Now().Before(agLoginFinishingUntil) {
		agLoginMu.Unlock()
		return 0, nil, errors.New("a sign-in is already being completed; wait for it to finish before starting another")
	}
	agLoginGeneration++
	generation := agLoginGeneration
	displaced := agLoginPendingState
	agLoginPendingState = nil
	agLoginFinishingUntil = time.Time{}
	agLoginMu.Unlock()
	return generation, displaced, nil
}

func installAgLogin(generation uint64, pending *agLoginPending) bool {
	agLoginMu.Lock()
	defer agLoginMu.Unlock()
	if agLoginGeneration != generation {
		return false
	}
	agLoginPendingState = pending
	return true
}

// agStartClaimed runs the spawn + handshake. A nil pending means the agent
// already held a usable credential and no link was produced.
func agStartClaimed(runtimeEnv map[string]string, methodID string) (*AntigravityLoginStartInfo, *agLoginPending, error) {
	cfg, ok := antigravityResolveSpawn("antigravity", os.TempDir(), runtimeEnv)
	if !ok {
		return nil, nil, &AgentUnavailableError{AgentType: "antigravity"}
	}
	cfg.Env = agSignInEnv(cfg.Env)
	urlCh := make(chan string, 1)
	cfg.OnOutput = func(_, line string) {
		if link, ok := agLinkFromLine(line); ok {
			select {
			case urlCh <- link:
			default:
			}
		}
	}
	proc, err := antigravitySpawnAgent(cfg)
	if err != nil {
		return nil, nil, err
	}

	// initialize must be answered before authenticate goes out: the URL print
	// shares stdout with the protocol frames.
	initCtx, initCancel := context.WithTimeout(context.Background(), agInitWait)
	_, err = proc.Peer.Call(initCtx, "initialize", map[string]any{
		"protocolVersion":    ACPProtocolVersion,
		"clientCapabilities": map[string]any{},
	})
	initCancel()
	if err != nil {
		_ = proc.Close()
		return nil, nil, fmt.Errorf("Antigravity did not accept `initialize`: %v", err)
	}

	authCtx, authCancel := context.WithTimeout(context.Background(), agPendingTTL)
	authCh := make(chan error, 1)
	go func() {
		_, callErr := proc.Peer.Call(authCtx, "authenticate", map[string]any{"methodId": methodID})
		authCh <- callErr
	}()

	timer := time.NewTimer(agURLWait)
	defer timer.Stop()
	var authURL string
	select {
	case authURL = <-urlCh:
	case callErr := <-authCh:
		authCancel()
		_ = proc.Close()
		if callErr == nil {
			// A cached, still-refreshable token: nothing to complete.
			return &AntigravityLoginStartInfo{
				AlreadySignedIn: true,
				MethodID:        methodID,
				ExpiresInSecs:   int64(agPendingTTL / time.Second),
			}, nil, nil
		}
		return nil, nil, fmt.Errorf("Antigravity refused to start a sign-in: %v", callErr)
	case <-timer.C:
		authCancel()
		_ = proc.Close()
		return nil, nil, fmt.Errorf("Antigravity did not produce a sign-in link.%s", agStderrHint(proc))
	}

	redirectURI, err := agExtractRedirectURI(authURL)
	if err != nil {
		authCancel()
		_ = proc.Close()
		return nil, nil, err
	}
	handle := newAgLoginHandle()
	info := &AntigravityLoginStartInfo{
		Handle:        &handle,
		AuthURL:       &authURL,
		RedirectURI:   &redirectURI,
		MethodID:      methodID,
		ExpiresInSecs: int64(agPendingTTL / time.Second),
	}
	return info, &agLoginPending{
		handle:         handle,
		methodID:       methodID,
		redirectURI:    redirectURI,
		state:          agQueryParam(authURL, "state"),
		credentialPath: antigravityCredentialPath(runtimeEnv, methodID),
		proc:           proc,
		authCh:         authCh,
		authCancel:     authCancel,
		started:        time.Now(),
	}, nil
}

// AntigravityLoginFinish delivers the redirect the user's browser could not
// reach, and reports whether the agent signed in.
func AntigravityLoginFinish(handle, pasted string) (*AntigravityLoginOutcome, error) {
	handle = strings.TrimSpace(handle)
	parsed := parseAgRedirect(pasted)

	agLoginMu.Lock()
	pending := agLoginPendingState
	if pending == nil {
		agLoginMu.Unlock()
		return nil, errors.New("no sign-in is waiting; start a new one")
	}
	if pending.handle != handle {
		agLoginMu.Unlock()
		return nil, errors.New("this sign-in is no longer active; start a new one")
	}
	if time.Since(pending.started) > agPendingTTL {
		agLoginPendingState = nil
		agLoginMu.Unlock()
		pending.kill()
		return agOutcome(false, "This sign-in expired — Antigravity stops waiting after 5 minutes. Start a new one.",
			false, pending.credentialPath), nil
	}
	if reason := agRejectLocally(pending.state, parsed); reason != "" {
		// Still waiting: the paste never reached the agent, so the consent is
		// still good and only the paste needs fixing.
		agLoginMu.Unlock()
		return agOutcome(false, reason, true, pending.credentialPath), nil
	}
	target, err := agBuildRedirectRequest(pending.redirectURI, pending.state, parsed)
	if err != nil {
		agLoginMu.Unlock()
		return nil, err
	}
	agLoginPendingState = nil
	generation := agLoginGeneration
	agLoginFinishingUntil = time.Now().Add(agFinishBudget)
	agLoginMu.Unlock()

	outcome := agFinishDelivering(target, pending)
	releaseAgLoginFinishing(generation)
	return outcome, nil
}

// AntigravityLoginCancel abandons a pending sign-in and stops its agent.
func AntigravityLoginCancel(handle string) error {
	handle = strings.TrimSpace(handle)
	agLoginMu.Lock()
	pending := agLoginPendingState
	if pending == nil {
		agLoginMu.Unlock()
		return errors.New("no sign-in is waiting; start a new one")
	}
	if pending.handle != handle {
		agLoginMu.Unlock()
		return errors.New("this sign-in is no longer active; start a new one")
	}
	agLoginPendingState = nil
	agLoginMu.Unlock()
	pending.kill()
	return nil
}

func releaseAgLoginFinishing(generation uint64) {
	agLoginMu.Lock()
	defer agLoginMu.Unlock()
	if agLoginGeneration == generation && !agLoginFinishingUntil.IsZero() {
		agLoginFinishingUntil = time.Time{}
	}
}

func agFinishDelivering(target string, pending *agLoginPending) *AntigravityLoginOutcome {
	defer pending.kill()
	if reason := agDeliverRedirect(target); reason != "" {
		return agOutcome(false, reason, false, pending.credentialPath)
	}
	select {
	case err := <-pending.authCh:
		if err == nil {
			return agOutcome(true, "", false, pending.credentialPath)
		}
		return agOutcome(false, err.Error(), false, pending.credentialPath)
	case <-time.After(agAuthenticateWait):
		return agOutcome(false, "Antigravity did not finish the sign-in within 3 minutes.",
			false, pending.credentialPath)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func isAntigravityAuthMethod(method string) bool {
	for _, known := range AntigravityAuthMethods {
		if method == known {
			return true
		}
	}
	return false
}

// agSignInEnv layers the variables the sign-in child needs: unbuffered stdout
// (otherwise the printed link never reaches us) and a no-op BROWSER so the
// agent cannot block on a text browser.
func agSignInEnv(base map[string]string) map[string]string {
	out := make(map[string]string, len(base)+2)
	for k, v := range base {
		out[k] = v
	}
	out["PYTHONUNBUFFERED"] = "1"
	if runtime.GOOS != "windows" {
		out["BROWSER"] = "true"
	}
	return out
}

func agLinkFromLine(line string) (string, bool) {
	i := strings.Index(line, agAuthPromptMarker)
	if i < 0 {
		return "", false
	}
	rest := strings.TrimSpace(line[i+len(agAuthPromptMarker):])
	if rest == "" {
		return "", false
	}
	// The URL runs to the first whitespace; a JSON frame spliced onto the same
	// line would start after one.
	link := strings.Fields(rest)[0]
	if !strings.HasPrefix(link, "https://") {
		return "", false
	}
	return link, true
}

func agStderrHint(proc *AgentProcess) string {
	if proc == nil {
		return ""
	}
	lines := proc.StderrTail()
	if len(lines) == 0 {
		return ""
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return ""
	}
	if len(last) > 300 {
		last = last[:300] + "…"
	}
	return " Last output: " + last
}

func agExtractRedirectURI(authURL string) (string, error) {
	redirect := agQueryParam(authURL, "redirect_uri")
	if redirect == "" {
		return "", errors.New("Antigravity's sign-in link carries no redirect address, so codeg cannot complete it")
	}
	if !agIsLoopbackRedirect(redirect) {
		return "", errors.New("Antigravity's sign-in link points somewhere other than the loopback interface; refusing to complete it")
	}
	return redirect, nil
}

func agQueryParam(rawURL, key string) string {
	i := strings.IndexByte(rawURL, '?')
	if i < 0 {
		return ""
	}
	query := rawURL[i+1:]
	if j := strings.IndexByte(query, '#'); j >= 0 {
		query = query[:j]
	}
	for _, pair := range strings.Split(query, "&") {
		name, value, ok := strings.Cut(pair, "=")
		if !ok || name != key {
			continue
		}
		return agPercentDecode(value)
	}
	return ""
}

func agPercentDecode(raw string) string {
	plusDecoded := strings.ReplaceAll(raw, "+", " ")
	decoded, err := url.QueryUnescape(plusDecoded)
	if err != nil {
		return plusDecoded
	}
	return decoded
}

func agIsLoopbackRedirect(uri string) bool {
	rest, ok := strings.CutPrefix(uri, agLoopbackPrefix)
	if !ok {
		return false
	}
	authority := rest
	if i := strings.IndexAny(authority, "/?#"); i >= 0 {
		authority = authority[:i]
	}
	if authority == "" {
		return false
	}
	for _, r := range authority {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type agParsedRedirect struct {
	code  string
	state string
	error string
}

// parseAgRedirect accepts a full URL, a bare query string, or a lone code.
func parseAgRedirect(raw string) agParsedRedirect {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return agParsedRedirect{}
	}
	var query string
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		query = raw[i+1:]
	} else if strings.Contains(raw, "=") {
		query = raw
	} else {
		return agParsedRedirect{code: raw}
	}
	if j := strings.IndexByte(query, '#'); j >= 0 {
		query = query[:j]
	}
	var parsed agParsedRedirect
	for _, pair := range strings.Split(query, "&") {
		name, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		value = agPercentDecode(value)
		if value == "" {
			continue
		}
		switch strings.TrimSpace(name) {
		case "code":
			parsed.code = value
		case "state":
			parsed.state = value
		case "error":
			parsed.error = value
		}
	}
	return parsed
}

func agRejectLocally(expectedState string, parsed agParsedRedirect) string {
	// An error is a legitimate answer (the user pressed "deny"): forward it so
	// the agent stops waiting.
	if parsed.error == "" && parsed.code == "" {
		return "No authorization code found. Paste the whole address your browser ended up on — it starts with http://127.0.0.1: and contains `code=`."
	}
	if parsed.state != "" && expectedState != "" && parsed.state != expectedState {
		return "That address belongs to a different sign-in attempt. Use the link this sign-in gave you, or start a new one."
	}
	return ""
}

// agBuildRedirectRequest rebuilds the request from the captured listener
// address; only the OAuth parameters come from the user's paste.
func agBuildRedirectRequest(redirectURI, expectedState string, parsed agParsedRedirect) (string, error) {
	if !agIsLoopbackRedirect(redirectURI) {
		return "", errors.New("Antigravity asked for a redirect target that is not on the loopback interface; refusing to contact it")
	}
	query := url.Values{}
	if expectedState != "" {
		query.Set("state", expectedState)
	}
	if parsed.code != "" {
		query.Set("code", parsed.code)
	}
	if parsed.error != "" {
		query.Set("error", parsed.error)
	}
	separator := "?"
	if strings.Contains(redirectURI, "?") {
		separator = "&"
	}
	return redirectURI + separator + query.Encode(), nil
}

// agDeliverRedirect performs the redirect against the agent's one-shot listener.
func agDeliverRedirect(target string) string {
	resp, err := agLoginHTTPClient.Get(target)
	if err != nil {
		var netErr net.Error
		switch {
		case errors.Is(err, syscall.ECONNREFUSED):
			return "Antigravity is no longer listening for this sign-in (it waits 5 minutes). Start a new sign-in."
		case errors.As(err, &netErr) && netErr.Timeout():
			return "Antigravity's sign-in listener did not answer in time. Start a new sign-in."
		default:
			return "Could not deliver the redirect: " + err.Error()
		}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return ""
	}
	return fmt.Sprintf("Antigravity's sign-in listener answered %s instead of accepting the redirect.", resp.Status)
}

func agOutcome(signedIn bool, message string, retryable bool, credentialPath *string) *AntigravityLoginOutcome {
	out := &AntigravityLoginOutcome{SignedIn: signedIn, Retryable: retryable, CredentialPath: credentialPath}
	if message != "" {
		out.Message = &message
	}
	return out
}

// antigravityCredentialPath names the file this sign-in will produce, except
// when that would be a guess (macOS keychain storage, or an unresolvable home).
func antigravityCredentialPath(runtimeEnv map[string]string, methodID string) *string {
	if runtime.GOOS == "darwin" {
		force := strings.ToLower(strings.TrimSpace(runtimeEnv["AGY_ACP_FORCE_FILE_STORAGE"]))
		if force != "1" && force != "true" && force != "yes" {
			return nil
		}
	}
	dir, err := antigravityACPdirForEnv(runtimeEnv)
	if err != nil {
		return nil
	}
	name := "acp_token.json"
	if methodID == "oauth-business" {
		name = "acp_business_token.json"
	}
	path := filepath.Join(dir, name)
	return &path
}

func newAgLoginHandle() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
