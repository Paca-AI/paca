package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/jev"
)

type fakeAutoAssignTaskService struct {
	task       *taskdom.Task
	lastUpdate *taskdom.UpdateTaskInput
	// beforeDecide, if set, runs against the live task immediately before
	// UpdateTaskAtomic's decide callback sees it — simulating a concurrent
	// write (e.g. a human's PATCH) that lands exactly once the "row lock"
	// is taken, i.e. after GetTask's earlier, now-stale read but before
	// decide's fresh one.
	beforeDecide func(task *taskdom.Task)
}

func (f *fakeAutoAssignTaskService) GetTask(_ context.Context, _, _ uuid.UUID) (*taskdom.Task, error) {
	cp := *f.task
	return &cp, nil
}

func (f *fakeAutoAssignTaskService) UpdateTaskAtomic(_ context.Context, _, _ uuid.UUID, decide func(current *taskdom.Task) (taskdom.UpdateTaskInput, bool)) (*taskdom.Task, error) {
	if f.beforeDecide != nil {
		f.beforeDecide(f.task)
	}
	current := *f.task
	in, ok := decide(&current)
	if !ok {
		return f.task, nil
	}
	f.lastUpdate = &in
	return f.task, nil
}

type fakeMemberLister struct {
	members []*projectdom.ProjectMember
}

func (f *fakeMemberLister) ListMembers(_ context.Context, _ uuid.UUID) ([]*projectdom.ProjectMember, error) {
	return f.members, nil
}

type fakeProjectReader struct {
	project *projectdom.Project
}

func (f *fakeProjectReader) GetByID(_ context.Context, _ uuid.UUID) (*projectdom.Project, error) {
	return f.project, nil
}

func newTestAutoAssignConsumer(taskSvc taskAutoAssignTaskService, memberLister projectMemberLister, projectSvc projectSettingsReader) *TaskAutoAssignConsumer {
	return &TaskAutoAssignConsumer{
		taskService:  taskSvc,
		memberLister: memberLister,
		projectSvc:   projectSvc,
		// The consumer's default transport is SSRF-safe (netguard) and
		// rejects the loopback httptest.Servers these tests point
		// jev_base_url at — see WithHTTPClient.
		jevHTTPClient: &http.Client{Timeout: 5 * time.Second},
		log:           slog.Default(),
	}
}

// projectWithSettings returns a project with a configured Jev API key (so
// jev.ClientForProject resolves to an Enabled() client) and the given
// projects.settings["jev"] sub-object.
func projectWithSettings(t *testing.T, jevSettings map[string]any) *projectdom.Project {
	t.Helper()
	return &projectdom.Project{
		ID:              uuid.New(),
		Settings:        map[string]any{"jev": jevSettings},
		JevAPIKeySecret: "test-key",
	}
}

func TestAssigneeCandidates_FiltersToHumanScope(t *testing.T) {
	humanID, agentID := uuid.New(), uuid.New()
	members := []*projectdom.ProjectMember{
		{ID: humanID, MemberType: "human", FullName: "Alice"},
		{ID: agentID, MemberType: "agent", AgentName: "Bot"},
	}
	c := newTestAutoAssignConsumer(nil, &fakeMemberLister{members: members}, nil)

	all, err := c.assigneeCandidates(context.Background(), uuid.New(), projectdom.JevAutoAssignScopeAll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected both members with scope=all, got %d", len(all))
	}

	humans, err := c.assigneeCandidates(context.Background(), uuid.New(), projectdom.JevAutoAssignScopeHuman)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(humans) != 1 || humans[0].ID != humanID {
		t.Errorf("expected only the human member with scope=human, got %v", humans)
	}
}

