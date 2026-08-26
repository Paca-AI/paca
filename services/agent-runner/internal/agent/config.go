package agent

import "github.com/google/uuid"

// Config is the subset of agentdom.Agent this service needs to run an
// llm-type conversation — agent-runner never handles acp-type agents (those
// stay on apps/acp-bridge), so ACPProvider/ACPCommand and friends are
// intentionally omitted here.
type Config struct {
	ID     uuid.UUID
	Name   string
	Handle string

	LLMProvider string
	LLMModel    string
	// LLMAPIKeySecret is the encrypted value stored in agents.llm_api_key_secret
	// — decrypt with secret.Encryptor before injecting into a container. Never
	// log this field.
	LLMAPIKeySecret string
	LLMBaseURL      string

	SystemPrompt      string
	MaxIterations     int
	TimeoutMinutes    int
	GitCommitterName  string
	GitCommitterEmail string
	// DockerEnabled opts this agent into the per-conversation Docker-in-
	// Docker sandbox sidecar (see internal/sandbox/docker/dind.go and
	// internal/sandbox/k8s/dind.go) — off by default, since most agents
	// never run a Docker command and the sidecar is real per-session
	// latency and resource cost to pay unconditionally.
	DockerEnabled bool

	MCPServers []MCPServer
	Skills     []Skill
	EnvVars    []EnvVar
}

// MCPServer mirrors agentdom.AgentMCPServer.
type MCPServer struct {
	ServerName string
	Transport  string // stdio | sse | http
	Command    string
	Args       []string
	URL        string
	Env        map[string]string
	IsEnabled  bool
}

// Skill mirrors agentdom.AgentSkill. executor.splitSkills decides per-skill
// whether it's written to the sandbox as a real SKILL.md for Goose's own
// skill discovery/`load_skill` tool, or (the `paca` root skill, plus any
// skill whose content isn't already a well-formed SKILL.md) folded directly
// into the initial prompt — see executor/skills.go's package doc comment.
// Trigger-based (keyword-activated) skills have no direct Goose equivalent
// — Triggers is unused for now, since Goose's own discovery already
// provides on-demand loading gated on the model's own judgment of
// relevance, which supersedes what Triggers was meant to eventually do.
type Skill struct {
	SkillName    string
	SkillContent string
	Triggers     []string
	IsEnabled    bool
}

// EnvVar mirrors agentdom.AgentEnvironmentVariable — injected into the
// sandbox container at run time, decrypted the same way as LLMAPIKeySecret.
type EnvVar struct {
	Key            string
	EncryptedValue string
}
