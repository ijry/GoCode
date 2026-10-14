package handlers

// Server-mode reverse proxy for `officecli watch` preview servers — the Go port
// of upstream web/handlers/office_watch_proxy.rs.
//
// In standalone / Docker deployments the browser can't reach the server's
// loopback, so the office preview iframe loads
// /api/office-watch-proxy/{port}/?cap=... and we forward it to
// http://127.0.0.1:{port}/... . Desktop skips this (the native window loads the
// loopback URL directly).
//
// Path-rewriting shim (the crux): officecli's watch page is a single
// self-contained HTML whose inline JS hits root-absolute endpoints
// (EventSource('/events'), fetch('/api/edit'), fetch('/')). Loaded inside an
// iframe those resolve against the app origin, bypassing the proxy, so the
// live-refresh SSE never connects. We inject a tiny JS shim at the top of
// <head> that re-prefixes fetch/EventSource/XMLHttpRequest with
// /api/office-watch-proxy/{port} and carries the capability.
//
// Auth + SSRF: an iframe navigation can't carry a Bearer header, so this route
// is unauthenticated (RegisterAPIPrefix) and self-validates `?cap=` against
// officewatch.ValidateWatchCap — a per-watch capability minted by the
// Bearer-authed start_office_watch, which also gates {port} against the SSRF
// whitelist (only ports we bound to a watch). The global token never enters the
// iframe.
//
// CORS: the router's ServeHTTP already stamps `Access-Control-Allow-Origin: *`
// on every response and short-circuits OPTIONS, so we deliberately add no CORS
// headers here (a second one would be rejected by browsers).

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"yoyocode/src-go/officewatch"
)

// OfficeWatchProxyPrefix is the public route prefix for the preview proxy.
const OfficeWatchProxyPrefix = "/api/office-watch-proxy/"

// RegisterOfficeWatchProxy wires the 3 proxy routes (upstream proxy_root ×2 and
// proxy) under one public prefix handler. The literal prefix is kept in the
// call (not the const) so scripts/gen_inventory.js can attribute the route.
func RegisterOfficeWatchProxy(reg *Registry) {
	reg.RegisterAPIPrefix("/api/office-watch-proxy/", officeWatchProxyHandle)
}

// officeWatchProxyClient forwards to loopback only: no system proxy, a bounded
// TCP connect, and NO overall timeout — the watch refresh channel is a
// long-lived SSE stream that an overall timeout would sever.
var officeWatchProxyClient = &http.Client{
	Transport: &http.Transport{
		Proxy:       nil, // never route through a system proxy
		DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
	},
}

// officeWatchProxyHandle parses /api/office-watch-proxy/{port}[/{rest}] and
// forwards to the watch's loopback server.
func officeWatchProxyHandle(w http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(req.URL.Path, OfficeWatchProxyPrefix)
	portStr, tail := rest, ""
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		portStr, tail = rest[:i], rest[i+1:]
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		http.Error(w, "invalid watch port", http.StatusBadRequest)
		return
	}
	proxyInner(w, req, port, tail)
}

func proxyInner(w http.ResponseWriter, req *http.Request, port int, rest string) {
	raw := req.URL.RawQuery

	// 1. AUTH + SSRF — the request must name a port we bound to a watch and
	//    carry that watch's exact capability. An unknown port or wrong cap
	//    fails closed.
	cap, _ := queryParam(raw, "cap")
	if cap == "" || !officewatch.ValidateWatchCap(port, cap) {
		http.Error(w, "invalid capability", http.StatusUnauthorized)
		return
	}
	officewatch.NoteProxyRequest(port)

	// 2. Build the upstream URL, stripping `cap` so it never reaches officecli.
	upstream := "http://127.0.0.1:" + strconv.Itoa(port) + "/" + rest + queryWithout(raw, "cap")

	// 3. Forward method + curated request headers + body.
	var body io.Reader
	if req.Body != nil {
		body = req.Body
	}
	outReq, err := http.NewRequestWithContext(req.Context(), req.Method, upstream, body)
	if err != nil {
		http.Error(w, "watch upstream unreachable", http.StatusBadGateway)
		return
	}
	for name, vals := range req.Header {
		if !forwardRequestHeader(strings.ToLower(name)) {
			continue
		}
		for _, v := range vals {
			outReq.Header.Add(name, v)
		}
	}

	upResp, err := officeWatchProxyClient.Do(outReq)
	if err != nil {
		http.Error(w, "watch upstream unreachable", http.StatusBadGateway)
		return
	}
	defer upResp.Body.Close()

	ct := strings.ToLower(upResp.Header.Get("content-type"))
	isHTML := strings.Contains(ct, "text/html")
	isSSE := strings.Contains(ct, "text/event-stream")

	for name, vals := range upResp.Header {
		if !forwardResponseHeader(strings.ToLower(name)) {
			continue
		}
		for _, v := range vals {
			w.Header().Add(name, v)
		}
	}

	// 4a. HTML document → buffer, inject the path-rewriting shim, return fixed.
	//     The rewritten page carries the cap, so don't let an intermediary
	//     cache it.
	if isHTML {
		rawHTML, rerr := io.ReadAll(upResp.Body)
		if rerr != nil {
			http.Error(w, "watch upstream read error", http.StatusBadGateway)
			return
		}
		prefix := "/api/office-watch-proxy/" + strconv.Itoa(port)
		shim := strings.ReplaceAll(shimTemplate, "__PFX__", prefix)
		shim = strings.ReplaceAll(shim, "__CAP__", cap)
		html := injectShim(string(rawHTML), shim)
		w.Header().Set("cache-control", "no-store")
		w.WriteHeader(upResp.StatusCode)
		_, _ = io.WriteString(w, html)
		return
	}

	// 4b. Everything else (SSE /events, JSON /api/*) → stream transparently.
	w.Header().Set("x-accel-buffering", "no")
	if isSSE {
		// Hold an SSE lease for the life of this response. When the client
		// disconnects the copy loop returns → the deferred release runs,
		// letting the sweep reap an abandoned web preview even if no
		// stop_office_watch ever arrives.
		officewatch.AcquireSSELease(port)
		defer officewatch.ReleaseSSELease(port)
	}
	w.WriteHeader(upResp.StatusCode)
	streamCopy(w, upResp.Body, isSSE)
}