func TestProcessTask_NotEligibleWhenNotAutoMode(t *testing.T) {
	task := &taskdom.Task{ID: uuid.New(), AssignmentMode: taskdom.AssignmentModeManual}
	taskSvc := &fakeAutoAssignTaskService{task: task}
	project := projectWithSettings(t, map[string]any{})
	c := newTestAutoAssignConsumer(taskSvc, &fakeMemberLister{}, &fakeProjectReader{project: project})

	if err := c.processTask(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskSvc.lastUpdate != nil {
		t.Error("expected no UpdateTask call for a manual-mode task")
	}
}

func TestProcessTask_NotEligibleWhenAlreadyAssigned(t *testing.T) {
	task := &taskdom.Task{
		ID:             uuid.New(),
		AssignmentMode: taskdom.AssignmentModeAuto,
		AssigneeIDs:    []uuid.UUID{uuid.New()},
	}
	taskSvc := &fakeAutoAssignTaskService{task: task}
	project := projectWithSettings(t, map[string]any{})
	c := newTestAutoAssignConsumer(taskSvc, &fakeMemberLister{}, &fakeProjectReader{project: project})

	if err := c.processTask(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskSvc.lastUpdate != nil {
		t.Error("expected no UpdateTask call when already assigned — naturally idempotent, no retry")
	}
}

func TestProcessTask_UnconfiguredJevIsNoop(t *testing.T) {
	task := &taskdom.Task{ID: uuid.New(), AssignmentMode: taskdom.AssignmentModeAuto}
	taskSvc := &fakeAutoAssignTaskService{task: task}
	// No JevAPIKeySecret set — this project hasn't configured Jev.
	project := &projectdom.Project{ID: uuid.New()}
	c := newTestAutoAssignConsumer(taskSvc, &fakeMemberLister{}, &fakeProjectReader{project: project})

	if err := c.processTask(context.Background(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskSvc.lastUpdate != nil {
		t.Error("expected no UpdateTask call when Jev isn't configured")
	}
}

// TestProcessTask_RecordsActivityOnSuccessfulAssignment is a regression test
// for the report that assigning a task to Auto mode never resulted in an
// assignment: with taskChangedFields now tracking assignment_mode changes
// (see task_handler.go), switching a task to Auto mode always publishes a
// task.updated event, which is what wakes this consumer up in the first
// place. This test then verifies the consumer's own write — which bypasses
// TaskHandler — records its own task.updated activity too, so the resulting
// assignment shows up in the task's activity feed.
func TestProcessTask_RecordsActivityOnSuccessfulAssignment(t *testing.T) {
	memberID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		confidence := 0.9
		_ = json.NewEncoder(w).Encode(jev.Response{
			Answers: map[string]jev.Answer{
				"assignee": {Type: jev.TypeChoice, Choice: memberID.String(), Confidence: &confidence},
			},
		})
	}))
	defer srv.Close()

	taskID := uuid.New()
	task := &taskdom.Task{ID: taskID, AssignmentMode: taskdom.AssignmentModeAuto}
	taskSvc := &fakeAutoAssignTaskService{task: task}
	memberLister := &fakeMemberLister{members: []*projectdom.ProjectMember{
		{ID: memberID, MemberType: "human", FullName: "Alice"},
	}}
	project := &projectdom.Project{ID: uuid.New(), JevAPIKeySecret: "test-key", JevBaseURL: srv.URL}
	rec := &fakeActivityRecorder{}
	c := newTestAutoAssignConsumer(taskSvc, memberLister, &fakeProjectReader{project: project})
	c.activityRec = rec

	if err := c.processTask(context.Background(), uuid.New(), taskID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskSvc.lastUpdate == nil || len(*taskSvc.lastUpdate.AssigneeIDs) != 1 || (*taskSvc.lastUpdate.AssigneeIDs)[0] != memberID {
		t.Fatalf("expected the task to be assigned to %s, got %+v", memberID, taskSvc.lastUpdate)
	}

	if len(rec.recorded) != 1 {
		t.Fatalf("expected exactly one recorded activity, got %d", len(rec.recorded))
	}
	got := rec.recorded[0]
	if got.TaskID != taskID || got.ActivityType != taskdom.ActivityTypeTaskUpdated {
		t.Fatalf("unexpected recorded activity: %+v", got)
	}
	if got.ActorID != nil || got.ActorAgentID != nil {
		t.Errorf("expected a system-attributed activity (nil actor), got ActorID=%v ActorAgentID=%v", got.ActorID, got.ActorAgentID)
	}
	var payload struct {
		Changes []taskdom.FieldChange `json:"changes"`
	}
	if err := json.Unmarshal(got.Content, &payload); err != nil {
		t.Fatalf("could not decode activity content: %v", err)
	}
	if len(payload.Changes) != 1 || payload.Changes[0].Field != "assignee" {
		t.Fatalf("expected a single assignee change, got %+v", payload.Changes)
	}
}

// TestProcessTask_LowConfidenceRevertsToManualAndRecordsActivity is a
// regression test for the report that a low-confidence Jev pick left the
// task stuck showing "Auto" with no assignee and no visible explanation:
// the consumer should revert AssignmentMode to "manual" (so the UI renders
// a plain unassigned task, not a perpetually-pending "Auto" one) and record
// an ActivityTypeAutoAssignSkipped entry explaining why.
func TestProcessTask_LowConfidenceRevertsToManualAndRecordsActivity(t *testing.T) {
	memberID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		confidence := 0.34
		_ = json.NewEncoder(w).Encode(jev.Response{
			Answers: map[string]jev.Answer{
				"assignee": {Type: jev.TypeChoice, Choice: memberID.String(), Confidence: &confidence},
			},
		})
	}))
	defer srv.Close()

	taskID := uuid.New()
	task := &taskdom.Task{ID: taskID, AssignmentMode: taskdom.AssignmentModeAuto}
	taskSvc := &fakeAutoAssignTaskService{task: task}
	memberLister := &fakeMemberLister{members: []*projectdom.ProjectMember{
		{ID: memberID, MemberType: "human", FullName: "Alice"},
	}}
	project := &projectdom.Project{ID: uuid.New(), JevAPIKeySecret: "test-key", JevBaseURL: srv.URL}
	rec := &fakeActivityRecorder{}
	c := newTestAutoAssignConsumer(taskSvc, memberLister, &fakeProjectReader{project: project})
	c.activityRec = rec

	if err := c.processTask(context.Background(), uuid.New(), taskID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if taskSvc.lastUpdate == nil || taskSvc.lastUpdate.AssignmentMode == nil ||
		*taskSvc.lastUpdate.AssignmentMode != taskdom.AssignmentModeManual {
		t.Fatalf("expected AssignmentMode reverted to manual, got %+v", taskSvc.lastUpdate)
	}
	if taskSvc.lastUpdate.AssigneeIDs != nil {
		t.Errorf("expected AssigneeIDs left untouched, got %+v", *taskSvc.lastUpdate.AssigneeIDs)
	}

	if len(rec.recorded) != 1 {
		t.Fatalf("expected exactly one recorded activity, got %d", len(rec.recorded))
	}
	got := rec.recorded[0]
	if got.TaskID != taskID || got.ActivityType != taskdom.ActivityTypeAutoAssignSkipped {
		t.Fatalf("unexpected recorded activity: %+v", got)
	}
	if got.ActorID != nil || got.ActorAgentID != nil {
		t.Errorf("expected a system-attributed activity (nil actor), got ActorID=%v ActorAgentID=%v", got.ActorID, got.ActorAgentID)
	}
	var payload struct {
		Reason     string  `json:"reason"`
		Confidence float64 `json:"confidence"`
		Threshold  float64 `json:"threshold"`
	}
	if err := json.Unmarshal(got.Content, &payload); err != nil {
		t.Fatalf("could not decode activity content: %v", err)
	}
	if payload.Reason != "low_confidence" || payload.Confidence != 0.34 || payload.Threshold != assigneeConfidenceThreshold {
		t.Fatalf("unexpected activity content: %+v", payload)
	}
}

