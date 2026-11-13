package agentdom

import (
	"context"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
)

// Service is the combined AI Agent service contract.
type Service interface {
	AgentService
	MCPServerService
	SkillService
	EnvVarService
	ConversationService
	ChatSessionService
}

// AgentService defines agent CRUD use cases.
type AgentService interface {
	// ListAgents returns one cursor-paginated page of the agents visible in the
	// project — see AgentRepository.ListAgents.
	ListAgents(ctx context.Context, projectID uuid.UUID, filter AgentFilter, cursorAfter *string, limit int) ([]*Agent, bool, error)
	// ListAllAgents returns every agent visible in the project, for server-side
	// jobs — see AgentRepository.ListAllAgents.
	ListAllAgents(ctx context.Context, projectID uuid.UUID, scope AgentScope) ([]*Agent, error)
	GetAgent(ctx context.Context, projectID, agentID uuid.UUID) (*Agent, error)
	CreateAgent(ctx context.Context, projectID uuid.UUID, in CreateAgentInput) (*Agent, error)
	UpdateAgent(ctx context.Context, projectID, agentID uuid.UUID, in UpdateAgentInput) (*Agent, error)
	DeleteAgent(ctx context.Context, projectID, agentID uuid.UUID) error
	TriggerDescriptionWrite(ctx context.Context, projectID, agentID, taskID, triggeredByMemberID uuid.UUID) (*AgentConversation, error)
	// GenerateACPBridgeToken issues a new local-bridge auth token for an
	// ACP-type agent, replacing any existing one, and returns the plaintext
	// once — only its SHA-256 hash is persisted.
	GenerateACPBridgeToken(ctx context.Context, projectID, agentID uuid.UUID) (plaintext string, err error)
	// GenerateAgentMCPKey issues a new MCP API key for an ACP-type agent,
	// replacing any existing one, and returns the plaintext once — only its
	// SHA-256 hash is persisted. The previous key stops authenticating the
	// moment this is called, so there is only ever one live key per agent
	// (same behavior as GenerateACPBridgeToken). Used in the agent's MCP
	// connect command so tool calls are attributed to the agent itself
	// instead of to whichever human generated the command.
	GenerateAgentMCPKey(ctx context.Context, projectID, agentID uuid.UUID) (plaintext string, err error)
	// VerifyCLILogin probes whether a provider_cli agent's CLI is currently
	// authenticated inside its default environment (a file-existence check
	// — see docs/ai-agent/overview.md's provider_cli section), and records
	// the result's timestamp on success via the repository's
	// SetCLILoginVerifiedAt. Returns ErrAgentNotProviderCLI for any other
	// agent_type.
	VerifyCLILogin(ctx context.Context, projectID, agentID uuid.UUID) (authenticated bool, err error)

	// InitiateAvatarUpload starts an avatar upload for a project-scoped agent.
	InitiateAvatarUpload(ctx context.Context, projectID, agentID uuid.UUID, fileName, contentType string, fileSize int64, uploadedBy uuid.UUID) (*attachmentdom.UploadSession, error)
	// CompleteAvatarUpload finishes an avatar upload for a project-scoped agent.
	CompleteAvatarUpload(ctx context.Context, projectID, agentID, fileID uuid.UUID) (*Agent, error)
	// RemoveAvatar clears a project-scoped agent's avatar.
	RemoveAvatar(ctx context.Context, projectID, agentID uuid.UUID) (*Agent, error)

	// -- Global agents (AgentScope == AgentScopeGlobal). See the Agent doc
	// comment. These never take a projectID: a global agent has none of its
	// own, and is attached to projects only indirectly via project_members
	// (see project.MemberService.AddMember's AgentID branch).

	ListGlobalAgents(ctx context.Context, filter AgentFilter, cursorAfter *string, limit int) ([]*Agent, bool, error)
	// ListAllGlobalAgents returns every global-scope agent, for server-side jobs.
	ListAllGlobalAgents(ctx context.Context) ([]*Agent, error)
	GetGlobalAgent(ctx context.Context, agentID uuid.UUID) (*Agent, error)
	CreateGlobalAgent(ctx context.Context, in CreateGlobalAgentInput) (*Agent, error)
	UpdateGlobalAgent(ctx context.Context, agentID uuid.UUID, in UpdateAgentInput) (*Agent, error)
	// DeleteGlobalAgent soft-deletes the agent and every project_members row
	// referencing it, across every project it was invited into.
	DeleteGlobalAgent(ctx context.Context, agentID uuid.UUID) error
	// ListInvitedProjectIDs returns the IDs of every project a global agent
	// currently has an active project_members row in. Used by the
	// GET /agents/me/projects self-service endpoint.
	ListInvitedProjectIDs(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error)
	// GenerateGlobalACPBridgeToken is GenerateACPBridgeToken's global-agent
	// sibling — same token generation, ownership verified via GetGlobalAgent
	// instead of a projectID match.
	GenerateGlobalACPBridgeToken(ctx context.Context, agentID uuid.UUID) (plaintext string, err error)
	// GenerateGlobalAgentMCPKey is GenerateAgentMCPKey's global-agent
	// sibling — ownership verified via GetGlobalAgent instead of a
	// projectID match.
	GenerateGlobalAgentMCPKey(ctx context.Context, agentID uuid.UUID) (plaintext string, err error)

	// InitiateGlobalAvatarUpload is InitiateAvatarUpload's global-agent sibling.
	InitiateGlobalAvatarUpload(ctx context.Context, agentID uuid.UUID, fileName, contentType string, fileSize int64, uploadedBy uuid.UUID) (*attachmentdom.UploadSession, error)
	// CompleteGlobalAvatarUpload is CompleteAvatarUpload's global-agent sibling.
	CompleteGlobalAvatarUpload(ctx context.Context, agentID, fileID uuid.UUID) (*Agent, error)
	// RemoveGlobalAvatar is RemoveAvatar's global-agent sibling.
	RemoveGlobalAvatar(ctx context.Context, agentID uuid.UUID) (*Agent, error)
}

