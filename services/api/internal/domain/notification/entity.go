// Package notificationdom defines the notification aggregate and its domain contracts.
package notificationdom

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NotificationType categorises a notification.
type NotificationType string

const (
	// NotificationTypeAssigned is sent when a task is assigned to a user.
	NotificationTypeAssigned NotificationType = "assigned"
	// NotificationTypeMentioned is sent when a user is @mentioned in a comment.
	NotificationTypeMentioned NotificationType = "mentioned"
)

// Notification is a single notification entry.
type Notification struct {
	ID uuid.UUID
	// RecipientUserID is the users.id of the user who receives this notification.
	RecipientUserID uuid.UUID
	// ActorMemberID is the project_members.id of the user who triggered the
	// notification.  Nil when the actor account has been deleted.
	ActorMemberID *uuid.UUID
	// ActorFullName and ActorUsername are denormalised from the actor's user record.
	ActorFullName string
	ActorUsername string
	// ActorAvatarKey and ActorAvatarThumbKey are the actor's avatar
	// object-storage keys, resolved to presigned URLs by the HTTP handler.
	// Nil when the actor has no avatar or the actor account has been deleted.
	ActorAvatarKey      *string
	ActorAvatarThumbKey *string
	// ActorMemberType is "human" or "agent". ActorAgentType/
	// ActorAgentLLMProvider/ActorAgentACPProvider mirror the agent's own
	// fields and are only meaningful when ActorMemberType == "agent" — the
	// frontend uses them to pick a default provider-logo avatar when the
	// actor has no custom avatar uploaded (see projectdom.ProjectMember's
	// equivalent fields, which this deliberately mirrors).
	ActorMemberType       string
	ActorAgentType        string
	ActorAgentLLMProvider string
	ActorAgentACPProvider *string
	// Type is one of the NotificationType constants.
	Type NotificationType
	// TaskID is the task this notification is about.
	TaskID *uuid.UUID
	// TaskTitle and TaskNumber are denormalised from the task record.
	TaskTitle  string
	TaskNumber int
	// ProjectID is the project the task belongs to.
	ProjectID uuid.UUID
	// ProjectName is denormalised from the project record.
	ProjectName string
	// ReadAt is nil when the notification has not yet been read.
	ReadAt    *time.Time
	CreatedAt time.Time
}

// CreateNotificationInput carries the data needed to create a notification.
type CreateNotificationInput struct {
	RecipientUserID uuid.UUID
	ActorMemberID   *uuid.UUID
	Type            NotificationType
	TaskID          *uuid.UUID
	ProjectID       uuid.UUID
}

// Repository defines persistence operations for notifications.
type Repository interface {
	// Create persists a new notification.
	Create(ctx context.Context, n *Notification) error
	// ListForUser returns up to limit notifications for the given user,
	// ordered newest first (created_at DESC, id DESC), keyset-paginated via
	// cursorAfter (nil for the first page — see EncodeNotificationCursor /
	// DecodeNotificationCursor). hasMore reports whether another page
	// remains beyond the returned items.
	ListForUser(ctx context.Context, userID uuid.UUID, limit int, cursorAfter *string) (items []*Notification, hasMore bool, err error)
	// UnreadCount returns the number of unread notifications for the given user.
	UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error)
	// MarkAsRead sets read_at on a notification owned by userID.
	// Returns ErrNotificationNotFound when the notification does not exist or
	// does not belong to userID. Idempotent: already-read notifications succeed.
	MarkAsRead(ctx context.Context, id, userID uuid.UUID) error
	// MarkAllAsRead sets read_at on all unread notifications for userID.
	MarkAllAsRead(ctx context.Context, userID uuid.UUID) error
}