// TestProcessTask_NoSuitableCandidateLeavesUnassigned verifies a confident
// "nobody fits" answer never assigns anyone: the task reverts to manual and
// the skip is recorded with its own reason.
func TestProcessTask_NoSuitableCandidateLeavesUnassigned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		confidence := 0.9
		_ = json.NewEncoder(w).Encode(jev.Response{
			Answers: map[string]jev.Answer{
				"assignee": {Type: jev.TypeChoice, Choice: noneChoiceKey, Confidence: &confidence},
			},
		})
	}))
	defer srv.Close()

	taskID := uuid.New()
	taskSvc := &fakeAutoAssignTaskService{task: &taskdom.Task{ID: taskID, AssignmentMode: taskdom.AssignmentModeAuto}}
	memberLister := &fakeMemberLister{members: []*projectdom.ProjectMember{
		{ID: uuid.New(), MemberType: "human", FullName: "Alice"},
	}}
	project := &projectdom.Project{ID: uuid.New(), JevAPIKeySecret: "test-key", JevBaseURL: srv.URL}
	rec := &fakeActivityRecorder{}
	c := newTestAutoAssignConsumer(taskSvc, memberLister, &fakeProjectReader{project: project})
	c.activityRec = rec

	if err := c.processTask(context.Background(), uuid.New(), taskID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskSvc.lastUpdate == nil || taskSvc.lastUpdate.AssigneeIDs != nil ||
		taskSvc.lastUpdate.AssignmentMode == nil || *taskSvc.lastUpdate.AssignmentMode != taskdom.AssignmentModeManual {
		t.Fatalf("expected a revert to manual with no assignee, got %+v", taskSvc.lastUpdate)
	}
	if len(rec.recorded) != 1 || rec.recorded[0].ActivityType != taskdom.ActivityTypeAutoAssignSkipped {
		t.Fatalf("expected one skipped activity, got %+v", rec.recorded)
	}
	var payload struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(rec.recorded[0].Content, &payload)
	if payload.Reason != "no_suitable_candidate" {
		t.Errorf("expected reason no_suitable_candidate, got %q", payload.Reason)
	}
}

