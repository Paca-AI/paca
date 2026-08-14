package agent

import "github.com/google/uuid"

// ACPConfig is an acp-type agent's dispatch configuration — a much smaller
// field set than Config (the llm-type equivalent): an ACP agent's "sandbox"
// is a WebSocket connection from a daemon running on the user's own
// machine, so there's no LLM provider, no MCP servers, no skills, no env
// vars to configure here — those are the user's own local responsibility.
// Mirrors services/ai-agent's AgentConfig fields actually read by
// acp_dispatch.py.
type ACPConfig struct {
	ID uuid.UUID
	// ProjectID is nil for a global-scope ACP agent — mirrors
	// find_agent_by_bridge_token_hash's (agent_id, project_id) return,
	// where project_id is None for the same case.
	ProjectID      *uuid.UUID
	ACPProvider    string
	ACPCommand     []string
	TimeoutMinutes int
}
