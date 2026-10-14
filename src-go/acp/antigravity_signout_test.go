package acp

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// fakeAntigravityAgent wires an in-memory ACP agent for sign-out tests: it
// answers `initialize` (optionally advertising auth.logout) and records any
// `logout` request.
func fakeAntigravityAgent(t *testing.T, advertises bool, gotLogout *bool) func(SpawnConfig) (*AgentProcess, error) {
	t.Helper()
	return func(SpawnConfig) (*AgentProcess, error) {
		cr, aw := io.Pipe() // agent -> client
		ar, cw := io.Pipe() // client -> agent
		clientPeer := NewPeer(cr, cw)
		agentPeer := NewPeer(ar, aw)

		agentPeer.HandleRequest("initialize", func(json.RawMessage) (any, error) {
			auth := map[string]any{}
			if advertises {
				auth["logout"] = map[string]any{}
			}
			return map[string]any{
				"protocolVersion":    ACPProtocolVersion,
				"agentCapabilities":  map[string]any{"auth": auth},
			}, nil
		})
		agentPeer.HandleRequest("logout", func(json.RawMessage) (any, error) {
			if gotLogout != nil {
				*gotLogout = true
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

func withFakeAntigravitySpawn(t *testing.T, resolveOK bool, spawn func(SpawnConfig) (*AgentProcess, error)) {
	t.Helper()
	origResolve, origSpawn := antigravityResolveSpawn, antigravitySpawnAgent
	t.Cleanup(func() { antigravityResolveSpawn, antigravitySpawnAgent = origResolve, origSpawn })
	antigravityResolveSpawn = func(agentType, cwd string, env map[string]string) (SpawnConfig, bool) {
		if !resolveOK {
			return SpawnConfig{}, false
		}
		return SpawnConfig{Command: "antigravity-fake"}, true
	}
	if spawn != nil {
		antigravitySpawnAgent = spawn
	}
}

func TestAntigravitySignOutSendsLogout(t *testing.T) {
	loggedOut := false
	withFakeAntigravitySpawn(t, true, fakeAntigravityAgent(t, true, &loggedOut))

	if err := AntigravitySignOut(map[string]string{}); err != nil {
		t.Fatalf("sign out: %v", err)
	}
	if !loggedOut {
		t.Fatalf("logout request was not sent")
	}
}

func TestAntigravitySignOutRequiresCapability(t *testing.T) {
	loggedOut := false
	withFakeAntigravitySpawn(t, true, fakeAntigravityAgent(t, false, &loggedOut))

	err := AntigravitySignOut(map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "cannot sign out") {
		t.Fatalf("err = %v, want a 'cannot sign out' capability error", err)
	}
	if loggedOut {
		t.Fatalf("logout must not be sent when the agent does not advertise it")
	}
}

func TestAntigravitySignOutBinaryMissing(t *testing.T) {
	withFakeAntigravitySpawn(t, false, nil)
	err := AntigravitySignOut(map[string]string{})
	if err == nil {
		t.Fatalf("expected an error when the binary cannot be resolved")
	}
	if _, ok := err.(*AgentUnavailableError); !ok {
		t.Fatalf("err = %T, want *AgentUnavailableError", err)
	}
}

func TestAntigravityAdvertisesLogout(t *testing.T) {
	if !antigravityAdvertisesLogout(json.RawMessage(`{"agentCapabilities":{"auth":{"logout":{}}}}`)) {
		t.Fatalf("should advertise logout")
	}
	if antigravityAdvertisesLogout(json.RawMessage(`{"agentCapabilities":{"auth":{}}}`)) {
		t.Fatalf("missing logout should be false")
	}
	if antigravityAdvertisesLogout(json.RawMessage(`{"agentCapabilities":{"auth":{"logout":null}}}`)) {
		t.Fatalf("null logout should be false")
	}
	if antigravityAdvertisesLogout(json.RawMessage(`not json`)) {
		t.Fatalf("garbage should be false")
	}
}
