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
	ActivityFeedRepository
}

// AgentRepository defines storage operations for agents.
type AgentRepository interface {
	// ListAgents returns agents visible in the given project. scope narrows
	// the result to just that AgentScope ("project" or "global"); the zero
	// value (AgentScope("")) returns both — its own project-scoped agents
	// plus any global agents currently invited into it.
	ListAgents(ctx context.Context, projectID uuid.UUID, scope AgentScope) ([]*Agent, error)
	FindAgentByID(ctx context.Context, id uuid.UUID) (*Agent, error)
	// FindVisibleAgentInProject returns a single agent by ID, but only if it
	// is visible in projectID — its own project-scoped agent, or a global
	// agent currently invited into it (same project_members join as
	// ListAgents/FindAgentByHandle). Returns ErrAgentNotFound otherwise, so
	// a project-scoped detail page (GET /projects/:id/agents/:agentId) can
	// resolve an invited global agent the same way the project's agent list
	// already does, instead of 404ing on it.
	FindVisibleAgentInProject(ctx context.Context, projectID, agentID uuid.UUID) (*Agent, error)
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
	// SetMCPAPIKeyHash stores the SHA-256 hash of a newly generated MCP API
	// key, replacing any previous one — only one key is ever live per agent,
	// so the previous key stops authenticating the moment this is called.
	SetMCPAPIKeyHash(ctx context.Context, agentID uuid.UUID, hash string) error
	// SetCLILoginVerifiedAt records that a provider_cli agent's CLI login
	// was just confirmed (a file-existence probe — see VerifyCLILogin), by
	// itself, never as part of the general UpdateAgent path — an unrelated
	// name/model edit must never reset or backdate this timestamp.
	SetCLILoginVerifiedAt(ctx context.Context, agentID uuid.UUID, t time.Time) error
	// FindAgentByMCPAPIKeyHash resolves the agent whose current MCP API key
	// hashes to hash, for the authn middleware to identify the caller
	// directly from the key it presented — no separate identity claim
	// needed. Returns ErrAgentNotFound if no live agent matches.
	FindAgentByMCPAPIKeyHash(ctx context.Context, hash string) (*Agent, error)

	// -- Global agents (AgentScope == AgentScopeGlobal, ProjectID == uuid.Nil).

	ListGlobalAgents(ctx context.Context) ([]*Agent, error)
	// FindGlobalAgentByHandle looks up a global agent by its instance-wide
	// unique handle (uq_agents_global_handle). Distinct from
	// FindAgentByHandle, which resolves within one project's visible agents
	// (its own project-scoped agents plus any global agents invited into it)
	// via the project_members join and would never match on projectID ==
	// uuid.Nil.
	FindGlobalAgentByHandle(ctx context.Context, handle string) (*Agent, error)
	CreateGlobalAgent(ctx context.Context, a *Agent) error
	// SoftDeleteGlobalAgentCascade soft-deletes the agent row and every
	// active project_members row referencing it, across every project it
	// was invited into, in one transaction.
	SoftDeleteGlobalAgentCascade(ctx context.Context, agentID uuid.UUID) error
	// ListInvitedProjectIDs returns the IDs of every project a global agent
	// currently has an active project_members row in — used to invalidate
	// each project's member-list cache when the agent is deleted.
	ListInvitedProjectIDs(ctx context.Context, agentID uuid.UUID) ([]uuid.UUID, error)
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
	// ListConversationEvents returns one page of a conversation's events per
	// window (see ConversationEventWindow), plus the conversation's current
	// total event count.
	ListConversationEvents(ctx context.Context, conversationID uuid.UUID, window ConversationEventWindow) ([]*AgentConversationEvent, int64, error)
	CreateConversationEvent(ctx context.Context, e *AgentConversationEvent) error

	// -- Parallelism queue (agent_pending_triggers) — see
	// Agent.ParallelismLimit and PendingTrigger's doc comments.

	// CountRunningConversations returns how many of agentID's conversations
	// are currently status "running", across every project — the count
	// dispatchOrEnqueue and AdvanceQueue compare against ParallelismLimit.
	CountRunningConversations(ctx context.Context, agentID uuid.UUID) (int, error)
	// CountRunningConversationsInFolder returns how many conversations,
	// across every agent, are currently status "running" and occupying
	// folderID (or any of its ancestor/descendant folders — see the
	// postgres implementation's folderOverlapPredicate for why a folder and
	// anything path-nested inside it share the same working directory on
	// disk and so count as the same occupied slot) within environmentID —
	// the count checkFolderCapacity compares against zero. folderID nil
	// matches (and is matched by) every folder in environmentID, treating a
	// conversation with no specific folder set as spanning the whole
	// environment.
	CountRunningConversationsInFolder(ctx context.Context, environmentID uuid.UUID, folderID *uuid.UUID) (int, error)
	// CreatePendingTrigger persists a trigger that couldn't be dispatched
	// immediately — see PendingTrigger's doc comment.
	CreatePendingTrigger(ctx context.Context, t *PendingTrigger) error
	// DequeueOldestPendingTrigger atomically returns and deletes agentID's
	// oldest PendingTrigger (FIFO), or (nil, nil) if it has none. Must use
	// SELECT ... FOR UPDATE SKIP LOCKED so that two concurrent callers (this
	// codebase runs multiple consumer-group replicas — see
	// worker.AutomationConsumer) never both dequeue the same row and
	// double-dispatch it.
	DequeueOldestPendingTrigger(ctx context.Context, agentID uuid.UUID) (*PendingTrigger, error)
	// DequeueOldestPendingTriggerForFolder is DequeueOldestPendingTrigger's
	// folder-scoped sibling: finds and removes the oldest PendingTrigger
	// whose own target folder overlaps environmentID/folderID (same
	// ancestor/descendant matching as CountRunningConversationsInFolder —
	// the returned trigger's folder need not equal folderID exactly),
	// regardless of which agent it belongs to, or (nil, nil) if none. Same
	// FOR UPDATE SKIP LOCKED requirement and rationale. Because a match
	// isn't necessarily an exact folderID match, a caller dequeuing "to see
	// what was waiting on folderID" must still re-verify the returned
	// trigger's OWN folder is actually free (see AdvanceFolderQueue) before
	// dispatching it — freeing folderID doesn't guarantee every folder that
	// merely overlaps it is also fully free of unrelated occupants.
	DequeueOldestPendingTriggerForFolder(ctx context.Context, environmentID uuid.UUID, folderID *uuid.UUID) (*PendingTrigger, error)
	// DeletePendingTriggerByConversationID removes conversationID's pending
	// trigger row, if it has one, reporting whether a row actually existed
	// to delete — called when a still-queued conversation is stopped before
	// ever being dispatched, so it can't be dequeued and published later
	// after being marked stopped. StopConversation uses the returned bool to
	// skip telling agent-runner to interrupt a conversation it never
	// actually dispatched. (false, nil) if no such row exists.
	DeletePendingTriggerByConversationID(ctx context.Context, conversationID uuid.UUID) (bool, error)
}

