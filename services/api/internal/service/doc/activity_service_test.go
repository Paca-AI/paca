// Package docsvc_test contains unit tests for the doc activity service.
// Tests use in-memory fake repositories and do not require any infrastructure.
package docsvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeCommentActivityRepo is a minimal in-memory docdom.ActivityRepository.
type fakeCommentActivityRepo struct {
	activities map[uuid.UUID]*docdom.Activity
}

func newFakeCommentActivityRepo() *fakeCommentActivityRepo {
	return &fakeCommentActivityRepo{activities: make(map[uuid.UUID]*docdom.Activity)}
}

func (r *fakeCommentActivityRepo) ListActivities(_ context.Context, documentID uuid.UUID) ([]*docdom.Activity, error) {
	var out []*docdom.Activity
	for _, a := range r.activities {
		if a.DocumentID == documentID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *fakeCommentActivityRepo) FindActivityByID(_ context.Context, id uuid.UUID) (*docdom.Activity, error) {
	a, ok := r.activities[id]
	if !ok {
		return nil, docdom.ErrActivityNotFound
	}
	return a, nil
}

func (r *fakeCommentActivityRepo) CreateActivity(_ context.Context, a *docdom.Activity) error {
	r.activities[a.ID] = a
	return nil
}

func (r *fakeCommentActivityRepo) UpdateActivity(_ context.Context, a *docdom.Activity) error {
	r.activities[a.ID] = a
	return nil
}

func (r *fakeCommentActivityRepo) DeleteActivity(_ context.Context, id uuid.UUID) error {
	delete(r.activities, id)
	return nil
}

// fakeCommentMemberRepo mirrors production FindMemberByActor semantics
// closely enough to exercise the unidentified-actor branch: the
// userdom.SystemActorUserID identity (with no agentID) is never itself a
// project member, exactly like the real repository.
type fakeCommentMemberRepo struct {
	membersByUser map[uuid.UUID]*projectdom.ProjectMember
}

func (r *fakeCommentMemberRepo) FindMemberByActor(_ context.Context, _ uuid.UUID, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error) {
	if agentID != nil {
		return &projectdom.ProjectMember{ID: *agentID, MemberType: "agent"}, nil
	}
	if actorID == userdom.SystemActorUserID {
		// Matches production: the system/agent-bot identity is never itself
		// a project member.
		return nil, projectdom.ErrMemberNotFound
	}
	if m, ok := r.membersByUser[actorID]; ok {
		return m, nil
	}
	return nil, projectdom.ErrMemberNotFound
}

func validCommentContent() json.RawMessage {
	return json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"hello"}]}]`)
}

// ---------------------------------------------------------------------------
// AddComment
// ---------------------------------------------------------------------------

func TestActivitySvc_AddComment_UnidentifiedSystemActor_ReturnsClearError(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := docsvc.NewActivityService(repo, memberRepo, nil)

	_, err := svc.AddComment(context.Background(), docdom.AddCommentInput{
		DocumentID: uuid.New(),
		ProjectID:  uuid.New(),
		ActorID:    userdom.SystemActorUserID, // shared agent key, no X-Agent-ID
		AgentID:    nil,
		Content:    validCommentContent(),
	})

	if !errors.Is(err, docdom.ErrCommentActorUnidentified) {
		t.Fatalf("expected ErrCommentActorUnidentified, got %v", err)
	}
	if errors.Is(err, projectdom.ErrMemberNotFound) {
		t.Errorf("clear error should not also satisfy errors.Is(ErrMemberNotFound); callers must not accidentally treat this as the generic not-a-member case")
	}
}

func TestActivitySvc_AddComment_GenuineNonMember_ReturnsMemberNotFound(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := docsvc.NewActivityService(repo, memberRepo, nil)

	realUserID := uuid.New() // a real human, just not a member of this project

	_, err := svc.AddComment(context.Background(), docdom.AddCommentInput{
		DocumentID: uuid.New(),
		ProjectID:  uuid.New(),
		ActorID:    realUserID,
		AgentID:    nil,
		Content:    validCommentContent(),
	})

	if !errors.Is(err, projectdom.ErrMemberNotFound) {
		t.Fatalf("expected ErrMemberNotFound for a genuine non-member, got %v", err)
	}
	if errors.Is(err, docdom.ErrCommentActorUnidentified) {
		t.Errorf("a genuine non-member should not be rewrapped as ErrCommentActorUnidentified")
	}
}

func TestActivitySvc_AddComment_ResolvedMember_Succeeds(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	memberID := uuid.New()
	actorID := uuid.New()
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		actorID: {ID: memberID, MemberType: "human"},
	}}
	svc := docsvc.NewActivityService(repo, memberRepo, nil)

	a, err := svc.AddComment(context.Background(), docdom.AddCommentInput{
		DocumentID: uuid.New(),
		ProjectID:  uuid.New(),
		ActorID:    actorID,
		Content:    validCommentContent(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.ActorID == nil || *a.ActorID != memberID {
		t.Errorf("expected comment actor_id %s, got %v", memberID, a.ActorID)
	}
}

// ---------------------------------------------------------------------------
// UpdateComment / DeleteComment
// ---------------------------------------------------------------------------

func TestActivitySvc_UpdateComment_UnidentifiedSystemActor_ReturnsClearError(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	commentID := uuid.New()
	existingAuthor := uuid.New()
	repo.activities[commentID] = &docdom.Activity{
		ID:           commentID,
		ActivityType: docdom.ActivityTypeComment,
		ActorID:      &existingAuthor,
		Content:      validCommentContent(),
	}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := docsvc.NewActivityService(repo, memberRepo, nil)

	_, err := svc.UpdateComment(context.Background(), commentID, uuid.New(), userdom.SystemActorUserID, nil, validCommentContent())

	if !errors.Is(err, docdom.ErrCommentActorUnidentified) {
		t.Fatalf("expected ErrCommentActorUnidentified, got %v", err)
	}
}

func TestActivitySvc_DeleteComment_UnidentifiedSystemActor_ReturnsClearError(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	commentID := uuid.New()
	existingAuthor := uuid.New()
	repo.activities[commentID] = &docdom.Activity{
		ID:           commentID,
		ActivityType: docdom.ActivityTypeComment,
		ActorID:      &existingAuthor,
		Content:      validCommentContent(),
	}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := docsvc.NewActivityService(repo, memberRepo, nil)

	err := svc.DeleteComment(context.Background(), commentID, uuid.New(), userdom.SystemActorUserID, nil)

	if !errors.Is(err, docdom.ErrCommentActorUnidentified) {
		t.Fatalf("expected ErrCommentActorUnidentified, got %v", err)
	}
}
