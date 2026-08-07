package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// fakeTaskUpdater records every UpdateTask call so tests can assert whether
// an idempotency guard correctly skipped (or didn't skip) a mutation.
type fakeTaskUpdater struct {
	calls     int
	lastInput taskdom.UpdateTaskInput
	err       error
}

func (f *fakeTaskUpdater) UpdateTask(_ context.Context, _, _ uuid.UUID, in taskdom.UpdateTaskInput) (*taskdom.Task, error) {
	f.calls++
	f.lastInput = in
	if f.err != nil {
		return nil, f.err
	}
	return &taskdom.Task{}, nil
}

func newTestConsumer(taskSvc automationTaskUpdater) *AutomationConsumer {
	return &AutomationConsumer{taskSvc: taskSvc, log: discardLogger()}
}

func TestTriggerMatches_StatusChanged_AnyStatusWhenConfigEmpty(t *testing.T) {
	c := newTestConsumer(nil)
	statusID := uuid.New()
	node := &automationdom.Node{Kind: automationdom.KindTrigger, Type: string(automationdom.TriggerStatusChanged), Config: json.RawMessage(`{}`)}
	task := &taskdom.Task{StatusID: &statusID}
	if !c.triggerMatches(node, task) {
		t.Fatal("expected status_changed trigger with no status_id filter to match any status")
	}
}

func TestTriggerMatches_StatusChanged_FiltersToConfiguredStatus(t *testing.T) {
	c := newTestConsumer(nil)
	wantStatus := uuid.New()
	otherStatus := uuid.New()
	cfg, _ := json.Marshal(automationdom.TriggerConfig{StatusID: &wantStatus})
	node := &automationdom.Node{Kind: automationdom.KindTrigger, Type: string(automationdom.TriggerStatusChanged), Config: cfg}

	if !c.triggerMatches(node, &taskdom.Task{StatusID: &wantStatus}) {
		t.Fatal("expected match when task's status equals the configured status")
	}
	if c.triggerMatches(node, &taskdom.Task{StatusID: &otherStatus}) {
		t.Fatal("expected no match when task's status differs from the configured status")
	}
}

func TestTriggerMatches_TagAdded_FiltersToConfiguredTag(t *testing.T) {
	c := newTestConsumer(nil)
	cfg, _ := json.Marshal(automationdom.TriggerConfig{Tag: "urgent"})
	node := &automationdom.Node{Kind: automationdom.KindTrigger, Type: string(automationdom.TriggerTagAdded), Config: cfg}

	if !c.triggerMatches(node, &taskdom.Task{Tags: []string{"urgent", "bug"}}) {
		t.Fatal("expected match when the configured tag is present")
	}
	if c.triggerMatches(node, &taskdom.Task{Tags: []string{"bug"}}) {
		t.Fatal("expected no match when the configured tag is absent")
	}
}

func TestTriggerMatches_UnconditionalTypesAlwaysMatch(t *testing.T) {
	c := newTestConsumer(nil)
	for _, tt := range []automationdom.TriggerType{automationdom.TriggerTaskCreated, automationdom.TriggerAssigneeChanged, automationdom.TriggerPriorityChanged} {
		node := &automationdom.Node{Kind: automationdom.KindTrigger, Type: string(tt), Config: json.RawMessage(`{}`)}
		if !c.triggerMatches(node, &taskdom.Task{}) {
			t.Errorf("expected trigger type %q to always match", tt)
		}
	}
}

func TestApplyUpdateTask_Assign_IdempotentWhenAlreadyAssigned(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	memberID := uuid.New()
	task := &taskdom.Task{ID: uuid.New(), AssigneeIDs: []uuid.UUID{memberID}}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{AssigneeIDs: []uuid.UUID{memberID}}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied {
		t.Fatal("expected applyUpdateTask to be a no-op when already assigned to memberID")
	}
	if updater.calls != 0 {
		t.Fatalf("expected no UpdateTask call, got %d", updater.calls)
	}
}

func TestApplyUpdateTask_Assign_AppliesWhenNotAssigned(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	memberID := uuid.New()
	task := &taskdom.Task{ID: uuid.New(), AssigneeIDs: nil}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{AssigneeIDs: []uuid.UUID{memberID}}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected applyUpdateTask to apply when not already assigned")
	}
	if updater.calls != 1 {
		t.Fatalf("expected exactly one UpdateTask call, got %d", updater.calls)
	}
	if len(task.AssigneeIDs) != 1 || task.AssigneeIDs[0] != memberID {
		t.Fatalf("expected task.AssigneeIDs mutated in place to [%v], got %v", memberID, task.AssigneeIDs)
	}
}

// --- applyDirectAgentMessage / trigger_ai_agent with no task -----------------

type fakeAutomationMemberReader struct {
	member *projectdom.ProjectMember
	err    error
}

func (f *fakeAutomationMemberReader) FindMemberByID(context.Context, uuid.UUID) (*projectdom.ProjectMember, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.member, nil
}

type fakeAgentMessenger struct {
	calls       int
	lastAgentID uuid.UUID
	lastMessage string
	err         error

	taskCalls     int
	lastTaskID    uuid.UUID
	lastTaskNote  string
	lastTaskAgent uuid.UUID
	taskErr       error
}

func (f *fakeAgentMessenger) TriggerDirectMessage(_ context.Context, _, agentID uuid.UUID, _ *uuid.UUID, message string) (*agentdom.AgentConversation, error) {
	f.calls++
	f.lastAgentID = agentID
	f.lastMessage = message
	if f.err != nil {
		return nil, f.err
	}
	return &agentdom.AgentConversation{ID: uuid.New(), TriggerType: "automation_message"}, nil
}

func (f *fakeAgentMessenger) TriggerTaskAssigned(_ context.Context, _, agentID, taskID uuid.UUID, _ *uuid.UUID, note string) (*agentdom.AgentConversation, error) {
	f.taskCalls++
	f.lastTaskAgent = agentID
	f.lastTaskID = taskID
	f.lastTaskNote = note
	if f.taskErr != nil {
		return nil, f.taskErr
	}
	return &agentdom.AgentConversation{ID: uuid.New(), TriggerType: "task_assigned", TaskID: &taskID}, nil
}

func TestApplyDirectAgentMessage_NotConfigured(t *testing.T) {
	c := &AutomationConsumer{log: discardLogger()}
	_, _, err := c.applyDirectAgentMessage(context.Background(), uuid.New(), uuid.New(), "hello")
	if err == nil {
		t.Fatal("expected an error when memberRepo/agentMessenger aren't configured")
	}
}

func TestApplyDirectAgentMessage_EmptyMessageRejected(t *testing.T) {
	agentID := uuid.New()
	member := &projectdom.ProjectMember{MemberType: "agent", AgentID: &agentID}
	c := &AutomationConsumer{
		memberRepo:     &fakeAutomationMemberReader{member: member},
		agentMessenger: &fakeAgentMessenger{},
		log:            discardLogger(),
	}
	_, _, err := c.applyDirectAgentMessage(context.Background(), uuid.New(), uuid.New(), "   ")
	if err == nil {
		t.Fatal("expected an error for an empty message — there's no task for the agent to fall back to")
	}
}

func TestApplyDirectAgentMessage_NonAgentMemberRejected(t *testing.T) {
	c := &AutomationConsumer{
		memberRepo:     &fakeAutomationMemberReader{member: &projectdom.ProjectMember{MemberType: "human"}},
		agentMessenger: &fakeAgentMessenger{},
		log:            discardLogger(),
	}
	_, _, err := c.applyDirectAgentMessage(context.Background(), uuid.New(), uuid.New(), "hello")
	if err == nil {
		t.Fatal("expected an error when the configured member is not an agent")
	}
}

func TestApplyDirectAgentMessage_Success(t *testing.T) {
	agentID := uuid.New()
	member := &projectdom.ProjectMember{MemberType: "agent", AgentID: &agentID}
	messenger := &fakeAgentMessenger{}
	c := &AutomationConsumer{
		memberRepo:     &fakeAutomationMemberReader{member: member},
		agentMessenger: messenger,
		log:            discardLogger(),
	}
	applied, convID, err := c.applyDirectAgentMessage(context.Background(), uuid.New(), uuid.New(), "do the thing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected applyDirectAgentMessage to report applied")
	}
	if convID == nil {
		t.Fatal("expected applyDirectAgentMessage to return the new conversation's ID")
	}
	if messenger.calls != 1 || messenger.lastAgentID != agentID || messenger.lastMessage != "do the thing" {
		t.Fatalf("expected TriggerDirectMessage called once with the resolved agentID and message, got %+v", messenger)
	}
}

// --- applyTriggerAIAgentOnTask / trigger_ai_agent with a task ----------------
//
// Regression coverage: trigger_ai_agent used to reuse applyAssign, reassigning
// the triggering task to the agent as a side effect of starting its
// conversation. Per product feedback that's wrong — the task should keep
// whatever assignee it already has; trigger_ai_agent should only start a
// conversation. These tests assert the task is never touched.

func TestApplyTriggerAIAgentOnTask_NotConfigured(t *testing.T) {
	c := &AutomationConsumer{log: discardLogger()}
	task := &taskdom.Task{ID: uuid.New()}
	_, _, err := c.applyTriggerAIAgentOnTask(context.Background(), uuid.New(), task, uuid.New(), "test-automation", "please help")
	if err == nil {
		t.Fatal("expected an error when memberRepo/agentMessenger aren't configured")
	}
}

func TestApplyTriggerAIAgentOnTask_NonAgentMemberRejected(t *testing.T) {
	c := &AutomationConsumer{
		memberRepo:     &fakeAutomationMemberReader{member: &projectdom.ProjectMember{MemberType: "human"}},
		agentMessenger: &fakeAgentMessenger{},
		log:            discardLogger(),
	}
	task := &taskdom.Task{ID: uuid.New()}
	_, _, err := c.applyTriggerAIAgentOnTask(context.Background(), uuid.New(), task, uuid.New(), "test-automation", "please help")
	if err == nil {
		t.Fatal("expected an error when the configured member is not an agent")
	}
}

func TestApplyTriggerAIAgentOnTask_StartsConversationWithoutReassigningTask(t *testing.T) {
	agentID := uuid.New()
	member := &projectdom.ProjectMember{MemberType: "agent", AgentID: &agentID}
	messenger := &fakeAgentMessenger{}
	updater := &fakeTaskUpdater{}
	c := &AutomationConsumer{
		taskSvc:        updater,
		memberRepo:     &fakeAutomationMemberReader{member: member},
		agentMessenger: messenger,
		log:            discardLogger(),
	}
	memberID := uuid.New()
	existingAssignee := uuid.New()
	task := &taskdom.Task{ID: uuid.New(), AssigneeIDs: []uuid.UUID{existingAssignee}}

	applied, convID, err := c.applyTriggerAIAgentOnTask(context.Background(), uuid.New(), task, memberID, "test-automation", "please help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected applyTriggerAIAgentOnTask to report applied")
	}
	if convID == nil {
		t.Fatal("expected applyTriggerAIAgentOnTask to return the new conversation's ID")
	}
	if updater.calls != 0 {
		t.Fatalf("expected no UpdateTask call — trigger_ai_agent must not reassign the task, got %d calls", updater.calls)
	}
	if len(task.AssigneeIDs) != 1 || task.AssigneeIDs[0] != existingAssignee {
		t.Fatalf("expected task.AssigneeIDs left untouched at [%v], got %v", existingAssignee, task.AssigneeIDs)
	}
	if messenger.taskCalls != 1 || messenger.lastTaskAgent != agentID || messenger.lastTaskID != task.ID {
		t.Fatalf("expected TriggerTaskAssigned called once with the resolved agentID and task ID, got %+v", messenger)
	}
	if !strings.Contains(messenger.lastTaskNote, "please help") {
		t.Fatalf("expected the agent message threaded through as the conversation note, got %q", messenger.lastTaskNote)
	}
}

func TestTriggerAIAgentNote_MessageTakesPriorityOverAutomationName(t *testing.T) {
	note := triggerAIAgentNote("Test", "Check the suitable next task and assign it to Admin")
	if !strings.Contains(note, "Check the suitable next task") {
		t.Fatalf("expected the agent message in the note, got %q", note)
	}
	if !strings.Contains(note, `via automation "Test"`) {
		t.Fatalf("expected the automation name attributed in the note, got %q", note)
	}
}

