package acp

import "os/exec"

// PreflightReport is one agent's preflight verdict (acp_preflight wire shape).
type PreflightReport struct {
	AgentType   string `json:"agent_type"`
	Available   bool   `json:"available"`
	Installable bool   `json:"installable"`
	Reason      string `json:"reason"`
}

// DetectAgent reports whether agentType can actually be launched on this machine
// (available) and whether the binary cache can fetch it (installable).
//
// npx/uvx agents are available only when their runner is on PATH; binary agents
// are available when the cached download or a system-installed copy resolves.
// Installable is true for binary-distributed agents that have a recipe on the
// current platform (npx/uvx agents install through their own package manager,
// so they are not "installable" through this flow).
func DetectAgent(agentType string) (available, installable bool, reason string) {
	spec, ok := LaunchSpecFor(agentType)
	if !ok {
		return false, false, "unknown agent"
	}
	_, installable = BinaryDistFor(agentType)
	switch spec.Kind {
	case "npx":
		if _, err := exec.LookPath("npx"); err == nil {
			available = true
		} else {
			reason = "Node.js / npx not found on PATH"
		}
	case "uvx":
		if _, err := exec.LookPath("uvx"); err == nil {
			available = true
		} else {
			reason = "uvx (Python) not found on PATH"
		}
	case "binary":
		if _, ok := ResolveSpawn(agentType, "", nil); ok {
			available = true
		} else {
			reason = "binary agent not installed"
		}
	default:
		reason = "unsupported distribution: " + spec.Kind
	}
	return available, installable, reason
}

// Preflight runs DetectAgent over the built-in catalog.
func Preflight() []PreflightReport {
	out := make([]PreflightReport, 0, len(BuiltinAgents))
	for _, a := range BuiltinAgents {
		av, inst, reason := DetectAgent(a.AgentType)
		out = append(out, PreflightReport{
			AgentType:   a.AgentType,
			Available:   av,
			Installable: inst,
			Reason:      reason,
		})
	}
	return out
}
