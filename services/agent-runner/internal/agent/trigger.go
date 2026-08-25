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
	ConversationID uuid.UUID
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
	// EnvironmentID names the static environment (see
	// docs/ai-agent/environment-management.md) this conversation is
	// attached to, when one is — resolved server-side by services/api at
	// chat-start time (agent.DefaultEnvironmentID or an explicit
	// StartChatSessionRequest.environment_id) and published as-is, the
	// same "already-resolved, not re-looked-up" principle RepoPluginIDs
	// above follows: agent-runner never decides which environment a
	// conversation should use, only attaches to the one it's told. nil for
	// the (still-default) ephemeral-sandbox path.
	EnvironmentID *uuid.UUID
	// Workdir is the absolute path inside EnvironmentID's container the
	// ACP session's cwd should be set to — an environment_folders.path
	// row, resolved server-side the same way EnvironmentID is. Only
	// meaningful alongside EnvironmentID; nil whenever EnvironmentID is
	// nil.
	Workdir *string
}