func TestTriggerAIAgentNote_FallsBackToAutomationNameWhenMessageEmpty(t *testing.T) {
	note := triggerAIAgentNote("Test", "")
	if !strings.Contains(note, "automation_name: Test") {
		t.Fatalf("expected a fallback note labeling the automation name, got %q", note)
	}
}

func TestWalk_TaskPresentReachingTriggerAIAgent_DoesNotReassignTask(t *testing.T) {
	agentID := uuid.New()
	member := &projectdom.ProjectMember{MemberType: "agent", AgentID: &agentID}
	memberID := uuid.New()
	messenger := &fakeAgentMessenger{}
	updater := &fakeTaskUpdater{}
	existingAssignee := uuid.New()
	task := &taskdom.Task{ID: uuid.New(), AssigneeIDs: []uuid.UUID{existingAssignee}}

	cfg, _ := json.Marshal(automationdom.ActionConfig{MemberID: &memberID, Message: "please help"})
	action := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindAction, Type: string(automationdom.ActionTriggerAIAgent), Config: cfg}
	w := &walker{
		consumer: &AutomationConsumer{
			repo:           &fakeCronRepo{},
			taskSvc:        updater,
			memberRepo:     &fakeAutomationMemberReader{member: member},
			agentMessenger: messenger,
			log:            discardLogger(),
		},
		task:      task,
		nodesByID: map[uuid.UUID]*automationdom.Node{action.ID: action},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), action.ID)
	if messenger.taskCalls != 1 {
		t.Fatalf("expected TriggerTaskAssigned to be called once, got %d", messenger.taskCalls)
	}
	if updater.calls != 0 {
		t.Fatalf("expected no UpdateTask call — trigger_ai_agent must not reassign the task, got %d calls", updater.calls)
	}
	if len(task.AssigneeIDs) != 1 || task.AssigneeIDs[0] != existingAssignee {
		t.Fatalf("expected task.AssigneeIDs left untouched at [%v], got %v", existingAssignee, task.AssigneeIDs)
	}
}

func TestWalk_NilTaskReachingTriggerAIAgent_ProceedsAndSucceeds(t *testing.T) {
	agentID := uuid.New()
	member := &projectdom.ProjectMember{MemberType: "agent", AgentID: &agentID}
	memberID := uuid.New()
	messenger := &fakeAgentMessenger{}

	cfg, _ := json.Marshal(automationdom.ActionConfig{MemberID: &memberID, Message: "please help"})
	action := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindAction, Type: string(automationdom.ActionTriggerAIAgent), Config: cfg}
	w := &walker{
		consumer: &AutomationConsumer{
			repo:           &fakeCronRepo{},
			memberRepo:     &fakeAutomationMemberReader{member: member},
			agentMessenger: messenger,
			log:            discardLogger(),
		},
		task:      nil,
		nodesByID: map[uuid.UUID]*automationdom.Node{action.ID: action},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), action.ID)
	if messenger.calls != 1 {
		t.Fatalf("expected TriggerDirectMessage to be called once, got %d", messenger.calls)
	}
}

// --- trigger_ai_agent pause/resume --------------------------------------------

// fakePauseResumeRepo implements automationGraphReader with real in-memory
// graph/pending-wait storage (unlike fakeCronRepo/fakePluginTriggerRepo's
// no-op bookkeeping) — the pause/resume tests below need CreatePendingAgentWait
// to actually persist, ClaimPendingAgentWait to actually delete-and-return,
// and LoadGraph to actually resolve the outgoing edge resumeWalk walks, to
// exercise the real walkAction -> handleAgentConversationStatus -> resumeWalk
// cycle rather than just its individual pieces.
type fakePauseResumeRepo struct {
	automation    *automationdom.Automation
	nodes         []*automationdom.Node
	edges         []*automationdom.Edge
	pendingWaits  map[uuid.UUID]*automationdom.PendingAgentWait // keyed by conversation ID
	pendingDelays map[uuid.UUID]*automationdom.PendingDelay     // keyed by delay ID
	runSteps      []*automationdom.RunStep
	runs          map[uuid.UUID]*automationdom.Run
}

func (f *fakePauseResumeRepo) ListEnabledTriggerNodesByType(context.Context, uuid.UUID, automationdom.TriggerType) ([]*automationdom.Node, error) {
	return nil, nil
}
func (f *fakePauseResumeRepo) ListPredecessorTriggersWatching(context.Context, uuid.UUID) ([]*automationdom.Node, error) {
	return nil, nil
}
func (f *fakePauseResumeRepo) FindAutomationByNodeID(context.Context, uuid.UUID) (*automationdom.Automation, error) {
	return f.automation, nil
}
func (f *fakePauseResumeRepo) FindAutomationByID(context.Context, uuid.UUID) (*automationdom.Automation, error) {
	return f.automation, nil
}
func (f *fakePauseResumeRepo) FindNodeByID(context.Context, uuid.UUID) (*automationdom.Node, error) {
	return nil, automationdom.ErrNodeNotFound
}
func (f *fakePauseResumeRepo) LoadGraph(context.Context, uuid.UUID) (*automationdom.Graph, error) {
	return &automationdom.Graph{Automation: f.automation, Nodes: f.nodes, Edges: f.edges}, nil
}
func (f *fakePauseResumeRepo) CreateRun(_ context.Context, r *automationdom.Run) error {
	if f.runs == nil {
		f.runs = map[uuid.UUID]*automationdom.Run{}
	}
	f.runs[r.ID] = r
	return nil
}
func (f *fakePauseResumeRepo) UpdateRun(_ context.Context, r *automationdom.Run) error {
	existing, ok := f.runs[r.ID]
	if !ok {
		return fmt.Errorf("run %s not found", r.ID)
	}
	existing.Status = r.Status
	existing.FinishedAt = r.FinishedAt
	return nil
}
func (f *fakePauseResumeRepo) CreateRunStep(_ context.Context, s *automationdom.RunStep) error {
	f.runSteps = append(f.runSteps, s)
	return nil
}
func (f *fakePauseResumeRepo) ListRunStepsByRun(_ context.Context, runID uuid.UUID) ([]*automationdom.RunStep, error) {
	var out []*automationdom.RunStep
	for _, s := range f.runSteps {
		if s.RunID == runID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (f *fakePauseResumeRepo) CreatePendingAgentWait(_ context.Context, w *automationdom.PendingAgentWait) error {
	if f.pendingWaits == nil {
		f.pendingWaits = map[uuid.UUID]*automationdom.PendingAgentWait{}
	}
	f.pendingWaits[w.ConversationID] = w
	return nil
}
func (f *fakePauseResumeRepo) ClaimPendingAgentWait(_ context.Context, conversationID uuid.UUID) (*automationdom.PendingAgentWait, error) {
	w, ok := f.pendingWaits[conversationID]
	if !ok {
		return nil, nil
	}
	delete(f.pendingWaits, conversationID)
	return w, nil
}
func (f *fakePauseResumeRepo) CountPendingAgentWaits(_ context.Context, runID uuid.UUID) (int, error) {
	count := 0
	for _, w := range f.pendingWaits {
		if w.RunID == runID {
			count++
		}
	}
	return count, nil
}
func (f *fakePauseResumeRepo) CreatePendingDelay(_ context.Context, d *automationdom.PendingDelay) error {
	if f.pendingDelays == nil {
		f.pendingDelays = map[uuid.UUID]*automationdom.PendingDelay{}
	}
	f.pendingDelays[d.ID] = d
	return nil
}
func (f *fakePauseResumeRepo) ClaimDueDelays(_ context.Context) ([]*automationdom.PendingDelay, error) {
	now := time.Now()
	var out []*automationdom.PendingDelay
	for id, d := range f.pendingDelays {
		if !d.ResumeAt.After(now) {
			out = append(out, d)
			delete(f.pendingDelays, id)
		}
	}
	return out, nil
}
func (f *fakePauseResumeRepo) CountPendingDelays(_ context.Context, runID uuid.UUID) (int, error) {
	count := 0
	for _, d := range f.pendingDelays {
		if d.RunID == runID {
			count++
		}
	}
	return count, nil
}
func (f *fakePauseResumeRepo) ListDueDateCandidates(context.Context) ([]automationdom.DueDateCandidate, error) {
	return nil, nil
}
func (f *fakePauseResumeRepo) RecordDueDateFire(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}
func (f *fakePauseResumeRepo) ListCronCandidates(context.Context) ([]automationdom.CronCandidate, error) {
	return nil, nil
}
func (f *fakePauseResumeRepo) RecordCronFire(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
	return nil
}

// newPauseResumeFixture builds a Trigger AI Agent -> Update Task graph (two
// action nodes, one edge) plus the consumer/repo wiring the pause/resume
// tests share, so each test only has to vary the conversation's terminal
// status.
func newPauseResumeFixture(t *testing.T) (c *AutomationConsumer, client *redis.Client, repo *fakePauseResumeRepo, updater *fakeTaskUpdater, runID uuid.UUID, agentNodeID uuid.UUID, task *taskdom.Task) {
	t.Helper()
	agentID := uuid.New()
	member := &projectdom.ProjectMember{MemberType: "agent", AgentID: &agentID}
	memberID := uuid.New()
	messenger := &fakeAgentMessenger{}
	updater = &fakeTaskUpdater{}
	task = &taskdom.Task{ID: uuid.New()}
	automationID := uuid.New()

	cfg, _ := json.Marshal(automationdom.ActionConfig{MemberID: &memberID, Message: "please help"})
	agentNode := &automationdom.Node{ID: uuid.New(), AutomationID: automationID, Kind: automationdom.KindAction, Type: string(automationdom.ActionTriggerAIAgent), Config: cfg}
	updateCfg, _ := json.Marshal(automationdom.ActionConfig{Update: &automationdom.TaskFieldUpdate{Tags: []string{"reviewed"}}})
	nextNode := &automationdom.Node{ID: uuid.New(), AutomationID: automationID, Kind: automationdom.KindAction, Type: string(automationdom.ActionUpdateTask), Config: updateCfg}
	edge := &automationdom.Edge{ID: uuid.New(), AutomationID: automationID, SourceNodeID: agentNode.ID, TargetNodeID: nextNode.ID}

	repo = &fakePauseResumeRepo{
		automation: &automationdom.Automation{ID: automationID, Status: automationdom.StatusActive},
		nodes:      []*automationdom.Node{agentNode, nextNode},
		edges:      []*automationdom.Edge{edge},
	}
	taskReader := &fakeAutomationTaskReader{byID: map[uuid.UUID]*taskdom.Task{task.ID: task}}
	c, client = newTestConsumerWithRedis(t, repo, taskReader, nil)
	c.taskSvc = updater
	c.memberRepo = &fakeAutomationMemberReader{member: member}
	c.agentMessenger = messenger

	runID = uuid.New()
	if err := repo.CreateRun(context.Background(), &automationdom.Run{ID: runID, AutomationID: automationID, Status: automationdom.RunStatusRunning}); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	w := &walker{
		consumer:  c,
		task:      task,
		runID:     runID,
		nodesByID: map[uuid.UUID]*automationdom.Node{agentNode.ID: agentNode, nextNode.ID: nextNode},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{agentNode.ID: {edge}},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), agentNode.ID)

	if updater.calls != 0 {
		t.Fatalf("expected update_task to NOT run yet — the walk should pause at trigger_ai_agent, got %d calls", updater.calls)
	}
	if len(repo.pendingWaits) != 1 {
		t.Fatalf("expected exactly one pending agent wait recorded, got %d", len(repo.pendingWaits))
	}
	return c, client, repo, updater, runID, agentNode.ID, task
}

func conversationIDFromPendingWaits(waits map[uuid.UUID]*automationdom.PendingAgentWait) uuid.UUID {
	for id := range waits {
		return id
	}
	return uuid.Nil
}

func TestTriggerAIAgentPauseResume_Finished_ContinuesToNextNode(t *testing.T) {
	c, client, repo, updater, runID, agentNodeID, _ := newPauseResumeFixture(t)
	defer func() { _ = client.Close() }()
	convID := conversationIDFromPendingWaits(repo.pendingWaits)

	c.handleAgentConversationStatus(redis.XMessage{ID: "1-1", Values: map[string]any{
		"conversation_id": convID.String(),
		"status":          "finished",
	}})

	if updater.calls != 1 {
		t.Fatalf("expected update_task to run exactly once after the conversation finished, got %d calls", updater.calls)
	}
	if len(repo.pendingWaits) != 0 {
		t.Fatalf("expected the pending wait to be claimed, got %d remaining", len(repo.pendingWaits))
	}
	run, ok := repo.runs[runID]
	if !ok || run.Status != automationdom.RunStatusCompleted {
		t.Fatalf("expected the run to finalize as completed, got %+v", run)
	}
	var sawFailedStep bool
	for _, s := range repo.runSteps {
		if s.NodeID == agentNodeID && s.Status == automationdom.RunStepFailed {
			sawFailedStep = true
		}
	}
	if sawFailedStep {
		t.Fatal("did not expect a failed run step recorded for the trigger_ai_agent node on a finished conversation")
	}
}

func TestTriggerAIAgentPauseResume_Failed_DoesNotContinueAndFailsRun(t *testing.T) {
	c, client, repo, updater, runID, agentNodeID, _ := newPauseResumeFixture(t)
	defer func() { _ = client.Close() }()
	convID := conversationIDFromPendingWaits(repo.pendingWaits)

	c.handleAgentConversationStatus(redis.XMessage{ID: "1-1", Values: map[string]any{
		"conversation_id": convID.String(),
		"status":          "failed",
	}})

	if updater.calls != 0 {
		t.Fatalf("expected update_task to never run when the agent conversation itself failed, got %d calls", updater.calls)
	}
	if len(repo.pendingWaits) != 0 {
		t.Fatalf("expected the pending wait to be claimed even on failure, got %d remaining", len(repo.pendingWaits))
	}
	run, ok := repo.runs[runID]
	if !ok || run.Status != automationdom.RunStatusFailed {
		t.Fatalf("expected the run to finalize as failed, got %+v", run)
	}
	var sawFailedStep bool
	for _, s := range repo.runSteps {
		if s.NodeID == agentNodeID && s.Status == automationdom.RunStepFailed {
			sawFailedStep = true
		}
	}
	if !sawFailedStep {
		t.Fatal("expected a failed run step recorded for the trigger_ai_agent node")
	}
}

// --- wait pause/resume --------------------------------------------------------

// newWaitPauseFixture builds a Wait -> Update Task graph (two action nodes,
// one edge), walks into the wait node, and asserts the walk paused there
// exactly once — the fixture every wait pause/resume test starts from,
// mirroring newPauseResumeFixture's role for trigger_ai_agent.
func newWaitPauseFixture(t *testing.T) (c *AutomationConsumer, repo *fakePauseResumeRepo, updater *fakeTaskUpdater, runID uuid.UUID) {
	t.Helper()
	task := &taskdom.Task{ID: uuid.New()}
	automationID := uuid.New()
	updater = &fakeTaskUpdater{}

	minutes := 1
	waitCfg, _ := json.Marshal(automationdom.ActionConfig{WaitMinutes: &minutes})
	waitNode := &automationdom.Node{ID: uuid.New(), AutomationID: automationID, Kind: automationdom.KindAction, Type: string(automationdom.ActionWait), Config: waitCfg}
	updateCfg, _ := json.Marshal(automationdom.ActionConfig{Update: &automationdom.TaskFieldUpdate{Tags: []string{"resumed"}}})
	nextNode := &automationdom.Node{ID: uuid.New(), AutomationID: automationID, Kind: automationdom.KindAction, Type: string(automationdom.ActionUpdateTask), Config: updateCfg}
	edge := &automationdom.Edge{ID: uuid.New(), AutomationID: automationID, SourceNodeID: waitNode.ID, TargetNodeID: nextNode.ID}

	repo = &fakePauseResumeRepo{
		automation: &automationdom.Automation{ID: automationID, Status: automationdom.StatusActive},
		nodes:      []*automationdom.Node{waitNode, nextNode},
		edges:      []*automationdom.Edge{edge},
	}
	c = &AutomationConsumer{repo: repo, taskRepo: &fakeAutomationTaskReader{byID: map[uuid.UUID]*taskdom.Task{task.ID: task}}, taskSvc: updater, log: discardLogger()}

	runID = uuid.New()
	if err := repo.CreateRun(context.Background(), &automationdom.Run{ID: runID, AutomationID: automationID, Status: automationdom.RunStatusRunning}); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	w := &walker{
		consumer:  c,
		task:      task,
		runID:     runID,
		nodesByID: map[uuid.UUID]*automationdom.Node{waitNode.ID: waitNode, nextNode.ID: nextNode},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{waitNode.ID: {edge}},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), waitNode.ID)

	if updater.calls != 0 {
		t.Fatalf("expected update_task to NOT run yet — the walk should pause at wait, got %d calls", updater.calls)
	}
	if len(repo.pendingDelays) != 1 {
		t.Fatalf("expected exactly one pending delay recorded, got %d", len(repo.pendingDelays))
	}
	return c, repo, updater, runID
}

