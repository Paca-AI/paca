package activitysvc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/events"
)

const defaultPageSize = 50

// MemberLookup resolves an authenticated actor to their project membership.
type MemberLookup interface {
	FindMemberByActor(ctx context.Context, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error)
}

// Service reads the activity log and owns comments. It embeds its Recorder,
// so it is also the Recorder a caller already holding a Service uses.
type Service struct {
	Recorder
	repo    activitydom.Repository
	members MemberLookup
}

// New returns a Service. rec may be nil, in which case nothing is recorded.
func New(repo activitydom.Repository, members MemberLookup, rec Recorder) *Service {
	if rec == nil {
		rec = &FanoutRecorder{}
	}
	return &Service{Recorder: rec, repo: repo, members: members}
}

// List returns up to limit entries matching f, newest first, and whether
// more exist. A non-positive limit uses the default page size.
func (s *Service) List(ctx context.Context, f activitydom.ListFilter, limit int) ([]*activitydom.Activity, bool, error) {
	if limit <= 0 {
		limit = defaultPageSize
	}
	return s.repo.List(ctx, f, limit)
}

// ListForEntity returns one entity's timeline, oldest first. The caller
// checks the entity belongs to the project it authorized against.
func (s *Service) ListForEntity(ctx context.Context, entityType events.EntityType, entityID uuid.UUID) ([]*activitydom.Activity, error) {
	return s.repo.ListForEntity(ctx, string(entityType), entityID)
}

// --- Entity entries ---------------------------------------------------------

// EntityPayload is the stream/realtime body task and doc entries have always
// carried: the activity itself, with its content JSON-encoded as a string
// under "content" (worker.ActivityConsumer unwraps it) and the entity ID
// under idKey ("task_id" / "document_id"), which realtime clients key on.
func EntityPayload(a *activitydom.Activity, idKey string) map[string]any {
	p := map[string]any{
		"id":            a.ID,
		idKey:           a.EntityIDOrNil(),
		"project_id":    a.ProjectID,
		"activity_type": a.ActivityType,
		"content":       string(a.Content),
		"created_at":    a.CreatedAt,
		"updated_at":    a.UpdatedAt,
	}
	if a.ActorID != nil {
		p["actor_id"] = a.ActorID.String()
	}
	return p
}

// --- Comments ---------------------------------------------------------------

// CommentKind describes the comments of one entity type: where they live,
// the realtime topics they publish, and the errors each outcome maps to
// (task and doc comments surface different API error codes).
type CommentKind struct {
	EntityType events.EntityType
	// IDKey names the entity ID in event payloads, e.g. "task_id".
	IDKey                                  string
	TopicAdded, TopicUpdated, TopicDeleted string

	ErrNotFound, ErrForbidden, ErrNotAComment error
	ErrContentInvalid, ErrActorUnidentified   error
}

// CommentInput is a new comment. The caller has already checked EntityID
// belongs to ProjectID.
type CommentInput struct {
	ProjectID uuid.UUID
	EntityID  uuid.UUID
	ActorID   uuid.UUID  // authenticated user
	AgentID   *uuid.UUID // set when an agent is acting
	Content   json.RawMessage
}

