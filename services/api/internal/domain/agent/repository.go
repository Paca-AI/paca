package agentdom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository is the storage contract for agent aggregates.
type Repository interface {
	AgentRepository
	MCPServerRepository
	SkillRepository
	EnvVarRepository
	ConversationRepository
	ChatSessionRepository
}

// AgentRepository defines storage operations for agents.
type AgentRepository interface {
	ListAgents(ctx context.Context, projectID uuid.UUID) ([]*Agent, error)
	FindAgentByID(ctx context.Context, id uuid.UUID) (*Agent, error)
	FindAgentByHandle(ctx context.Context, projectID uuid.UUID, handle string) (*Agent, error)
	CreateAgent(ctx context.Context, a *Agent) error
	UpdateAgent(ctx context.Context, a *Agent) error
	SoftDeleteAgent(ctx context.Context, id uuid.UUID) error
	// SetMemberID sets the project_members.id for an agent after it has been added.
	SetAgentMemberID(ctx context.Context, agentID, memberID uuid.UUID) error
	// CreateAgentWithMembership atomically inserts the agent and its
	// project_members row within a single database transaction.
	CreateAgentWithMembership(ctx context.Context, a *Agent, memberID, projectID, roleID uuid.UUID) error
	// SoftDeleteAgentWithMembership atomically soft-deletes both the agent and
	// its project_members row within a single database transaction.
	SoftDeleteAgentWithMembership(ctx context.Context, projectID, agentID uuid.UUID) error
	// SetACPBridgeTokenHash stores the SHA-256 hash of a newly generated
	// local-bridge auth token, replacing any previous one.
	SetACPBridgeTokenHash(ctx context.Context, agentID uuid.UUID, hash string) error
}

// MCPServerRepository defines storage for agent MCP server configurations.
type MCPServerRepository interface {
	ListMCPServers(ctx context.Context, agentID uuid.UUID) ([]*AgentMCPServer, error)
	FindMCPServerByID(ctx context.Context, id uuid.UUID) (*AgentMCPServer, error)
	CreateMCPServer(ctx context.Context, s *AgentMCPServer) error
	UpdateMCPServer(ctx context.Context, s *AgentMCPServer) error
	DeleteMCPServer(ctx context.Context, id uuid.UUID) error
}

// SkillRepository defines storage for agent skills.
type SkillRepository interface {
	ListSkills(ctx context.Context, agentID uuid.UUID) ([]*AgentSkill, error)
	FindSkillByID(ctx context.Context, id uuid.UUID) (*AgentSkill, error)
	CreateSkill(ctx context.Context, s *AgentSkill) error
	UpdateSkill(ctx context.Context, s *AgentSkill) error
	DeleteSkill(ctx context.Context, id uuid.UUID) error
}

// EnvVarRepository defines storage for agent secret environment variables.
type EnvVarRepository interface {
	ListEnvVars(ctx context.Context, agentID uuid.UUID) ([]*AgentEnvironmentVariable, error)
	FindEnvVarByID(ctx context.Context, id uuid.UUID) (*AgentEnvironmentVariable, error)
	FindEnvVarByKey(ctx context.Context, agentID uuid.UUID, key string) (*AgentEnvironmentVariable, error)
	CreateEnvVar(ctx context.Context, v *AgentEnvironmentVariable) error
	UpdateEnvVar(ctx context.Context, v *AgentEnvironmentVariable) error
	DeleteEnvVar(ctx context.Context, id uuid.UUID) error
}

// ConversationRepository defines storage for agent conversations.
type ConversationRepository interface {
	// ListConversations returns up to limit conversations matching the
	// filter, ordered newest-first, plus whether more pages remain.
	ListConversations(ctx context.Context, in ListConversationsFilter, limit int) (convs []*AgentConversation, hasMore bool, err error)
	FindConversationByID(ctx context.Context, id uuid.UUID) (*AgentConversation, error)
	// FindLatestConversationByChatSession returns the most recently created
	// conversation for a chat session, or (nil, nil) if the session has none
	// yet — an unstarted chat session is a normal state, not an error.
	FindLatestConversationByChatSession(ctx context.Context, chatSessionID uuid.UUID) (*AgentConversation, error)
	CreateConversation(ctx context.Context, c *AgentConversation) error
	UpdateConversationStatus(ctx context.Context, id uuid.UUID, status string) error
	// ClaimConversationStatus atomically transitions a conversation from
	// fromStatus to toStatus and reports whether it won the race (false means
	// another caller already moved the conversation out of fromStatus).
	ClaimConversationStatus(ctx context.Context, id uuid.UUID, fromStatus, toStatus string) (bool, error)
	UpdateConversation(ctx context.Context, c *AgentConversation) error
	ListConversationEvents(ctx context.Context, conversationID uuid.UUID, offset, limit int) ([]*AgentConversationEvent, int64, error)
	CreateConversationEvent(ctx context.Context, e *AgentConversationEvent) error
}

// ChatSessionRepository defines storage for agent chat sessions.
type ChatSessionRepository interface {
	ListChatSessions(ctx context.Context, agentID, memberID uuid.UUID) ([]*AgentChatSession, error)
	FindChatSessionByID(ctx context.Context, id uuid.UUID) (*AgentChatSession, error)
	CreateChatSession(ctx context.Context, s *AgentChatSession) error
	UpdateChatSession(ctx context.Context, s *AgentChatSession) error
}

// ListConversationsFilter carries optional filters for listing conversations.
type ListConversationsFilter struct {
	AgentIDs     []uuid.UUID
	ProjectID    *uuid.UUID
	TaskID       *uuid.UUID
	Statuses     []string
	TriggerTypes []string
	// CreatedAfter/CreatedBefore bound created_at as [CreatedAfter,
	// CreatedBefore) — inclusive lower bound, exclusive upper bound. Both are
	// resolved to absolute instants by the handler (see
	// parseCreatedAfterBound/parseCreatedBeforeBound) before reaching here,
	// so the query can compare them directly against the TIMESTAMPTZ column
	// without any date-cast/timezone ambiguity.
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	// Search matches conversations that have at least one event whose
	// extracted text (see agent_conversation_event_search_text in migration
	// 000028) contains this text.
	Search      *string
	CursorAfter *string // opaque base64 cursor; when set, resumes after this conversation
}
