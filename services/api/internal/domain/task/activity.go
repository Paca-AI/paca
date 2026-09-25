package taskdom

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ActivityType categorises a task activity entry.
type ActivityType string

// Activity type constants.  The "comment" type is user-initiated; all others
// are system-generated when task-related operations occur.
const (
	// --- Task-level events ---------------------------------------------------

	// ActivityTypeTaskCreated is recorded when a task is first created.
	ActivityTypeTaskCreated ActivityType = "task.created"
	// ActivityTypeTaskUpdated is recorded when mutable task fields change.
	ActivityTypeTaskUpdated ActivityType = "task.updated"
	// ActivityTypeTaskDeleted is recorded when a task is soft-deleted.
	ActivityTypeTaskDeleted ActivityType = "task.deleted"

	// --- Attachment events ---------------------------------------------------

	// ActivityTypeAttachmentAdded is recorded when a file is attached to a task.
	ActivityTypeAttachmentAdded ActivityType = "task.attachment.added"
	// ActivityTypeAttachmentRemoved is recorded when an attachment is detached.
	ActivityTypeAttachmentRemoved ActivityType = "task.attachment.removed"

	// --- Comment -------------------------------------------------------------

	// ActivityTypeComment is a user-authored comment on the task.
	ActivityTypeComment ActivityType = "comment"

	// --- Link events ----------------------------------------------------------

	// ActivityTypeTaskLinkAdded is recorded when a link is created between tasks.
	ActivityTypeTaskLinkAdded ActivityType = "task.link.added"
	// ActivityTypeTaskLinkRemoved is recorded when a link between tasks is deleted.
	ActivityTypeTaskLinkRemoved ActivityType = "task.link.removed"

	// --- Agent session events -------------------------------------------------

	// ActivityTypeAgentSessionStarted is recorded when an AI agent begins a
	// conversation session triggered by a task assignment.
	ActivityTypeAgentSessionStarted ActivityType = "agent.session.started"

	// --- Automation events -----------------------------------------------------

	// ActivityTypeAutomationApplied is recorded when the automation graph
	// engine mutates a task via an Action node. Content carries
	// {automation_name, ...action-specific fields} so the activity feed can
	// attribute the change to the automation instead of a human actor.
	ActivityTypeAutomationApplied ActivityType = "automation.applied"

	// --- Auto-assign events -----------------------------------------------------

	// ActivityTypeAutoAssignSkipped is recorded when TaskAutoAssignConsumer
	// declines to assign a task it was asked to auto-assign — e.g. Jev's
	// pick fell below assigneeConfidenceThreshold. The task is (and stays)
	// unassigned; this activity is the only visible trace that auto-assign
	// even ran, since the alternative is total silence from the user's
	// point of view. Content carries {reason, ...reason-specific fields}.
	ActivityTypeAutoAssignSkipped ActivityType = "task.auto_assign.skipped"
)

// Activity is a single entry in a task's activity log.  It represents either
// a system-generated change event (e.g. status change) or a user comment.
type Activity struct {
	ID            uuid.UUID
	TaskID        uuid.UUID
	ActorID       *uuid.UUID // nil when the actor account has been deleted
	ActorName     string     // denormalised full name (populated on read)
	ActorUsername string     // denormalised username   (populated on read)
	// ActorAvatarKey/ActorAvatarThumbKey are the actor's avatar object-storage
	// keys (populated on read). Both nil when the actor has no avatar.
	ActorAvatarKey      *string
	ActorAvatarThumbKey *string
	ActivityType        ActivityType
	Content             json.RawMessage
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time // non-nil for soft-deleted comments
}

// FieldChange records a single before/after value for task.updated events.
type FieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// Origin identifies what caused an activity, independently of the actor.
//
// It exists because ActorID/ActorAgentID are both nil for every system-driven
// change, so they cannot distinguish "the automation engine did this" from
// "Jev autofill did this" — and that distinction is load-bearing: the
// automation consumer matches triggers off task.updated events, so an event
// the automation engine itself caused must be distinguishable from one a
// human caused, or a status_changed automation that sets a status re-triggers
// itself indefinitely. See OriginAutomation.
type Origin string

