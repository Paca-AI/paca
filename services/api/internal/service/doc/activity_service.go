package docsvc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/events"
	mentionpkg "github.com/Paca-AI/api/internal/pkg/mention"
	"github.com/Paca-AI/api/internal/platform/messaging"
)

// memberLookup is the minimal interface ActivitySvc needs to resolve an actor
// to a project member UUID.
type memberLookup interface {
	FindMemberByActor(ctx context.Context, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error)
}

// ActivitySvc implements docdom.ActivityService (which includes
// docdom.ActivityRecorder via embedding).
type ActivitySvc struct {
	repo            docdom.ActivityRepository
	memberRepo      memberLookup
	publisher       *messaging.Publisher
	notificationSvc notificationdom.Service
}

// NewActivityService creates a new ActivitySvc backed by repo.
// memberRepo is used to resolve user UUIDs to project-member UUIDs for comment
// operations; if nil, comment operations (AddComment, UpdateComment,
// DeleteComment) will return ErrMemberNotFound.
// publisher may be nil; stream events are then skipped silently.
func NewActivityService(repo docdom.ActivityRepository, memberRepo memberLookup, publisher *messaging.Publisher) *ActivitySvc {
	return &ActivitySvc{repo: repo, memberRepo: memberRepo, publisher: publisher}
}

// WithNotificationService attaches a notification service used to dispatch
// @mention notifications when comments are created.
func (s *ActivitySvc) WithNotificationService(svc notificationdom.Service) *ActivitySvc {
	s.notificationSvc = svc
	return s
}

// --- ActivityRecorder -------------------------------------------------------

// RecordActivity publishes a system-generated activity event to the Valkey
// stream (StreamDocActivities). The DocActivityConsumer worker reads that
// stream and writes the entry to the database, so this method intentionally
// does NOT touch the database itself.
func (s *ActivitySvc) RecordActivity(ctx context.Context, in docdom.RecordActivityInput) error {
	now := time.Now()
	content := in.Content
	if len(content) == 0 {
		content = json.RawMessage("{}")
	}
	a := &docdom.Activity{
		ID:           uuid.New(),
		DocumentID:   in.DocumentID,
		ActorID:      in.ActorID,
		ActivityType: in.ActivityType,
		Content:      content,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.publishToActivityStream(ctx, a, in.ProjectID, in.ActorAgentID)
	return nil
}

// --- ActivityService --------------------------------------------------------

// ListActivities returns all non-deleted activities for a document, oldest first.
func (s *ActivitySvc) ListActivities(ctx context.Context, documentID uuid.UUID) ([]*docdom.Activity, error) {
	return s.repo.ListActivities(ctx, documentID)
}

// AddComment creates a user comment on the document.
func (s *ActivitySvc) AddComment(ctx context.Context, in docdom.AddCommentInput) (*docdom.Activity, error) {
	if isContentEmpty(in.Content) || !isContentTypeValid(in.Content) {
		return nil, docdom.ErrCommentContentInvalid
	}
	if s.memberRepo == nil {
		return nil, projectdom.ErrMemberNotFound
	}
	member, err := s.memberRepo.FindMemberByActor(ctx, in.ProjectID, in.ActorID, in.AgentID)
	if err != nil {
		return nil, wrapMemberLookupErr(err, in.ActorID, in.AgentID)
	}
	now := time.Now()
	a := &docdom.Activity{
		ID:           uuid.New(),
		DocumentID:   in.DocumentID,
		ActorID:      &member.ID,
		ActivityType: docdom.ActivityTypeComment,
		Content:      in.Content,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.CreateActivity(ctx, a); err != nil {
		return nil, err
	}
	s.publishRealtimeOnly(ctx, events.TopicDocCommentAdded, activityPayload(a, in.ProjectID))

	// Intentionally skip mention notifications for document comments here.
	// NotifyMentioned is task-scoped and persists task_id; passing uuid.Nil for
	// a document comment can violate the notifications FK and fail silently.
	// This should be re-enabled once the notification contract supports
	// document-scoped mentions (for example, nullable TaskID or a doc-specific type).
	_ = mentionpkg.ExtractTeamMentionsFromBlocks
	_ = notificationdom.NotifyMentionedInput{}

	return a, nil
}

// UpdateComment edits the content of an existing comment.
func (s *ActivitySvc) UpdateComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID, content json.RawMessage) (*docdom.Activity, error) {
	a, err := s.repo.FindActivityByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.ActivityType != docdom.ActivityTypeComment {
		return nil, docdom.ErrActivityNotAComment
	}

	if s.memberRepo == nil {
		return nil, projectdom.ErrMemberNotFound
	}
	member, err := s.memberRepo.FindMemberByActor(ctx, projectID, actorID, agentID)
	if err != nil {
		return nil, wrapMemberLookupErr(err, actorID, agentID)
	}
	if a.ActorID == nil || *a.ActorID != member.ID {
		return nil, docdom.ErrActivityForbidden
	}

	if isContentEmpty(content) || !isContentTypeValid(content) {
		return nil, docdom.ErrCommentContentInvalid
	}
	a.Content = content
	a.UpdatedAt = time.Now()
	if err := s.repo.UpdateActivity(ctx, a); err != nil {
		return nil, err
	}
	s.publishRealtimeOnly(ctx, events.TopicDocCommentUpdated, activityPayload(a, uuid.Nil))
	return a, nil
}

// DeleteComment soft-deletes a comment.
func (s *ActivitySvc) DeleteComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID) error {
	a, err := s.repo.FindActivityByID(ctx, id)
	if err != nil {
		return err
	}
	if a.ActivityType != docdom.ActivityTypeComment {
		return docdom.ErrActivityNotAComment
	}

	if s.memberRepo == nil {
		return projectdom.ErrMemberNotFound
	}
	member, err := s.memberRepo.FindMemberByActor(ctx, projectID, actorID, agentID)
	if err != nil {
		return wrapMemberLookupErr(err, actorID, agentID)
	}
	if a.ActorID == nil || *a.ActorID != member.ID {
		return docdom.ErrActivityForbidden
	}

	if err := s.repo.DeleteActivity(ctx, id); err != nil {
		return err
	}
	s.publishRealtimeOnly(ctx, events.TopicDocCommentDeleted, map[string]any{
		"id":          id,
		"document_id": a.DocumentID,
		"actor_id":    actorID,
	})
	return nil
}