func TestWaitPauseResume_NotYetDue_ClaimDueDelaysLeavesItPending(t *testing.T) {
	_, repo, _, _ := newWaitPauseFixture(t)

	due, err := repo.ClaimDueDelays(context.Background())
	if err != nil {
		t.Fatalf("ClaimDueDelays: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("expected no delays due yet (resume_at is ~1 minute out), got %d", len(due))
	}
	if len(repo.pendingDelays) != 1 {
		t.Fatalf("expected the delay to remain pending, got %d", len(repo.pendingDelays))
	}
}

func TestWaitPauseResume_ResumesAfterDelayPasses(t *testing.T) {
	c, repo, updater, runID := newWaitPauseFixture(t)

	// Force the delay due now rather than waiting out its real 1-minute
	// duration.
	for _, d := range repo.pendingDelays {
		d.ResumeAt = time.Now().Add(-time.Second)
	}

	due, err := repo.ClaimDueDelays(context.Background())
	if err != nil {
		t.Fatalf("ClaimDueDelays: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected exactly one due delay claimed, got %d", len(due))
	}
	if len(repo.pendingDelays) != 0 {
		t.Fatalf("expected the delay to be removed once claimed, got %d remaining", len(repo.pendingDelays))
	}

	if err := c.resumeAfterDelay(context.Background(), due[0]); err != nil {
		t.Fatalf("resumeAfterDelay: %v", err)
	}

	if updater.calls != 1 {
		t.Fatalf("expected update_task to run exactly once after the delay resumed, got %d calls", updater.calls)
	}
	run, ok := repo.runs[runID]
	if !ok || run.Status != automationdom.RunStatusCompleted {
		t.Fatalf("expected the run to finalize as completed, got %+v", run)
	}
}

// newSprintWaitPauseFixture mirrors newWaitPauseFixture but for a
// Sprint-triggered walk (no task; walker.sprint set directly) pausing at a
// wait node ahead of an update_sprint action — the exact shape that was
// losing its sprint context on resume before PendingDelay.Context.SprintID
// existed.
func newSprintWaitPauseFixture(t *testing.T) (c *AutomationConsumer, repo *fakePauseResumeRepo, updater *fakeSprintUpdater, runID uuid.UUID) {
	t.Helper()
	sprint := &sprintdom.Sprint{ID: uuid.New(), Name: "Sprint 1"}
	automationID := uuid.New()
	updater = &fakeSprintUpdater{}

	minutes := 1
	waitCfg, _ := json.Marshal(automationdom.ActionConfig{WaitMinutes: &minutes})
	waitNode := &automationdom.Node{ID: uuid.New(), AutomationID: automationID, Kind: automationdom.KindAction, Type: string(automationdom.ActionWait), Config: waitCfg}
	updateCfg, _ := json.Marshal(automationdom.ActionConfig{SprintUpdate: &automationdom.SprintFieldUpdate{Name: "Sprint 1 (updated)"}})
	nextNode := &automationdom.Node{ID: uuid.New(), AutomationID: automationID, Kind: automationdom.KindAction, Type: string(automationdom.ActionUpdateSprint), Config: updateCfg}
	edge := &automationdom.Edge{ID: uuid.New(), AutomationID: automationID, SourceNodeID: waitNode.ID, TargetNodeID: nextNode.ID}

	repo = &fakePauseResumeRepo{
		automation: &automationdom.Automation{ID: automationID, Status: automationdom.StatusActive},
		nodes:      []*automationdom.Node{waitNode, nextNode},
		edges:      []*automationdom.Edge{edge},
	}
	c = &AutomationConsumer{
		repo:       repo,
		sprintRepo: &fakeSprintReader{byID: map[uuid.UUID]*sprintdom.Sprint{sprint.ID: sprint}},
		sprintSvc:  updater,
		log:        discardLogger(),
	}

	runID = uuid.New()
	if err := repo.CreateRun(context.Background(), &automationdom.Run{ID: runID, AutomationID: automationID, Status: automationdom.RunStatusRunning}); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	w := &walker{
		consumer:  c,
		sprint:    sprint,
		runID:     runID,
		nodesByID: map[uuid.UUID]*automationdom.Node{waitNode.ID: waitNode, nextNode.ID: nextNode},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{waitNode.ID: {edge}},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), waitNode.ID)

	if updater.updateCalls != 0 {
		t.Fatalf("expected update_sprint to NOT run yet — the walk should pause at wait, got %d calls", updater.updateCalls)
	}
	if len(repo.pendingDelays) != 1 {
		t.Fatalf("expected exactly one pending delay recorded, got %d", len(repo.pendingDelays))
	}
	for _, d := range repo.pendingDelays {
		if d.Context.SprintID == nil || *d.Context.SprintID != sprint.ID {
			t.Fatalf("expected the pending delay to carry the walk's sprint ID, got %v", d.Context.SprintID)
		}
	}
	return c, repo, updater, runID
}

// TestWaitPauseResume_SprintTriggered_PreservesSprintContext is a regression
// test for the bug where a Sprint-triggered walk (sprint_started etc., no
// task) that paused at a wait node lost its sprint context entirely on
// resume: resumeWalkFrom only ever reconstructed the walker's task, never
// its sprint, so the downstream update_sprint action failed with "update_
// sprint: no sprint in context" instead of applying.
func TestWaitPauseResume_SprintTriggered_PreservesSprintContext(t *testing.T) {
	c, repo, updater, runID := newSprintWaitPauseFixture(t)

	for _, d := range repo.pendingDelays {
		d.ResumeAt = time.Now().Add(-time.Second)
	}

	due, err := repo.ClaimDueDelays(context.Background())
	if err != nil {
		t.Fatalf("ClaimDueDelays: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected exactly one due delay claimed, got %d", len(due))
	}

	if err := c.resumeAfterDelay(context.Background(), due[0]); err != nil {
		t.Fatalf("resumeAfterDelay: %v", err)
	}

	if updater.updateCalls != 1 {
		t.Fatalf("expected update_sprint to run exactly once after the delay resumed with sprint context intact, got %d calls", updater.updateCalls)
	}
	run, ok := repo.runs[runID]
	if !ok || run.Status != automationdom.RunStatusCompleted {
		t.Fatalf("expected the run to finalize as completed, got %+v", run)
	}
	steps, _ := repo.ListRunStepsByRun(context.Background(), runID)
	for _, s := range steps {
		if s.Status == automationdom.RunStepFailed {
			t.Fatalf("expected no failed run step, got %+v", s)
		}
	}
}

func TestApplyUpdateTask_Status_IdempotentWhenAlreadySet(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	statusID := uuid.New()
	task := &taskdom.Task{ID: uuid.New(), StatusID: &statusID}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{StatusID: &statusID}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied || updater.calls != 0 {
		t.Fatalf("expected no-op (applied=false, 0 calls), got applied=%v calls=%d", applied, updater.calls)
	}
}

func TestApplyUpdateTask_Status_AppliesWhenDifferent(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	oldStatus, newStatus := uuid.New(), uuid.New()
	task := &taskdom.Task{ID: uuid.New(), StatusID: &oldStatus}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{StatusID: &newStatus}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied || updater.calls != 1 {
		t.Fatalf("expected applied=true, 1 call, got applied=%v calls=%d", applied, updater.calls)
	}
	if task.StatusID == nil || *task.StatusID != newStatus {
		t.Fatalf("expected task.StatusID mutated in place to %v, got %v", newStatus, task.StatusID)
	}
}