// MCPServerService defines MCP server CRUD use cases.
type MCPServerService interface {
	ListMCPServers(ctx context.Context, agentID uuid.UUID) ([]*AgentMCPServer, error)
	AddMCPServer(ctx context.Context, agentID uuid.UUID, in AddMCPServerInput) (*AgentMCPServer, error)
	UpdateMCPServer(ctx context.Context, agentID, serverID uuid.UUID, in UpdateMCPServerInput) (*AgentMCPServer, error)
	DeleteMCPServer(ctx context.Context, agentID, serverID uuid.UUID) error
}

// SkillService defines skill CRUD use cases.
type SkillService interface {
	ListSkills(ctx context.Context, agentID uuid.UUID) ([]*AgentSkill, error)
	AddSkill(ctx context.Context, agentID uuid.UUID, in AddSkillInput) (*AgentSkill, error)
	UpdateSkill(ctx context.Context, agentID, skillID uuid.UUID, in UpdateSkillInput) (*AgentSkill, error)
	DeleteSkill(ctx context.Context, agentID, skillID uuid.UUID) error
}

// EnvVarService defines secret environment variable CRUD use cases.
type EnvVarService interface {
	ListEnvVars(ctx context.Context, agentID uuid.UUID) ([]*AgentEnvironmentVariable, error)
	AddEnvVar(ctx context.Context, agentID uuid.UUID, in AddEnvVarInput) (*AgentEnvironmentVariable, error)
	UpdateEnvVar(ctx context.Context, agentID, envVarID uuid.UUID, in UpdateEnvVarInput) (*AgentEnvironmentVariable, error)
	DeleteEnvVar(ctx context.Context, agentID, envVarID uuid.UUID) error
}

