package acp

// api_retry.go surfaces an in-flight API retry ("rate limited · HTTP 429 ·
// attempt 2/5 · next in 3.0s"). claude-agent-acp mirrors raw SDK messages on
// the `_claude/sdkMessage` notification; a `system`/`api_retry` message there is
// the only one this build cares about (upstream does the same and drops the
// rest). The reading is transient: it clears as soon as the turn makes real
// progress or ends.

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// ApiRetryState is one in-flight API retry (upstream ClaudeApiRetryState).
// Numeric fields are pointers so an absent value stays distinguishable from 0.
type ApiRetryState struct {
	Attempt      *int   `json:"attempt,omitempty"`
	MaxRetries   *int   `json:"max_retries,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorStatus  *int   `json:"error_status,omitempty"`
	RetryDelayMs *int   `json:"retry_delay_ms,omitempty"`
}

// parseApiRetry reads an `_claude/sdkMessage` notification's params and returns
// the retry state when its `message` is a `system`/`api_retry` record.
func parseApiRetry(params json.RawMessage) (*ApiRetryState, bool) {
	// `_claude/sdkMessage` fires for EVERY SDK message, so skip a full unmarshal
	// of (often large) assistant payloads that cannot be a retry.
	if !bytes.Contains(params, []byte(`"api_retry"`)) {
		return nil, false
	}
	var p struct {
		Message map[string]any `json:"message"`
	}
	if json.Unmarshal(params, &p) != nil || p.Message == nil {
		return nil, false
	}
	if retryStr(p.Message["type"]) != "system" || retryStr(p.Message["subtype"]) != "api_retry" {
		return nil, false
	}
	return &ApiRetryState{
		Attempt:      retryIntPtr(p.Message["attempt"]),
		MaxRetries:   retryIntPtr(p.Message["max_retries"]),
		Error:        strings.TrimSpace(retryStr(p.Message["error"])),
		ErrorStatus:  retryIntPtr(p.Message["error_status"]),
		RetryDelayMs: retryIntPtr(p.Message["retry_delay_ms"]),
	}, true
}

// retryStr coerces a decoded JSON value to a string ("" when it is not one).
func retryStr(v any) string {
	s, _ := v.(string)
	return s
}

// retryIntPtr coerces a decoded JSON value to *int, accepting a number or a
// numeric string (the SDK has been seen to send either).
func retryIntPtr(v any) *int {
	switch n := v.(type) {
	case float64:
		i := int(n)
		return &i
	case int:
		i := n
		return &i
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			i := int(f)
			return &i
		}
	}
	return nil
}