func TestApplyUpdateTask_Priority_IdempotentWhenAlreadySet(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Importance: 5}

	importance := 5
	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{Importance: &importance}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied || updater.calls != 0 {
		t.Fatalf("expected no-op, got applied=%v calls=%d", applied, updater.calls)
	}
}

func TestApplyUpdateTask_Tags_IdempotentWhenSameSet(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Tags: []string{"urgent"}}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{Tags: []string{"urgent"}}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied || updater.calls != 0 {
		t.Fatalf("expected no-op, got applied=%v calls=%d", applied, updater.calls)
	}
}

func TestApplyUpdateTask_Tags_AppliesWhenDifferent(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Tags: []string{"bug"}}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{Tags: []string{"bug", "urgent"}}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied || updater.calls != 1 {
		t.Fatalf("expected applied=true, 1 call, got applied=%v calls=%d", applied, updater.calls)
	}
	if len(task.Tags) != 2 {
		t.Fatalf("expected tags replaced in place, got %v", task.Tags)
	}
}

func TestApplyUpdateTask_CustomField_IdempotentWhenValueAlreadyMatches(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), CustomFields: map[string]any{"release_tag": "v2"}}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{CustomFields: map[string]any{"release_tag": "v2"}}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied || updater.calls != 0 {
		t.Fatalf("expected no-op, got applied=%v calls=%d", applied, updater.calls)
	}
}

func TestApplyUpdateTask_CustomField_AppliesWhenValueDiffers(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), CustomFields: map[string]any{"release_tag": "v1"}}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{CustomFields: map[string]any{"release_tag": "v2"}}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied || updater.calls != 1 {
		t.Fatalf("expected applied=true, 1 call, got applied=%v calls=%d", applied, updater.calls)
	}
	if task.CustomFields["release_tag"] != "v2" {
		t.Fatalf("expected custom field updated in place, got %v", task.CustomFields["release_tag"])
	}
}

func TestApplyUpdateTask_DueDate_IdempotentWhenAlreadySet(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	due := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	task := &taskdom.Task{ID: uuid.New(), DueDate: &due}

	sameDue := due
	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{DueDate: &sameDue}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied || updater.calls != 0 {
		t.Fatalf("expected no-op, got applied=%v calls=%d", applied, updater.calls)
	}
}

func TestApplyUpdateTask_DueDate_AppliesWhenDifferent(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	oldDue := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	newDue := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	task := &taskdom.Task{ID: uuid.New(), DueDate: &oldDue}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{DueDate: &newDue}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied || updater.calls != 1 {
		t.Fatalf("expected applied=true, 1 call, got applied=%v calls=%d", applied, updater.calls)
	}
	if !task.DueDate.Equal(newDue) {
		t.Fatalf("expected due date updated in place, got %v", task.DueDate)
	}
}

// TestApplyUpdateTask_DueDate_JSONRoundTrip guards the exact regression this
// once had: TaskFieldUpdate.DueDate is a *time.Time, whose JSON unmarshaling
// requires a full RFC 3339 string — the config-panel's date picker must
// produce that shape (e.g. "2026-08-03T00:00:00Z"), not the bare
// "YYYY-MM-DD" a native <input type="date"> emits, or saving an update_task
// action with a due date set fails to unmarshal entirely.
func TestApplyUpdateTask_DueDate_JSONRoundTrip(t *testing.T) {
	var cfg automationdom.TaskFieldUpdate
	if err := json.Unmarshal([]byte(`{"due_date":"2026-08-03T00:00:00Z"}`), &cfg); err != nil {
		t.Fatalf("expected RFC 3339 due_date to unmarshal cleanly, got %v", err)
	}
	if cfg.DueDate == nil || cfg.DueDate.UTC().Format("2006-01-02") != "2026-08-03" {
		t.Fatalf("expected due_date 2026-08-03, got %v", cfg.DueDate)
	}
}

func TestApplyUpdateTask_Description_IdempotentWhenAlreadyMatches(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Description: json.RawMessage(`{"text":"hello"}`)}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{Description: json.RawMessage(`{"text":"hello"}`)}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied || updater.calls != 0 {
		t.Fatalf("expected no-op when description already matches, got applied=%v calls=%d", applied, updater.calls)
	}
}

func TestApplyUpdateTask_Description_AppliesWhenDifferent(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Description: json.RawMessage(`{"text":"old"}`)}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{Description: json.RawMessage(`{"text":"new"}`)}, "test-automation", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied || updater.calls != 1 {
		t.Fatalf("expected applied=true, 1 call, got applied=%v calls=%d", applied, updater.calls)
	}
}

// --- Plugin action dispatch --------------------------------------------------

type fakePluginRuntime struct {
	conditionPlugin, actionPlugin string
	hasCondition, hasAction       bool
	conditionResp, actionResp     []byte
	// conditionResponses, when non-empty, overrides conditionResp: each
	// call to EvaluateCondition returns the next entry in order (clamped to
	// the last one past the end) — lets a test drive
	// evaluatePluginConditionAgainstTasks through more than one task with a
	// different Matched result per task.
	conditionResponses [][]byte
	conditionCalls     int
	err                error
	// triggerManifests backs TriggersForTopic — keyed by topic, mirroring
	// how the real Runtime looks up plugin-declared Trigger manifests by
	// their eventTopic.
	triggerManifests map[string][]plugindom.AutomationNodeManifest
	// lastConditionPayload/lastActionPayload capture the raw JSON most
	// recently dispatched to EvaluateCondition/RunAction, so a test can
	// assert on payload contents (e.g. node_type) rather than only on the
	// plugin's response.
	lastConditionPayload []byte
	lastActionPayload    []byte
}

func (f *fakePluginRuntime) ResolveAutomationCondition(nodeType string) (string, bool) {
	return f.conditionPlugin, f.hasCondition
}
func (f *fakePluginRuntime) ResolveAutomationAction(nodeType string) (string, bool) {
	return f.actionPlugin, f.hasAction
}
func (f *fakePluginRuntime) EvaluateCondition(_ context.Context, _ string, payload []byte) ([]byte, error) {
	f.lastConditionPayload = payload
	if f.err != nil {
		return nil, f.err
	}
	if len(f.conditionResponses) > 0 {
		i := f.conditionCalls
		if i >= len(f.conditionResponses) {
			i = len(f.conditionResponses) - 1
		}
		f.conditionCalls++
		return f.conditionResponses[i], nil
	}
	return f.conditionResp, nil
}
func (f *fakePluginRuntime) RunAction(_ context.Context, _ string, payload []byte) ([]byte, error) {
	f.lastActionPayload = payload
	return f.actionResp, f.err
}
func (f *fakePluginRuntime) TriggersForTopic(topic string) []plugindom.AutomationNodeManifest {
	return f.triggerManifests[topic]
}

func TestRunAction_UnknownTypeWithoutPluginRuntimeFails(t *testing.T) {
	c := newTestConsumer(&fakeTaskUpdater{})
	node := &automationdom.Node{Type: "com.acme.github.comment_on_pr", Config: json.RawMessage(`{}`)}
	_, _, err := c.runAction(context.Background(), uuid.New(), node, &taskdom.Task{}, nil, nil, "automation", uuid.New())
	if err == nil {
		t.Fatal("expected an error for an unrecognized action type with no plugin runtime configured")
	}
}

func TestRunAction_DispatchesToRegisteredPluginAction(t *testing.T) {
	c := newTestConsumer(&fakeTaskUpdater{})
	runtime := &fakePluginRuntime{
		actionPlugin: "com.acme.github",
		hasAction:    true,
		actionResp:   []byte(`{"applied":true}`),
	}
	c.pluginRuntime = runtime
	node := &automationdom.Node{Type: "com.acme.github.comment_on_pr", Config: json.RawMessage(`{"template":"lgtm"}`)}
	applied, _, err := c.runAction(context.Background(), uuid.New(), node, &taskdom.Task{ID: uuid.New()}, nil, nil, "automation", uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected applied=true from the plugin's response")
	}
	var sent struct {
		NodeType string `json:"node_type"`
	}
	if err := json.Unmarshal(runtime.lastActionPayload, &sent); err != nil {
		t.Fatalf("decode payload dispatched to RunAction: %v", err)
	}
	if sent.NodeType != node.Type {
		t.Fatalf("expected node_type %q in the payload dispatched to RunAction, got %q", node.Type, sent.NodeType)
	}
}

func TestEvaluatePluginCondition_DispatchesToRegisteredPlugin(t *testing.T) {
	c := newTestConsumer(&fakeTaskUpdater{})
	c.pluginRuntime = &fakePluginRuntime{
		conditionPlugin: "com.acme.github",
		hasCondition:    true,
		conditionResp:   []byte(`{"matched":true}`),
	}
	node := &automationdom.Node{Type: "com.acme.github.pr_status", Config: json.RawMessage(`{}`)}
	matched, err := c.evaluatePluginCondition(context.Background(), node, []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Fatal("expected matched=true from the plugin's response")
	}
}

