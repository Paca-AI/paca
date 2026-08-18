// Package notificationsvc implements the notification domain service.
package notificationsvc

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
)

// userLookup resolves a recipient's name/email for the notification.assigned/
// notification.mentioned/notification.doc_mentioned/
// notification.task_description_mentioned plugin-event payloads —
// ProjectMember doesn't carry email, and a subscribing plugin has no direct
// DB access to resolve it itself.
type userLookup interface {
	FindByID(ctx context.Context, id uuid.UUID) (*userdom.User, error)
}

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
	userRepo   userLookup
	publicURL  string
}

// New returns a configured Svc.
// publisher may be nil; real-time events are then skipped silently.
func New(repo notificationdom.Repository, memberRepo memberLookup, publisher *messaging.Publisher) *Svc {
	return &Svc{repo: repo, memberRepo: memberRepo, publisher: publisher}
}

// WithEventPublishing configures publishing notification.assigned/
// notification.mentioned/notification.doc_mentioned/
// notification.task_description_mentioned events to the plugin event stream
// alongside this service's existing in-app notification — what, if
// anything, a subscribing plugin does with them is up to the plugin.
// publicURL is the workspace's public base URL, used to build a direct link
// to the task/document. When userRepo is nil, the plugin event is skipped —
// the in-app notification is unaffected.
func (s *Svc) WithEventPublishing(userRepo userLookup, publicURL string) *Svc {
	s.userRepo = userRepo
	s.publicURL = publicURL
	return s
}

// publishNotificationEvent resolves recipientUserID's name/email and
// publishes topic to the plugin event stream with recipient/actor names and
// a direct link to whatever was mentioned/assigned — the same event fires
// regardless of whether the recipient has an email on file, since paca has
// no visibility into what a subscribing plugin does with it (an email
// plugin cares about recipient_email; some other plugin might not).
// recipient_email is simply empty in the payload when unset. Best-effort —
// errors are swallowed, matching publishCreated's existing convention,
// since the in-app notification (already created by the caller, where one
// exists) is this service's primary responsibility.
func (s *Svc) publishNotificationEvent(ctx context.Context, topic string, recipientUserID uuid.UUID, actorName, linkURL string) {
	if s.publisher == nil || s.userRepo == nil {
		return
	}
	recipient, err := s.userRepo.FindByID(ctx, recipientUserID)
	if err != nil {
		return
	}
	recipientEmail := ""
	if recipient.Email != nil {
		recipientEmail = *recipient.Email
	}

	payload := map[string]any{
		"recipient_user_id": recipientUserID,
		"recipient_email":   recipientEmail,
		"recipient_name":    recipient.FullName,
		"actor_name":        actorName,
		"link_url":          linkURL,
	}
	_ = s.publisher.Append(ctx, events.StreamPluginEvents, topic, payload)
}

// resolveUserName returns userID's full name, or "" if it can't be resolved
// (no userRepo configured, or the lookup fails).
func (s *Svc) resolveUserName(ctx context.Context, userID uuid.UUID) string {
	if s.userRepo == nil {
		return ""
	}
	u, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return ""
	}
	return u.FullName
}

// taskURL builds a direct link to a task.
func (s *Svc) taskURL(projectID, taskID uuid.UUID) string {
	return fmt.Sprintf("%s/projects/%s/tasks/%s", strings.TrimRight(s.publicURL, "/"), projectID, taskID)
}

// docURL builds a direct link to a document.
func (s *Svc) docURL(projectID, docID uuid.UUID) string {
	return fmt.Sprintf("%s/projects/%s/docs/%s", strings.TrimRight(s.publicURL, "/"), projectID, docID)
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

	actorName := ""
	if actorMember != nil {
		actorName = actorMember.FullName
	}
	s.publishNotificationEvent(ctx, events.TopicNotificationAssigned, assigneeMember.UserID, actorName, s.taskURL(in.ProjectID, taskID))
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
	// Resolved once (not per-mentioned-user) since the actor is the same
	// commenter for every notification created below.
	actorName := ""
	if actorMember, err := s.memberRepo.FindMemberByID(ctx, actorID); err == nil {
		actorName = actorMember.FullName
	}
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
		s.publishNotificationEvent(ctx, events.TopicNotificationMentioned, mentionedUserID, actorName, s.taskURL(in.ProjectID, taskID))
	}
	return nil
}

// NotifyDocMentioned publishes a notification.doc_mentioned plugin event for
// a user newly @mentioned in a document's content. Unlike
// NotifyAssigned/NotifyMentioned, this does not create an in-app
// Notification row — the notifications table's task_id is a non-null FK, so
// it cannot yet represent a doc-scoped target (see
// docsvc.ActivitySvc.AddComment's doc-comment mention handling, which hits
// the same constraint), so the plugin event stream is the only way this
// reaches a subscribing plugin today. The caller is expected to have
// already diffed old vs. new content so this is only called for genuinely
// new mentions.
func (s *Svc) NotifyDocMentioned(ctx context.Context, mentionedUserID, actorUserID uuid.UUID, projectID, docID uuid.UUID) {
	if mentionedUserID == actorUserID {
		return
	}
	actorName := s.resolveUserName(ctx, actorUserID)
	s.publishNotificationEvent(ctx, events.TopicNotificationDocMentioned, mentionedUserID, actorName, s.docURL(projectID, docID))
}

// NotifyTaskDescriptionMentioned publishes a
// notification.task_description_mentioned plugin event for a user newly
// @mentioned in a task's description field (as opposed to a comment, which
// NotifyMentioned already handles). See NotifyDocMentioned's doc comment for
// why this doesn't create an in-app row.
func (s *Svc) NotifyTaskDescriptionMentioned(ctx context.Context, mentionedUserID, actorUserID uuid.UUID, projectID, taskID uuid.UUID) {
	if mentionedUserID == actorUserID {
		return
	}
	actorName := s.resolveUserName(ctx, actorUserID)
	s.publishNotificationEvent(ctx, events.TopicNotificationTaskDescriptionMentioned, mentionedUserID, actorName, s.taskURL(projectID, taskID))
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
