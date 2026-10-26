package docsvc

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/events"
	mentionpkg "github.com/Paca-AI/api/internal/pkg/mention"
	activitysvc "github.com/Paca-AI/api/internal/service/activity"
)

// documentLookup is the minimal interface ActivitySvc needs to verify that a
// document belongs to the project the caller was authorized against, before
// returning or adding to its timeline.
type documentLookup interface {
	FindDocumentByID(ctx context.Context, id uuid.UUID) (*docdom.Document, error)
}

// agentCommentTrigger starts an agent conversation when an agent is
// @-mentioned in a document comment.
type agentCommentTrigger interface {
	TriggerDocCommentMention(ctx context.Context, projectID, agentID, documentID, commentID, triggeredByMemberID uuid.UUID, message string) (*agentdom.AgentConversation, error)
}

// docComments configures activitysvc's comment handling for documents.
var docComments = activitysvc.CommentKind{
	EntityType:           events.EntityDoc,
	IDKey:                "document_id",
	TopicAdded:           events.TopicDocCommentAdded,
	TopicUpdated:         events.TopicDocCommentUpdated,
	TopicDeleted:         events.TopicDocCommentDeleted,
	ErrNotFound:          docdom.ErrActivityNotFound,
	ErrForbidden:         docdom.ErrActivityForbidden,
	ErrNotAComment:       docdom.ErrActivityNotAComment,
	ErrContentInvalid:    docdom.ErrCommentContentInvalid,
	ErrActorUnidentified: docdom.ErrCommentActorUnidentified,
}

// ActivitySvc implements docdom.ActivityService on top of activitysvc,
// adding only the scoping of each call to the document's project.
//
// People @mentioned in a doc comment are notified (WithNotificationService);
// @mentioned agents are triggered (WithAgentTrigger) so a reply to an agent's
// question on a document wakes it, as it does on a task.
type ActivitySvc struct {
	act          *activitysvc.Service
	docRepo      documentLookup
	memberRepo   memberLookup
	agentTrigger agentCommentTrigger
	notifier     docMentionNotifier
}

// docMentionNotifier notifies a person @mentioned in a document comment.
type docMentionNotifier interface {
	NotifyDocCommentMentioned(ctx context.Context, in notificationdom.NotifyDocCommentMentionedInput) error
}

// WithNotificationService attaches the notifier used to tell people
// @mentioned in a document comment (agents are triggered instead).
func (s *ActivitySvc) WithNotificationService(n docMentionNotifier) *ActivitySvc {
	s.notifier = n
	return s
}

// WithAgentTrigger attaches the trigger used to start a conversation when an
// agent is @-mentioned in a document comment; members resolves the mention to
// an agent member.
func (s *ActivitySvc) WithAgentTrigger(trigger agentCommentTrigger, members memberLookup) *ActivitySvc {
	s.agentTrigger = trigger
	s.memberRepo = members
	return s
}

// NewActivityService returns an ActivitySvc over act. docRepo verifies a
// document belongs to the caller's authorized project.
func NewActivityService(act *activitysvc.Service, docRepo documentLookup) *ActivitySvc {
	return &ActivitySvc{act: act, docRepo: docRepo}
}

// documentInProject returns nil when documentID resolves to a document in
// projectID, and notFoundErr otherwise.
func (s *ActivitySvc) documentInProject(ctx context.Context, projectID, documentID uuid.UUID, notFoundErr error) error {
	d, err := s.docRepo.FindDocumentByID(ctx, documentID)
	if err != nil {
		return err
	}
	if d.ProjectID != projectID {
		return notFoundErr
	}
	return nil
}

