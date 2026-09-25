// Package docsvc_test contains unit tests for the doc activity service.
// Tests use in-memory fake repositories and do not require any infrastructure.
package docsvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	activitydom "github.com/Paca-AI/api/internal/domain/activity"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	activitysvc "github.com/Paca-AI/api/internal/service/activity"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeCommentActivityRepo holds the docs ActivitySvc checks project
// ownership against, plus the shared in-memory activity log behind it.
type fakeCommentActivityRepo struct {
	docs map[uuid.UUID]*docdom.Document
	log  *activitysvc.MemoryRepository
}

func newFakeCommentActivityRepo() *fakeCommentActivityRepo {
	return &fakeCommentActivityRepo{
		docs: make(map[uuid.UUID]*docdom.Document),
		log:  activitysvc.NewMemoryRepository(),
	}
}

func (r *fakeCommentActivityRepo) FindDocumentByID(_ context.Context, id uuid.UUID) (*docdom.Document, error) {
	e, ok := r.docs[id]
	if !ok {
		return nil, docdom.ErrDocNotFound
	}
	return e, nil
}

// seed stores a in the activity log, under its doc's project.
func (r *fakeCommentActivityRepo) seed(a *docdom.Activity) {
	entityID := a.DocumentID
	_ = r.log.Create(context.Background(), &activitydom.Activity{
		ID:           a.ID,
		ProjectID:    r.docs[entityID].ProjectID,
		EntityType:   "doc",
		EntityID:     &entityID,
		ActorID:      a.ActorID,
		ActivityType: string(a.ActivityType),
		Content:      a.Content,
	})
}

// count returns how many entries the activity log holds for the project.
func (r *fakeCommentActivityRepo) count(projectID uuid.UUID) int {
	items, _, _ := r.log.List(context.Background(), activitydom.ListFilter{ProjectID: projectID}, 1000)
	return len(items)
}

// find returns one entry from the activity log.
func (r *fakeCommentActivityRepo) find(id uuid.UUID) *activitydom.Activity {
	a, _ := r.log.FindByID(context.Background(), id)
	return a
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
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo)

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
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo)

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
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo)

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
	repo.seed(&docdom.Activity{
		ID:           commentID,
		DocumentID:   documentID,
		ActivityType: docdom.ActivityTypeComment,
		ActorID:      &existingAuthor,
		Content:      validCommentContent(),
	})
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo)

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
	repo.seed(&docdom.Activity{
		ID:           commentID,
		DocumentID:   documentID,
		ActivityType: docdom.ActivityTypeComment,
		ActorID:      &existingAuthor,
		Content:      validCommentContent(),
	})
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo)

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
	repo.seed(&docdom.Activity{ID: uuid.New(), DocumentID: documentID, ActivityType: docdom.ActivityTypeDocCreated})
	memberRepo := &fakeCommentMemberRepo{}
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo)

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
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo)

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
	if repo.count(attackerProjectID) != 0 {
		t.Errorf("no comment should have been persisted, found %d activities", repo.count(attackerProjectID))
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
	repo.seed(&docdom.Activity{
		ID:           commentID,
		DocumentID:   documentID,
		ActivityType: docdom.ActivityTypeComment,
		ActorID:      &author,
		Content:      validCommentContent(),
	})
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		author: {ID: author, MemberType: "human"},
	}}
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo)

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
	repo.seed(&docdom.Activity{
		ID:           commentID,
		DocumentID:   documentID,
		ActivityType: docdom.ActivityTypeComment,
		ActorID:      &author,
		Content:      validCommentContent(),
	})
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		author: {ID: author, MemberType: "human"},
	}}
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo)

	err := svc.DeleteComment(context.Background(), commentID, attackerProjectID, author, nil)
	if !errors.Is(err, docdom.ErrActivityNotFound) {
		t.Fatalf("expected ErrActivityNotFound for cross-project DeleteComment, got %v", err)
	}
	if repo.find(commentID).DeletedAt != nil {
		t.Error("comment must not be deleted by a cross-project request")
	}
}