// ConversationService defines conversation management use cases.
type ConversationService interface {
	ListConversations(ctx context.Context, in ListConversationsFilter, limit int) (convs []*AgentConversation, hasMore bool, err error)
	// GetConversation returns a single project conversation after verifying
	// it belongs to projectID and that memberID may read it (project-shared
	// conversations are readable by any project member; owner-private ones
	// only by their chat-session owner).
	GetConversation(ctx context.Context, projectID, conversationID, memberID uuid.UUID) (*AgentConversation, error)
	// GetConversationForAgent returns a conversation for an agent-authenticated
	// caller — used by the read_conversation MCP tool. callerAgentID is the
	// agent account the API key / token proves (never a client-supplied
	// value). Whether that agent may read conversations at all is the router's
	// conversations:read gate on /agents/me/conversations/*, judged on the
	// agent's own roles over the conversation's resource — so a role can be
	// limited to some conversations (for instance its own) with an IAM
	// condition. This only applies the data rules the gate can't see:
	//   - a soft-deleted conversation is not found;
	//   - an owner-private chat is never reachable, the agent's own included:
	//     an agent is not the member who owns it, and one agent chats with many
	//     members, so nothing proves the human it acts for is that owner.
	// Anything else returns ErrConversationNotFound.
	GetConversationForAgent(ctx context.Context, conversationID, callerAgentID uuid.UUID) (*AgentConversation, error)
	ListConversationEvents(ctx context.Context, conversationID uuid.UUID, window ConversationEventWindow) ([]*AgentConversationEvent, int64, error)
	// StopConversation interrupts (if running) and permanently tears down the
	// conversation's sandbox. memberID gates ownership (see GetConversation).
	StopConversation(ctx context.Context, projectID, conversationID, memberID uuid.UUID) error
	// PauseConversation interrupts the in-flight turn only — the sandbox
	// stays alive and the conversation can be replied to again once it pauses.
	PauseConversation(ctx context.Context, projectID, conversationID, memberID uuid.UUID) error
	// UpdateConversationTitle renames a conversation, returning the updated
	// row. Once set this way, agent-runner's own best-effort copy of
	// goose's session title never overwrites it again (see
	// ConversationRepository.UpdateConversationTitle).
	UpdateConversationTitle(ctx context.Context, projectID, conversationID, memberID uuid.UUID, title string) (*AgentConversation, error)
	// DeleteConversation removes a conversation from the Conversations list
	// for good, auto-stopping it first if it isn't already in a terminal
	// status — deleting a still-active conversation is one action, not
	// "stop, then delete". Soft delete, not a hard DELETE — see
	// ConversationRepository.SoftDeleteConversation's doc comment.
	DeleteConversation(ctx context.Context, projectID, conversationID, memberID uuid.UUID) error
	// Heartbeat refreshes a chat conversation's idle timer; called
	// periodically by the frontend while a conversation is loaded in a tab.
	Heartbeat(ctx context.Context, projectID, conversationID, memberID uuid.UUID) error
	// SendConversationMessage replies to conversationID. onBusy ("" |
	// OnBusyQueue | OnBusyForce — see OnBusyQueue's doc comment) only
	// matters when this resumes an ACP or environment-attached conversation
	// in place (see the implementation's doc comment): every other
	// conversation must already be "running" to accept a reply at all, so
	// there's no capacity decision to make there.
	SendConversationMessage(ctx context.Context, projectID, conversationID uuid.UUID, message string, memberID uuid.UUID, contextItems []ContextItemRef, onBusy string) error

	// -- Global chat conversations (ProjectID == uuid.Nil). Thin siblings of
	// the methods above with the ownership check inverted (ProjectID must be
	// uuid.Nil instead of matching a given projectID) and the actor
	// identified by ActorAccountID instead of a resolved project_members.id.

	// ListGlobalConversations returns the caller's own global-chat
	// conversations (never another user's — actorAccountID is forced
	// server-side, not client-suppliable) matching the filter. Global chat
	// has no project-team concept to grant shared visibility the way project
	// conversations do, so this stays scoped to the caller.
	ListGlobalConversations(ctx context.Context, actorAccountID uuid.UUID, in ListConversationsFilter, limit int) (convs []*AgentConversation, hasMore bool, err error)
	// GetGlobalConversation returns a single conversation after verifying it
	// is both a global-chat conversation and owned by actorAccountID — the
	// global-chat equivalent of GetConversation's projectID ownership check.
	// Every other Global* conversation method below funnels through this for
	// its existence+ownership gate, so actorAccountID must never be dropped
	// from any of them.
	GetGlobalConversation(ctx context.Context, conversationID, actorAccountID uuid.UUID) (*AgentConversation, error)
	StopGlobalConversation(ctx context.Context, conversationID, actorAccountID uuid.UUID) error
	PauseGlobalConversation(ctx context.Context, conversationID, actorAccountID uuid.UUID) error
	// UpdateGlobalConversationTitle/DeleteGlobalConversation are
	// UpdateConversationTitle/DeleteConversation's global-chat siblings —
	// see those doc comments.
	UpdateGlobalConversationTitle(ctx context.Context, conversationID, actorAccountID uuid.UUID, title string) (*AgentConversation, error)
	DeleteGlobalConversation(ctx context.Context, conversationID, actorAccountID uuid.UUID) error
	GlobalHeartbeat(ctx context.Context, conversationID, actorAccountID uuid.UUID) error
	// SendGlobalConversationMessage is SendConversationMessage's global-chat
	// sibling — see its doc comment for onBusy.
	SendGlobalConversationMessage(ctx context.Context, conversationID uuid.UUID, message string, actorAccountID uuid.UUID, contextItems []ContextItemRef, onBusy string) error
}