// ChatSessionRepository defines storage for agent chat sessions.
type ChatSessionRepository interface {
	ListChatSessions(ctx context.Context, agentID, memberID uuid.UUID) ([]*AgentChatSession, error)
	FindChatSessionByID(ctx context.Context, id uuid.UUID) (*AgentChatSession, error)
	CreateChatSession(ctx context.Context, s *AgentChatSession) error
	UpdateChatSession(ctx context.Context, s *AgentChatSession) error
	// ListGlobalChatSessions is ListChatSessions' sibling for global chat
	// sessions, keyed by actor_user_id instead of member_id.
	ListGlobalChatSessions(ctx context.Context, agentID, actorUserID uuid.UUID) ([]*AgentChatSession, error)
	// HasActiveGlobalChatSession reports whether agentID has ever started a
	// global chat session with actorUserID. Used by the agent-API-key
	// authentication middleware (middleware.AgentIdentityVerifier) to verify
	// an X-Actor-User-ID claim is backed by a real session rather than an
	// arbitrary value riding on the single shared static agent API key —
	// see that type's doc comment for the full threat model.
	HasActiveGlobalChatSession(ctx context.Context, agentID, actorUserID uuid.UUID) (bool, error)
}

// ListConversationsFilter carries optional filters for listing conversations.
type ListConversationsFilter struct {
	AgentIDs  []uuid.UUID
	ProjectID *uuid.UUID
	// GlobalOnly, when true, restricts the listing to global-chat
	// conversations (project_id IS NULL) — used by the "my global
	// conversations" endpoint. Mutually exclusive with ProjectID; callers
	// should set at most one.
	GlobalOnly bool
	// ActorUserID, when set, restricts the listing to conversations with
	// this actor_user_id — used by ListGlobalConversations to scope the
	// "my global conversations" endpoint to the caller only, since global
	// chat has no project-team visibility to share it with.
	ActorUserID *uuid.UUID
	// ViewerMemberID, when set, is the caller's project_members.id. It makes
	// the listing include owner-private conversations only when the caller is
	// that conversation's chat-session owner (audience is enforced in SQL so
	// the search subquery never touches a private conversation the caller
	// cannot read). project-shared conversations are always included. Only
	// meaningful together with ProjectID.
	ViewerMemberID *uuid.UUID
	TaskID         *uuid.UUID
	Statuses       []string
	TriggerTypes   []string
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

// ConversationEventWindow selects one keyset-paginated page of a single
// conversation's events (always returned ascending by event_index). At most
// one of After/Before is set:
//   - Neither set: the newest Limit events — opens a conversation on its
//     tail without needing to know its length upfront.
//   - After set: events with event_index > After's, ascending — pages
//     forward, e.g. catching up to events realtime has reported.
//   - Before set: the Limit events immediately preceding Before's
//     event_index — pages backward into older history.
//
// After/Before carry opaque cursors produced by EncodeConversationEventCursor
// (mirroring ListConversationsFilter.CursorAfter above); the repository
// decodes them.
type ConversationEventWindow struct {
	After  *string
	Before *string
	Limit  int
}