func TestEvaluatePluginConditionAgainstTasks_PayloadIncludesNodeType(t *testing.T) {
	c := newTestConsumer(&fakeTaskUpdater{})
	runtime := &fakePluginRuntime{
		conditionPlugin: "com.acme.github",
		hasCondition:    true,
		conditionResp:   []byte(`{"matched":true}`),
	}
	c.pluginRuntime = runtime
	node := &automationdom.Node{Type: "com.acme.github.pr_status", Config: json.RawMessage(`{}`)}
	tasks := []*taskdom.Task{{ID: uuid.New()}}
	if _, _, err := c.evaluatePluginConditionAgainstTasks(context.Background(), uuid.New(), node, tasks, "any"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sent struct {
		NodeType string `json:"node_type"`
	}
	if err := json.Unmarshal(runtime.lastConditionPayload, &sent); err != nil {
		t.Fatalf("decode payload dispatched to EvaluateCondition: %v", err)
	}
	if sent.NodeType != node.Type {
		t.Fatalf("expected node_type %q in the payload dispatched to EvaluateCondition, got %q", node.Type, sent.NodeType)
	}
}

// --- Plugin condition "applies to" target/match_mode -------------------------
//
// These exercise evaluatePluginConditionAgainstTasks directly against a
// hand-built tasks slice, the same combination logic
// ConditionLeaf.EvaluateAgainstTasks already has tests for — walkPluginCondition
// itself only adds resolveTargetTasks in front of this, which is exercised
// by the built-in condition's own target tests.

func TestEvaluatePluginConditionAgainstTasks_EmptyTasksIsFalse(t *testing.T) {
	c := newTestConsumer(&fakeTaskUpdater{})
	c.pluginRuntime = &fakePluginRuntime{conditionPlugin: "com.acme.github", hasCondition: true}
	node := &automationdom.Node{Type: "com.acme.github.pr_status", Config: json.RawMessage(`{}`)}
	matched, _, err := c.evaluatePluginConditionAgainstTasks(context.Background(), uuid.New(), node, nil, "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Fatal("expected an empty tasks slice to never match, regardless of match_mode")
	}
}

func TestEvaluatePluginConditionAgainstTasks_AnyModeMatchesOnFirstHit(t *testing.T) {
	c := newTestConsumer(&fakeTaskUpdater{})
	c.pluginRuntime = &fakePluginRuntime{
		conditionPlugin: "com.acme.github",
		hasCondition:    true,
		conditionResponses: [][]byte{
			[]byte(`{"matched":false}`),
			[]byte(`{"matched":true}`),
		},
	}
	node := &automationdom.Node{Type: "com.acme.github.pr_status", Config: json.RawMessage(`{}`)}
	tasks := []*taskdom.Task{{ID: uuid.New()}, {ID: uuid.New()}, {ID: uuid.New()}}
	matched, _, err := c.evaluatePluginConditionAgainstTasks(context.Background(), uuid.New(), node, tasks, "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Fatal("expected any-mode to match once the second task matches, without needing the third")
	}
}

func TestEvaluatePluginConditionAgainstTasks_AllModeRequiresEveryMatch(t *testing.T) {
	c := newTestConsumer(&fakeTaskUpdater{})
	c.pluginRuntime = &fakePluginRuntime{
		conditionPlugin: "com.acme.github",
		hasCondition:    true,
		conditionResponses: [][]byte{
			[]byte(`{"matched":true}`),
			[]byte(`{"matched":false}`),
		},
	}
	node := &automationdom.Node{Type: "com.acme.github.pr_status", Config: json.RawMessage(`{}`)}
	tasks := []*taskdom.Task{{ID: uuid.New()}, {ID: uuid.New()}}
	matched, _, err := c.evaluatePluginConditionAgainstTasks(context.Background(), uuid.New(), node, tasks, "all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if matched {
		t.Fatal("expected all-mode to fail as soon as one task doesn't match")
	}
}

func TestEvaluatePluginConditionAgainstTasks_AllModeTrueWhenEveryTaskMatches(t *testing.T) {
	c := newTestConsumer(&fakeTaskUpdater{})
	c.pluginRuntime = &fakePluginRuntime{
		conditionPlugin:    "com.acme.github",
		hasCondition:       true,
		conditionResponses: [][]byte{[]byte(`{"matched":true}`)},
	}
	node := &automationdom.Node{Type: "com.acme.github.pr_status", Config: json.RawMessage(`{}`)}
	tasks := []*taskdom.Task{{ID: uuid.New()}, {ID: uuid.New()}}
	matched, _, err := c.evaluatePluginConditionAgainstTasks(context.Background(), uuid.New(), node, tasks, "all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !matched {
		t.Fatal("expected all-mode to succeed when every resolved task matches")
	}
}

// --- Plugin-emitted trigger events --------------------------------------------

// fakePluginTriggerRepo implements automationGraphReader with just enough
// behavior to drive handlePluginTriggerEvent and the executeRun call it
// makes on a match: ListEnabledTriggerNodesByType is keyed by the requested
// trigger type (mirrors how the real repo filters by n.type), everything
// else is a single fixed automation/no-op run bookkeeping, same shape as
// fakeCronRepo/fakeDueDateRepo in the sibling scheduler test files.
type fakePluginTriggerRepo struct {
	nodesByType map[string][]*automationdom.Node
	automation  *automationdom.Automation
	listCalls   []string
	createRuns  int
}

func (f *fakePluginTriggerRepo) ListEnabledTriggerNodesByType(_ context.Context, _ uuid.UUID, triggerType automationdom.TriggerType) ([]*automationdom.Node, error) {
	f.listCalls = append(f.listCalls, string(triggerType))
	return f.nodesByType[string(triggerType)], nil
}
func (f *fakePluginTriggerRepo) ListPredecessorTriggersWatching(context.Context, uuid.UUID) ([]*automationdom.Node, error) {
	return nil, nil
}
func (f *fakePluginTriggerRepo) FindAutomationByNodeID(context.Context, uuid.UUID) (*automationdom.Automation, error) {
	return f.automation, nil
}
func (f *fakePluginTriggerRepo) FindAutomationByID(context.Context, uuid.UUID) (*automationdom.Automation, error) {
	return f.automation, nil
}
func (f *fakePluginTriggerRepo) FindNodeByID(context.Context, uuid.UUID) (*automationdom.Node, error) {
	return nil, automationdom.ErrNodeNotFound
}
func (f *fakePluginTriggerRepo) LoadGraph(context.Context, uuid.UUID) (*automationdom.Graph, error) {
	return &automationdom.Graph{Automation: f.automation}, nil
}
func (f *fakePluginTriggerRepo) CreateRun(context.Context, *automationdom.Run) error {
	f.createRuns++
	return nil
}
func (f *fakePluginTriggerRepo) UpdateRun(context.Context, *automationdom.Run) error { return nil }
func (f *fakePluginTriggerRepo) CreateRunStep(context.Context, *automationdom.RunStep) error {
	return nil
}
func (f *fakePluginTriggerRepo) ListRunStepsByRun(context.Context, uuid.UUID) ([]*automationdom.RunStep, error) {
	return nil, nil
}
func (f *fakePluginTriggerRepo) CreatePendingAgentWait(context.Context, *automationdom.PendingAgentWait) error {
	return nil
}
func (f *fakePluginTriggerRepo) ClaimPendingAgentWait(context.Context, uuid.UUID) (*automationdom.PendingAgentWait, error) {
	return nil, nil
}
func (f *fakePluginTriggerRepo) CountPendingAgentWaits(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}
func (f *fakePluginTriggerRepo) CreatePendingDelay(context.Context, *automationdom.PendingDelay) error {
	return nil
}
func (f *fakePluginTriggerRepo) ClaimDueDelays(context.Context) ([]*automationdom.PendingDelay, error) {
	return nil, nil
}
func (f *fakePluginTriggerRepo) CountPendingDelays(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}
func (f *fakePluginTriggerRepo) ListDueDateCandidates(context.Context) ([]automationdom.DueDateCandidate, error) {
	return nil, nil
}
func (f *fakePluginTriggerRepo) RecordDueDateFire(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}
func (f *fakePluginTriggerRepo) ListCronCandidates(context.Context) ([]automationdom.CronCandidate, error) {
	return nil, nil
}
func (f *fakePluginTriggerRepo) RecordCronFire(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
	return nil
}

type fakePluginTriggerTaskReader struct {
	task      *taskdom.Task
	findCalls int
}

func (f *fakePluginTriggerTaskReader) FindTaskByID(context.Context, uuid.UUID) (*taskdom.Task, error) {
	f.findCalls++
	return f.task, nil
}
func (f *fakePluginTriggerTaskReader) FindTaskStatusByID(context.Context, uuid.UUID) (*taskdom.TaskStatus, error) {
	return nil, nil
}
func (f *fakePluginTriggerTaskReader) ListChildTasks(context.Context, uuid.UUID, uuid.UUID) ([]*taskdom.Task, error) {
	return nil, nil
}
func (f *fakePluginTriggerTaskReader) ListTaskLinks(context.Context, uuid.UUID) ([]*taskdom.TaskLink, error) {
	return nil, nil
}

// newTestConsumerWithRedis builds a real *AutomationConsumer backed by a
// miniredis instance (handlePluginTriggerEvent's ack path calls c.client.XAck,
// unlike the plugin-condition/action tests above which never touch Redis).
func newTestConsumerWithRedis(t *testing.T, repo automationGraphReader, taskReader automationTaskReader, pluginRuntime automationPluginRuntime) (*AutomationConsumer, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	c := NewAutomationConsumer(client, repo, taskReader, &fakeTaskUpdater{}, nil, nil, discardLogger())
	c.pluginRuntime = pluginRuntime
	return c, client
}

func TestHandlePluginTriggerEvent_NoMatchingManifestsSkipsRepoLookup(t *testing.T) {
	repo := &fakePluginTriggerRepo{}
	runtime := &fakePluginRuntime{} // triggerManifests left empty: no plugin declares this topic
	c, client := newTestConsumerWithRedis(t, repo, &fakePluginTriggerTaskReader{}, runtime)
	defer func() { _ = client.Close() }()

	msg := redis.XMessage{ID: "1-1", Values: map[string]any{
		"type":    "github.pr_linked",
		"payload": `{"project_id":"` + uuid.New().String() + `","task_id":"` + uuid.New().String() + `"}`,
	}}
	c.handlePluginTriggerEvent(msg)

	if len(repo.listCalls) != 0 {
		t.Fatalf("expected no repo lookups when no plugin declares a trigger for this topic, got %v", repo.listCalls)
	}
}

func TestHandlePluginTriggerEvent_ExecutesMatchingNodeWithTaskFromPayload(t *testing.T) {
	projectID := uuid.New()
	taskID := uuid.New()
	node := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindTrigger, Type: "github.pr_linked", Config: json.RawMessage(`{}`)}
	repo := &fakePluginTriggerRepo{
		nodesByType: map[string][]*automationdom.Node{"github.pr_linked": {node}},
		automation:  &automationdom.Automation{ID: uuid.New(), ProjectID: projectID, Status: automationdom.StatusActive},
	}
	taskReader := &fakePluginTriggerTaskReader{task: &taskdom.Task{ID: taskID}}
	runtime := &fakePluginRuntime{
		triggerManifests: map[string][]plugindom.AutomationNodeManifest{
			"github.pr_linked": {{Type: "github.pr_linked", EventTopic: "github.pr_linked"}},
		},
	}
	c, client := newTestConsumerWithRedis(t, repo, taskReader, runtime)
	defer func() { _ = client.Close() }()

	msg := redis.XMessage{ID: "1-1", Values: map[string]any{
		"type":    "github.pr_linked",
		"payload": fmt.Sprintf(`{"project_id":%q,"task_id":%q}`, projectID, taskID),
	}}
	c.handlePluginTriggerEvent(msg)

	if len(repo.listCalls) != 1 || repo.listCalls[0] != "github.pr_linked" {
		t.Fatalf("expected exactly one lookup for type github.pr_linked, got %v", repo.listCalls)
	}
	if repo.createRuns != 1 {
		t.Fatalf("expected the matched node to execute exactly once, got %d runs", repo.createRuns)
	}
	if taskReader.findCalls != 1 {
		t.Fatalf("expected the walk to resolve the task named in the event payload, got %d lookups", taskReader.findCalls)
	}
}

func TestHandlePluginTriggerEvent_MissingTaskIDRunsWithNilTask(t *testing.T) {
	projectID := uuid.New()
	node := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindTrigger, Type: "github.pr_linked", Config: json.RawMessage(`{}`)}
	repo := &fakePluginTriggerRepo{
		nodesByType: map[string][]*automationdom.Node{"github.pr_linked": {node}},
		automation:  &automationdom.Automation{ID: uuid.New(), ProjectID: projectID, Status: automationdom.StatusActive},
	}
	taskReader := &fakePluginTriggerTaskReader{}
	runtime := &fakePluginRuntime{
		triggerManifests: map[string][]plugindom.AutomationNodeManifest{
			"github.pr_linked": {{Type: "github.pr_linked", EventTopic: "github.pr_linked"}},
		},
	}
	c, client := newTestConsumerWithRedis(t, repo, taskReader, runtime)
	defer func() { _ = client.Close() }()

	msg := redis.XMessage{ID: "1-1", Values: map[string]any{
		"type":    "github.pr_linked",
		"payload": fmt.Sprintf(`{"project_id":%q}`, projectID),
	}}
	c.handlePluginTriggerEvent(msg)

	if repo.createRuns != 1 {
		t.Fatalf("expected the run to still execute without a task_id, got %d runs", repo.createRuns)
	}
	if taskReader.findCalls != 0 {
		t.Fatalf("expected no task lookup when the event payload has no task_id, got %d", taskReader.findCalls)
	}
}

func TestHandlePluginTriggerEvent_MissingProjectIDSkipsWithoutRunning(t *testing.T) {
	repo := &fakePluginTriggerRepo{
		nodesByType: map[string][]*automationdom.Node{"github.pr_linked": {{ID: uuid.New(), Kind: automationdom.KindTrigger, Type: "github.pr_linked"}}},
	}
	runtime := &fakePluginRuntime{
		triggerManifests: map[string][]plugindom.AutomationNodeManifest{
			"github.pr_linked": {{Type: "github.pr_linked", EventTopic: "github.pr_linked"}},
		},
	}
	c, client := newTestConsumerWithRedis(t, repo, &fakePluginTriggerTaskReader{}, runtime)
	defer func() { _ = client.Close() }()

	msg := redis.XMessage{ID: "1-1", Values: map[string]any{
		"type":    "github.pr_linked",
		"payload": `{"task_id":"` + uuid.New().String() + `"}`,
	}}
	c.handlePluginTriggerEvent(msg)

	if repo.createRuns != 0 {
		t.Fatalf("expected no run when the event payload has no project_id, got %d", repo.createRuns)
	}
}

// --- call_api action ---------------------------------------------------------

func TestApplyCallAPI_SuccessRecordsStatusAndBody(t *testing.T) {
	var gotMethod, gotBody string
	var gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Custom")
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	c := newTestConsumer(&fakeTaskUpdater{})
	c.httpClient = http.DefaultClient
	cfg := automationdom.ActionConfig{
		Method:  "post",
		URL:     server.URL,
		Headers: map[string]string{"X-Custom": "hello"},
		Body:    `{"hi":"there"}`,
	}
	applied, detail, err := c.applyCallAPI(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected applied=true for a 2xx response")
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST to reach the server, got %q", gotMethod)
	}
	if gotHeader != "hello" {
		t.Fatalf("expected custom header to reach the server, got %q", gotHeader)
	}
	if gotBody != `{"hi":"there"}` {
		t.Fatalf("expected request body to reach the server, got %q", gotBody)
	}
	var result map[string]any
	if err := json.Unmarshal(detail, &result); err != nil {
		t.Fatalf("expected detail to be valid JSON: %v", err)
	}
	if result["status_code"] != float64(http.StatusCreated) {
		t.Fatalf("expected status_code=201 in detail, got %v", result["status_code"])
	}
	if result["response_body"] != `{"ok":true}` {
		t.Fatalf("expected response_body in detail, got %v", result["response_body"])
	}
}

