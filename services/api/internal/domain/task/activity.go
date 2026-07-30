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
)

// Activity is a single entry in a task's activity log.  It represents either
// a system-generated change event (e.g. status change) or a user comment.
type Activity struct {
	ID            uuid.UUID
	TaskID        uuid.UUID
	ActorID       *uuid.UUID // nil when the actor account has been deleted
	ActorName     string     // denormalised full name (populated on read)
	ActorUsername string     // denormalised username   (populated on read)
	ActivityType  ActivityType
	Content       json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time // non-nil for soft-deleted comments
}

// FieldChange records a single before/after value for task.updated events.
type FieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// ActivityRepository defines persistence for task activities.
type ActivityRepository interface {
	// ListActivities returns all non-deleted activity entries for a task
	// ordered by created_at ascending.
	ListActivities(ctx context.Context, taskID uuid.UUID) ([]*Activity, error)
	// FindActivityByID returns a single activity (including soft-deleted).
	FindActivityByID(ctx context.Context, id uuid.UUID) (*Activity, error)
	// CreateActivity persists a new activity entry.
	CreateActivity(ctx context.Context, a *Activity) error
	// UpdateActivity persists mutable changes to an existing activity.
	UpdateActivity(ctx context.Context, a *Activity) error
	// DeleteActivity soft-deletes the activity (sets deleted_at).
	DeleteActivity(ctx context.Context, id uuid.UUID) error
}

// ActivityService defines use-cases for task activity and comments.
// It embeds ActivityRecorder so a single interface covers both user-facing
// comment operations and system-generated activity recording.
type ActivityService interface {
	ActivityRecorder
	// ListActivities returns all activities (system + comments) for a task.
	ListActivities(ctx context.Context, taskID uuid.UUID) ([]*Activity, error)
	// AddComment creates a new user comment on the task.
	AddComment(ctx context.Context, in AddCommentInput) (*Activity, error)
	// UpdateComment edits the content of an existing comment.
	// Returns ErrActivityForbidden when actorID != comment's author.
	UpdateComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID, content json.RawMessage) (*Activity, error)
	// DeleteComment soft-deletes a comment.
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
}
