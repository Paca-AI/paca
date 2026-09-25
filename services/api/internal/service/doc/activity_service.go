package docsvc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	"github.com/Paca-AI/api/internal/events"
	activitysvc "github.com/Paca-AI/api/internal/service/activity"
)

// documentLookup is the minimal interface ActivitySvc needs to verify that a
// document belongs to the project the caller was authorized against, before
// returning or adding to its timeline.
type documentLookup interface {
	FindDocumentByID(ctx context.Context, id uuid.UUID) (*docdom.Document, error)
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
// @mentions in doc comments are not notified: NotifyMentioned is task-scoped
// and persists a task_id. Re-enable once notifications support documents.
type ActivitySvc struct {
	act     *activitysvc.Service
	docRepo documentLookup
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
	if in.ActorAgentID != nil {
		payload["actor_agent_id"] = in.ActorAgentID.String()
	}
	origin := events.OriginSystem
	if in.ActorAgentID != nil {
		origin = events.OriginAgent
	} else if in.ActorID != nil {
		origin = events.OriginUser
	}
	s.act.Record(ctx, activitysvc.Entry{
		ProjectID:    in.ProjectID,
		EntityType:   events.EntityDoc,
		EntityID:     in.DocumentID,
		Topic:        a.ActivityType,
		Payload:      payload,
		ActorID:      in.ActorID,
		ActorAgentID: in.ActorAgentID,
		Origin:       origin,
		Plugins:      true,
	})
	return nil
}

// ListActivities returns all non-deleted activities for a document, oldest first.
func (s *ActivitySvc) ListActivities(ctx context.Context, projectID, documentID uuid.UUID) ([]*docdom.Activity, error) {
	if err := s.documentInProject(ctx, projectID, documentID, docdom.ErrDocNotFound); err != nil {
		return nil, err
	}
	items, err := s.act.ListForEntity(ctx, events.EntityDoc, documentID)
	if err != nil {
		return nil, err
	}
	out := make([]*docdom.Activity, 0, len(items))
	for _, a := range items {
		out = append(out, toDocActivity(a))
	}
	return out, nil
}

// AddComment creates a comment on the document.
func (s *ActivitySvc) AddComment(ctx context.Context, in docdom.AddCommentInput) (*docdom.Activity, error) {
	if err := s.documentInProject(ctx, in.ProjectID, in.DocumentID, docdom.ErrDocNotFound); err != nil {
		return nil, err
	}
	a, _, err := s.act.AddComment(ctx, docComments, activitysvc.CommentInput{
		ProjectID: in.ProjectID,
		EntityID:  in.DocumentID,
		ActorID:   in.ActorID,
		AgentID:   in.AgentID,
		Content:   in.Content,
	})
	if err != nil {
		return nil, err
	}
	return toDocActivity(a), nil
}

// UpdateComment edits the content of an existing comment.
func (s *ActivitySvc) UpdateComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID, content json.RawMessage) (*docdom.Activity, error) {
	a, err := s.act.UpdateComment(ctx, docComments, id, projectID, actorID, agentID, content)
	if err != nil {
		return nil, err
	}
	return toDocActivity(a), nil
}

// DeleteComment soft-deletes a comment.
func (s *ActivitySvc) DeleteComment(ctx context.Context, id uuid.UUID, projectID uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID) error {
	return s.act.DeleteComment(ctx, docComments, id, projectID, actorID, agentID)
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
