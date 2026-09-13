package agent

import "github.com/google/uuid"

// AgentScopeGlobal is the Config.AgentScope value for a global agent (mirrors
// services/api's agentdom.AgentScopeGlobal — see Config.AgentScope's doc
// comment).
const AgentScopeGlobal = "global"

// AgentType values this service actually executes — mirrors services/api's
// agentdom.AgentType* constants (agent-runner is a separate Go module and
// can't import that package directly, same reason ContextItemRef keeps its
// own byte-identical copy — see that type's doc comment). agent_type=acp
// has no constant here at all: those agents stay entirely on
// apps/acp-bridge and never reach this service (see
// postgres.AgentRepository.FindByID's ErrNotLLMAgent gate).
const (
	AgentTypeLLM         = "llm"
	AgentTypeProviderCLI = "provider_cli"
)

// CLIAuthMode values — mirrors services/api's agentdom.CLIAuthMode*
// constants. Only meaningful when Config.AgentType == AgentTypeProviderCLI.
const (
	CLIAuthModeAPIKey = "api_key"
	CLIAuthModeLogin  = "login"
)

// Config is the subset of agentdom.Agent this service needs to run an
// llm-type or provider_cli-type conversation — both execute through the
// exact same goose-serve-over-ACP path (buildAgentContainerEnv branches on
// AgentType for the rest). agent-runner never handles acp-type agents
// (those stay on apps/acp-bridge), so ACPProvider/ACPCommand and friends
// are intentionally omitted here.
type Config struct {
	ID     uuid.UUID
	Name   string
	Handle string
	// AgentScope is "project" or "global" (agents.agent_scope). Only a
	// global agent may legitimately be attributed to a human actor — see
	// buildMCPServers's use of this field and services/api's
	// verifyAgentIdentity, which enforces the same rule server-side
	// ("a project-scoped agent's actions are attributed via its
	// project_members.id, never a raw actor_user_id").
	AgentScope string
	// AgentType is "llm" or "provider_cli" (see the AgentType* consts
	// above) — branches buildAgentContainerEnv and coldStartEnvironment's
	// skills/MCP-sync step between Goose's own native config (llm) and a
	// provider_cli agent's underlying CLI's own config files (see
	// internal/executor/providercli).
	AgentType string

	LLMProvider string
	LLMModel    string
	// LLMAPIKeySecret is the encrypted value stored in agents.llm_api_key_secret
	// — decrypt with secret.Encryptor before injecting into a container. Never
	// log this field. LLM-only.
	LLMAPIKeySecret string
	LLMBaseURL      string

	// CLIProvider is one of claude-code | codex | cursor-agent | gemini-cli
	// — set only when AgentType == AgentTypeProviderCLI. Passed through as
	// GOOSE_PROVIDER (see executor/provider.go's resolveCLIProviderEnv).
	CLIProvider string
	// CLIModel is passed through as GOOSE_MODEL when set — provider-
	// specific free text (e.g. "sonnet"/"haiku" for claude-code).
	CLIModel string
	// CLIAuthMode is "api_key" or "login" (see the CLIAuthMode* consts
	// above). Goose itself never brokers auth for a CLI provider — this
	// only controls whether buildProviderCLIContainerEnv injects
	// CLIAPIKeySecret under the CLI's own native auth env var.
	CLIAuthMode string
	// CLIAPIKeySecret is the encrypted value stored in
	// agents.cli_api_key_secret — decrypt like LLMAPIKeySecret. Empty when
	// CLIAuthMode is "login". Never log this field.
	CLIAPIKeySecret string

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

	// MCPServers/Skills are read from the exact same agent_mcp_servers/
	// agent_skills rows for both llm and provider_cli agents — only the
	// *consumer* differs at execution time: an llm agent's are written into
	// Goose's own .agents/skills discovery + ACP session/new MCP list
	// (buildMCPServers/buildSkillsTar); a provider_cli agent's are instead
	// synced into its underlying CLI's own config files on every
	// conversation attach (see internal/executor/providercli), since Goose
	// ignores its own extension/skill config entirely once a CLI provider
	// is active.
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