// AddComment validates, stores and publishes a comment, returning it and the
// author's membership (for mention handling).
func (s *Service) AddComment(ctx context.Context, k CommentKind, in CommentInput) (*activitydom.Activity, *projectdom.ProjectMember, error) {
	if !IsCommentContentValid(in.Content) {
		return nil, nil, k.ErrContentInvalid
	}
	member, err := s.resolveMember(ctx, k, in.ProjectID, in.ActorID, in.AgentID)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	entityID := in.EntityID
	a := &activitydom.Activity{
		ID:           uuid.New(),
		ProjectID:    in.ProjectID,
		EntityType:   string(k.EntityType),
		EntityID:     &entityID,
		ActorID:      &member.ID,
		Origin:       string(actorOrigin(in.AgentID)),
		ActivityType: activitydom.TypeComment,
		Content:      in.Content,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.Create(ctx, a); err != nil {
		return nil, nil, err
	}
	s.publishComment(ctx, k, k.TopicAdded, a, EntityPayload(a, k.IDKey))
	return a, member, nil
}

// UpdateComment replaces a comment's content. Only its author may edit it.
func (s *Service) UpdateComment(ctx context.Context, k CommentKind, id, projectID, actorID uuid.UUID, agentID *uuid.UUID, content json.RawMessage) (*activitydom.Activity, error) {
	if !IsCommentContentValid(content) {
		return nil, k.ErrContentInvalid
	}
	a, err := s.ownComment(ctx, k, id, projectID, actorID, agentID)
	if err != nil {
		return nil, err
	}
	a.Content = content
	a.UpdatedAt = time.Now()
	if err := s.repo.UpdateContent(ctx, a.ID, a.Content, a.UpdatedAt); err != nil {
		return nil, err
	}
	s.publishComment(ctx, k, k.TopicUpdated, a, EntityPayload(a, k.IDKey))
	return a, nil
}

// DeleteComment soft-deletes a comment. Only its author may delete it.
func (s *Service) DeleteComment(ctx context.Context, k CommentKind, id, projectID, actorID uuid.UUID, agentID *uuid.UUID) error {
	a, err := s.ownComment(ctx, k, id, projectID, actorID, agentID)
	if err != nil {
		return err
	}
	if err := s.repo.SoftDelete(ctx, a.ID); err != nil {
		return err
	}
	s.publishComment(ctx, k, k.TopicDeleted, a, map[string]any{
		"id":         a.ID,
		k.IDKey:      a.EntityIDOrNil(),
		"project_id": projectID,
		"actor_id":   actorID,
	})
	return nil
}

// ownComment loads comment id and checks it is a k comment in projectID
// written by the caller.
func (s *Service) ownComment(ctx context.Context, k CommentKind, id, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*activitydom.Activity, error) {
	a, err := s.repo.FindByID(ctx, id)
	if errors.Is(err, activitydom.ErrNotFound) {
		return nil, k.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	// The row carries its project, so a comment ID from another project is
	// indistinguishable from a missing one.
	if a.ProjectID != projectID || a.EntityType != string(k.EntityType) || a.DeletedAt != nil {
		return nil, k.ErrNotFound
	}
	if a.ActivityType != activitydom.TypeComment {
		return nil, k.ErrNotAComment
	}
	member, err := s.resolveMember(ctx, k, projectID, actorID, agentID)
	if err != nil {
		return nil, err
	}
	if a.ActorID == nil || *a.ActorID != member.ID {
		return nil, k.ErrForbidden
	}
	return a, nil
}

// resolveMember maps the caller to their project membership. The shared
// agent API key without X-Agent-ID is never a member by design, so for it
// "member not found" becomes the clearer ErrActorUnidentified.
func (s *Service) resolveMember(ctx context.Context, k CommentKind, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error) {
	if s.members == nil {
		return nil, projectdom.ErrMemberNotFound
	}
	m, err := s.members.FindMemberByActor(ctx, projectID, actorID, agentID)
	if errors.Is(err, projectdom.ErrMemberNotFound) && userdom.IsUnidentifiedSystemActor(actorID, agentID) {
		return nil, k.ErrActorUnidentified
	}
	return m, err
}

// publishComment fans out a comment event. The row was already written, and
// the stream copy carries the same "id", so ActivityConsumer's insert of it
// is a no-op.
func (s *Service) publishComment(ctx context.Context, k CommentKind, topic string, a *activitydom.Activity, payload map[string]any) {
	s.Record(ctx, Entry{
		ProjectID:  a.ProjectID,
		EntityType: k.EntityType,
		EntityID:   a.EntityIDOrNil(),
		Topic:      topic,
		Payload:    payload,
		Plugins:    true,
	})
}

// OriginFor returns explicit when set, otherwise the origin implied by the
// actor: agent, user, or system when there is none.
func OriginFor(explicit events.Origin, actorID, agentID *uuid.UUID) events.Origin {
	switch {
	case explicit != "":
		return explicit
	case agentID != nil:
		return events.OriginAgent
	case actorID != nil:
		return events.OriginUser
	}
	return events.OriginSystem
}

func actorOrigin(agentID *uuid.UUID) events.Origin {
	if agentID != nil {
		return events.OriginAgent
	}
	return events.OriginUser
}

// IsCommentContentValid reports whether content is a non-empty BlockNote
// blocks array or legacy {"text": "..."} object — the only shapes the web UI
// can render.
func IsCommentContentValid(content json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" || trimmed == "[]" || trimmed == "null" {
		return false
	}
	var arr []any
	if json.Unmarshal([]byte(trimmed), &arr) == nil {
		return true
	}
	var legacy struct {
		Text string `json:"text"`
	}
	return json.Unmarshal([]byte(trimmed), &legacy) == nil && strings.TrimSpace(legacy.Text) != ""
}
