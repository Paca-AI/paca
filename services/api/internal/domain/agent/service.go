package agentdom

import (
	"context"

	"github.com/google/uuid"
)

// Service is the combined AI Agent service contract.
type Service interface {
	AgentService
	MCPServerService
	SkillService
	EnvVarService
	ConversationService
	ChatSessionService
	ActivityFeedService
}

// AgentService defines agent CRUD use cases.
type AgentService interface {
	// ListAgents returns agents visible in the given project, optionally
	// narrowed to a single AgentScope. See AgentRepository.ListAgents.
	ListAgents(ctx context.Context, projectID uuid.UUID, scope AgentScope) ([]*Agent, error)
	GetAgent(ctx context.Context, projectID, agentID uuid.UUID) (*Agent, error)
	CreateAgent(ctx context.Context, projectID uuid.UUID, in CreateAgentInput) (*Agent, error)
	UpdateAgent(ctx context.Context, projectID, agentID uuid.UUID, in UpdateAgentInput) (*Agent, error)
	DeleteAgent(ctx context.Context, projectID, agentID uuid.UUID) error
	TriggerDescriptionWrite(ctx context.Context, projectID, agentID, taskID, triggeredByMemberID uuid.UUID) (*AgentConversation, error)
	// GenerateACPBridgeToken issues a new local-bridge auth token for an
	// ACP-type agent, replacing any existing one, and returns the plaintext
	// once — only its SHA-256 hash is persisted.
	GenerateACPBridgeToken(ctx context.Context, projectID, agentID uuid.UUID) (plaintext string, err error)

	// -- Global agents (AgentScope == AgentScopeGlobal). See the Agent doc
	// comment. These never take a projectID: a global agent has none of its
	// own, and is attached to projects only indirectly via project_members
	// (see project.MemberService.AddMember's AgentID branch).

	ListGlobalAgents(ctx context.Context) ([]*Agent, error)
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
	GetConversation(ctx context.Context, projectID, conversationID uuid.UUID) (*AgentConversation, error)
	ListConversationEvents(ctx context.Context, conversationID uuid.UUID, offset, limit int) ([]*AgentConversationEvent, int64, error)
	// StopConversation interrupts (if running) and permanently tears down the
	// conversation's sandbox. Unchanged from before.
	StopConversation(ctx context.Context, projectID, conversationID uuid.UUID) error
	// PauseConversation interrupts the in-flight turn only — the sandbox
	// stays alive and the conversation can be replied to again once it pauses.
	PauseConversation(ctx context.Context, projectID, conversationID uuid.UUID) error
	// Heartbeat refreshes a chat conversation's idle timer; called
	// periodically by the frontend while a conversation is loaded in a tab.
	Heartbeat(ctx context.Context, projectID, conversationID uuid.UUID) error
	SendConversationMessage(ctx context.Context, projectID, conversationID uuid.UUID, message string, memberID uuid.UUID) error

	// -- Global chat conversations (ProjectID == uuid.Nil). Thin siblings of
	// the methods above with the ownership check inverted (ProjectID must be
	// uuid.Nil instead of matching a given projectID) and the actor
	// identified by ActorUserID instead of a resolved project_members.id.

	// ListGlobalConversations returns the caller's own global-chat
	// conversations (never another user's — actorUserID is forced
	// server-side, not client-suppliable) matching the filter. Global chat
	// has no project-team concept to grant shared visibility the way project
	// conversations do, so this stays scoped to the caller.
	ListGlobalConversations(ctx context.Context, actorUserID uuid.UUID, in ListConversationsFilter, limit int) (convs []*AgentConversation, hasMore bool, err error)
	GetGlobalConversation(ctx context.Context, conversationID uuid.UUID) (*AgentConversation, error)
	StopGlobalConversation(ctx context.Context, conversationID uuid.UUID) error
	PauseGlobalConversation(ctx context.Context, conversationID uuid.UUID) error
	GlobalHeartbeat(ctx context.Context, conversationID uuid.UUID) error
	SendGlobalConversationMessage(ctx context.Context, conversationID uuid.UUID, message string, actorUserID uuid.UUID) error
}

