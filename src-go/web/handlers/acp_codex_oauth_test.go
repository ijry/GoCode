package handlers_test

// codex_request_device_code / codex_poll_device_code coverage. The OpenAI auth
// server is stubbed with httptest and pointed at via handlers.CodexOAuthIssuer,
// so the device flow runs offline and deterministically.

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"yoyocode/src-go/web/handlers"
)

func makeCodexTestJWT(accountID string) string {
	payload := map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	}
	pj, _ := json.Marshal(payload)
	return "hdr." + base64.RawURLEncoding.EncodeToString(pj) + ".sig"
}

// useCodexOAuthIssuer points the handlers at a stub server for the test's scope.
func useCodexOAuthIssuer(t *testing.T, mux *http.ServeMux) string {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	orig := handlers.CodexOAuthIssuer
	handlers.CodexOAuthIssuer = srv.URL
	t.Cleanup(func() { handlers.CodexOAuthIssuer = orig })
	return srv.URL
}

func TestCodexRequestDeviceCodeWireShape(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/deviceauth/usercode", func(w http.ResponseWriter, r *http.Request) {
		// `usercode` (no underscore) + interval as a STRING — both quirks the
		// port must accept.
		_, _ = w.Write([]byte(`{"device_auth_id":"da1","usercode":"UC-123","interval":"7"}`))
	})
	base := useCodexOAuthIssuer(t, mux)

	app := newTestServer(t)
	code, out := call(t, app, "codex_request_device_code", nil)
	if code != 200 {
		t.Fatalf("status %d: %v", code, out)
	}
	if out["userCode"] != "UC-123" {
		t.Fatalf("userCode = %v (should accept usercode alias)", out["userCode"])
	}
	if out["deviceAuthId"] != "da1" {
		t.Fatalf("deviceAuthId = %v", out["deviceAuthId"])
	}
	if out["verificationUrl"] != base+"/codex/device" {
		t.Fatalf("verificationUrl = %v", out["verificationUrl"])
	}
	if iv, ok := out["interval"].(float64); !ok || iv != 7 {
		t.Fatalf("interval = %v (string '7' should parse to 7)", out["interval"])
	}
}

func TestCodexPollDeviceCodeSuccess(t *testing.T) {
	jwt := makeCodexTestJWT("acct-42")
	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/deviceauth/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"authorization_code":"ac","code_challenge":"cc","code_verifier":"cv"}`))
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		out, _ := json.Marshal(map[string]string{
			"id_token": jwt, "access_token": "at", "refresh_token": "rt",
		})
		_, _ = w.Write(out)
	})
	useCodexOAuthIssuer(t, mux)

	app := newTestServer(t)
	code, out := call(t, app, "codex_poll_device_code", map[string]any{
		"deviceAuthId": "da1", "userCode": "UC-123",
	})
	if code != 200 {
		t.Fatalf("status %d: %v", code, out)
	}
	if out["status"] != "success" {
		t.Fatalf("status = %v", out["status"])
	}
	if out["accountId"] != "acct-42" {
		t.Fatalf("accountId = %v (should be extracted from id_token JWT)", out["accountId"])
	}
	if out["accessToken"] != "at" || out["refreshToken"] != "rt" || out["idToken"] != jwt {
		t.Fatalf("tokens wrong: %v", out)
	}
}

func TestCodexPollPending(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/deviceauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // not ready → 4xx
	})
	useCodexOAuthIssuer(t, mux)

	app := newTestServer(t)
	code, out := call(t, app, "codex_poll_device_code", map[string]any{
		"deviceAuthId": "da1", "userCode": "UC-123",
	})
	if code != 200 {
		t.Fatalf("status %d (pending should still be 200): %v", code, out)
	}
	if out["status"] != "pending" {
		t.Fatalf("status = %v (want pending)", out["status"])
	}
	if out["idToken"] != nil {
		t.Fatalf("pending must carry null tokens: %v", out)
	}
}

func TestCodexPollTokenExchangeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/deviceauth/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"authorization_code":"ac","code_verifier":"cv"}`))
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	useCodexOAuthIssuer(t, mux)

	app := newTestServer(t)
	code, out := call(t, app, "codex_poll_device_code", map[string]any{
		"deviceAuthId": "da1", "userCode": "UC-123",
	})
	if code != 200 {
		t.Fatalf("status %d (soft error should be 200): %v", code, out)
	}
	if out["status"] != "error" {
		t.Fatalf("status = %v (want error)", out["status"])
	}
	if out["message"] == nil || out["message"] == "" {
		t.Fatalf("error should carry a message: %v", out)
	}
}

func TestCodexRequestDeviceCodeUpstreamError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/accounts/deviceauth/usercode", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	})
	useCodexOAuthIssuer(t, mux)

	app := newTestServer(t)
	code, _ := call(t, app, "codex_request_device_code", nil)
	if code == 200 {
		t.Fatalf("upstream 500 should surface as an error, got 200")
	}
}
