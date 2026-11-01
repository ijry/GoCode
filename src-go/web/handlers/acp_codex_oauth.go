package handlers

// codex RequestDeviceCode / PollDeviceCode OAuth (upstream commands/acp.rs
// codex_request_device_code_core / codex_poll_device_code_core). A React-free
// port of the OpenAI device-authorization flow against auth.openai.com:
//
//   request → POST /api/accounts/deviceauth/usercode  → {user_code, device_auth_id, interval}
//   poll    → POST /api/accounts/deviceauth/token      → authorization_code+verifier
//             POST /oauth/token (form)                 → {id_token, access_token, refresh_token}
//
// Wire shapes are camelCase (CodexDeviceCodeResponse / CodexDeviceCodePollResult).
// Stateless — nothing is persisted here.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gocode/src-go/web"
)

const (
	codexOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	codexOAuthTimeout  = 30 * time.Second
)

// CodexOAuthIssuer is the OAuth issuer base URL. Overridable (test hook) so the
// device flow can run against a stub server.
var CodexOAuthIssuer = "https://auth.openai.com"

var codexOAuthHTTPClient = &http.Client{Timeout: codexOAuthTimeout}

// RegisterACPCodexOAuth wires codex_request_device_code / codex_poll_device_code.
func RegisterACPCodexOAuth(reg *Registry) {
	reg.Register("codex_request_device_code", func(c *web.Context) (any, error) {
		return codexRequestDeviceCode()
	})
	reg.Register("codex_poll_device_code", func(c *web.Context) (any, error) {
		deviceAuthID := strVal(c.Body["deviceAuthId"], c.Body["device_auth_id"])
		userCode := strVal(c.Body["userCode"], c.Body["user_code"])
		return codexPollDeviceCode(deviceAuthID, userCode)
	})
}

type codexDeviceCodeResponse struct {
	UserCode        string `json:"userCode"`
	VerificationURL string `json:"verificationUrl"`
	DeviceAuthID    string `json:"deviceAuthId"`
	Interval        int    `json:"interval"`
}

// codexDeviceCodePollResult uses pointers so a nil field marshals to JSON null
// (pending/error states), matching upstream's Option<…>.
type codexDeviceCodePollResult struct {
	Status       string  `json:"status"`
	Message      *string `json:"message"`
	IDToken      *string `json:"idToken"`
	AccessToken  *string `json:"accessToken"`
	RefreshToken *string `json:"refreshToken"`
	AccountID    *string `json:"accountId"`
}

func codexPollPending() codexDeviceCodePollResult {
	return codexDeviceCodePollResult{Status: "pending"}
}

func codexRequestDeviceCode() (codexDeviceCodeResponse, error) {
	body, _ := json.Marshal(map[string]any{"client_id": codexOAuthClientID})
	resp, err := codexOAuthHTTPClient.Post(
		CodexOAuthIssuer+"/api/accounts/deviceauth/usercode",
		"application/json", bytes.NewReader(body),
	)
	if err != nil {
		return codexDeviceCodeResponse{}, web.ErrTaskExecution("device code request failed: " + err.Error())
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return codexDeviceCodeResponse{}, web.ErrTaskExecution(
			fmt.Sprintf("device code request returned %d: %s", resp.StatusCode, string(raw)))
	}

	// Parse into a map: `user_code` may arrive as `usercode`, and `interval` is
	// a number OR a numeric string, defaulting to 5 seconds when absent.
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return codexDeviceCodeResponse{}, web.ErrTaskExecution(
			"parse device code response failed: " + err.Error() + " | body: " + string(raw))
	}
	uc := strVal(doc["user_code"], doc["usercode"])
	interval := parseCodexInterval(doc["interval"])
	return codexDeviceCodeResponse{
		UserCode:        uc,
		VerificationURL: CodexOAuthIssuer + "/codex/device",
		DeviceAuthID:    strVal(doc["device_auth_id"]),
		Interval:        interval,
	}, nil
}

func codexPollDeviceCode(deviceAuthID, userCode string) (codexDeviceCodePollResult, error) {
	pollBody, _ := json.Marshal(map[string]any{
		"device_auth_id": deviceAuthID,
		"user_code":      userCode,
	})
	resp, err := codexOAuthHTTPClient.Post(
		CodexOAuthIssuer+"/api/accounts/deviceauth/token",
		"application/json", bytes.NewReader(pollBody),
	)
	if err != nil {
		return codexDeviceCodePollResult{}, web.ErrTaskExecution("device code poll failed: " + err.Error())
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Not ready yet — upstream reports "pending" (no error).
		return codexPollPending(), nil
	}

	var codeResp struct {
		AuthorizationCode string `json:"authorization_code"`
		CodeVerifier      string `json:"code_verifier"`
	}
	if err := json.Unmarshal(raw, &codeResp); err != nil {
		return codexDeviceCodePollResult{}, web.ErrTaskExecution(
			"parse poll response failed: " + err.Error() + " | body: " + string(raw))
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", codeResp.AuthorizationCode)
	form.Set("redirect_uri", CodexOAuthIssuer+"/deviceauth/callback")
	form.Set("client_id", codexOAuthClientID)
	form.Set("code_verifier", codeResp.CodeVerifier)
	tresp, err := codexOAuthHTTPClient.Post(
		CodexOAuthIssuer+"/oauth/token",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()),
	)
	if err != nil {
		return codexDeviceCodePollResult{}, web.ErrTaskExecution("token exchange failed: " + err.Error())
	}
	traw, _ := io.ReadAll(tresp.Body)
	tresp.Body.Close()
	if tresp.StatusCode < 200 || tresp.StatusCode >= 300 {
		msg := fmt.Sprintf("token exchange returned %d: %s", tresp.StatusCode, string(traw))
		return codexDeviceCodePollResult{Status: "error", Message: &msg}, nil
	}

	var tok struct {
		IDToken      string `json:"id_token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(traw, &tok); err != nil {
		return codexDeviceCodePollResult{}, web.ErrTaskExecution("parse token response failed: " + err.Error())
	}
	acct := extractJwtAccountID(tok.IDToken)
	return codexDeviceCodePollResult{
		Status:       "success",
		IDToken:      &tok.IDToken,
		AccessToken:  &tok.AccessToken,
		RefreshToken: &tok.RefreshToken,
		AccountID:    &acct,
	}, nil
}

// extractJwtAccountID reads chatgpt_account_id from a JWT's second segment,
// under the "https://api.openai.com/auth" claim. Returns "" on any failure
// (matching upstream's unwrap_or_default()).
func extractJwtAccountID(jwt string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return ""
	}
	payload := parts[1]
	var decoded []byte
	var err error
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	} {
		if decoded, err = enc.DecodeString(payload); err == nil {
			break
		}
	}
	if decoded == nil {
		return ""
	}
	var v struct {
		Auth map[string]any `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(decoded, &v) != nil {
		return ""
	}
	if s, ok := v.Auth["chatgpt_account_id"].(string); ok {
		return s
	}
	return ""
}

func parseCodexInterval(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i
		}
	}
	return 5 // default_interval
}