// ChatSessionService defines chat session use cases.
type ChatSessionService interface {
	ListChatSessions(ctx context.Context, projectID, agentID, memberID uuid.UUID) ([]*AgentChatSession, error)
	StartChatSession(ctx context.Context, projectID, agentID, memberID uuid.UUID, message string) (*AgentChatSession, *AgentConversation, error)
	SendChatMessage(ctx context.Context, projectID, sessionID, memberID uuid.UUID, message string) (*AgentConversation, error)
	ListChatMessages(ctx context.Context, sessionID uuid.UUID, offset, limit int) ([]*AgentConversationEvent, int64, error)

	// -- Global chat sessions (chatting with a global agent from the home
	// page / admin pages — no project context). See ChatSession's doc
	// comment.

	ListGlobalChatSessions(ctx context.Context, agentID, actorUserID uuid.UUID) ([]*AgentChatSession, error)
	StartGlobalChatSession(ctx context.Context, agentID, actorUserID uuid.UUID, message string) (*AgentChatSession, *AgentConversation, error)
	SendGlobalChatMessage(ctx context.Context, sessionID, actorUserID uuid.UUID, message string) (*AgentConversation, error)
}

// ActivityFeedService defines the agent activity feed use case.
type ActivityFeedService interface {
	ListAgentActivities(ctx context.Context, in ListAgentActivitiesFilter, limit int) (items []*ActivityFeedItem, hasMore bool, err error)
}

// --- Input types ---

// CreateAgentInput carries fields required to create an agent.
type CreateAgentInput struct {
	Name   string
	Handle string
	// AgentType is "llm" (default) or "acp". LLM fields below are required
	// (and ACP fields ignored) for "llm"; ACP fields are required (and LLM
	// fields ignored) for "acp".
	AgentType         string
	LLMProvider       string
	LLMModel          string
	LLMAPIKey         string // plain text key; stored encrypted by service
	LLMBaseURL        string
	ACPProvider       string
	ACPCommand        []string
	SystemPrompt      string
	MaxIterations     int
	TimeoutMinutes    int
	GitCommitterName  string
	GitCommitterEmail string
	ProjectRoleID     uuid.UUID
	CreatedBy         *uuid.UUID
}

// CreateGlobalAgentInput carries fields required to create a global agent.
// Mirrors CreateAgentInput minus ProjectRoleID (nothing to assign at
// creation time — a global agent gets a project role only later, when
// invited into a project), plus GlobalRoleID.
type CreateGlobalAgentInput struct {
	Name              string
	Handle            string
	AgentType         string
	LLMProvider       string
	LLMModel          string
	LLMAPIKey         string
	LLMBaseURL        string
	ACPProvider       string
	ACPCommand        []string
	SystemPrompt      string
	MaxIterations     int
	TimeoutMinutes    int
	GitCommitterName  string
	GitCommitterEmail string
	GlobalRoleID      *uuid.UUID
	CreatedBy         *uuid.UUID
}

// UpdateAgentInput carries mutable agent fields.
type UpdateAgentInput struct {
	Name              *string
	Handle            *string
	LLMProvider       *string
	LLMModel          *string
	LLMAPIKey         *string
	LLMBaseURL        *string
	ACPProvider       *string
	ACPCommand        []string
	SystemPrompt      *string
	MaxIterations     *int
	TimeoutMinutes    *int
	GitCommitterName  *string
	GitCommitterEmail *string
	// GlobalRoleID is only meaningful for AgentScopeGlobal agents (see
	// UpdateGlobalAgent); ignored by UpdateAgent for project-scoped agents.
	GlobalRoleID *uuid.UUID
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