// RecordActivity records a system-generated doc activity. The
// ActivityConsumer worker persists it from the activity stream.
func (s *ActivitySvc) RecordActivity(ctx context.Context, in docdom.RecordActivityInput) error {
	now := time.Now()
	docID := in.DocumentID
	a := &activitydom.Activity{
		ID:           uuid.New(),
		ProjectID:    in.ProjectID,
		EntityType:   string(events.EntityDoc),
		EntityID:     &docID,
		ActorID:      in.ActorID,
		ActivityType: string(in.ActivityType),
		Content:      in.Content,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if len(a.Content) == 0 {
		a.Content = json.RawMessage("{}")
	}
	payload := activitysvc.EntityPayload(a, docComments.IDKey)
	s.act.Record(ctx, activitysvc.Entry{
		ProjectID:  in.ProjectID,
		EntityType: events.EntityDoc,
		EntityID:   in.DocumentID,
		Topic:      a.ActivityType,
		Payload:    payload,
		ActorID:    in.ActorID,
		Origin:     activitysvc.OriginFor(ctx, "", in.ActorID),
		Plugins:    true,
	})
	return nil
}

// ListActivities returns one page of a document's activities, newest first.
func (s *ActivitySvc) ListActivities(ctx context.Context, projectID, documentID uuid.UUID, cursor *string, limit int) ([]*docdom.Activity, *string, error) {
	if err := s.documentInProject(ctx, projectID, documentID, docdom.ErrDocNotFound); err != nil {
		return nil, nil, err
	}
	items, next, err := s.act.ListForEntity(ctx, events.EntityDoc, documentID, cursor, limit)
	if err != nil {
		return nil, nil, err
	}
	out := make([]*docdom.Activity, 0, len(items))
	for _, a := range items {
		out = append(out, toDocActivity(a))
	}
	return out, next, nil
}

// AddComment creates a comment on the document.
func (s *ActivitySvc) AddComment(ctx context.Context, in docdom.AddCommentInput) (*docdom.Activity, error) {
	if err := s.documentInProject(ctx, in.ProjectID, in.DocumentID, docdom.ErrDocNotFound); err != nil {
		return nil, err
	}
	a, member, err := s.act.AddComment(ctx, docComments, activitysvc.CommentInput{
		ProjectID: in.ProjectID,
		EntityID:  in.DocumentID,
		ActorID:   in.ActorID,
		Content:   in.Content,
	})
	if err != nil {
		return nil, err
	}
	s.handleMentions(ctx, in.ProjectID, in.DocumentID, a, member, in.ActorID)
	return toDocActivity(a), nil
}

// handleMentions acts on each @mention in the stored comment (which already
// has plain-text mentions linked): an agent is started, a person is sent an
// in-app notification. Best-effort: the comment is already posted.
func (s *ActivitySvc) handleMentions(ctx context.Context, projectID, documentID uuid.UUID, a *activitydom.Activity, author *projectdom.ProjectMember, authorAccountID uuid.UUID) {
	if author == nil || (s.agentTrigger == nil && s.notifier == nil) || s.memberRepo == nil {
		return
	}
	text := ""
	seen := map[uuid.UUID]bool{}
	for _, m := range mentionpkg.ExtractTeamMentionsFromBlocks(a.Content) {
		// The web UI embeds agent_id (not user_id) as an agent mention's id.
		mentionedID, err := uuid.Parse(m.ID)
		if err != nil || seen[mentionedID] {
			continue
		}
		seen[mentionedID] = true
		member, err := s.memberRepo.FindMember(ctx, projectID, mentionedID)
		if err != nil {
			continue // not a member of this project
		}
		if member.IsAgent() {
			if s.agentTrigger == nil || member.AgentID == nil {
				continue
			}
			// An agent mentioning itself must not retrigger its own conversation.
			if member.ID == author.ID {
				continue
			}
			if text == "" {
				text = commentText(a.Content)
			}
			if _, err := s.agentTrigger.TriggerDocCommentMention(ctx, projectID, *member.AgentID, documentID, a.ID, author.ID, text); err != nil {
				slog.WarnContext(ctx, "doc comment mention trigger failed", "error", err, "project_id", projectID, "agent_id", *member.AgentID, "document_id", documentID)
			}
			continue
		}
		if s.notifier == nil {
			continue
		}
		if err := s.notifier.NotifyDocCommentMentioned(ctx, notificationdom.NotifyDocCommentMentionedInput{
			DocumentID:      documentID,
			ProjectID:       projectID,
			ActorMemberID:   author.ID,
			ActorID:         authorAccountID,
			MentionedUserID: mentionedID,
		}); err != nil {
			slog.WarnContext(ctx, "doc comment mention notification failed", "error", err, "project_id", projectID, "document_id", documentID)
		}
	}
}

// commentText flattens a BlockNote comment to the plain text handed to the
// agent as its initial message.
func commentText(raw json.RawMessage) string {
	var blocks []struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			Props map[string]any `json:"props"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var b strings.Builder
	for i, block := range blocks {
		if i > 0 {
			b.WriteString("\n")
		}
		for _, c := range block.Content {
			if c.Type == "teamMention" {
				if name, _ := c.Props["name"].(string); name != "" {
					b.WriteString("@" + name)
				}
				continue
			}
			b.WriteString(c.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

// UpdateComment edits the content of an existing comment.
func (s *ActivitySvc) UpdateComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, content json.RawMessage) (*docdom.Activity, error) {
	a, err := s.act.UpdateComment(ctx, docComments, id, projectID, actorID, content)
	if err != nil {
		return nil, err
	}
	return toDocActivity(a), nil
}

// DeleteComment soft-deletes a comment.
func (s *ActivitySvc) DeleteComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID) error {
	return s.act.DeleteComment(ctx, docComments, id, projectID, actorID)
}

// toDocActivity maps a log entry to the doc timeline's shape.
func toDocActivity(a *activitydom.Activity) *docdom.Activity {
	return &docdom.Activity{
		ID:                  a.ID,
		DocumentID:          a.EntityIDOrNil(),
		ActorID:             a.ActorID,
		ActorName:           a.ActorName,
		ActorUsername:       a.ActorUsername,
		ActorAvatarKey:      a.ActorAvatarKey,
		ActorAvatarThumbKey: a.ActorAvatarThumbKey,
		ActivityType:        docdom.ActivityType(a.ActivityType),
		Content:             a.Content,
		CreatedAt:           a.CreatedAt,
		UpdatedAt:           a.UpdatedAt,
		DeletedAt:           a.DeletedAt,
	}
}
