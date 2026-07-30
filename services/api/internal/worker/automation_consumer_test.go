package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// fakeTaskUpdater records every UpdateTask call so tests can assert whether
// an idempotency guard correctly skipped (or didn't skip) a mutation.
type fakeTaskUpdater struct {
	calls int
	err   error
}

func (f *fakeTaskUpdater) UpdateTask(_ context.Context, _, _ uuid.UUID, _ taskdom.UpdateTaskInput) (*taskdom.Task, error) {
	f.calls++
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

func TestApplyAssign_IdempotentWhenAlreadyAssigned(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	memberID := uuid.New()
	task := &taskdom.Task{ID: uuid.New(), AssigneeIDs: []uuid.UUID{memberID}}

	applied, err := c.applyAssign(context.Background(), uuid.New(), task, memberID, "test-automation")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied {
		t.Fatal("expected applyAssign to be a no-op when already assigned to memberID")
	}
	if updater.calls != 0 {
		t.Fatalf("expected no UpdateTask call, got %d", updater.calls)
	}
}

func TestApplyAssign_AppliesWhenNotAssigned(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	memberID := uuid.New()
	task := &taskdom.Task{ID: uuid.New(), AssigneeIDs: nil}

	applied, err := c.applyAssign(context.Background(), uuid.New(), task, memberID, "test-automation")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected applyAssign to apply when not already assigned")
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
	_, err := c.applyDirectAgentMessage(context.Background(), uuid.New(), uuid.New(), "hello")
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
	_, err := c.applyDirectAgentMessage(context.Background(), uuid.New(), uuid.New(), "   ")
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
	_, err := c.applyDirectAgentMessage(context.Background(), uuid.New(), uuid.New(), "hello")
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
	applied, err := c.applyDirectAgentMessage(context.Background(), uuid.New(), uuid.New(), "do the thing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected applyDirectAgentMessage to report applied")
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
	_, err := c.applyTriggerAIAgentOnTask(context.Background(), uuid.New(), task, uuid.New(), "test-automation", "please help")
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
	_, err := c.applyTriggerAIAgentOnTask(context.Background(), uuid.New(), task, uuid.New(), "test-automation", "please help")
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

	applied, err := c.applyTriggerAIAgentOnTask(context.Background(), uuid.New(), task, memberID, "test-automation", "please help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected applyTriggerAIAgentOnTask to report applied")
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
	if w.failed {
		t.Fatal("expected trigger_ai_agent with a task to succeed via the task-conversation path")
	}
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
	if w.failed {
		t.Fatal("expected a nil task reaching trigger_ai_agent to succeed via the direct-message path")
	}
	if messenger.calls != 1 {
		t.Fatalf("expected TriggerDirectMessage to be called once, got %d", messenger.calls)
	}
}

func TestApplySetStatus_IdempotentWhenAlreadySet(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	statusID := uuid.New()
	task := &taskdom.Task{ID: uuid.New(), StatusID: &statusID}

	applied, err := c.applySetStatus(context.Background(), uuid.New(), task, statusID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied || updater.calls != 0 {
		t.Fatalf("expected no-op (applied=false, 0 calls), got applied=%v calls=%d", applied, updater.calls)
	}
}

func TestApplySetStatus_AppliesWhenDifferent(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	oldStatus, newStatus := uuid.New(), uuid.New()
	task := &taskdom.Task{ID: uuid.New(), StatusID: &oldStatus}

	applied, err := c.applySetStatus(context.Background(), uuid.New(), task, newStatus)
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

func TestApplySetPriority_IdempotentWhenAlreadySet(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Importance: 5}

	applied, err := c.applySetPriority(context.Background(), uuid.New(), task, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied || updater.calls != 0 {
		t.Fatalf("expected no-op, got applied=%v calls=%d", applied, updater.calls)
	}
}

func TestApplyAddTag_IdempotentWhenTagAlreadyPresent(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Tags: []string{"urgent"}}

	applied, err := c.applyAddTag(context.Background(), uuid.New(), task, "urgent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied || updater.calls != 0 {
		t.Fatalf("expected no-op, got applied=%v calls=%d", applied, updater.calls)
	}
}

func TestApplyAddTag_AppliesWhenTagMissing(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Tags: []string{"bug"}}

	applied, err := c.applyAddTag(context.Background(), uuid.New(), task, "urgent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied || updater.calls != 1 {
		t.Fatalf("expected applied=true, 1 call, got applied=%v calls=%d", applied, updater.calls)
	}
	if len(task.Tags) != 2 {
		t.Fatalf("expected tag appended in place, got %v", task.Tags)
	}
}

func TestApplySetCustomField_IdempotentWhenValueAlreadyMatches(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), CustomFields: map[string]any{"release_tag": "v2"}}

	applied, err := c.applySetCustomField(context.Background(), uuid.New(), task, "release_tag", "v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if applied || updater.calls != 0 {
		t.Fatalf("expected no-op, got applied=%v calls=%d", applied, updater.calls)
	}
}