// ChatSessionService defines chat session use cases.
type ChatSessionService interface {
	ListChatSessions(ctx context.Context, projectID, agentID, memberID uuid.UUID) ([]*AgentChatSession, error)
	// StartChatSession creates a new chat session and its first conversation.
	// environmentID/folderID are optional (nil means "no override" —
	// environmentID falls back to the agent's own DefaultEnvironmentID;
	// folderID auto-selects if the resolved environment has exactly one
	// folder). See environmentdom.EnvironmentService.ResolveConversationWorkdir.
	// onBusy is one of "" (ask, the default) | OnBusyQueue | OnBusyForce —
	// see OnBusyQueue's doc comment for what each does when agentID is
	// already at ParallelismLimit running conversations.
	StartChatSession(ctx context.Context, projectID, agentID, memberID uuid.UUID, message string, environmentID, folderID *uuid.UUID, contextItems []ContextItemRef, onBusy string) (*AgentChatSession, *AgentConversation, error)
	SendChatMessage(ctx context.Context, projectID, agentID, sessionID, memberID uuid.UUID, message string, contextItems []ContextItemRef, onBusy string) (*AgentConversation, error)
	ListChatMessages(ctx context.Context, sessionID, memberID uuid.UUID, offset, limit int) ([]*AgentConversationEvent, int64, error)

	// -- Global chat sessions (chatting with a global agent from the home
	// page / admin pages — no project context). See ChatSession's doc
	// comment.

	ListGlobalChatSessions(ctx context.Context, agentID, actorAccountID uuid.UUID) ([]*AgentChatSession, error)
	StartGlobalChatSession(ctx context.Context, agentID, actorAccountID uuid.UUID, message string, contextItems []ContextItemRef, onBusy string) (*AgentChatSession, *AgentConversation, error)
	SendGlobalChatMessage(ctx context.Context, sessionID, actorAccountID uuid.UUID, message string, contextItems []ContextItemRef, onBusy string) (*AgentConversation, error)
}

// --- Input types ---

// CreateAgentInput carries fields required to create an agent.
type CreateAgentInput struct {
	Name        string
	Handle      string
	Description string
	// AgentType is "llm" (default) or "acp". LLM fields below are required
	// (and ACP fields ignored) for "llm"; ACP fields are required (and LLM
	// fields ignored) for "acp".
	AgentType   string
	LLMProvider string
	LLMModel    string
	LLMAPIKey   string // plain text key; stored encrypted by service
	LLMBaseURL  string
	ACPProvider string
	ACPCommand  []string
	ACPModel    string
	ACPEffort   string
	// CLIProvider is one of claude-code | codex | cursor-agent | gemini-cli
	// — required (and the fields below meaningful) only when AgentType is
	// "provider_cli". See Agent.CLIProvider's doc comment.
	CLIProvider string
	CLIModel    string
	// CLIAuthMode is "api_key" or "login"; defaults to "login" if empty.
	CLIAuthMode string
	// CLIAPIKey is a plaintext key; stored encrypted by the service. Only
	// meaningful when CLIAuthMode is "api_key".
	CLIAPIKey         string
	SystemPrompt      string
	MaxIterations     int
	TimeoutMinutes    int
	GitCommitterName  string
	GitCommitterEmail string
	DockerEnabled     bool
	// DefaultEnvironmentID, when set, must belong to the same project this
	// agent is being created in — see Agent.DefaultEnvironmentID's doc
	// comment. Rejected by CreateAgent for AgentScopeGlobal agents (there is
	// no AgentScope field here because CreateAgentInput is only ever used
	// for project-scoped creation — CreateGlobalAgentInput is its own type
	// below, and deliberately has no DefaultEnvironmentID field at all).
	DefaultEnvironmentID *uuid.UUID
	// DefaultFolderID, when set, must belong to DefaultEnvironmentID (also
	// set in the same request) — see Agent.DefaultFolderID's doc comment.
	// Like DefaultEnvironmentID, CreateGlobalAgentInput has no equivalent
	// field.
	DefaultFolderID *uuid.UUID
	// ParallelismLimit is clamped the same way MaxIterations is (<= 0 ->
	// default of 1, above the cap -> the cap) — see Agent.ParallelismLimit's
	// doc comment.
	ParallelismLimit int
	// RoleIDs are the roles the agent holds in the project (at least one),
	// attached together with its membership.
	RoleIDs   []uuid.UUID
	CreatedBy *uuid.UUID
}