// --- helpers ----------------------------------------------------------------

// wrapMemberLookupErr replaces ErrMemberNotFound with the clearer
// docdom.ErrCommentActorUnidentified when the actor is the system/agent-bot
// identity with no agentID (see userdom.IsUnidentifiedSystemActor) — i.e. the
// request authenticated with the shared agent API key but omitted
// X-Agent-ID. That identity is never itself a project member by design, so
// "member not found" is misleading; the caller instead needs to know to
// supply X-Agent-ID. Any other lookup failure (a genuine non-member) is
// returned unchanged.
func wrapMemberLookupErr(err error, actorID uuid.UUID, agentID *uuid.UUID) error {
	if errors.Is(err, projectdom.ErrMemberNotFound) && userdom.IsUnidentifiedSystemActor(actorID, agentID) {
		return docdom.ErrCommentActorUnidentified
	}
	return err
}

// activityPayload builds the full stream message body for a doc activity.
// projectID is included so the consumer can resolve the actor (user UUID) to
// the correct project_members.id.
func activityPayload(a *docdom.Activity, projectID uuid.UUID) map[string]any {
	p := map[string]any{
		"id":            a.ID,
		"document_id":   a.DocumentID,
		"project_id":    projectID,
		"activity_type": string(a.ActivityType),
		"content":       string(a.Content),
		"created_at":    a.CreatedAt,
		"updated_at":    a.UpdatedAt,
	}
	if a.ActorID != nil {
		p["actor_id"] = a.ActorID.String()
	}
	return p
}

// publishToActivityStream appends the activity to the dedicated doc-activity
// Valkey stream and also broadcasts a real-time pub/sub notification.
// agentID, when non-nil, is embedded so the consumer can resolve the actor as
// an agent member instead of a user member.
// Errors are intentionally swallowed — a messaging failure must not block
// the primary HTTP response.
func (s *ActivitySvc) publishToActivityStream(ctx context.Context, a *docdom.Activity, projectID uuid.UUID, agentID *uuid.UUID) {
	if s.publisher == nil {
		return
	}
	payload := activityPayload(a, projectID)
	if agentID != nil {
		payload["actor_agent_id"] = agentID.String()
	}
	_ = s.publisher.Append(ctx, events.StreamDocActivities, string(a.ActivityType), payload)
	_ = s.publisher.Publish(ctx, events.ChannelRealtime, map[string]any{
		"type":    string(a.ActivityType),
		"payload": payload,
	})
}

// publishRealtimeOnly sends a real-time pub/sub notification without writing
// to any stream.  Used for comment operations that already write to the DB
// directly and don't need the consumer-persistence path.
func (s *ActivitySvc) publishRealtimeOnly(ctx context.Context, topic string, payload any) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.Publish(ctx, events.ChannelRealtime, map[string]any{
		"type":    topic,
		"payload": payload,
	})
}

// extractTextFromBlocks walks a BlockNote JSON blocks array and concatenates
// all "text" values found in inline content.  Falls back to the legacy
// {"text":"..."} object format for backward compatibility.
func extractTextFromBlocks(raw json.RawMessage) string {
	var blocks []struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) == nil && len(blocks) > 0 {
		var parts []string
		for _, b := range blocks {
			for _, c := range b.Content {
				if c.Text != "" {
					parts = append(parts, c.Text)
				}
			}
		}
		return strings.Join(parts, " ")
	}
	var legacy struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &legacy) == nil {
		return legacy.Text
	}
	return ""
}

// isContentTypeValid returns true only when content is a JSON array (BlockNote
// blocks) or the legacy {"text": "..."} object.  A bare JSON string, number,
// boolean, or any other value is rejected to prevent comments that the web UI
// cannot render.
func isContentTypeValid(content json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(content))
	var arr []any
	if json.Unmarshal([]byte(trimmed), &arr) == nil {
		return true // blocks array
	}
	var legacy struct {
		Text string `json:"text"`
	}
	return json.Unmarshal([]byte(trimmed), &legacy) == nil && legacy.Text != ""
}

// isContentEmpty checks if json.RawMessage content is empty or contains only whitespace.
// It handles: empty byte slice, "null", an empty JSON array, or a whitespace-only JSON string.
func isContentEmpty(content json.RawMessage) bool {
	if len(content) == 0 {
		return true
	}

	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" {
		return true
	}

	var value any
	if json.Unmarshal([]byte(trimmed), &value) != nil {
		return false
	}

	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	default:
		return false
	}
}
