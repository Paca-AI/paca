package agentturnsvc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/platform/authz"
)

type authCall struct {
	projectID uuid.UUID
	required  []authz.Permission
}

type fakeAuthorizer struct {
	results []bool
	calls   []authCall
}

func (f *fakeAuthorizer) HasPermissions(_ context.Context, _ uuid.UUID, projectID *uuid.UUID, _ string, required ...authz.Permission) (bool, error) {
	call := authCall{required: append([]authz.Permission(nil), required...)}
	if projectID != nil {
		call.projectID = *projectID
	}
	f.calls = append(f.calls, call)
	if len(f.results) == 0 {
		return false, errors.New("unexpected authorization call")
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result, nil
}

type fakeAgentFinder struct {
	agent *agentdom.Agent
}

func (f fakeAgentFinder) FindVisibleAgentInProject(_ context.Context, projectID, agentID uuid.UUID) (*agentdom.Agent, error) {
	if f.agent == nil || f.agent.ID != agentID || f.agent.ProjectID != projectID {
		return nil, agentdom.ErrAgentNotFound
	}
	return f.agent, nil
}

type fakeTurnRepo struct {
	agentdom.TurnRepository
	resolve        func(projectID, memberID, snapshotID uuid.UUID, sources []agentdom.SessionContextSource) ([]agentdom.TurnContextItem, error)
	create         func(agentdom.CreateSessionTurnInput) (*agentdom.TurnBundle, bool, error)
	createdReplay  *agentdom.TurnBundle
	appendReplay   *agentdom.TurnBundle
	ownerTurn      *agentdom.TurnBundle
	preparation    *agentdom.ConclusionPreparation
	confirmCalled  bool
	publication    *agentdom.ConclusionPublication
	publicationOut []agentdom.ConclusionPublicationView
	stopInput      *agentdom.StopOwnerTurnInput
	stopResult     *agentdom.TurnResult
}

func (f *fakeTurnRepo) GetOwnerCreatedChatByRequest(_ context.Context, _, _ uuid.UUID, _ string) (*agentdom.TurnBundle, error) {
	if f.createdReplay == nil {
		return nil, agentdom.ErrTurnNotFound
	}
	return f.createdReplay, nil
}

func (f *fakeTurnRepo) GetOwnerSessionTurnByIdempotency(_ context.Context, _, _, _ uuid.UUID, _ string) (*agentdom.TurnBundle, error) {
	if f.appendReplay == nil {
		return nil, agentdom.ErrTurnNotFound
	}
	return f.appendReplay, nil
}

func (f *fakeTurnRepo) ResolveContextItems(_ context.Context, projectID, memberID, snapshotID uuid.UUID, sources []agentdom.SessionContextSource) ([]agentdom.TurnContextItem, error) {
	if f.resolve == nil {
		return nil, nil
	}
	return f.resolve(projectID, memberID, snapshotID, sources)
}

func (f *fakeTurnRepo) CreateSessionTurn(_ context.Context, in agentdom.CreateSessionTurnInput) (*agentdom.TurnBundle, bool, error) {
	if f.create == nil {
		return nil, false, errors.New("unexpected create")
	}
	return f.create(in)
}

func (f *fakeTurnRepo) GetOwnerConclusionPreparation(_ context.Context, projectID, preparationID, memberID, userID uuid.UUID) (*agentdom.ConclusionPreparation, error) {
	if f.preparation == nil || f.preparation.ID != preparationID || f.preparation.ProjectID != projectID ||
		f.preparation.PreparedByMemberID != memberID || f.preparation.PreparedByUserID != userID {
		return nil, agentdom.ErrConclusionNotFound
	}
	return f.preparation, nil
}

func (f *fakeTurnRepo) GetOwnerTurn(_ context.Context, projectID, turnID, _ uuid.UUID) (*agentdom.TurnBundle, error) {
	if f.ownerTurn == nil || f.ownerTurn.Turn == nil || f.ownerTurn.Turn.ID != turnID ||
		f.ownerTurn.Turn.ProjectID == nil || *f.ownerTurn.Turn.ProjectID != projectID {
		return nil, agentdom.ErrTurnNotFound
	}
	return f.ownerTurn, nil
}

func (f *fakeTurnRepo) ConfirmConclusion(_ context.Context, _ agentdom.ConfirmConclusionInput) (*agentdom.ConclusionPublication, bool, error) {
	f.confirmCalled = true
	if f.publication != nil {
		return f.publication, false, nil
	}
	return &agentdom.ConclusionPublication{}, false, nil
}

func (f *fakeTurnRepo) ListTaskConclusionPublications(_ context.Context, _ agentdom.ConclusionPublicationListFilter) ([]agentdom.ConclusionPublicationView, bool, error) {
	return append([]agentdom.ConclusionPublicationView(nil), f.publicationOut...), false, nil
}

func (f *fakeTurnRepo) StopOwnerTurn(_ context.Context, input agentdom.StopOwnerTurnInput) (*agentdom.TurnResult, error) {
	f.stopInput = &input
	return f.stopResult, nil
}

func TestStopProjectChatTurnKeepsOwnerAndSessionScope(t *testing.T) {
	projectID, sessionID, turnID := uuid.New(), uuid.New(), uuid.New()
	actor := agentdom.ChatActor{UserID: uuid.New(), MemberID: uuid.New(), LegacyRole: "USER"}
	repo := &fakeTurnRepo{stopResult: &agentdom.TurnResult{TurnID: turnID, TerminalStatus: agentdom.TurnStatusStopped}}
	service := New(repo, fakeAgentFinder{}, &fakeAuthorizer{results: []bool{true}})
	result, err := service.StopProjectChatTurn(context.Background(), agentdom.StopProjectChatTurnInput{
		ProjectID: projectID, SessionID: sessionID, TurnID: turnID, Actor: actor,
	})
	if err != nil || result != repo.stopResult || repo.stopInput == nil {
		t.Fatalf("stop turn: result=%+v input=%+v err=%v", result, repo.stopInput, err)
	}
	if repo.stopInput.ProjectID != projectID || repo.stopInput.SessionID != sessionID ||
		repo.stopInput.TurnID != turnID || repo.stopInput.MemberID != actor.MemberID || repo.stopInput.UserID != actor.UserID {
		t.Fatalf("stop scope changed: %+v", repo.stopInput)
	}
}

func TestPrepareProjectConclusionOnlyAcceptsNewPublishedWritebacks(t *testing.T) {
	projectID := uuid.New()
	actor := agentdom.ChatActor{UserID: uuid.New(), MemberID: uuid.New()}
	relatedPublicationID := uuid.New()
	tests := []struct {
		name      string
		kind      agentdom.ConclusionKind
		relatedID *uuid.UUID
	}{
		{name: "revision", kind: agentdom.ConclusionRevised},
		{name: "withdrawal", kind: agentdom.ConclusionWithdrawn},
		{name: "published with relation", kind: agentdom.ConclusionPublished, relatedID: &relatedPublicationID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorizer := &fakeAuthorizer{}
			service := New(&fakeTurnRepo{}, fakeAgentFinder{}, authorizer)
			_, _, err := service.PrepareProjectConclusion(context.Background(), agentdom.PrepareProjectConclusionInput{
				ProjectID: projectID, SourceTurnID: uuid.New(), TargetTaskID: uuid.New(), Actor: actor,
				Kind: test.kind, RelatedPublicationID: test.relatedID,
				IdempotencyKey: "prepare-new-writeback", ExpiresAt: time.Now().Add(15 * time.Minute),
			})
			if !errors.Is(err, agentdom.ErrProjectChatInvalid) {
				t.Fatalf("expected invalid new relation command, got %v", err)
			}
			if len(authorizer.calls) != 0 {
				t.Fatalf("invalid relation command reached authorization: %#v", authorizer.calls)
			}
		})
	}
}

