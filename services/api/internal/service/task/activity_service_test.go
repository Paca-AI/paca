// Package tasksvc_test contains unit tests for the task activity service.
// Tests use in-memory fake repositories and do not require any infrastructure.
package tasksvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeCommentActivityRepo is a minimal in-memory taskdom.ActivityRepository
// that also serves as the taskLookup ActivitySvc uses to verify a task
// belongs to the caller's authorized project.
type fakeCommentActivityRepo struct {
	activities map[uuid.UUID]*taskdom.Activity
	tasks      map[uuid.UUID]*taskdom.Task
}

func newFakeCommentActivityRepo() *fakeCommentActivityRepo {
	return &fakeCommentActivityRepo{
		activities: make(map[uuid.UUID]*taskdom.Activity),
		tasks:      make(map[uuid.UUID]*taskdom.Task),
	}
}

func (r *fakeCommentActivityRepo) FindTaskByID(_ context.Context, id uuid.UUID) (*taskdom.Task, error) {
	t, ok := r.tasks[id]
	if !ok {
		return nil, taskdom.ErrTaskNotFound
	}
	return t, nil
}

func (r *fakeCommentActivityRepo) ListActivities(_ context.Context, taskID uuid.UUID) ([]*taskdom.Activity, error) {
	var out []*taskdom.Activity
	for _, a := range r.activities {
		if a.TaskID == taskID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *fakeCommentActivityRepo) FindActivityByID(_ context.Context, id uuid.UUID) (*taskdom.Activity, error) {
	a, ok := r.activities[id]
	if !ok {
		return nil, taskdom.ErrActivityNotFound
	}
	return a, nil
}

func (r *fakeCommentActivityRepo) CreateActivity(_ context.Context, a *taskdom.Activity) error {
	r.activities[a.ID] = a
	return nil
}

func (r *fakeCommentActivityRepo) UpdateActivity(_ context.Context, a *taskdom.Activity) error {
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
	// membersByUser maps a user UUID to the ProjectMember it resolves to.
	// Any user UUID not present here (other than the system actor) simulates
	// a genuine "not a member of this project" case.
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

func (r *fakeCommentMemberRepo) FindMemberByAgent(_ context.Context, _ uuid.UUID, agentID uuid.UUID) (*projectdom.ProjectMember, error) {
	return &projectdom.ProjectMember{ID: agentID, MemberType: "agent", AgentID: &agentID}, nil
}

// fakeAgentTrigger records TriggerCommentMention invocations so tests can
// assert whether an agent conversation was (or wasn't) started.
type fakeAgentTrigger struct {
	calls []uuid.UUID // agentID passed on each call
}

func (f *fakeAgentTrigger) TriggerCommentMention(_ context.Context, _, agentID, _, _, _ uuid.UUID, _ string) (*agentdom.AgentConversation, error) {
	f.calls = append(f.calls, agentID)
	return &agentdom.AgentConversation{ID: uuid.New()}, nil
}

// teamMentionContent builds BlockNote block content embedding a single
// @-mention whose id is mentionedID, matching the shape ExtractTeamMentionsFromBlocks parses.
func teamMentionContent(mentionedID uuid.UUID) json.RawMessage {
	blocks := []map[string]any{
		{
			"content": []map[string]any{
				{
					"type": "teamMention",
					"props": map[string]any{
						"id":   mentionedID.String(),
						"name": "bot",
					},
				},
			},
		},
	}
	b, _ := json.Marshal(blocks)
	return b
}

func validCommentContent() json.RawMessage {
	return json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"hello"}]}]`)
}

// ---------------------------------------------------------------------------
// AddComment
// ---------------------------------------------------------------------------

func TestActivitySvc_AddComment_UnidentifiedSystemActor_ReturnsClearError(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	taskID := uuid.New()
	projectID := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: projectID}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := tasksvc.NewActivityService(repo, repo, memberRepo, nil)

	_, err := svc.AddComment(context.Background(), taskdom.AddCommentInput{
		TaskID:    taskID,
		ProjectID: projectID,
		ActorID:   userdom.SystemActorUserID, // shared agent key, no X-Agent-ID
		AgentID:   nil,
		Content:   validCommentContent(),
	})

	if !errors.Is(err, taskdom.ErrCommentActorUnidentified) {
		t.Fatalf("expected ErrCommentActorUnidentified, got %v", err)
	}
	if errors.Is(err, projectdom.ErrMemberNotFound) {
		t.Errorf("clear error should not also satisfy errors.Is(ErrMemberNotFound); callers must not accidentally treat this as the generic not-a-member case")
	}
}

func TestActivitySvc_AddComment_GenuineNonMember_ReturnsMemberNotFound(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	taskID := uuid.New()
	projectID := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: projectID}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := tasksvc.NewActivityService(repo, repo, memberRepo, nil)

	realUserID := uuid.New() // a real human, just not a member of this project

	_, err := svc.AddComment(context.Background(), taskdom.AddCommentInput{
		TaskID:    taskID,
		ProjectID: projectID,
		ActorID:   realUserID,
		AgentID:   nil,
		Content:   validCommentContent(),
	})

	if !errors.Is(err, projectdom.ErrMemberNotFound) {
		t.Fatalf("expected ErrMemberNotFound for a genuine non-member, got %v", err)
	}
	if errors.Is(err, taskdom.ErrCommentActorUnidentified) {
		t.Errorf("a genuine non-member should not be rewrapped as ErrCommentActorUnidentified")
	}
}

func TestActivitySvc_AddComment_ResolvedMember_Succeeds(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	taskID := uuid.New()
	projectID := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: projectID}
	memberID := uuid.New()
	actorID := uuid.New()
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		actorID: {ID: memberID, MemberType: "human"},
	}}
	svc := tasksvc.NewActivityService(repo, repo, memberRepo, nil)

	a, err := svc.AddComment(context.Background(), taskdom.AddCommentInput{
		TaskID:    taskID,
		ProjectID: projectID,
		ActorID:   actorID,
		Content:   validCommentContent(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.ActorID == nil || *a.ActorID != memberID {
		t.Errorf("expected comment actor_id %s, got %v", memberID, a.ActorID)
	}
}

// ---------------------------------------------------------------------------
// AddComment: agent self-mention guard
// ---------------------------------------------------------------------------

// TestActivitySvc_AddComment_AgentSelfMention_DoesNotRetrigger pins the guard
// that prevents the #354 production incident: an agent re-reading its own
// prior comment and treating an embedded @mention of itself as a fresh
// instruction, spawning a new conversation for itself with no cooldown
// anywhere in the trigger path to break the resulting loop.
func TestActivitySvc_AddComment_AgentSelfMention_DoesNotRetrigger(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	taskID := uuid.New()
	projectID := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: projectID}
	agentMemberID := uuid.New() // same agent posts the comment and is mentioned in it
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	trigger := &fakeAgentTrigger{}
	svc := tasksvc.NewActivityService(repo, repo, memberRepo, nil).WithAgentTrigger(trigger)

	_, err := svc.AddComment(context.Background(), taskdom.AddCommentInput{
		TaskID:    taskID,
		ProjectID: projectID,
		ActorID:   uuid.New(),
		AgentID:   &agentMemberID,
		Content:   teamMentionContent(agentMemberID),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trigger.calls) != 0 {
		t.Errorf("agent mentioning itself should not retrigger a conversation, got %d call(s): %v", len(trigger.calls), trigger.calls)
	}
}

// TestActivitySvc_AddComment_MentioningDifferentAgent_Triggers is the
// positive control for the self-mention guard above: a mention of a
// different agent must still start a conversation as before.
func TestActivitySvc_AddComment_MentioningDifferentAgent_Triggers(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	taskID := uuid.New()
	projectID := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: projectID}
	posterAgentID := uuid.New()
	mentionedAgentID := uuid.New()
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	trigger := &fakeAgentTrigger{}
	svc := tasksvc.NewActivityService(repo, repo, memberRepo, nil).WithAgentTrigger(trigger)

	_, err := svc.AddComment(context.Background(), taskdom.AddCommentInput{
		TaskID:    taskID,
		ProjectID: projectID,
		ActorID:   uuid.New(),
		AgentID:   &posterAgentID,
		Content:   teamMentionContent(mentionedAgentID),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(trigger.calls) != 1 || trigger.calls[0] != mentionedAgentID {
		t.Errorf("expected exactly one trigger call for agent %s, got %v", mentionedAgentID, trigger.calls)
	}
}

// ---------------------------------------------------------------------------
// UpdateComment / DeleteComment
// ---------------------------------------------------------------------------

func TestActivitySvc_UpdateComment_UnidentifiedSystemActor_ReturnsClearError(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	commentID := uuid.New()
	taskID := uuid.New()
	projectID := uuid.New()
	existingAuthor := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: projectID}
	repo.activities[commentID] = &taskdom.Activity{
		ID:           commentID,
		TaskID:       taskID,
		ActivityType: taskdom.ActivityTypeComment,
		ActorID:      &existingAuthor,
		Content:      validCommentContent(),
	}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := tasksvc.NewActivityService(repo, repo, memberRepo, nil)

	_, err := svc.UpdateComment(context.Background(), commentID, projectID, userdom.SystemActorUserID, nil, validCommentContent())

	if !errors.Is(err, taskdom.ErrCommentActorUnidentified) {
		t.Fatalf("expected ErrCommentActorUnidentified, got %v", err)
	}
}

func TestActivitySvc_DeleteComment_UnidentifiedSystemActor_ReturnsClearError(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	commentID := uuid.New()
	taskID := uuid.New()
	projectID := uuid.New()
	existingAuthor := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: projectID}
	repo.activities[commentID] = &taskdom.Activity{
		ID:           commentID,
		TaskID:       taskID,
		ActivityType: taskdom.ActivityTypeComment,
		ActorID:      &existingAuthor,
		Content:      validCommentContent(),
	}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{}}
	svc := tasksvc.NewActivityService(repo, repo, memberRepo, nil)

	err := svc.DeleteComment(context.Background(), commentID, projectID, userdom.SystemActorUserID, nil)

	if !errors.Is(err, taskdom.ErrCommentActorUnidentified) {
		t.Fatalf("expected ErrCommentActorUnidentified, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Cross-project isolation tests (same bug class as GHSA-xwmv-9c7h-g947)
//
// Every activity/comment operation is authorized against the URL project,
// but the task/comment ID itself is caller-supplied. A member of project A
// must not be able to read or mutate project B's task activity by
// guessing/knowing its UUID.
// ---------------------------------------------------------------------------

func TestActivitySvc_ListActivities_WrongProject_ReturnsNotFound(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	taskID := uuid.New()
	ownerProjectID := uuid.New()
	attackerProjectID := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: ownerProjectID}
	repo.activities[uuid.New()] = &taskdom.Activity{ID: uuid.New(), TaskID: taskID, ActivityType: taskdom.ActivityTypeTaskCreated}
	svc := tasksvc.NewActivityService(repo, repo, &fakeCommentMemberRepo{}, nil)

	_, err := svc.ListActivities(context.Background(), attackerProjectID, taskID)
	if !errors.Is(err, taskdom.ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound for cross-project ListActivities, got %v", err)
	}
}

func TestActivitySvc_AddComment_WrongProject_ReturnsNotFound(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	taskID := uuid.New()
	ownerProjectID := uuid.New()
	attackerProjectID := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: ownerProjectID}
	actorID := uuid.New()
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		actorID: {ID: uuid.New(), MemberType: "human"},
	}}
	svc := tasksvc.NewActivityService(repo, repo, memberRepo, nil)

	// actorID is a legitimate member of attackerProjectID, but taskID
	// belongs to a different project — the comment must be rejected even
	// though the actor would resolve to a valid member.
	_, err := svc.AddComment(context.Background(), taskdom.AddCommentInput{
		TaskID:    taskID,
		ProjectID: attackerProjectID,
		ActorID:   actorID,
		Content:   validCommentContent(),
	})
	if !errors.Is(err, taskdom.ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound for cross-project AddComment, got %v", err)
	}
	if len(repo.activities) != 0 {
		t.Errorf("no comment should have been persisted, found %d activities", len(repo.activities))
	}
}

func TestActivitySvc_UpdateComment_WrongProject_ReturnsNotFound(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	commentID := uuid.New()
	taskID := uuid.New()
	ownerProjectID := uuid.New()
	attackerProjectID := uuid.New()
	author := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: ownerProjectID}
	repo.activities[commentID] = &taskdom.Activity{
		ID:           commentID,
		TaskID:       taskID,
		ActivityType: taskdom.ActivityTypeComment,
		ActorID:      &author,
		Content:      validCommentContent(),
	}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		author: {ID: author, MemberType: "human"},
	}}
	svc := tasksvc.NewActivityService(repo, repo, memberRepo, nil)

	_, err := svc.UpdateComment(context.Background(), commentID, attackerProjectID, author, nil, validCommentContent())
	if !errors.Is(err, taskdom.ErrActivityNotFound) {
		t.Fatalf("expected ErrActivityNotFound for cross-project UpdateComment, got %v", err)
	}
}

func TestActivitySvc_DeleteComment_WrongProject_ReturnsNotFound(t *testing.T) {
	repo := newFakeCommentActivityRepo()
	commentID := uuid.New()
	taskID := uuid.New()
	ownerProjectID := uuid.New()
	attackerProjectID := uuid.New()
	author := uuid.New()
	repo.tasks[taskID] = &taskdom.Task{ID: taskID, ProjectID: ownerProjectID}
	repo.activities[commentID] = &taskdom.Activity{
		ID:           commentID,
		TaskID:       taskID,
		ActivityType: taskdom.ActivityTypeComment,
		ActorID:      &author,
		Content:      validCommentContent(),
	}
	memberRepo := &fakeCommentMemberRepo{membersByUser: map[uuid.UUID]*projectdom.ProjectMember{
		author: {ID: author, MemberType: "human"},
	}}
	svc := tasksvc.NewActivityService(repo, repo, memberRepo, nil)

	err := svc.DeleteComment(context.Background(), commentID, attackerProjectID, author, nil)
	if !errors.Is(err, taskdom.ErrActivityNotFound) {
		t.Fatalf("expected ErrActivityNotFound for cross-project DeleteComment, got %v", err)
	}
	if repo.activities[commentID].DeletedAt != nil {
		t.Error("comment must not be deleted by a cross-project request")
	}
}