// CreateGlobalAgentInput carries fields required to create a global agent.
// Mirrors CreateAgentInput minus RoleIDs (nothing to assign at
// creation time — a global agent gets a project role only later, when
// invited into a project). A new global agent has no global role either: it
// gets the default role and is given others through PUT /admin/agents/{agentId}/roles.
type CreateGlobalAgentInput struct {
	Name              string
	Handle            string
	Description       string
	AgentType         string
	LLMProvider       string
	LLMModel          string
	LLMAPIKey         string
	LLMBaseURL        string
	ACPProvider       string
	ACPCommand        []string
	ACPModel          string
	ACPEffort         string
	SystemPrompt      string
	MaxIterations     int
	TimeoutMinutes    int
	GitCommitterName  string
	GitCommitterEmail string
	DockerEnabled     bool
	ParallelismLimit  int
	CreatedBy         *uuid.UUID
}

// UpdateAgentInput carries mutable agent fields.
type UpdateAgentInput struct {
	Name        *string
	Handle      *string
	Description *string
	LLMProvider *string
	LLMModel    *string
	LLMAPIKey   *string
	LLMBaseURL  *string
	ACPProvider *string
	ACPCommand  []string
	ACPModel    *string
	ACPEffort   *string
	// CLIProvider/CLIModel/CLIAuthMode/CLIAPIKey mirror CreateAgentInput's
	// fields — nil means "unchanged" (same convention as every other
	// pointer field here); only meaningful for an existing provider_cli
	// agent (see UpdateAgent's per-type field guard).
	CLIProvider       *string
	CLIModel          *string
	CLIAuthMode       *string
	CLIAPIKey         *string
	SystemPrompt      *string
	MaxIterations     *int
	TimeoutMinutes    *int
	GitCommitterName  *string
	GitCommitterEmail *string
	DockerEnabled     *bool
	// ParallelismLimit: nil means unchanged, same convention as every other
	// pointer field here. See Agent.ParallelismLimit's doc comment.
	ParallelismLimit *int
	// DefaultEnvironmentID: nil means "the request didn't mention
	// default_environment_id — leave it unchanged" (a JSON body with the
	// key absent, or explicit null, decode to the same nil pointer either
	// way); a pointer to uuid.Nil explicitly clears it (a plain pointer
	// can't otherwise tell "omitted" from "explicit null"). Ignored by
	// UpdateGlobalAgent — a global agent can never have a default
	// environment (see Agent.DefaultEnvironmentID's doc comment).
	DefaultEnvironmentID *uuid.UUID
	// DefaultFolderID: same "nil means unchanged, uuid.Nil clears" contract
	// as DefaultEnvironmentID above. When both fields are set in the same
	// request, DefaultFolderID is validated against the *newly* resolved
	// DefaultEnvironmentID; when DefaultEnvironmentID changes without a
	// DefaultFolderID alongside it, the agent's existing default folder
	// (which belongs to the old environment) is cleared automatically —
	// see agentsvc.Service.UpdateAgent. Ignored by UpdateGlobalAgent, same
	// as DefaultEnvironmentID.
	DefaultFolderID *uuid.UUID
}

// AddMCPServerInput carries fields to add an MCP server.
type AddMCPServerInput struct {
	ServerName string
	Transport  string
	Command    *string
	Args       []string
	URL        *string
	Env        map[string]string
}

// UpdateMCPServerInput carries mutable MCP server fields.
type UpdateMCPServerInput struct {
	Command   *string
	Args      []string
	URL       *string
	Env       map[string]string
	IsEnabled *bool
}

// AddEnvVarInput carries fields to add a secret environment variable.
type AddEnvVarInput struct {
	Key   string
	Value string // plain text; encrypted by the service before storage
}

// UpdateEnvVarInput carries the new value for an existing environment variable.
type UpdateEnvVarInput struct {
	Value string // plain text; encrypted by the service before storage
}

// AddSkillInput carries fields to add a skill.
type AddSkillInput struct {
	SkillName    string
	SkillSource  string
	SkillContent string
	SourceURL    *string
	Triggers     []string
}

// UpdateSkillInput carries mutable skill fields.
type UpdateSkillInput struct {
	SkillContent *string
	Triggers     []string
	IsEnabled    *bool
}