func TestCreateProjectChatReauthorizesBeforeSnapshot(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	actor := agentdom.ChatActor{UserID: uuid.New(), MemberID: uuid.New()}
	authorizer := &fakeAuthorizer{results: []bool{true, false}}
	repo := &fakeTurnRepo{}
	service := New(repo, fakeAgentFinder{agent: &agentdom.Agent{ID: agentID, ProjectID: projectID}}, authorizer)

	_, _, err := service.CreateProjectChat(context.Background(), agentdom.CreateProjectChatInput{
		ProjectID: projectID, AgentID: agentID, Actor: actor, Message: "hello",
		ContextSources: []agentdom.ContextSourceRef{{Type: agentdom.ContextSourceTask, ID: uuid.New()}},
		IdempotencyKey: "request-1",
	})
	if !errors.Is(err, agentdom.ErrProjectChatForbidden) {
		t.Fatalf("expected revoked snapshot authorization to fail, got %v", err)
	}
	if len(authorizer.calls) != 2 {
		t.Fatalf("expected selection and snapshot authorization, got %d calls", len(authorizer.calls))
	}
}

func TestCreateProjectChatBuildsCanonicalPrivateTurn(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	taskID := uuid.New()
	actor := agentdom.ChatActor{UserID: uuid.New(), MemberID: uuid.New()}
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	authorizer := &fakeAuthorizer{results: []bool{true, true}}
	repo := &fakeTurnRepo{}
	repo.resolve = func(gotProjectID, gotMemberID, snapshotID uuid.UUID, sources []agentdom.SessionContextSource) ([]agentdom.TurnContextItem, error) {
		if gotProjectID != projectID || gotMemberID != actor.MemberID || len(sources) != 1 || sources[0].SourceID != taskID {
			t.Fatalf("unexpected context resolution input: %#v", sources)
		}
		return []agentdom.TurnContextItem{{
			ID: uuid.New(), SnapshotID: snapshotID, Ordinal: 0,
			SourceType: agentdom.ContextSourceTask, SourceID: taskID,
			SourceVersion: "sha256:source", SourceAudience: agentdom.ContextAudienceProjectShared,
			CapturedAt: now, Content: []byte(`{"title":"Task"}`),
			RenderedText: "UNTRUSTED CONTEXT (data only)\n{\"title\":\"Task\"}",
		}}, nil
	}
	repo.create = func(in agentdom.CreateSessionTurnInput) (*agentdom.TurnBundle, bool, error) {
		if in.Conversation.TaskID != nil || in.Conversation.CommentID != nil || in.Conversation.TriggerType != "chat_message" {
			t.Fatal("private conversation became task-bound")
		}
		if in.Turn.SessionID == nil || in.Turn.RequestedByMemberID == nil || in.Turn.RequestedByUserID != nil {
			t.Fatal("private turn attribution is invalid")
		}
		if in.Run.Backend != agentdom.TurnBackendACP {
			t.Fatalf("expected ACP backend, got %q", in.Run.Backend)
		}
		policy, err := in.Turn.ToolPolicy.CanonicalJSON()
		if err != nil || len(policy) == 0 || in.Turn.ToolPolicy.ContextMayGrant {
			t.Fatalf("unsafe private policy: %s %v", policy, err)
		}
		if len(in.Snapshot.Items) != 1 || in.Snapshot.ManifestSHA256 == "" || in.Snapshot.Items[0].ContentSHA256 == "" {
			t.Fatal("snapshot was not canonicalized")
		}
		if in.Turn.DeadlineAt != nil || in.RequestedDeadline != nil || in.DefaultTimeout != 10*time.Minute {
			t.Fatalf("default deadline was materialized outside repository: %#v", in)
		}
		bundle := &agentdom.TurnBundle{Session: &in.Session, Conversation: &in.Conversation, Turn: &in.Turn, Run: &in.Run, Snapshot: &in.Snapshot}
		return bundle, false, nil
	}
	service := New(repo, fakeAgentFinder{agent: &agentdom.Agent{
		ID: agentID, ProjectID: projectID, AgentType: agentdom.AgentTypeACP, TimeoutMinutes: 10,
	}}, authorizer)
	service.now = func() time.Time { return now }

	bundle, replayed, err := service.CreateProjectChat(context.Background(), agentdom.CreateProjectChatInput{
		ProjectID: projectID, AgentID: agentID, Actor: actor, Message: " hello ",
		ContextSources: []agentdom.ContextSourceRef{{Type: agentdom.ContextSourceTask, ID: taskID}},
		IdempotencyKey: "request-1",
	})
	if err != nil || replayed || bundle == nil {
		t.Fatalf("create project chat: bundle=%v replayed=%v err=%v", bundle, replayed, err)
	}
	if bundle.Turn.InputText != "hello" || bundle.Turn.DeadlineAt != nil {
		t.Fatalf("unexpected normalized turn: %#v", bundle.Turn)
	}
}

