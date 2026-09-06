package agentdom

import "errors"

// Agent errors
var (
	ErrAgentNotFound      = errors.New("agent not found")
	ErrAgentHandleTaken   = errors.New("agent handle already in use")
	ErrAgentHandleInvalid = errors.New("agent handle is invalid")
	ErrAgentNameInvalid   = errors.New("agent name is empty or invalid")
	ErrAgentTypeInvalid   = errors.New("agent_type must be one of: llm, acp, provider_cli")
	ErrACPProviderInvalid = errors.New("acp_provider must be one of: claude-code, codex, gemini-cli, goose, custom")
	ErrACPCommandRequired = errors.New("acp_command is required when acp_provider is custom")
	// ErrNotSupportedForACPAgent is returned when a caller tries to manage
	// an MCP server, skill, or environment variable on an ACP-type agent.
	// ACP agents run entirely in the user's own local CLI via the
	// paca-acp-bridge daemon — Paca never forwards any of that
	// configuration into the ACP conversation (services/ai-agent's
	// acp_dispatch.py never reads agent_mcp_servers/agent_skills/
	// agent_environment_variables at all) — so accepting these calls for
	// an ACP agent would silently no-op rather than do anything.
	ErrNotSupportedForACPAgent = errors.New("not supported for ACP-type agents — the local ACP client owns its own tool/MCP/skill/environment configuration")
	// ErrDefaultEnvironmentInvalid is returned when default_environment_id
	// doesn't resolve to a static environment (environmentdom.Environment)
	// belonging to this agent's own project, or is set on a global-scope
	// agent — see Agent.DefaultEnvironmentID's doc comment for why a global
	// agent can never have one.
	ErrDefaultEnvironmentInvalid = errors.New("default environment must be a static environment belonging to this agent's own project")
	// ErrDefaultFolderInvalid is returned when default_folder_id doesn't
	// resolve to a folder belonging to this agent's own
	// default_environment_id, is set without a default_environment_id also
	// set, or is set on a global-scope agent — see
	// Agent.DefaultFolderID's doc comment.
	ErrDefaultFolderInvalid = errors.New("default folder must belong to this agent's own default environment")
	// ErrParallelismLimitRequiresIsolatedSandbox is returned when
	// parallelism_limit > 1 is requested for an agent that can't safely run
	// more than one conversation at once — see
	// agentsvc.requiresSerialDispatch's doc comment for the two cases this
	// covers: an ACP-type agent (apps/acp-bridge's own Runner rejects a
	// second concurrent turn sharing its session key rather than queueing
	// it) and any agent (LLM or provider_cli) attached to a static
	// DefaultEnvironmentID (its filesystem is shared across every
	// conversation attached to it, unlike the default ephemeral
	// per-conversation sandbox).
	ErrParallelismLimitRequiresIsolatedSandbox = errors.New("parallelism_limit greater than 1 is only supported for agents using an ephemeral per-conversation sandbox — not ACP-type agents or agents attached to a shared default environment")
)

// Provider-CLI agent errors
var (
	ErrCLIProviderInvalid = errors.New("cli_provider must be one of: claude-code, codex, cursor-agent, gemini-cli")
	ErrCLIAuthModeInvalid = errors.New("cli_auth_mode must be one of: api_key, login")
	// ErrCLIProviderNoAPIKeyAuth is returned when cli_auth_mode=api_key is
	// requested for a cli_provider with no known non-interactive API-key
	// auth path — see agentdom.CLIProvidersWithAPIKeyAuth.
	ErrCLIProviderNoAPIKeyAuth = errors.New("this CLI provider does not support api_key auth mode — use login mode and log in via the environment terminal instead")
	// ErrDefaultEnvironmentRequiredForCLIProvider is returned when a
	// provider_cli agent is created/updated, or a conversation started for
	// one, without a default_environment_id resolving to a real
	// environment — a provider_cli agent's CLI login state must persist
	// across conversations, which only a static environment's persistent
	// volume provides; it never falls back to an ephemeral sandbox.
	ErrDefaultEnvironmentRequiredForCLIProvider = errors.New("provider_cli agents require a default_environment_id — CLI login state must persist across conversations")
	// ErrCLIProviderNotSupportedForGlobalAgents is returned when
	// agent_type=provider_cli is requested for a global-scope agent — see
	// Agent.DefaultEnvironmentID's doc comment on why a global agent has no
	// single project's environments to default to.
	ErrCLIProviderNotSupportedForGlobalAgents = errors.New("provider_cli agents are not supported for global-scope agents")
	// ErrAgentNotProviderCLI is returned by VerifyCLILogin for any other
	// agent_type.
	ErrAgentNotProviderCLI = errors.New("agent is not a provider_cli-type agent")
)

// Global agent errors
var (
	// ErrAgentNotGlobal is returned when a global-agent-only operation
	// (admin CRUD, global chat) targets a project-scoped agent.
	ErrAgentNotGlobal = errors.New("agent is not a global agent")
)

// MCP server errors
var (
	ErrMCPServerNotFound        = errors.New("MCP server not found")
	ErrMCPServerNameTaken       = errors.New("MCP server name already in use on this agent")
	ErrMCPServerCommandRequired = errors.New("command is required for stdio transport")
)

// Skill errors
var (
	ErrSkillNotFound     = errors.New("skill not found")
	ErrSkillNameTaken    = errors.New("skill name already in use on this agent")
	ErrSkillNameReserved = errors.New("skill name is reserved for internal agent scaffolding")
	// ErrSkillNameInvalid is returned for a skill name that would let the
	// on-disk SKILL.md path built from it (executor/skills.go's
	// buildSkillsTar, and providercli's claude_code.go SyncFiles) escape
	// the skills directory it's meant to land in — neither writer
	// sanitizes the name itself, so this is enforced once, here, at the
	// one place every skill name passes through before either ever sees
	// it.
	ErrSkillNameInvalid = errors.New("skill name must not be empty, \".\", \"..\", or contain \"/\" or \"\\\"")
)

// Environment variable errors
var (
	ErrEnvVarNotFound    = errors.New("environment variable not found")
	ErrEnvVarKeyTaken    = errors.New("environment variable key already in use on this agent")
	ErrEnvVarKeyInvalid  = errors.New("environment variable key must start with a letter or underscore and contain only letters, digits, and underscores")
	ErrEnvVarKeyReserved = errors.New("environment variable key is reserved for internal sandbox configuration")
)

// Conversation errors
var (
	ErrConversationNotFound       = errors.New("conversation not found")
	ErrConversationNotRunning     = errors.New("conversation is not running")
	ErrConversationAlreadyStopped = errors.New("conversation is already stopped")
	// ErrConversationBusy is returned when a chat reply is sent while the
	// session's current conversation is still mid-turn (status "running").
	ErrConversationBusy = errors.New("agent is still responding to the previous message")
	// ErrConversationInvalidCursor is returned when a client-supplied
	// pagination cursor fails to decode.
	ErrConversationInvalidCursor = errors.New("invalid pagination cursor")
	// ErrConversationEventInvalidCursor is returned when a client-supplied
	// conversation-events window cursor (after/before) fails to decode.
	ErrConversationEventInvalidCursor = errors.New("invalid pagination cursor")
)

// Chat session errors
var (
	ErrChatSessionNotFound = errors.New("chat session not found")
)

// Activity feed errors
var (
	// ErrActivityFeedInvalidCursor is returned when a client-supplied
	// activity feed pagination cursor fails to decode.
	ErrActivityFeedInvalidCursor = errors.New("invalid pagination cursor")
)