func TestApplySetCustomField_AppliesWhenValueDiffers(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), CustomFields: map[string]any{"release_tag": "v1"}}

	applied, err := c.applySetCustomField(context.Background(), uuid.New(), task, "release_tag", "v2")
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

// --- Plugin action dispatch --------------------------------------------------

type fakePluginRuntime struct {
	conditionPlugin, actionPlugin string
	hasCondition, hasAction       bool
	conditionResp, actionResp     []byte
	err                           error
}

func (f *fakePluginRuntime) ResolveAutomationCondition(nodeType string) (string, bool) {
	return f.conditionPlugin, f.hasCondition
}
func (f *fakePluginRuntime) ResolveAutomationAction(nodeType string) (string, bool) {
	return f.actionPlugin, f.hasAction
}
func (f *fakePluginRuntime) EvaluateCondition(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return f.conditionResp, f.err
}
func (f *fakePluginRuntime) RunAction(_ context.Context, _ string, _ []byte) ([]byte, error) {
	return f.actionResp, f.err
}

func TestRunAction_UnknownTypeWithoutPluginRuntimeFails(t *testing.T) {
	c := newTestConsumer(&fakeTaskUpdater{})
	node := &automationdom.Node{Type: "com.acme.github.comment_on_pr", Config: json.RawMessage(`{}`)}
	_, _, err := c.runAction(context.Background(), uuid.New(), node, &taskdom.Task{}, "automation", uuid.New())
	if err == nil {
		t.Fatal("expected an error for an unrecognized action type with no plugin runtime configured")
	}
}

func TestRunAction_DispatchesToRegisteredPluginAction(t *testing.T) {
	c := newTestConsumer(&fakeTaskUpdater{})
	c.pluginRuntime = &fakePluginRuntime{
		actionPlugin: "com.acme.github",
		hasAction:    true,
		actionResp:   []byte(`{"applied":true}`),
	}
	node := &automationdom.Node{Type: "com.acme.github.comment_on_pr", Config: json.RawMessage(`{"template":"lgtm"}`)}
	applied, _, err := c.runAction(context.Background(), uuid.New(), node, &taskdom.Task{ID: uuid.New()}, "automation", uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !applied {
		t.Fatal("expected applied=true from the plugin's response")
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
	applied, detail, err := c.applyCallAPI(context.Background(), cfg)
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
	applied, _, err := c.applyCallAPI(context.Background(), automationdom.ActionConfig{Method: "GET", URL: server.URL})
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
	_, _, err := c.applyCallAPI(context.Background(), automationdom.ActionConfig{Method: "GET", URL: closedURL})
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
	_, detail, err := c.applyCallAPI(context.Background(), automationdom.ActionConfig{Method: "GET", URL: server.URL})
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
	w := &walker{
		consumer:  &AutomationConsumer{repo: &fakeCronRepo{}, log: discardLogger()},
		task:      nil,
		nodesByID: map[uuid.UUID]*automationdom.Node{condition.ID: condition},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), condition.ID)
	if !w.failed {
		t.Fatal("expected a nil task reaching a condition node to mark the walk failed")
	}
}

func TestWalk_NilTaskReachingNonCallAPIAction_FailsStepInsteadOfPanicking(t *testing.T) {
	action := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindAction, Type: string(automationdom.ActionSetStatus), Config: json.RawMessage(`{}`)}
	w := &walker{
		consumer:  &AutomationConsumer{repo: &fakeCronRepo{}, log: discardLogger()},
		task:      nil,
		nodesByID: map[uuid.UUID]*automationdom.Node{action.ID: action},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), action.ID)
	if !w.failed {
		t.Fatal("expected a nil task reaching a set_status action to mark the walk failed")
	}
}