func TestCreateProjectChatReplayUsesFrozenCommandBeforeAgentAndDeadlineValidation(t *testing.T) {
	projectID := uuid.New()
	agentID := uuid.New()
	taskID := uuid.New()
	actor := agentdom.ChatActor{UserID: uuid.New(), MemberID: uuid.New()}
	deadline := time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)
	commandHash, err := projectChatCommandSHA(true, nil, projectID, agentID, actor.MemberID,
		"hello", nil, []agentdom.ContextSourceRef{{Type: agentdom.ContextSourceTask, ID: taskID}}, &deadline)
	if err != nil {
		t.Fatal(err)
	}
	existing := &agentdom.TurnBundle{
		Turn: &agentdom.AgentTurn{ID: uuid.New(), CommandSHA256: commandHash},
		Snapshot: &agentdom.TurnContextSnapshot{Items: []agentdom.TurnContextItem{{
			SourceType: agentdom.ContextSourceTask, SourceID: taskID,
		}}},
	}
	repo := &fakeTurnRepo{createdReplay: existing}
	repo.resolve = func(_, _, _ uuid.UUID, _ []agentdom.SessionContextSource) ([]agentdom.TurnContextItem, error) {
		t.Fatal("idempotent replay rebuilt live context")
		return nil, nil
	}
	service := New(repo, fakeAgentFinder{}, &fakeAuthorizer{results: []bool{true}})
	service.now = func() time.Time { return deadline.Add(2 * time.Hour) }

	bundle, replayed, err := service.CreateProjectChat(context.Background(), agentdom.CreateProjectChatInput{
		ProjectID: projectID, AgentID: agentID, Actor: actor, Message: "hello",
		ContextSources: []agentdom.ContextSourceRef{{Type: agentdom.ContextSourceTask, ID: taskID}},
		IdempotencyKey: "request-replay", DeadlineAt: &deadline,
	})
	if err != nil || !replayed || bundle != existing {
		t.Fatalf("frozen replay: bundle=%+v replayed=%v err=%v", bundle, replayed, err)
	}
}

