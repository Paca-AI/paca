// Package activitydom is the project activity log: one record of every change
// to every entity in a project — tasks, docs, sprints, views, automations,
// environments, members, roles, agents and the project itself.
//
// All of it lives in the single activities table. Entries are written by
// worker.ActivityConsumer from the StreamActivities stream (see
// events.Fanout); comments are additionally inserted directly by
// activitysvc, under the same ID, because their request returns the row.
// Only comments are ever mutated afterwards.
package activitydom

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TypeComment is the activity_type of a user/agent comment on a task or doc.
// Every other activity_type is the event topic, e.g. "task.updated".
const TypeComment = "comment"

// Activity is one entry in the activity log.
type Activity struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	EntityType string
	EntityID   *uuid.UUID
	ActorID    *uuid.UUID // project_members.id; nil for system events
	Origin     string
	// ActivityType is the event topic, e.g. "task.updated", or TypeComment.
	ActivityType string
	Content      json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time // set on soft-deleted comments

	// Populated on read.
	ActorName           string
	ActorUsername       string
	ActorAvatarKey      *string
	ActorAvatarThumbKey *string
	// EntityTitle is the entity's title, resolved at read time — still set
	// for a soft-deleted task/doc, empty once the row is gone. EntityDeleted
	// is true in either case.
	EntityTitle   string
	EntityDeleted bool
}

// EntityIDOrNil returns the entity ID, or uuid.Nil for an entry without one.
func (a *Activity) EntityIDOrNil() uuid.UUID {
	if a.EntityID == nil {
		return uuid.Nil
	}
	return *a.EntityID
}

// ListFilter narrows a project activity query. Zero values mean "no
// constraint"; list values OR within themselves and AND with each other.
type ListFilter struct {
	ProjectID      uuid.UUID
	EntityTypes    []string
	ActorMemberIDs []uuid.UUID
	Origins        []string
	// ActivityTypes matches the event topic exactly, e.g. "task.created".
	ActivityTypes []string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	// Search matches the entity's title or the activity content,
	// case-insensitively.
	Search string
	// Cursor continues after the last item of a previous page (see
	// EncodeCursor). Results are newest first.
	Cursor *Cursor
}

// Cursor is a keyset position in the newest-first ordering.
type Cursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

var (
	// ErrNotFound is returned when an activity ID does not resolve.
	ErrNotFound = errors.New("activity: not found")
	// ErrInvalidCursor is returned when a cursor string cannot be decoded.
	ErrInvalidCursor = errors.New("invalid activity cursor")
)

// EncodeCursor returns the opaque cursor that resumes after a.
func EncodeCursor(a *Activity) string {
	raw := a.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + a.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeCursor parses a cursor produced by EncodeCursor.
func DecodeCursor(s string) (*Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	ts, idStr, ok := strings.Cut(string(b), "|")
	if !ok {
		return nil, ErrInvalidCursor
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	return &Cursor{CreatedAt: t, ID: id}, nil
}

// Repository persists the activity log.
type Repository interface {
	// Create inserts one entry. Inserting an ID that already exists is a
	// no-op, so a stream message redelivered after a crash between insert
	// and ack — or the stream copy of a directly inserted comment — does not
	// duplicate.
	Create(ctx context.Context, a *Activity) error
	// List returns up to limit entries matching f newest first, and whether
	// more exist beyond them.
	List(ctx context.Context, f ListFilter, limit int) ([]*Activity, bool, error)
	// ListForEntity returns one entity's non-deleted entries, oldest first —
	// the timeline on a task or doc detail page.
	ListForEntity(ctx context.Context, entityType string, entityID uuid.UUID) ([]*Activity, error)
	// FindByID returns one entry, including a soft-deleted one, or
	// ErrNotFound.
	FindByID(ctx context.Context, id uuid.UUID) (*Activity, error)
	// UpdateContent replaces an entry's content (comment edits).
	UpdateContent(ctx context.Context, id uuid.UUID, content json.RawMessage, updatedAt time.Time) error
	// SoftDelete marks an entry deleted (comment deletes).
	SoftDelete(ctx context.Context, id uuid.UUID) error
}