// Service defines the notification use-cases exposed to callers.
type Service interface {
	// NotifyAssigned creates a notification for the new assignee when a task
	// is assigned.  actorMemberID is the project_members.id of the caller.
	// newAssigneeMemberID is the project_members.id of the new assignee.
	// Does nothing if the new assignee is nil or is the same as the actor.
	NotifyAssigned(ctx context.Context, in NotifyAssignedInput) error
	// NotifyMentioned creates notifications for all @mentioned users found in
	// commentText who are members of the project.
	NotifyMentioned(ctx context.Context, in NotifyMentionedInput) error
	// ListNotifications returns up to limit notifications for the authenticated
	// user, keyset-paginated via cursorAfter — see Repository.ListForUser.
	ListNotifications(ctx context.Context, userID uuid.UUID, limit int, cursorAfter *string) (items []*Notification, hasMore bool, err error)
	// UnreadCount returns the count of unread notifications for the user.
	UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error)
	// MarkAsRead marks a single notification as read.
	MarkAsRead(ctx context.Context, id, userID uuid.UUID) error
	// MarkAllAsRead marks all notifications for the user as read.
	MarkAllAsRead(ctx context.Context, userID uuid.UUID) error
}

// NotifyAssignedInput carries data for an assignment notification.
type NotifyAssignedInput struct {
	TaskID    uuid.UUID
	ProjectID uuid.UUID
	// NewAssigneeMemberID is the project_members.id of the new assignee.
	NewAssigneeMemberID uuid.UUID
	// ActorUserID is the users.id of the person who made the assignment.
	// Used to resolve the actor to a member and to skip self-assignment.
	ActorUserID uuid.UUID
	// ActorAgentID is the agent's id when the assignment was made by an AI
	// agent (an agent-authenticated API request) rather than a human. When
	// set, it takes precedence over ActorUserID for resolving the actor's
	// project member — see projectdom.MemberRepository.FindMemberByActor.
	ActorAgentID *uuid.UUID
}

// NotifyMentionedInput carries data for a mention notification.
// MentionedUserID can be provided for structured mentions (from BlockNote JSON)
// to directly reference the mentioned user by ID instead of parsing from text.
type NotifyMentionedInput struct {
	TaskID      uuid.UUID
	ProjectID   uuid.UUID
	CommentText string
	// ActorMemberID is the project_members.id of the commenter.
	ActorMemberID uuid.UUID
	// ActorUserID is the users.id of the commenter (used to exclude self-mention).
	ActorUserID uuid.UUID
	// MentionedUserID is an optional direct reference to the mentioned user's ID.
	// When provided, it takes precedence over username parsing from CommentText.
	MentionedUserID *uuid.UUID
}

// ErrNotificationNotFound is returned when a notification does not exist or
// does not belong to the requesting user.
var ErrNotificationNotFound = errNotificationNotFound("notification not found")

type errNotificationNotFound string

func (e errNotificationNotFound) Error() string { return string(e) }

// ErrInvalidCursor is returned when a client-supplied pagination cursor
// fails to decode — see agentdom.ErrConversationInvalidCursor, which this
// mirrors.
var ErrInvalidCursor = errInvalidCursor("invalid pagination cursor")

type errInvalidCursor string

func (e errInvalidCursor) Error() string { return string(e) }

// Cursor holds the stable ordering fields for keyset-based pagination over
// a user's notification list, which is always ordered by created_at DESC,
// id DESC — same shape as agentdom.ConversationCursor/ActivityFeedCursor.
type Cursor struct {
	CreatedAt time.Time `json:"ca"`
	ID        string    `json:"id"`
}

// EncodeNotificationCursor builds an opaque base64 cursor from the last
// notification on a page.
func EncodeNotificationCursor(n *Notification) string {
	cur := Cursor{CreatedAt: n.CreatedAt.UTC(), ID: n.ID.String()}
	b, _ := json.Marshal(cur)
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeNotificationCursor parses a cursor token produced by
// EncodeNotificationCursor.
func DecodeNotificationCursor(s string) (*Cursor, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode cursor base64: %w", err)
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("decode cursor json: %w", err)
	}
	c.CreatedAt = c.CreatedAt.UTC()
	return &c, nil
}