func TestConfirmProjectConclusionRechecksRevokedPermission(t *testing.T) {
	projectID := uuid.New()
	actor := agentdom.ChatActor{UserID: uuid.New(), MemberID: uuid.New()}
	preparationID := uuid.New()
	repo := &fakeTurnRepo{preparation: &agentdom.ConclusionPreparation{
		ID: preparationID, ProjectID: projectID,
		PreparedByMemberID: actor.MemberID, PreparedByUserID: actor.UserID,
	}}
	service := New(repo, fakeAgentFinder{}, &fakeAuthorizer{results: []bool{false}})

	_, _, err := service.ConfirmProjectConclusion(context.Background(), agentdom.ConfirmProjectConclusionInput{
		ProjectID: projectID, PreparationID: preparationID, Actor: actor,
		ExpectedVersion: 1, ExpectedSHA256: strings.Repeat("a", 64), IdempotencyKey: "confirm-1",
	})
	if !errors.Is(err, agentdom.ErrProjectChatForbidden) {
		t.Fatalf("expected revoked confirmation authorization to fail, got %v", err)
	}
	if repo.confirmCalled {
		t.Fatal("confirm reached repository after permission revocation")
	}
}

func TestConfirmProjectConclusionReturnsOwnerSessionPermalink(t *testing.T) {
	projectID := uuid.New()
	actor := agentdom.ChatActor{UserID: uuid.New(), MemberID: uuid.New()}
	preparationID, turnID, sessionID := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeTurnRepo{
		preparation: &agentdom.ConclusionPreparation{
			ID: preparationID, ProjectID: projectID, SourceTurnID: turnID,
			PreparedByMemberID: actor.MemberID, PreparedByUserID: actor.UserID,
		},
		ownerTurn: &agentdom.TurnBundle{
			Session: &agentdom.AgentChatSession{ID: sessionID},
			Turn:    &agentdom.AgentTurn{ID: turnID, ProjectID: &projectID},
		},
		publication: &agentdom.ConclusionPublication{ID: uuid.New(), SourceTurnID: turnID},
	}
	service := New(repo, fakeAgentFinder{}, &fakeAuthorizer{results: []bool{true}})
	view, replayed, err := service.ConfirmProjectConclusion(context.Background(), agentdom.ConfirmProjectConclusionInput{
		ProjectID: projectID, PreparationID: preparationID, Actor: actor,
		ExpectedVersion: 1, ExpectedSHA256: strings.Repeat("a", 64), IdempotencyKey: "confirm-link",
	})
	if err != nil || replayed || view == nil || !view.SourceAccessible ||
		view.SourceSessionID == nil || *view.SourceSessionID != sessionID ||
		view.SourceTurnID == nil || *view.SourceTurnID != turnID {
		t.Fatalf("owner publication permalink: view=%+v replayed=%v err=%v", view, replayed, err)
	}
}