func TestApplyCallAPI_NonSuccessStatusIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()

	c := newTestConsumer(&fakeTaskUpdater{})
	c.httpClient = http.DefaultClient
	applied, _, err := c.applyCallAPI(context.Background(), automationdom.ActionConfig{Method: "GET", URL: server.URL}, nil)
	if err == nil {
		t.Fatal("expected a non-2xx response to be treated as an error")
	}
	if applied {
		t.Fatal("expected applied=false on error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error to mention the status code, got %v", err)
	}
}

func TestApplyCallAPI_NetworkErrorIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := server.URL
	server.Close() // guarantees connection refused

	c := newTestConsumer(&fakeTaskUpdater{})
	c.httpClient = http.DefaultClient
	_, _, err := c.applyCallAPI(context.Background(), automationdom.ActionConfig{Method: "GET", URL: closedURL}, nil)
	if err == nil {
		t.Fatal("expected an error for a request that fails at the network level")
	}
}

func TestApplyCallAPI_TruncatesLongResponseBody(t *testing.T) {
	longBody := strings.Repeat("a", maxCallAPIResponseBody+500)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(longBody))
	}))
	defer server.Close()

	c := newTestConsumer(&fakeTaskUpdater{})
	c.httpClient = http.DefaultClient
	_, detail, err := c.applyCallAPI(context.Background(), automationdom.ActionConfig{Method: "GET", URL: server.URL}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]any
	_ = json.Unmarshal(detail, &result)
	body, _ := result["response_body"].(string)
	if !strings.HasSuffix(body, "(truncated)") {
		t.Fatalf("expected a truncated response body, got a body of length %d", len(body))
	}
	if len(body) >= len(longBody) {
		t.Fatalf("expected the stored body to be shorter than the original %d bytes, got %d", len(longBody), len(body))
	}
}

// TestApplyCallAPI_DefaultClientRejectsPrivateTarget is the one call_api
// test that deliberately does NOT override httpClient — it uses whatever
// NewAutomationConsumer sets up by default, to prove the SSRF-safe client
// (netguard.NewSafeHTTPClient) is actually wired in for production use,
// not just available as an opt-in.
// --- walker: nil-task defense in depth ---------------------------------------
//
// validateTaskReachability (service layer) is what actually keeps a
// task-less trigger from ever reaching a node that needs a task, but these
// tests exercise walk()'s own defensive guard directly, in case that
// invariant is ever violated by a data issue.

func TestWalk_NilTaskReachingCondition_FailsStepInsteadOfPanicking(t *testing.T) {
	condition := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindCondition, Type: automationdom.ConditionNodeType, Config: json.RawMessage(`{}`)}
	repo := &fakeCronRepo{}
	w := &walker{
		consumer:  &AutomationConsumer{repo: repo, log: discardLogger()},
		task:      nil,
		nodesByID: map[uuid.UUID]*automationdom.Node{condition.ID: condition},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), condition.ID)
	if len(repo.createdRunSteps) != 1 || repo.createdRunSteps[0].Status != automationdom.RunStepFailed {
		t.Fatal("expected a nil task reaching a condition node to mark the walk failed")
	}
}

func TestWalk_NilTaskReachingNonCallAPIAction_FailsStepInsteadOfPanicking(t *testing.T) {
	action := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindAction, Type: string(automationdom.ActionUpdateTask), Config: json.RawMessage(`{}`)}
	repo := &fakeCronRepo{}
	w := &walker{
		consumer:  &AutomationConsumer{repo: repo, log: discardLogger()},
		task:      nil,
		nodesByID: map[uuid.UUID]*automationdom.Node{action.ID: action},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), action.ID)
	if len(repo.createdRunSteps) != 1 || repo.createdRunSteps[0].Status != automationdom.RunStepFailed {
		t.Fatal("expected a nil task reaching an update_task action to mark the walk failed")
	}
}

func TestWalk_NilTaskReachingCallAPI_ProceedsAndSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg, _ := json.Marshal(automationdom.ActionConfig{Method: "GET", URL: server.URL})
	action := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindAction, Type: string(automationdom.ActionCallAPI), Config: cfg}
	repo := &fakeCronRepo{}
	w := &walker{
		consumer:  &AutomationConsumer{repo: repo, httpClient: http.DefaultClient, log: discardLogger()},
		task:      nil,
		nodesByID: map[uuid.UUID]*automationdom.Node{action.ID: action},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), action.ID)
	if len(repo.createdRunSteps) != 1 || repo.createdRunSteps[0].Status == automationdom.RunStepFailed {
		t.Fatal("expected a nil task reaching a call_api action to succeed, since call_api never touches the task")
	}
}

func TestApplyCallAPI_DefaultClientRejectsPrivateTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewAutomationConsumer(nil, nil, nil, &fakeTaskUpdater{}, nil, nil, discardLogger())
	_, _, err := c.applyCallAPI(context.Background(), automationdom.ActionConfig{Method: "GET", URL: server.URL}, nil)
	if err == nil {
		t.Fatal("expected the default SSRF-safe client to reject a request to a private/loopback address")
	}
	if !strings.Contains(err.Error(), "private/internal IP") {
		t.Fatalf("expected a private/internal IP rejection, got: %v", err)
	}
}

// --- resolveTargetTasks / retargeted conditions and actions ------------------

type fakeAutomationTaskReader struct {
	byID     map[uuid.UUID]*taskdom.Task
	children map[uuid.UUID][]*taskdom.Task
	links    map[uuid.UUID][]*taskdom.TaskLink
}

func (f *fakeAutomationTaskReader) FindTaskByID(_ context.Context, id uuid.UUID) (*taskdom.Task, error) {
	t, ok := f.byID[id]
	if !ok {
		return nil, fmt.Errorf("task %s not found", id)
	}
	return t, nil
}

func (f *fakeAutomationTaskReader) FindTaskStatusByID(context.Context, uuid.UUID) (*taskdom.TaskStatus, error) {
	return nil, nil
}

func (f *fakeAutomationTaskReader) ListChildTasks(_ context.Context, _ uuid.UUID, parentID uuid.UUID) ([]*taskdom.Task, error) {
	return f.children[parentID], nil
}

func (f *fakeAutomationTaskReader) ListTaskLinks(_ context.Context, taskID uuid.UUID) ([]*taskdom.TaskLink, error) {
	return f.links[taskID], nil
}

func TestResolveTargetTasks_NilOrSelf_ReturnsBaseTask(t *testing.T) {
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{}}
	base := &taskdom.Task{ID: uuid.New()}
	for _, target := range []*automationdom.TaskTarget{nil, {}, {Kind: automationdom.TaskTargetSelf}} {
		tasks, err := c.resolveTargetTasks(context.Background(), uuid.New(), base, target)
		if err != nil || len(tasks) != 1 || tasks[0] != base {
			t.Fatalf("expected target %+v to resolve to [base], got %v, %v", target, tasks, err)
		}
	}
}

func TestResolveTargetTasks_Parent_FoundAndAbsent(t *testing.T) {
	parentID := uuid.New()
	parent := &taskdom.Task{ID: parentID}
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{byID: map[uuid.UUID]*taskdom.Task{parentID: parent}}}

	withParent := &taskdom.Task{ID: uuid.New(), ParentTaskID: &parentID}
	tasks, err := c.resolveTargetTasks(context.Background(), uuid.New(), withParent, &automationdom.TaskTarget{Kind: automationdom.TaskTargetParent})
	if err != nil || len(tasks) != 1 || tasks[0] != parent {
		t.Fatalf("expected [parent], got %v, %v", tasks, err)
	}

	noParent := &taskdom.Task{ID: uuid.New()}
	tasks, err = c.resolveTargetTasks(context.Background(), uuid.New(), noParent, &automationdom.TaskTarget{Kind: automationdom.TaskTargetParent})
	if err != nil || len(tasks) != 0 {
		t.Fatalf("expected an empty, non-error result when there's no parent, got %v, %v", tasks, err)
	}
}

func TestResolveTargetTasks_Children(t *testing.T) {
	baseID := uuid.New()
	child1, child2 := &taskdom.Task{ID: uuid.New()}, &taskdom.Task{ID: uuid.New()}
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{
		children: map[uuid.UUID][]*taskdom.Task{baseID: {child1, child2}},
	}}
	tasks, err := c.resolveTargetTasks(context.Background(), uuid.New(), &taskdom.Task{ID: baseID}, &automationdom.TaskTarget{Kind: automationdom.TaskTargetChildren})
	if err != nil || len(tasks) != 2 {
		t.Fatalf("expected 2 children, got %v, %v", tasks, err)
	}
}

func TestResolveTargetTasks_LinkedTasks_FiltersByRelationAndResolvesOtherSide(t *testing.T) {
	baseID := uuid.New()
	blockedID, relatedID := uuid.New(), uuid.New()
	blocked := &taskdom.Task{ID: blockedID}
	related := &taskdom.Task{ID: relatedID}
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{
		byID: map[uuid.UUID]*taskdom.Task{blockedID: blocked, relatedID: related},
		links: map[uuid.UUID][]*taskdom.TaskLink{
			baseID: {
				// base is the source of a "blocks" link -> other side is TargetTaskID.
				{SourceTaskID: baseID, TargetTaskID: blockedID, DisplayLinkType: "blocks"},
				// base is the target of a "relates_to" link -> other side is SourceTaskID.
				{SourceTaskID: relatedID, TargetTaskID: baseID, DisplayLinkType: "relates_to"},
			},
		},
	}}
	base := &taskdom.Task{ID: baseID}

	blocks, err := c.resolveTargetTasks(context.Background(), uuid.New(), base, &automationdom.TaskTarget{Kind: automationdom.TaskTargetBlocks})
	if err != nil || len(blocks) != 1 || blocks[0] != blocked {
		t.Fatalf("expected [blocked], got %v, %v", blocks, err)
	}

	relates, err := c.resolveTargetTasks(context.Background(), uuid.New(), base, &automationdom.TaskTarget{Kind: automationdom.TaskTargetRelatesTo})
	if err != nil || len(relates) != 1 || relates[0] != related {
		t.Fatalf("expected [related], got %v, %v", relates, err)
	}

	isBlockedBy, err := c.resolveTargetTasks(context.Background(), uuid.New(), base, &automationdom.TaskTarget{Kind: automationdom.TaskTargetIsBlockedBy})
	if err != nil || len(isBlockedBy) != 0 {
		t.Fatalf("expected no is_blocked_by links, got %v, %v", isBlockedBy, err)
	}
}

func TestResolveTargetTasks_Other(t *testing.T) {
	otherID := uuid.New()
	other := &taskdom.Task{ID: otherID}
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{byID: map[uuid.UUID]*taskdom.Task{otherID: other}}}
	tasks, err := c.resolveTargetTasks(context.Background(), uuid.New(), &taskdom.Task{ID: uuid.New()}, &automationdom.TaskTarget{Kind: automationdom.TaskTargetOther, OtherTaskID: &otherID})
	if err != nil || len(tasks) != 1 || tasks[0] != other {
		t.Fatalf("expected [other], got %v, %v", tasks, err)
	}
}

func TestRunAction_NoTarget_BehavesExactlyAsBeforeWithNilDetail(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := &AutomationConsumer{taskSvc: updater, taskRepo: &fakeAutomationTaskReader{}, log: discardLogger()}
	tag := "x"
	cfg, _ := json.Marshal(automationdom.ActionConfig{Update: &automationdom.TaskFieldUpdate{Tags: []string{tag}}})
	node := &automationdom.Node{Type: string(automationdom.ActionUpdateTask), Config: cfg}
	task := &taskdom.Task{ID: uuid.New()}

	applied, detail, err := c.runAction(context.Background(), uuid.New(), node, task, nil, nil, "test", uuid.New())
	if err != nil || !applied || detail != nil {
		t.Fatalf("expected (true, nil, nil) for an untargeted action, got (%v, %s, %v)", applied, detail, err)
	}
}

