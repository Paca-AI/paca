// Package notificationsvc implements the notification domain service.
package notificationsvc

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
)

// mentionRegexp matches @username tokens in comment text.
// Usernames consist of letters, digits, dots, hyphens, and underscores.
var mentionRegexp = regexp.MustCompile(`@([a-zA-Z0-9._-]+)`)

// memberLookup is the minimal interface the service needs to resolve a
// project_members.id → users.id via an ID lookup.
type memberLookup interface {
	FindMemberByID(ctx context.Context, memberID uuid.UUID) (*projectdom.ProjectMember, error)
	FindMemberByUserProject(ctx context.Context, userID, projectID uuid.UUID) (*projectdom.ProjectMember, error)
	// FindMemberByActor resolves an actor to a project member: the agent's
	// member record when agentID is non-nil, otherwise the human user's.
	FindMemberByActor(ctx context.Context, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error)
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error)
}

// Svc implements notificationdom.Service.
type Svc struct {
	repo       notificationdom.Repository
	memberRepo memberLookup
	publisher  *messaging.Publisher
}

// New returns a configured Svc.
// publisher may be nil; real-time events are then skipped silently.
func New(repo notificationdom.Repository, memberRepo memberLookup, publisher *messaging.Publisher) *Svc {
	return &Svc{repo: repo, memberRepo: memberRepo, publisher: publisher}
}

// --- Service methods --------------------------------------------------------

// NotifyAssigned creates a notification for the new assignee when a task is
// assigned to them, unless the new assignee is the actor themselves.
func (s *Svc) NotifyAssigned(ctx context.Context, in notificationdom.NotifyAssignedInput) error {
	// Resolve the new assignee's project-member record to get their user_id.
	assigneeMember, err := s.memberRepo.FindMemberByID(ctx, in.NewAssigneeMemberID)
	if err != nil {
		// The member may have been removed; skip silently.
		return nil
	}

	// Skip self-assignment (actor is the same user as the new assignee).
	if assigneeMember.UserID == in.ActorUserID {
		return nil
	}

	// Resolve the actor's member ID for attribution. When ActorAgentID is
	// set (the assignment was made by an AI agent), this resolves the
	// agent's own member record so the notification shows the agent's name
	// and avatar instead of failing to find a human member.
	actorMember, err := s.memberRepo.FindMemberByActor(ctx, in.ProjectID, in.ActorUserID, in.ActorAgentID)
	if err != nil {
		// Actor not found in project; still create the notification without actor attribution.
		actorMember = nil
	}

	n := &notificationdom.Notification{
		ID:              uuid.New(),
		RecipientUserID: assigneeMember.UserID,
		Type:            notificationdom.NotificationTypeAssigned,
		ProjectID:       in.ProjectID,
		CreatedAt:       time.Now(),
	}
	if actorMember != nil {
		id := actorMember.ID
		n.ActorMemberID = &id
	}
	taskID := in.TaskID
	n.TaskID = &taskID

	if err := s.repo.Create(ctx, n); err != nil {
		return err
	}
	s.publishCreated(ctx, n, assigneeMember.UserID)
	return nil
}

// NotifyMentioned parses @mentions from commentText and creates a notification
// for each project member found, excluding the actor themselves.
// When MentionedUserID is provided in input, it is used directly instead of parsing.
func (s *Svc) NotifyMentioned(ctx context.Context, in notificationdom.NotifyMentionedInput) error {
	var mentionedUserIDs []uuid.UUID

	// If specific user ID is provided (from BlockNote mentions), use it directly
	if in.MentionedUserID != nil {
		mentionedUserIDs = []uuid.UUID{*in.MentionedUserID}
	} else {
		// Legacy behavior: parse @mentions from comment text
		mentioned := extractMentions(in.CommentText)
		if len(mentioned) == 0 {
			return nil
		}

		members, err := s.memberRepo.ListMembers(ctx, in.ProjectID)
		if err != nil {
			return nil // best-effort
		}

		// Build a username → member map for O(1) lookups.
		byUsername := make(map[string]*projectdom.ProjectMember, len(members))
		for _, m := range members {
			byUsername[strings.ToLower(m.Username)] = m
		}

		for username := range mentioned {
			member, ok := byUsername[strings.ToLower(username)]
			if !ok {
				continue
			}
			mentionedUserIDs = append(mentionedUserIDs, member.UserID)
		}
	}

	actorID := in.ActorMemberID
	seen := make(map[uuid.UUID]bool) // avoid duplicate notifications
	for _, mentionedUserID := range mentionedUserIDs {
		// Skip self-mention.
		if mentionedUserID == in.ActorUserID {
			continue
		}
		// Skip duplicate (same user mentioned multiple times in one comment).
		if seen[mentionedUserID] {
			continue
		}
		seen[mentionedUserID] = true

		// Resolve the mentioned user's member ID for the notification
		_, err := s.memberRepo.FindMemberByUserProject(ctx, mentionedUserID, in.ProjectID)
		if err != nil {
			continue // user not in project, skip
		}

		taskID := in.TaskID
		n := &notificationdom.Notification{
			ID:              uuid.New(),
			RecipientUserID: mentionedUserID,
			ActorMemberID:   &actorID,
			Type:            notificationdom.NotificationTypeMentioned,
			TaskID:          &taskID,
			ProjectID:       in.ProjectID,
			CreatedAt:       time.Now(),
		}
		if err := s.repo.Create(ctx, n); err != nil {
			continue // best-effort; don't abort for one failure
		}
		s.publishCreated(ctx, n, mentionedUserID)
	}
	return nil
}

// ListNotifications returns up to limit notifications for the user, newest
// first, keyset-paginated via cursorAfter.
func (s *Svc) ListNotifications(ctx context.Context, userID uuid.UUID, limit int, cursorAfter *string) ([]*notificationdom.Notification, bool, error) {
	return s.repo.ListForUser(ctx, userID, limit, cursorAfter)
}

// UnreadCount returns the count of unread notifications for the user.
func (s *Svc) UnreadCount(ctx context.Context, userID uuid.UUID) (int64, error) {
	return s.repo.UnreadCount(ctx, userID)
}

// MarkAsRead marks a single notification as read.
func (s *Svc) MarkAsRead(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.MarkAsRead(ctx, id, userID)
}

// MarkAllAsRead marks all notifications for the user as read.
func (s *Svc) MarkAllAsRead(ctx context.Context, userID uuid.UUID) error {
	return s.repo.MarkAllAsRead(ctx, userID)
}

// --- helpers ----------------------------------------------------------------

// extractMentions returns the unique set of usernames found in text.
func extractMentions(text string) map[string]struct{} {
	matches := mentionRegexp.FindAllStringSubmatch(text, -1)
	result := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		if len(m) >= 2 {
			result[m[1]] = struct{}{}
		}
	}
	return result
}

// publishCreated publishes a notification.created event to the realtime channel.
// Errors are silently swallowed so messaging failures don't block the caller.
func (s *Svc) publishCreated(ctx context.Context, n *notificationdom.Notification, recipientUserID uuid.UUID) {
	if s.publisher == nil {
		return
	}
	payload := map[string]any{
		"id":                n.ID,
		"recipient_user_id": recipientUserID,
		"actor_member_id":   n.ActorMemberID,
		"type":              string(n.Type),
		"task_id":           n.TaskID,
		"project_id":        n.ProjectID,
		"created_at":        n.CreatedAt,
	}
	_ = s.publisher.Publish(ctx, events.ChannelRealtime, map[string]any{
		"type":    events.TopicNotificationCreated,
		"payload": payload,
	})
}
