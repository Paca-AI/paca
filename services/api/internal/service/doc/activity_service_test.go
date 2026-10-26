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
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
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

// fakeCommentMemberRepo mirrors production FindMember semantics
// closely enough to exercise the unidentified-actor branch: the
// userdom.SystemActorUserID identity is never itself a
// project member, exactly like the real repository.
type fakeCommentMemberRepo struct {
	membersByUser map[uuid.UUID]*projectdom.ProjectMember
}

func (r *fakeCommentMemberRepo) FindMember(_ context.Context, _ uuid.UUID, actorID uuid.UUID) (*projectdom.ProjectMember, error) {
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

	_, err := svc.UpdateComment(context.Background(), commentID, projectID, userdom.SystemActorUserID, validCommentContent())

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

	err := svc.DeleteComment(context.Background(), commentID, projectID, userdom.SystemActorUserID)

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

	_, _, err := svc.ListActivities(context.Background(), attackerProjectID, documentID, nil, 10)
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

	_, err := svc.UpdateComment(context.Background(), commentID, attackerProjectID, author, validCommentContent())
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

	err := svc.DeleteComment(context.Background(), commentID, attackerProjectID, author)
	if !errors.Is(err, docdom.ErrActivityNotFound) {
		t.Fatalf("expected ErrActivityNotFound for cross-project DeleteComment, got %v", err)
	}
	if repo.find(commentID).DeletedAt != nil {
		t.Error("comment must not be deleted by a cross-project request")
	}
}

// fakeDocAgentTrigger records TriggerDocCommentMention calls.
type fakeDocAgentTrigger struct {
	calls []struct{ agentID, documentID, commentID, by uuid.UUID }
	text  []string
}

func (f *fakeDocAgentTrigger) TriggerDocCommentMention(_ context.Context, _, agentID, documentID, commentID, by uuid.UUID, message string) (*agentdom.AgentConversation, error) {
	f.calls = append(f.calls, struct{ agentID, documentID, commentID, by uuid.UUID }{agentID, documentID, commentID, by})
	f.text = append(f.text, message)
	return &agentdom.AgentConversation{ID: uuid.New()}, nil
}

func TestActivitySvc_AddComment_MentionedAgentIsTriggered(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	documentID, projectID := uuid.New(), uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: projectID}

	authorUser, agentID := uuid.New(), uuid.New()
	author := &projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, UserID: authorUser}
	agentMember := &projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, MemberType: "agent", AgentID: &agentID}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		authorUser: author,
		agentID:    agentMember,
	}}
	trigger := &fakeDocAgentTrigger{}
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo).
		WithAgentTrigger(trigger, memberRepo)

	content := json.RawMessage(`[{"type":"paragraph","content":[` +
		`{"type":"teamMention","props":{"id":"` + agentID.String() + `","name":"Helper"}},` +
		`{"type":"text","text":" Option A"}]}]`)
	a, err := svc.AddComment(context.Background(), docdom.AddCommentInput{
		DocumentID: documentID, ProjectID: projectID, ActorID: authorUser, Content: content,
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if len(trigger.calls) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(trigger.calls))
	}
	c := trigger.calls[0]
	if c.agentID != agentID || c.documentID != documentID || c.commentID != a.ID || c.by != author.ID {
		t.Fatalf("unexpected trigger: %+v", c)
	}
	if trigger.text[0] != "@Helper Option A" {
		t.Fatalf("unexpected message %q", trigger.text[0])
	}
}

func TestActivitySvc_AddComment_AgentMentioningItselfDoesNotTrigger(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	documentID, projectID := uuid.New(), uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: projectID}

	agentID := uuid.New()
	agentMember := &projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, MemberType: "agent", AgentID: &agentID}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{agentID: agentMember}}
	trigger := &fakeDocAgentTrigger{}
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo).
		WithAgentTrigger(trigger, memberRepo)

	content := json.RawMessage(`[{"type":"paragraph","content":[{"type":"teamMention","props":{"id":"` + agentID.String() + `","name":"Helper"}}]}]`)
	if _, err := svc.AddComment(context.Background(), docdom.AddCommentInput{
		DocumentID: documentID, ProjectID: projectID, ActorID: agentID, Content: content,
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if len(trigger.calls) != 0 {
		t.Fatalf("self-mention must not trigger, got %d", len(trigger.calls))
	}
}

// fakeDocNotifier records NotifyDocCommentMentioned calls.
type fakeDocNotifier struct {
	inputs []notificationdom.NotifyDocCommentMentionedInput
}

func (f *fakeDocNotifier) NotifyDocCommentMentioned(_ context.Context, in notificationdom.NotifyDocCommentMentionedInput) error {
	f.inputs = append(f.inputs, in)
	return nil
}

func TestActivitySvc_AddComment_MentionedPersonIsNotified(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	documentID, projectID := uuid.New(), uuid.New()
	repo.docs[documentID] = &docdom.Document{ID: documentID, ProjectID: projectID}

	agentID, personID := uuid.New(), uuid.New()
	author := &projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, MemberType: "agent", AgentID: &agentID}
	person := &projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, UserID: personID, AccountID: personID}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		agentID:  author,
		personID: person,
	}}
	notifier := &fakeDocNotifier{}
	svc := docsvc.NewActivityService(activitysvc.New(repo.log, memberRepo, nil), repo).
		WithAgentTrigger(&fakeDocAgentTrigger{}, memberRepo).
		WithNotificationService(notifier)

	content := json.RawMessage(`[{"type":"paragraph","content":[{"type":"teamMention","props":{"id":"` + personID.String() + `","name":"Admin"}}]}]`)
	if _, err := svc.AddComment(context.Background(), docdom.AddCommentInput{
		DocumentID: documentID, ProjectID: projectID, ActorID: agentID, Content: content,
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if len(notifier.inputs) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(notifier.inputs))
	}
	in := notifier.inputs[0]
	if in.MentionedUserID != personID || in.DocumentID != documentID || in.ActorMemberID != author.ID || in.ActorID != agentID {
		t.Fatalf("unexpected notification input: %+v", in)
	}
}