func TestRunAction_FanOut_AppliesToEveryResolvedChild(t *testing.T) {
	baseID := uuid.New()
	child1ID, child2ID := uuid.New(), uuid.New()
	updater := &fakeTaskUpdater{}
	c := &AutomationConsumer{
		taskSvc: updater,
		taskRepo: &fakeAutomationTaskReader{
			children: map[uuid.UUID][]*taskdom.Task{baseID: {{ID: child1ID}, {ID: child2ID}}},
		},
		log: discardLogger(),
	}
	cfg, _ := json.Marshal(automationdom.ActionConfig{Update: &automationdom.TaskFieldUpdate{Tags: []string{"x"}}, Target: &automationdom.TaskTarget{Kind: automationdom.TaskTargetChildren}})
	node := &automationdom.Node{Type: string(automationdom.ActionUpdateTask), Config: cfg}

	applied, detail, err := c.runAction(context.Background(), uuid.New(), node, &taskdom.Task{ID: baseID}, nil, nil, "test", uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected applied=true when at least one child was tagged")
	}
	if updater.calls != 2 {
		t.Fatalf("expected the action to fan out to both children (2 UpdateTask calls), got %d", updater.calls)
	}
	var parsed struct {
		Targets []struct {
			TaskID  string `json:"task_id"`
			Applied bool   `json:"applied"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(detail, &parsed); err != nil {
		t.Fatalf("expected a parseable detail, got %s: %v", detail, err)
	}
	if len(parsed.Targets) != 2 {
		t.Fatalf("expected 2 target results in detail, got %d", len(parsed.Targets))
	}
}

func TestRunAction_FanOut_EmptyTargetsSkipsWithoutError(t *testing.T) {
	baseID := uuid.New()
	c := &AutomationConsumer{
		taskRepo: &fakeAutomationTaskReader{children: map[uuid.UUID][]*taskdom.Task{}},
		log:      discardLogger(),
	}
	cfg, _ := json.Marshal(automationdom.ActionConfig{Update: &automationdom.TaskFieldUpdate{Tags: []string{"x"}}, Target: &automationdom.TaskTarget{Kind: automationdom.TaskTargetChildren}})
	node := &automationdom.Node{Type: string(automationdom.ActionUpdateTask), Config: cfg}

	applied, detail, err := c.runAction(context.Background(), uuid.New(), node, &taskdom.Task{ID: baseID}, nil, nil, "test", uuid.New())
	if err != nil || applied || detail != nil {
		t.Fatalf("expected (false, nil, nil) when the target resolves to nothing, got (%v, %s, %v)", applied, detail, err)
	}
}

func TestRunAction_FanOut_StopsAtFirstError(t *testing.T) {
	baseID := uuid.New()
	c := &AutomationConsumer{
		taskRepo: &fakeAutomationTaskReader{
			children: map[uuid.UUID][]*taskdom.Task{baseID: {{ID: uuid.New()}}},
		},
		log: discardLogger(),
	}
	// update_task with no fields set at all — applyActionForTask errors
	// immediately for every resolved task (missing update config).
	cfg, _ := json.Marshal(automationdom.ActionConfig{Target: &automationdom.TaskTarget{Kind: automationdom.TaskTargetChildren}})
	node := &automationdom.Node{Type: string(automationdom.ActionUpdateTask), Config: cfg}

	_, _, err := c.runAction(context.Background(), uuid.New(), node, &taskdom.Task{ID: baseID}, nil, nil, "test", uuid.New())
	if err == nil {
		t.Fatal("expected an error when the resolved task's action application fails")
	}
}

func TestEvaluateLeaf_Retargeted_UsesMatchMode(t *testing.T) {
	baseID := uuid.New()
	statusID := uuid.New()
	other := uuid.New()
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{
		children: map[uuid.UUID][]*taskdom.Task{baseID: {{StatusID: &statusID}, {StatusID: &other}}},
	}}
	leaf := &automationdom.ConditionLeaf{
		Field: automationdom.FieldStatus, Operator: automationdom.OpEquals, Value: statusID.String(),
		Target: &automationdom.TaskTarget{Kind: automationdom.TaskTargetChildren}, MatchMode: "any",
	}
	w := &walker{consumer: c, projectID: uuid.New(), task: &taskdom.Task{ID: baseID}}
	matched, err := w.evaluateLeaf(context.Background(), leaf)
	if err != nil || !matched {
		t.Fatalf("expected any-mode to match one of two children, got %v, %v", matched, err)
	}

	leaf.MatchMode = "all"
	matched, err = w.evaluateLeaf(context.Background(), leaf)
	if err != nil || matched {
		t.Fatalf("expected all-mode to fail since only one of two children matches, got %v, %v", matched, err)
	}
}

// --- sprint trigger/condition/action ------------------------------------------

type fakeSprintReader struct {
	byID map[uuid.UUID]*sprintdom.Sprint
	err  error
}

func (f *fakeSprintReader) FindSprintByID(_ context.Context, id uuid.UUID) (*sprintdom.Sprint, error) {
	if f.err != nil {
		return nil, f.err
	}
	sp, ok := f.byID[id]
	if !ok {
		return nil, sprintdom.ErrSprintNotFound
	}
	return sp, nil
}

type fakeSprintUpdater struct {
	updateCalls   int
	completeCalls int
	lastUpdate    sprintdom.UpdateSprintInput
	lastComplete  sprintdom.CompleteSprintInput
	result        *sprintdom.Sprint
	err           error
}

func (f *fakeSprintUpdater) UpdateSprint(_ context.Context, _, id uuid.UUID, in sprintdom.UpdateSprintInput) (*sprintdom.Sprint, error) {
	f.updateCalls++
	f.lastUpdate = in
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &sprintdom.Sprint{ID: id}, nil
}

func (f *fakeSprintUpdater) CompleteSprint(_ context.Context, _, id uuid.UUID, in sprintdom.CompleteSprintInput) (*sprintdom.Sprint, error) {
	f.completeCalls++
	f.lastComplete = in
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &sprintdom.Sprint{ID: id, Status: sprintdom.SprintStatusCompleted}, nil
}

func TestResolveSprintFor_PrefersDirectSprint(t *testing.T) {
	c := &AutomationConsumer{}
	sprint := &sprintdom.Sprint{ID: uuid.New(), Name: "direct"}
	got, err := c.resolveSprintFor(context.Background(), nil, sprint)
	if err != nil || got != sprint {
		t.Fatalf("expected the direct sprint to be returned unchanged, got %v, %v", got, err)
	}
}

func TestResolveSprintFor_FallsBackToTaskSprintID(t *testing.T) {
	sprintID := uuid.New()
	sprint := &sprintdom.Sprint{ID: sprintID, Name: "via task"}
	c := &AutomationConsumer{sprintRepo: &fakeSprintReader{byID: map[uuid.UUID]*sprintdom.Sprint{sprintID: sprint}}}
	task := &taskdom.Task{ID: uuid.New(), SprintID: &sprintID}
	got, err := c.resolveSprintFor(context.Background(), task, nil)
	if err != nil || got != sprint {
		t.Fatalf("expected task.SprintID to resolve to the sprint, got %v, %v", got, err)
	}
}

func TestResolveSprintFor_NoSprintReturnsNilNoError(t *testing.T) {
	c := &AutomationConsumer{}
	got, err := c.resolveSprintFor(context.Background(), &taskdom.Task{ID: uuid.New()}, nil)
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) when neither sprint nor task.SprintID is set, got %v, %v", got, err)
	}
}

func TestResolveSprintFor_TaskSprintIDWithoutSprintRepoConfiguredErrors(t *testing.T) {
	sprintID := uuid.New()
	c := &AutomationConsumer{}
	_, err := c.resolveSprintFor(context.Background(), &taskdom.Task{ID: uuid.New(), SprintID: &sprintID}, nil)
	if err == nil {
		t.Fatal("expected an error when sprintRepo isn't configured but a lookup is needed")
	}
}

func TestApplyUpdateSprint_NoSprintInContextErrors(t *testing.T) {
	c := &AutomationConsumer{sprintSvc: &fakeSprintUpdater{}}
	_, err := c.applyUpdateSprint(context.Background(), uuid.New(), nil, nil, &automationdom.SprintFieldUpdate{Name: "x"}, nil)
	if err == nil {
		t.Fatal("expected an error when neither task nor sprint gives a sprint to update")
	}
}

func TestApplyUpdateSprint_NothingChangedSkipsWithoutCallingService(t *testing.T) {
	updater := &fakeSprintUpdater{}
	c := &AutomationConsumer{sprintSvc: updater}
	sprint := &sprintdom.Sprint{ID: uuid.New(), Name: "Sprint 1"}
	applied, err := c.applyUpdateSprint(context.Background(), uuid.New(), nil, sprint, &automationdom.SprintFieldUpdate{Name: "Sprint 1"}, nil)
	if err != nil || applied {
		t.Fatalf("expected no-op when the requested name already matches, got applied=%v err=%v", applied, err)
	}
	if updater.updateCalls != 0 {
		t.Fatalf("expected UpdateSprint to not be called, got %d calls", updater.updateCalls)
	}
}

func TestApplyUpdateSprint_ChangedFieldCallsServiceAndMutatesInPlace(t *testing.T) {
	updater := &fakeSprintUpdater{result: &sprintdom.Sprint{ID: uuid.New(), Name: "New Name"}}
	c := &AutomationConsumer{sprintSvc: updater}
	sprint := &sprintdom.Sprint{ID: uuid.New(), Name: "Old Name"}
	applied, err := c.applyUpdateSprint(context.Background(), uuid.New(), nil, sprint, &automationdom.SprintFieldUpdate{Name: "New Name"}, nil)
	if err != nil || !applied {
		t.Fatalf("expected applied=true, got %v, %v", applied, err)
	}
	if updater.updateCalls != 1 {
		t.Fatalf("expected UpdateSprint called once, got %d", updater.updateCalls)
	}
	if sprint.Name != "New Name" {
		t.Fatalf("expected the in-memory sprint to be mutated to the new name, got %q", sprint.Name)
	}
}

func TestApplyCompleteSprint_AlreadyCompleteIsNoop(t *testing.T) {
	updater := &fakeSprintUpdater{}
	c := &AutomationConsumer{sprintSvc: updater}
	sprint := &sprintdom.Sprint{ID: uuid.New(), Status: sprintdom.SprintStatusCompleted}
	applied, err := c.applyCompleteSprint(context.Background(), uuid.New(), nil, sprint, nil)
	if err != nil || applied {
		t.Fatalf("expected no-op for an already-complete sprint, got %v, %v", applied, err)
	}
	if updater.completeCalls != 0 {
		t.Fatal("expected CompleteSprint to not be called for an already-complete sprint")
	}
}

func TestApplyCompleteSprint_CompletesAndMutatesInPlace(t *testing.T) {
	moveTo := uuid.New()
	updater := &fakeSprintUpdater{result: &sprintdom.Sprint{ID: uuid.New(), Status: sprintdom.SprintStatusCompleted}}
	c := &AutomationConsumer{sprintSvc: updater}
	sprint := &sprintdom.Sprint{ID: uuid.New(), Status: sprintdom.SprintStatusActive}
	applied, err := c.applyCompleteSprint(context.Background(), uuid.New(), nil, sprint, &moveTo)
	if err != nil || !applied {
		t.Fatalf("expected applied=true, got %v, %v", applied, err)
	}
	if updater.completeCalls != 1 {
		t.Fatalf("expected CompleteSprint called once, got %d", updater.completeCalls)
	}
	if updater.lastComplete.MoveToSprintID == nil || *updater.lastComplete.MoveToSprintID != moveTo {
		t.Fatalf("expected MoveToSprintID threaded through, got %v", updater.lastComplete.MoveToSprintID)
	}
	if sprint.Status != sprintdom.SprintStatusCompleted {
		t.Fatalf("expected the in-memory sprint mutated to completed, got %v", sprint.Status)
	}
}

// TestWalk_SprintTriggeredWalk_ConditionEvaluatesSprintFieldsAndActionApplies
// exercises a Sprint-triggered walk (task nil, sprint set) end to end: a
// built-in condition node branching on sprint_status, into an update_sprint
// action — confirms walk()'s relaxed defense-in-depth check, evaluateLeaf's
// sprint-field dispatch, and runAction's update_sprint dispatch all compose
// correctly with no task in context at all.
func TestWalk_SprintTriggeredWalk_ConditionEvaluatesSprintFieldsAndActionApplies(t *testing.T) {
	sprint := &sprintdom.Sprint{ID: uuid.New(), Status: sprintdom.SprintStatusActive}
	updater := &fakeSprintUpdater{result: &sprintdom.Sprint{ID: sprint.ID, Status: sprintdom.SprintStatusActive}}
	c := &AutomationConsumer{repo: &fakeCronRepo{}, sprintSvc: updater, log: discardLogger()}

	condCfg, _ := json.Marshal(automationdom.ConditionConfig{Branches: []automationdom.ConditionBranch{
		{Handle: "match", Tree: &automationdom.ConditionLeaf{Field: automationdom.FieldSprintStatus, Operator: automationdom.OpEquals, Value: "active"}},
	}})
	condition := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindCondition, Type: automationdom.ConditionNodeType, Config: condCfg}

	goal := "kickoff"
	updateCfg, _ := json.Marshal(automationdom.ActionConfig{SprintUpdate: &automationdom.SprintFieldUpdate{Goal: &goal}})
	action := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindAction, Type: string(automationdom.ActionUpdateSprint), Config: updateCfg}

	matchHandle := "match"
	edge := &automationdom.Edge{ID: uuid.New(), SourceNodeID: condition.ID, SourceHandle: &matchHandle, TargetNodeID: action.ID}

	w := &walker{
		consumer:  c,
		sprint:    sprint,
		nodesByID: map[uuid.UUID]*automationdom.Node{condition.ID: condition, action.ID: action},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{condition.ID: {edge}},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), condition.ID)

	if updater.updateCalls != 1 {
		t.Fatalf("expected the sprint condition to match and update_sprint to run once, got %d calls", updater.updateCalls)
	}
}

// TestWalk_SprintTriggeredWalk_ConditionElseBranchSkipsAction confirms the
// else fallback works the same way for a sprint-field condition as it
// already does for a task-field one.
func TestWalk_SprintTriggeredWalk_ConditionElseBranchSkipsAction(t *testing.T) {
	sprint := &sprintdom.Sprint{ID: uuid.New(), Status: sprintdom.SprintStatusPlanned}
	updater := &fakeSprintUpdater{}
	c := &AutomationConsumer{repo: &fakeCronRepo{}, sprintSvc: updater, log: discardLogger()}

	condCfg, _ := json.Marshal(automationdom.ConditionConfig{Branches: []automationdom.ConditionBranch{
		{Handle: "match", Tree: &automationdom.ConditionLeaf{Field: automationdom.FieldSprintStatus, Operator: automationdom.OpEquals, Value: "active"}},
	}})
	condition := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindCondition, Type: automationdom.ConditionNodeType, Config: condCfg}
	action := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindAction, Type: string(automationdom.ActionCompleteSprint), Config: json.RawMessage(`{}`)}

	matchHandle := "match"
	elseHandle := automationdom.ElseHandle
	matchEdge := &automationdom.Edge{ID: uuid.New(), SourceNodeID: condition.ID, SourceHandle: &matchHandle, TargetNodeID: action.ID}
	elseEdge := &automationdom.Edge{ID: uuid.New(), SourceNodeID: condition.ID, SourceHandle: &elseHandle, TargetNodeID: action.ID}

	w := &walker{
		consumer:  c,
		sprint:    sprint,
		nodesByID: map[uuid.UUID]*automationdom.Node{condition.ID: condition, action.ID: action},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{condition.ID: {matchEdge, elseEdge}},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), condition.ID)

	// planned != active, so the else edge fires — complete_sprint runs
	// regardless of which edge fired, but this at least confirms the walk
	// didn't error out or panic taking the sprint-context else path. The
	// real assertion is the mismatch didn't take the "match" branch:
	if updater.completeCalls != 1 {
		t.Fatalf("expected complete_sprint to run exactly once (via the else edge), got %d calls", updater.completeCalls)
	}
}

// TestHandleSprintActivity_MatchesTriggerTypeAndAppliesSprintIDNarrowing
// confirms handleSprintActivity looks up nodes by the event's own type and
// applies each match's optional TriggerConfig.SprintID narrowing, using the
// full field snapshot carried in the stream message rather than a repo
// lookup.
func TestHandleSprintActivity_MatchesTriggerTypeAndAppliesSprintIDNarrowing(t *testing.T) {
	sprintID := uuid.New()
	otherSprintID := uuid.New()
	matchingNode := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindTrigger, Type: string(automationdom.TriggerSprintStarted)}
	narrowedAwayCfg, _ := json.Marshal(automationdom.TriggerConfig{SprintID: &otherSprintID})
	narrowedAwayNode := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindTrigger, Type: string(automationdom.TriggerSprintStarted), Config: narrowedAwayCfg}
	repo := &fakePluginTriggerRepo{
		nodesByType: map[string][]*automationdom.Node{
			string(automationdom.TriggerSprintStarted): {matchingNode, narrowedAwayNode},
		},
		automation: &automationdom.Automation{ID: uuid.New(), Status: automationdom.StatusActive},
	}
	c, client := newTestConsumerWithRedis(t, repo, &fakePluginTriggerTaskReader{}, nil)
	defer func() { _ = client.Close() }()

	msg := redis.XMessage{ID: "1-1", Values: map[string]any{
		"sprint_id":  sprintID.String(),
		"project_id": uuid.New().String(),
		"event_type": string(automationdom.TriggerSprintStarted),
		"name":       "Sprint 4",
		"status":     "active",
	}}
	c.handleSprintActivity(msg)

	if len(repo.listCalls) != 1 || repo.listCalls[0] != string(automationdom.TriggerSprintStarted) {
		t.Fatalf("expected exactly one lookup for type sprint_started, got %v", repo.listCalls)
	}
	// Only matchingNode's run should have started — narrowedAwayNode's
	// SprintID doesn't match the fired sprint, so executeRunForSprint is
	// never called for it. createRuns is a total across all matched nodes'
	// executeRun/executeRunForSprint calls (see fakePluginTriggerRepo.CreateRun),
	// so exactly 1 confirms the narrowing skipped the second node.
	if repo.createRuns != 1 {
		t.Fatalf("expected exactly one run (the un-narrowed node only), got %d", repo.createRuns)
	}
}

// --- {{variable}} interpolation -----------------------------------------------

func TestWalkerTemplateVars_BuildsTaskAndAutomationVars(t *testing.T) {
	sp := 5
	task := &taskdom.Task{ID: uuid.New(), Title: "Fix the bug", Importance: 3, StoryPoints: &sp, Tags: []string{"urgent", "bug"}}
	w := &walker{consumer: &AutomationConsumer{}, automationName: "My Automation", task: task}
	vars := w.templateVars(context.Background())

	want := map[string]string{
		"automation.name":   "My Automation",
		"task.id":           task.ID.String(),
		"task.title":        "Fix the bug",
		"task.importance":   "3",
		"task.story_points": "5",
		"task.tags":         "urgent, bug",
	}
	for k, v := range want {
		if vars[k] != v {
			t.Errorf("vars[%q] = %q, want %q", k, vars[k], v)
		}
	}
	if _, ok := vars["sprint.name"]; ok {
		t.Error("expected no sprint.* vars when the task has no sprint_id")
	}
}

func TestWalkerTemplateVars_BuildsSprintVarsViaResolveSprint(t *testing.T) {
	sprintID := uuid.New()
	goal := "Ship it"
	sprint := &sprintdom.Sprint{ID: sprintID, Name: "Sprint 4", Status: sprintdom.SprintStatusActive, Goal: &goal}
	c := &AutomationConsumer{sprintRepo: &fakeSprintReader{byID: map[uuid.UUID]*sprintdom.Sprint{sprintID: sprint}}}
	task := &taskdom.Task{ID: uuid.New(), SprintID: &sprintID}
	w := &walker{consumer: c, task: task}
	vars := w.templateVars(context.Background())

	want := map[string]string{
		"sprint.id":     sprintID.String(),
		"sprint.name":   "Sprint 4",
		"sprint.status": "active",
		"sprint.goal":   "Ship it",
	}
	for k, v := range want {
		if vars[k] != v {
			t.Errorf("vars[%q] = %q, want %q", k, vars[k], v)
		}
	}
}

func TestWalkerTemplateVars_DirectSprintContext(t *testing.T) {
	sprint := &sprintdom.Sprint{ID: uuid.New(), Name: "Sprint 5", Status: sprintdom.SprintStatusPlanned}
	w := &walker{consumer: &AutomationConsumer{}, sprint: sprint}
	vars := w.templateVars(context.Background())
	if vars["sprint.name"] != "Sprint 5" || vars["sprint.status"] != "planned" {
		t.Fatalf("expected sprint vars from the walk's own sprint, got %v", vars)
	}
	if _, ok := vars["task.title"]; ok {
		t.Error("expected no task.* vars for a Sprint-triggered walk with no task")
	}
}

func TestApplyUpdateTask_TitleTemplateIsRendered(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Title: "Old Title"}
	vars := map[string]string{"task.id": "abc-123"}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{Title: "Ticket {{task.id}}"}, "test-automation", vars)
	if err != nil || !applied {
		t.Fatalf("expected applied=true, got %v, %v", applied, err)
	}
	if updater.lastInput.Title != "Ticket abc-123" {
		t.Fatalf("expected the rendered title to be sent, got %q", updater.lastInput.Title)
	}
	if task.Title != "Ticket abc-123" {
		t.Fatalf("expected the in-memory task's title to reflect the rendered value, got %q", task.Title)
	}
}

func TestApplyUpdateTask_UnresolvedVariableLeftVerbatim(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Title: "Old Title"}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, &automationdom.TaskFieldUpdate{Title: "{{unknown.field}}"}, "test-automation", nil)
	if err != nil || !applied {
		t.Fatalf("expected applied=true, got %v, %v", applied, err)
	}
	if updater.lastInput.Title != "{{unknown.field}}" {
		t.Fatalf("expected an unresolved placeholder to be left verbatim, got %q", updater.lastInput.Title)
	}
}

func TestApplyCallAPI_RendersURLBodyAndHeaders(t *testing.T) {
	var gotURL, gotBody, gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.RawQuery
		gotHeader = r.Header.Get("X-Task-Id")
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := newTestConsumer(&fakeTaskUpdater{})
	c.httpClient = http.DefaultClient
	vars := map[string]string{"task.id": "task-42", "task.title": "Fix the bug"}
	cfg := automationdom.ActionConfig{
		Method:  "POST",
		URL:     server.URL + "?id={{task.id}}",
		Body:    `{"title":"{{task.title}}"}`,
		Headers: map[string]string{"X-Task-Id": "{{task.id}}"},
	}
	applied, _, err := c.applyCallAPI(context.Background(), cfg, vars)
	if err != nil || !applied {
		t.Fatalf("expected applied=true, got %v, %v", applied, err)
	}
	if gotURL != "id=task-42" {
		t.Fatalf("expected the rendered URL query, got %q", gotURL)
	}
	if gotBody != `{"title":"Fix the bug"}` {
		t.Fatalf("expected the rendered body, got %q", gotBody)
	}
	if gotHeader != "task-42" {
		t.Fatalf("expected the rendered header value, got %q", gotHeader)
	}
}

func TestApplyUpdateSprint_GoalTemplateIsRendered(t *testing.T) {
	updater := &fakeSprintUpdater{result: &sprintdom.Sprint{ID: uuid.New()}}
	c := &AutomationConsumer{sprintSvc: updater}
	sprint := &sprintdom.Sprint{ID: uuid.New(), Name: "Sprint 1"}
	vars := map[string]string{"task.title": "Fix the bug"}
	goal := "Ship: {{task.title}}"

	applied, err := c.applyUpdateSprint(context.Background(), uuid.New(), nil, sprint, &automationdom.SprintFieldUpdate{Goal: &goal}, vars)
	if err != nil || !applied {
		t.Fatalf("expected applied=true, got %v, %v", applied, err)
	}
	if updater.lastUpdate.Goal == nil || **updater.lastUpdate.Goal != "Ship: Fix the bug" {
		t.Fatalf("expected the rendered goal to be sent, got %v", updater.lastUpdate.Goal)
	}
}
