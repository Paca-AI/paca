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

// fakeCommentActivityRepo is a minimal in-memory docdom.ActivityRepository
// that also serves as the documentLookup ActivitySvc uses to verify a
// document belongs to the caller's authorized project.
type fakeCommentActivityRepo struct {
	activities map[uuid.UUID]*docdom.Activity
	docs       map[uuid.UUID]*docdom.Document
}

func newFakeCommentActivityRepo() *fakeCommentActivityRepo {
	return &fakeCommentActivityRepo{
		activities: make(map[uuid.UUID]*docdom.Activity),
		docs:       make(map[uuid.UUID]*docdom.Document),
	}
}

func (r *fakeCommentActivityRepo) FindDocumentByID(_ context.Context, id uuid.UUID) (*docdom.Document, error) {
	d, ok := r.docs[id]
	if !ok {
		return nil, docdom.ErrDocNotFound
	}
	return d, nil
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
	documentID := uuid.New()
	projectID := uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: projectID}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := docsvc.NewActivityService(repo, repo, memberRepo, nil)

	_, err := svc.AddComment(context.Background(), docdom.AddCommentInput{
		DocumentID: documentID,
		ProjectID:  projectID,
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
	documentID := uuid.New()
	projectID := uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: projectID}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := docsvc.NewActivityService(repo, repo, memberRepo, nil)

	realUserID := uuid.New() // a real human, just not a member of this project

	_, err := svc.AddComment(context.Background(), docdom.AddCommentInput{
		DocumentID: documentID,
		ProjectID:  projectID,
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
	documentID := uuid.New()
	projectID := uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: projectID}
	memberID := uuid.New()
	actorID := uuid.New()
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		actorID: {ID: memberID, MemberType: "human"},
	}}
	svc := docsvc.NewActivityService(repo, repo, memberRepo, nil)

	a, err := svc.AddComment(context.Background(), docdom.AddCommentInput{
		DocumentID: documentID,
		ProjectID:  projectID,
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
	documentID := uuid.New()
	projectID := uuid.New()
	existingAuthor := uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: projectID}
	repo.activities[commentID] = &docdom.Activity{
		ID:           commentID,
		DocumentID:   documentID,
		ActivityType: docdom.ActivityTypeComment,
		ActorID:      &existingAuthor,
		Content:      validCommentContent(),
	}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := docsvc.NewActivityService(repo, repo, memberRepo, nil)

	_, err := svc.UpdateComment(context.Background(), commentID, projectID, userdom.SystemActorUserID, nil, validCommentContent())

	if !errors.Is(err, docdom.ErrCommentActorUnidentified) {
		t.Fatalf("expected ErrCommentActorUnidentified, got %v", err)
	}
}

func TestActivitySvc_DeleteComment_UnidentifiedSystemActor_ReturnsClearError(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	commentID := uuid.New()
	documentID := uuid.New()
	projectID := uuid.New()
	existingAuthor := uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: projectID}
	repo.activities[commentID] = &docdom.Activity{
		ID:           commentID,
		DocumentID:   documentID,
		ActivityType: docdom.ActivityTypeComment,
		ActorID:      &existingAuthor,
		Content:      validCommentContent(),
	}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := docsvc.NewActivityService(repo, repo, memberRepo, nil)

	err := svc.DeleteComment(context.Background(), commentID, projectID, userdom.SystemActorUserID, nil)

	if !errors.Is(err, docdom.ErrCommentActorUnidentified) {
		t.Fatalf("expected ErrCommentActorUnidentified, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Cross-project isolation tests (GHSA-xwmv-9c7h-g947 / PACA-001, PACA-002)
//
// Every activity/comment operation is authorized against the URL project,
// but the document/comment ID itself is caller-supplied. A member of project
// A must not be able to read or mutate project B's document activity by
// guessing/knowing its UUID.
// ---------------------------------------------------------------------------

func TestActivitySvc_ListActivities_WrongProject_ReturnsNotFound(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	documentID := uuid.New()
	ownerProjectID := uuid.New()
	attackerProjectID := uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: ownerProjectID}
	repo.activities[uuid.New()] = &docdom.Activity{ID: uuid.New(), DocumentID: documentID, ActivityType: docdom.ActivityTypeDocCreated}
	svc := docsvc.NewActivityService(repo, repo, &fakeCommentMemberRepo{}, nil)

	_, err := svc.ListActivities(context.Background(), attackerProjectID, documentID)
	if !errors.Is(err, docdom.ErrDocNotFound) {
		t.Fatalf("expected ErrDocNotFound for cross-project ListActivities, got %v", err)
	}
}

func TestActivitySvc_AddComment_WrongProject_ReturnsNotFound(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	documentID := uuid.New()
	ownerProjectID := uuid.New()
	attackerProjectID := uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: ownerProjectID}
	actorID := uuid.New()
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		actorID: {ID: uuid.New(), MemberType: "human"},
	}}
	svc := docsvc.NewActivityService(repo, repo, memberRepo, nil)

	// actorID is a legitimate member of attackerProjectID, but documentID
	// belongs to a different project — the comment must be rejected even
	// though the actor would resolve to a valid member.
	_, err := svc.AddComment(context.Background(), docdom.AddCommentInput{
		DocumentID: documentID,
		ProjectID:  attackerProjectID,
		ActorID:    actorID,
		Content:    validCommentContent(),
	})
	if !errors.Is(err, docdom.ErrDocNotFound) {
		t.Fatalf("expected ErrDocNotFound for cross-project AddComment, got %v", err)
	}
	if len(repo.activities) != 0 {
		t.Errorf("no comment should have been persisted, found %d activities", len(repo.activities))
	}
}

func TestActivitySvc_UpdateComment_WrongProject_ReturnsNotFound(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	commentID := uuid.New()
	documentID := uuid.New()
	ownerProjectID := uuid.New()
	attackerProjectID := uuid.New()
	author := uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: ownerProjectID}
	repo.activities[commentID] = &docdom.Activity{
		ID:           commentID,
		DocumentID:   documentID,
		ActivityType: docdom.ActivityTypeComment,
		ActorID:      &author,
		Content:      validCommentContent(),
	}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		author: {ID: author, MemberType: "human"},
	}}
	svc := docsvc.NewActivityService(repo, repo, memberRepo, nil)

	_, err := svc.UpdateComment(context.Background(), commentID, attackerProjectID, author, nil, validCommentContent())
	if !errors.Is(err, docdom.ErrActivityNotFound) {
		t.Fatalf("expected ErrActivityNotFound for cross-project UpdateComment, got %v", err)
	}
}

func TestActivitySvc_DeleteComment_WrongProject_ReturnsNotFound(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	commentID := uuid.New()
	documentID := uuid.New()
	ownerProjectID := uuid.New()
	attackerProjectID := uuid.New()
	author := uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: ownerProjectID}
	repo.activities[commentID] = &docdom.Activity{
		ID:           commentID,
		DocumentID:   documentID,
		ActivityType: docdom.ActivityTypeComment,
		ActorID:      &author,
		Content:      validCommentContent(),
	}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		author: {ID: author, MemberType: "human"},
	}}
	svc := docsvc.NewActivityService(repo, repo, memberRepo, nil)

	err := svc.DeleteComment(context.Background(), commentID, attackerProjectID, author, nil)
	if !errors.Is(err, docdom.ErrActivityNotFound) {
		t.Fatalf("expected ErrActivityNotFound for cross-project DeleteComment, got %v", err)
	}
	if repo.activities[commentID].DeletedAt != nil {
		t.Error("comment must not be deleted by a cross-project request")
	}
}
