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

// Skill mirrors agentdom.AgentSkill. Trigger-based (keyword-activated)
// skills have no direct Goose equivalent yet, so for now every enabled
// skill's content is folded into the initial prompt unconditionally,
// regardless of Triggers, rather than silently dropped. Revisit once the
// slash-command prototype lands.
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
