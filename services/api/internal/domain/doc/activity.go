package docdom

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ActivityType categorises a doc activity entry.
type ActivityType string

// Activity type constants.
// "comment" is user-initiated; all others are system-generated.
const (
	// ActivityTypeDocCreated is recorded when a document is first created.
	ActivityTypeDocCreated ActivityType = "doc.created"
	// ActivityTypeDocUpdated is recorded when document fields change.
	ActivityTypeDocUpdated ActivityType = "doc.updated"
	// ActivityTypeDocDeleted is recorded when a document is soft-deleted.
	ActivityTypeDocDeleted ActivityType = "doc.deleted"
	// ActivityTypeDocMoved is recorded when a document is moved to a different folder.
	ActivityTypeDocMoved ActivityType = "doc.moved"

	// ActivityTypeFolderCreated is recorded when a folder is created.
	ActivityTypeFolderCreated ActivityType = "doc.folder.created"
	// ActivityTypeFolderUpdated is recorded when a folder is renamed or moved.
	ActivityTypeFolderUpdated ActivityType = "doc.folder.updated"
	// ActivityTypeFolderDeleted is recorded when a folder is deleted.
	ActivityTypeFolderDeleted ActivityType = "doc.folder.deleted"

	// ActivityTypeComment is a user-authored comment on the document.
	ActivityTypeComment ActivityType = "comment"
)

// Activity is a single entry in a document's activity log.
// It represents either a system-generated event or a user comment.
type Activity struct {
	ID            uuid.UUID
	DocumentID    uuid.UUID
	ActorID       *uuid.UUID // nil when the actor account has been removed
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

// FieldChange records a single before/after value for doc.updated events.
type FieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// ActivityRepository defines persistence for doc activities.
type ActivityRepository interface {
	// ListActivities returns all non-deleted activities for a document, oldest first.
	ListActivities(ctx context.Context, documentID uuid.UUID) ([]*Activity, error)
	// FindActivityByID returns a single activity entry (including soft-deleted).
	FindActivityByID(ctx context.Context, id uuid.UUID) (*Activity, error)
	// CreateActivity persists a new activity entry.
	CreateActivity(ctx context.Context, a *Activity) error
	// UpdateActivity persists mutable changes to an existing activity.
	UpdateActivity(ctx context.Context, a *Activity) error
	// DeleteActivity soft-deletes the activity (sets deleted_at).
	DeleteActivity(ctx context.Context, id uuid.UUID) error
}

// ActivityService defines use-cases for doc activity and comments.
type ActivityService interface {
	ActivityRecorder
	// ListActivities returns all activities (system + comments) for a document.
	ListActivities(ctx context.Context, documentID uuid.UUID) ([]*Activity, error)
	// AddComment creates a new user comment on the document.
	AddComment(ctx context.Context, in AddCommentInput) (*Activity, error)
	// UpdateComment edits the content of an existing comment.
	// Returns ErrActivityForbidden when actorID != comment's author.
	UpdateComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID, content json.RawMessage) (*Activity, error)
	// DeleteComment soft-deletes a comment.
	// Returns ErrActivityForbidden when actorID != comment's author.
	DeleteComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID) error
}

// ActivityRecorder is the minimal interface used to persist system-generated
// activity entries directly to the database.
type ActivityRecorder interface {
	RecordActivity(ctx context.Context, in RecordActivityInput) error
}

// RecordActivityInput carries the data needed to persist a system-generated
// doc activity event.  ActorID holds the authenticated user UUID; the
// DocActivityConsumer resolves it to the corresponding project_members.id
// before writing to the database.
type RecordActivityInput struct {
	DocumentID   uuid.UUID
	ProjectID    uuid.UUID  // used by the consumer to resolve ActorID → member ID
	ActorID      *uuid.UUID // nil is allowed for system events
	ActorAgentID *uuid.UUID // agent UUID when the actor is an agent (takes priority over ActorID for resolution)
	ActivityType ActivityType
	Content      json.RawMessage
}

// AddCommentInput carries fields to add a comment to a document.
type AddCommentInput struct {
	DocumentID uuid.UUID
	ProjectID  uuid.UUID
	ActorID    uuid.UUID  // authenticated user UUID (resolved to project member)
	AgentID    *uuid.UUID // agent UUID set when the request comes from an agent
	Content    json.RawMessage
}