// streamCopy copies the upstream body to the client, flushing after each chunk
// for SSE so events arrive live.
func streamCopy(w http.ResponseWriter, src io.Reader, flush bool) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flush && flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

// ── pure helpers (ported 1:1 from office_watch_proxy.rs) ────────────────────

// shimTemplate is injected at the top of officecli's <head>. It patches the
// three URL entry points so every root-absolute request is re-prefixed through
// the proxy and carries the capability. __PFX__/__CAP__ are substituted per
// request (never formatted, to avoid escaping the JS braces). The rw() guard is
// idempotent so officecli's own fetch('/') re-fetch can't double-prefix.
const shimTemplate = `<script>(function(){
var P="__PFX__",C="__CAP__";
function rw(u){
  if(typeof u!=="string"||u.charAt(0)!=="/")return u;
  if(u===P||u.indexOf(P+"/")===0)return u;
  return P+u+(u.indexOf("?")>=0?"&":"?")+"cap="+C;
}
var of=window.fetch;
if(of)window.fetch=function(i,init){
  try{ if(typeof i==="string")i=rw(i); else if(i&&typeof i.url==="string")i=new Request(rw(i.url),i); }catch(e){}
  return of.call(this,i,init);
};
var OE=window.EventSource;
if(OE)window.EventSource=function(u,cfg){return new OE(rw(u),cfg);};
var xo=XMLHttpRequest.prototype.open;
XMLHttpRequest.prototype.open=function(m,u){ try{arguments[1]=rw(u);}catch(e){} return xo.apply(this,arguments); };
})();</script>`

// forwardRequestHeader is the downstream→upstream header whitelist. Deliberately
// excludes Host, Authorization, Cookie, and hop-by-hop headers. content-type is
// forwarded so officecli parses POST bodies (/api/edit, /api/selection).
func forwardRequestHeader(name string) bool {
	switch name {
	case "accept", "accept-language", "content-type", "range",
		"if-none-match", "if-modified-since":
		return true
	}
	return false
}

// forwardResponseHeader is the upstream→downstream header whitelist.
// content-length is intentionally dropped (the body is streamed, or rewritten
// for HTML).
func forwardResponseHeader(name string) bool {
	switch name {
	case "content-type", "cache-control", "etag", "last-modified", "expires":
		return true
	}
	return false
}

// queryParam extracts a single query param value (percent-decoded). The bool
// distinguishes "absent" from "present but empty", mirroring upstream's Option.
func queryParam(raw, key string) (string, bool) {
	for _, seg := range strings.Split(raw, "&") {
		if seg == "" {
			continue
		}
		kv := strings.SplitN(seg, "=", 2)
		if kv[0] != key {
			continue
		}
		rawVal := ""
		if len(kv) == 2 {
			rawVal = kv[1]
		}
		if dec, err := url.QueryUnescape(rawVal); err == nil {
			return dec, true
		}
		return rawVal, true
	}
	return "", false
}

// queryWithout re-serializes the query string with key removed, preserving the
// remaining segments verbatim (so officecli's own params pass through intact).
func queryWithout(raw, key string) string {
	kept := make([]string, 0, 4)
	for _, seg := range strings.Split(raw, "&") {
		if seg == "" {
			continue
		}
		if strings.SplitN(seg, "=", 2)[0] == key {
			continue
		}
		kept = append(kept, seg)
	}
	if len(kept) == 0 {
		return ""
	}
	return "?" + strings.Join(kept, "&")
}

// injectShim inserts the shim right after the opening <head ...> tag so it runs
// before officecli's inline scripts. Falls back to prepending if there's no
// <head>. Offsets come from an ASCII-lowercased copy, which preserves byte
// length and boundaries, so they index the original safely.
func injectShim(html, shim string) string {
	lower := asciiLower(html)
	if hs := strings.Index(lower, "<head"); hs >= 0 {
		if rel := strings.IndexByte(lower[hs:], '>'); rel >= 0 {
			at := hs + rel + 1
			return html[:at] + shim + html[at:]
		}
	}
	return shim + html
}

// asciiLower lowercases only ASCII A–Z, preserving byte length exactly (unlike
// strings.ToLower, whose Unicode folding could shift byte offsets on non-ASCII
// input and misindex the original HTML).
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
