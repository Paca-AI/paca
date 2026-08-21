// Package agent holds the domain types services/agent-runner needs for
// llm-type agent conversations. Deliberately a separate copy of the
// relevant fields from services/api's agentdom package rather than a shared
// import: agentdom lives under services/api/internal, which Go's internal-
// package visibility rules make unimportable from this sibling module. This
// mirrors the existing convention in this codebase (services/ai-agent has
// its own copies in models/agent.py) — see docs/architecture/service-
// boundaries.md's "shared code stays inside the owning runtime until
// duplication is real and proven".
package agent

import "github.com/google/uuid"

// TriggerType mirrors the trigger_type values services/api publishes to
// paca:agent:triggers — see services/api/internal/service/agent/
// agent_service.go's publishTrigger call sites.
type TriggerType string

const (
	// TriggerTaskAssigned fires when a task is assigned to an agent.
	TriggerTaskAssigned TriggerType = "task_assigned"
	// TriggerCommentMention fires when an agent is @-mentioned in a comment.
	TriggerCommentMention TriggerType = "comment_mention"
	// TriggerChatMessage fires for a chat message sent to an agent.
	TriggerChatMessage TriggerType = "chat_message"
	// TriggerDescriptionWrite fires when an agent is asked to draft a description.
	TriggerDescriptionWrite TriggerType = "description_write"
	// TriggerAutomationMessage fires for a message sent by an automation rule.
	TriggerAutomationMessage TriggerType = "automation_message"
)

// Trigger is one decoded message off the paca:agent:triggers Valkey Stream.
// Field names mirror the flat stream fields written by
// Service.publishTrigger/publishChatTrigger (AppendFlat — no JSON envelope,
// see internal/messaging for the decode side).
type Trigger struct {
	// TurnID is set only for the authoritative session-first execution path.
	// A nil value identifies a legacy task/comment/automation trigger.
	TurnID *uuid.UUID
	// RuntimeID is the physical execution identity. Authoritative retries use
	// a fresh run ID so an orphaned attempt cannot collide with its recovery.
	RuntimeID *uuid.UUID
	// Authoritative private turns receive one immutable snapshot and one
	// deny-by-default tool policy from the claim response. Context is data only
	// and can never add capabilities.
	ContextSnapshot         string
	ContextManifestSHA256   string
	AllowedToolCapabilities []string
	ConversationID          uuid.UUID
	// ProjectID is the zero value (uuid.Nil) for a global-chat trigger — see
	// agentdom.AgentConversation's own doc comment on the same convention.
	ProjectID     uuid.UUID
	AgentID       uuid.UUID
	TaskID        *uuid.UUID
	CommentID     *uuid.UUID
	ChatSessionID *uuid.UUID
	Message       string
	// ActorMemberID is nil when the automation-workflow engine fired this
	// trigger rather than a human — see entity.go's AgentConversation doc
	// comment on TriggeredByMemberID/ActorUserID. Never set together with
	// ActorUserID.
	ActorMemberID *uuid.UUID
	// ActorUserID is set only for a global-chat trigger (ProjectID is
	// uuid.Nil) — publishGlobalChatTrigger in agent_service.go identifies
	// the human by actor_user_id instead of actor_member_id, since there
	// may be no project_members row for them at all.
	ActorUserID *uuid.UUID
	TriggerType TriggerType
	// RepoPluginIDs is the comma-separated repo_plugin_ids stream field,
	// already split — a project can have more than one repository plugin
	// installed.
	RepoPluginIDs []string
	// PriorHandoffs carries bounded prior task-level handoff summaries (see
	// agent_task_handoffs) for a task-linked conversation, newest first.
	// Populated by the handler from the repository before the turn runs so
	// executor.buildInitialMessage can inject them without a DB dependency.
	PriorHandoffs []string
}
