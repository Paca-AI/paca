package agentdom

import "errors"

// Agent errors
var (
	ErrAgentNotFound      = errors.New("agent not found")
	ErrAgentHandleTaken   = errors.New("agent handle already in use")
	ErrAgentHandleInvalid = errors.New("agent handle is invalid")
	ErrAgentNameInvalid   = errors.New("agent name is empty or invalid")
	ErrAgentTypeInvalid   = errors.New("agent_type must be one of: llm, acp")
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