func TestListTaskConclusionsRedactsPrivateSourceWithoutAgentRead(t *testing.T) {
	projectID := uuid.New()
	actor := agentdom.ChatActor{UserID: uuid.New(), MemberID: uuid.New()}
	sessionID := uuid.New()
	turnID := uuid.New()
	repo := &fakeTurnRepo{publicationOut: []agentdom.ConclusionPublicationView{{
		Publication: agentdom.ConclusionPublication{ID: uuid.New()}, SourceAccessible: true,
		SourceSessionID: &sessionID, SourceTurnID: &turnID,
	}}}
	authorizer := &fakeAuthorizer{results: []bool{false}}
	service := New(repo, fakeAgentFinder{}, authorizer)

	views, _, err := service.ListProjectTaskConclusions(context.Background(), agentdom.ConclusionPublicationListFilter{
		ProjectID: projectID, TaskID: uuid.New(),
	}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].SourceAccessible || views[0].SourceSessionID != nil || views[0].SourceTurnID != nil {
		t.Fatalf("private source was not redacted: %#v", views)
	}
	if len(authorizer.calls) != 1 || len(authorizer.calls[0].required) != 1 ||
		authorizer.calls[0].required[0] != authz.PermissionAgentsRead {
		t.Fatalf("task audience was re-authorized instead of only checking source access: %#v", authorizer.calls)
	}
}

func TestListTaskConclusionsAllowsPublicAudienceWithoutPrivateSource(t *testing.T) {
	sessionID, turnID := uuid.New(), uuid.New()
	repo := &fakeTurnRepo{publicationOut: []agentdom.ConclusionPublicationView{{
		Publication: agentdom.ConclusionPublication{ID: uuid.New()}, SourceAccessible: true,
		SourceSessionID: &sessionID, SourceTurnID: &turnID,
	}}}
	service := New(repo, fakeAgentFinder{}, &fakeAuthorizer{})
	views, _, err := service.ListProjectTaskConclusions(context.Background(), agentdom.ConclusionPublicationListFilter{
		ProjectID: uuid.New(), TaskID: uuid.New(),
	}, agentdom.ChatActor{})
	if err != nil || len(views) != 1 || views[0].SourceAccessible ||
		views[0].SourceSessionID != nil || views[0].SourceTurnID != nil {
		t.Fatalf("public task conclusion leaked private source: views=%+v err=%v", views, err)
	}
}

func TestOwnerReadPermissionFailureIsNotFound(t *testing.T) {
	service := New(&fakeTurnRepo{}, fakeAgentFinder{}, &fakeAuthorizer{results: []bool{false}})
	_, err := service.GetChatSession(context.Background(), uuid.New(), uuid.New(), agentdom.ChatActor{
		UserID: uuid.New(), MemberID: uuid.New(),
	})
	if !errors.Is(err, agentdom.ErrChatSessionNotFound) {
		t.Fatalf("expected owner-private not-found, got %v", err)
	}
}

func TestContextPermissionClassificationOnlyRequiresTasksReadForTaskSources(t *testing.T) {
	if contextRefsRequireTasksRead([]agentdom.ContextSourceRef{{Type: agentdom.ContextSourceSession, ID: uuid.New()}, {Type: agentdom.ContextSourceRun, ID: uuid.New()}}) {
		t.Fatal("session/run context incorrectly required tasks.read")
	}
	if !contextRefsRequireTasksRead([]agentdom.ContextSourceRef{{Type: agentdom.ContextSourceTask, ID: uuid.New()}}) {
		t.Fatal("task context did not require tasks.read")
	}
	if sessionSourcesRequireTasksRead([]agentdom.SessionContextSource{{SourceType: agentdom.ContextSourceRun}}) {
		t.Fatal("run selection incorrectly required tasks.read")
	}
	if !snapshotItemsRequireTasksRead([]agentdom.TurnContextItem{{SourceType: agentdom.ContextSourceTask}}) {
		t.Fatal("task snapshot did not require tasks.read")
	}
}