// TestProcessTask_DoesNotOverwriteConcurrentHumanAssignment is a regression
// test for the race UpdateTaskAtomic exists to close: a human manually
// assigning the task while the Jev call above is in flight must never be
// silently overwritten by a Jev decision that was only ever valid against
// the task's state from before that assignment happened.
func TestProcessTask_DoesNotOverwriteConcurrentHumanAssignment(t *testing.T) {
	memberID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		confidence := 0.9
		_ = json.NewEncoder(w).Encode(jev.Response{
			Answers: map[string]jev.Answer{
				"assignee": {Type: jev.TypeChoice, Choice: memberID.String(), Confidence: &confidence},
			},
		})
	}))
	defer srv.Close()

	taskID := uuid.New()
	humanPickID := uuid.New()
	task := &taskdom.Task{ID: taskID, AssignmentMode: taskdom.AssignmentModeAuto}
	taskSvc := &fakeAutoAssignTaskService{task: task}
	// Simulates a human manually assigning the task via an ordinary PATCH
	// while the Jev call (srv, above) is in flight — by the time
	// UpdateTaskAtomic's decide runs (the row is now "locked"), the task is
	// no longer eligible.
	taskSvc.beforeDecide = func(current *taskdom.Task) {
		current.AssignmentMode = taskdom.AssignmentModeManual
		current.AssigneeIDs = []uuid.UUID{humanPickID}
	}
	memberLister := &fakeMemberLister{members: []*projectdom.ProjectMember{
		{ID: memberID, MemberType: "human", FullName: "Alice"},
	}}
	project := &projectdom.Project{ID: uuid.New(), JevAPIKeySecret: "test-key", JevBaseURL: srv.URL}
	rec := &fakeActivityRecorder{}
	c := newTestAutoAssignConsumer(taskSvc, memberLister, &fakeProjectReader{project: project})
	c.activityRec = rec

	if err := c.processTask(context.Background(), uuid.New(), taskID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskSvc.lastUpdate != nil {
		t.Fatalf("expected no write once the task is no longer eligible, got %+v", taskSvc.lastUpdate)
	}
	if task.AssignmentMode != taskdom.AssignmentModeManual || len(task.AssigneeIDs) != 1 || task.AssigneeIDs[0] != humanPickID {
		t.Fatalf("expected the human's concurrent assignment to survive untouched, got mode=%v assignees=%v", task.AssignmentMode, task.AssigneeIDs)
	}
	if len(rec.recorded) != 0 {
		t.Fatalf("expected no activity recorded once the task is no longer eligible, got %+v", rec.recorded)
	}
}
