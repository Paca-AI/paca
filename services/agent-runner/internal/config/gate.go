package config

import "github.com/google/uuid"

// Gate answers "does this process own execution for this agent?" —
// originally the Go side of the migration plan's "gated per-agent or
// per-project" rollout against the now-removed Python services/ai-agent,
// back when both services read every message on paca:agent:triggers via
// independent consumer groups and something had to stop them from
// double-executing the same trigger.
//
// agent-runner is the only consumer of paca:agent:triggers now, so Gate no
// longer coordinates between two services — multiple agent-runner replicas
// in the *same* consumer group already load-balance correctly without it
// (Valkey Streams' own guarantee). What's left is a plain admin override:
// AllowedAgentIDs can restrict execution to a specific set of agents (e.g.
// while validating a config change), with "*" — the default — meaning
// every agent.
type Gate struct {
	allowAll bool
	allowed  map[string]bool
}

// NewGate builds a Gate from config.Settings.AllowedAgentIDs. The literal
// value "*" allows every agent — intended for local development or a
// deployment that has already fully cut over, not as a default.
func NewGate(agentIDs []string) Gate {
	g := Gate{allowed: make(map[string]bool, len(agentIDs))}
	for _, id := range agentIDs {
		if id == "*" {
			g.allowAll = true
			return g
		}
		g.allowed[id] = true
	}
	return g
}

// Allowed reports whether this replica is allowed to act on agentID.
func (g Gate) Allowed(agentID uuid.UUID) bool {
	return g.allowAll || g.allowed[agentID.String()]
}