// Origin values. "user" and "agent" describe a human or agent acting through
// the API; the rest are system paths.
const (
	// OriginUser is a human acting through the HTTP API.
	OriginUser Origin = "user"
	// OriginAgent is an AI agent acting through the API.
	OriginAgent Origin = "agent"
	// OriginAutomation is the automation graph engine applying an action.
	// The automation consumer skips events carrying this origin — that guard
	// is what keeps a walk from re-triggering on its own writes, and it means
	// automations intentionally do not chain into one another.
	OriginAutomation Origin = "automation"
	// OriginJev is a Jev-backed flow (task autofill, task auto-assign).
	OriginJev Origin = "jev"
	// OriginAnnotation is work started from a page annotation.
	OriginAnnotation Origin = "annotation"
	// OriginSystem is any other system path, and the default when Origin is
	// left unset.
	OriginSystem Origin = "system"
)

// OrSystem returns the origin itself, or OriginSystem when unset, so callers
// never have to special-case the empty string.
func (o Origin) OrSystem() Origin {
	if o == "" {
		return OriginSystem
	}
	return o
}

// ActivityService defines use-cases for task activity and comments.
// It embeds ActivityRecorder so a single interface covers both user-facing
// comment operations and system-generated activity recording.
type ActivityService interface {
	ActivityRecorder
	// ListActivities returns all activities (system + comments) for a task.
	// projectID is used to verify the task belongs to the expected project.
	ListActivities(ctx context.Context, projectID, taskID uuid.UUID) ([]*Activity, error)
	// AddComment creates a new user comment on the task.
	// in.ProjectID is used to verify the task belongs to the expected
	// project, in addition to resolving the actor to a project member.
	AddComment(ctx context.Context, in AddCommentInput) (*Activity, error)
	// UpdateComment edits the content of an existing comment.
	// projectID is used to verify the comment's task belongs to the expected
	// project, in addition to resolving the actor to a project member.
	// Returns ErrActivityForbidden when actorID != comment's author.
	UpdateComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID, content json.RawMessage) (*Activity, error)
	// DeleteComment soft-deletes a comment.
	// projectID is used to verify the comment's task belongs to the expected
	// project, in addition to resolving the actor to a project member.
	// Returns ErrActivityForbidden when actorID != comment's author.
	DeleteComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID) error
}

// ActivityRecorder is the minimal interface used to persist system-generated
// activity entries.  It is embedded in ActivityService so a single concrete
// implementation satisfies both.
type ActivityRecorder interface {
	RecordActivity(ctx context.Context, in RecordActivityInput) error
}

// AddCommentInput carries the data needed to post a comment.
type AddCommentInput struct {
	TaskID    uuid.UUID
	ProjectID uuid.UUID
	ActorID   uuid.UUID  // authenticated user UUID; resolved to member UUID by the service
	AgentID   *uuid.UUID // agent UUID set when the request comes from an agent
	Content   json.RawMessage
}

// RecordActivityInput carries the data needed to persist a system event.
type RecordActivityInput struct {
	TaskID       uuid.UUID
	ProjectID    uuid.UUID  // needed by consumer to resolve ActorID (user) → member ID
	ActorID      *uuid.UUID // nil is allowed for system events; contains the user UUID
	ActorAgentID *uuid.UUID // agent UUID when the actor is an agent (takes priority over ActorID for resolution)
	ActivityType ActivityType
	Content      json.RawMessage
	// Origin identifies what caused this activity. Left unset it is treated
	// as OriginSystem. It travels as a stream envelope field, is stored in
	// activities.origin, and drives the automation engine's self-trigger
	// guard.
	Origin Origin
}