func TestWalk_NilTaskReachingCallAPI_ProceedsAndSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg, _ := json.Marshal(automationdom.ActionConfig{Method: "GET", URL: server.URL})
	action := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindAction, Type: string(automationdom.ActionCallAPI), Config: cfg}
	w := &walker{
		consumer:  &AutomationConsumer{repo: &fakeCronRepo{}, httpClient: http.DefaultClient, log: discardLogger()},
		task:      nil,
		nodesByID: map[uuid.UUID]*automationdom.Node{action.ID: action},
		outgoing:  map[uuid.UUID][]*automationdom.Edge{},
		visited:   map[uuid.UUID]bool{},
	}
	w.walk(context.Background(), action.ID)
	if w.failed {
		t.Fatal("expected a nil task reaching a call_api action to succeed, since call_api never touches the task")
	}
}

func TestApplyCallAPI_DefaultClientRejectsPrivateTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewAutomationConsumer(nil, nil, nil, &fakeTaskUpdater{}, nil, nil, discardLogger())
	_, _, err := c.applyCallAPI(context.Background(), automationdom.ActionConfig{Method: "GET", URL: server.URL})
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
	cfg, _ := json.Marshal(automationdom.ActionConfig{Tag: tag})
	node := &automationdom.Node{Type: string(automationdom.ActionAddTag), Config: cfg}
	task := &taskdom.Task{ID: uuid.New()}

	applied, detail, err := c.runAction(context.Background(), uuid.New(), node, task, "test", uuid.New())
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
	cfg, _ := json.Marshal(automationdom.ActionConfig{Tag: "x", Target: &automationdom.TaskTarget{Kind: automationdom.TaskTargetChildren}})
	node := &automationdom.Node{Type: string(automationdom.ActionAddTag), Config: cfg}

	applied, detail, err := c.runAction(context.Background(), uuid.New(), node, &taskdom.Task{ID: baseID}, "test", uuid.New())
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
	cfg, _ := json.Marshal(automationdom.ActionConfig{Tag: "x", Target: &automationdom.TaskTarget{Kind: automationdom.TaskTargetChildren}})
	node := &automationdom.Node{Type: string(automationdom.ActionAddTag), Config: cfg}

	applied, detail, err := c.runAction(context.Background(), uuid.New(), node, &taskdom.Task{ID: baseID}, "test", uuid.New())
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
	// set_status with no status_id configured — applyActionForTask errors
	// immediately for every resolved task.
	cfg, _ := json.Marshal(automationdom.ActionConfig{Target: &automationdom.TaskTarget{Kind: automationdom.TaskTargetChildren}})
	node := &automationdom.Node{Type: string(automationdom.ActionSetStatus), Config: cfg}

	_, _, err := c.runAction(context.Background(), uuid.New(), node, &taskdom.Task{ID: baseID}, "test", uuid.New())
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
	matched, err := c.evaluateLeaf(context.Background(), uuid.New(), &taskdom.Task{ID: baseID}, leaf)
	if err != nil || !matched {
		t.Fatalf("expected any-mode to match one of two children, got %v, %v", matched, err)
	}

	leaf.MatchMode = "all"
	matched, err = c.evaluateLeaf(context.Background(), uuid.New(), &taskdom.Task{ID: baseID}, leaf)
	if err != nil || matched {
		t.Fatalf("expected all-mode to fail since only one of two children matches, got %v, %v", matched, err)
	}
}
