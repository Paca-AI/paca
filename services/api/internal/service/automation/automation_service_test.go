package automationsvc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

func newNode(kind automationdom.Kind, nodeType string, config string) *automationdom.Node {
	return &automationdom.Node{
		ID:     uuid.New(),
		Kind:   kind,
		Type:   nodeType,
		Config: json.RawMessage(config),
	}
}

func newEdge(source, target uuid.UUID) *automationdom.Edge {
	return &automationdom.Edge{ID: uuid.New(), SourceNodeID: source, TargetNodeID: target}
}

func TestWouldCreateCycle_DetectsDirectCycle(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	edges := []*automationdom.Edge{newEdge(a, b)}
	// Adding b -> a would close a 2-node cycle.
	if !wouldCreateCycle(edges, b, a) {
		t.Fatal("expected b->a to be detected as creating a cycle given a->b exists")
	}
}

func TestWouldCreateCycle_AllowsAcyclicAddition(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	edges := []*automationdom.Edge{newEdge(a, b)}
	if wouldCreateCycle(edges, b, c) {
		t.Fatal("expected b->c to be safe given only a->b exists")
	}
}

func TestWouldCreateCycle_DetectsIndirectCycle(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	edges := []*automationdom.Edge{newEdge(a, b), newEdge(b, c)}
	// c -> a would close a 3-node cycle (a->b->c->a).
	if !wouldCreateCycle(edges, c, a) {
		t.Fatal("expected c->a to be detected as creating a cycle given a->b->c exists")
	}
}

func TestHasCycle_DetectsCycleInExistingGraph(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	nodes := []*automationdom.Node{{ID: a}, {ID: b}, {ID: c}}
	edges := []*automationdom.Edge{newEdge(a, b), newEdge(b, c), newEdge(c, a)}
	if !hasCycle(nodes, edges) {
		t.Fatal("expected a->b->c->a to be detected as a cycle")
	}
}

func TestHasCycle_NoFalsePositiveOnDAG(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	nodes := []*automationdom.Node{{ID: a}, {ID: b}, {ID: c}}
	edges := []*automationdom.Edge{newEdge(a, b), newEdge(a, c)}
	if hasCycle(nodes, edges) {
		t.Fatal("expected a fan-out DAG to not be flagged as a cycle")
	}
}

func TestValidateEdgeHandle_TriggerSourceRejectsHandle(t *testing.T) {
	source := newNode(automationdom.KindTrigger, "status_changed", "{}")
	handle := "some-handle"
	if err := validateEdgeHandle(source, &handle); err != automationdom.ErrEdgeHandleNotAllowed {
		t.Fatalf("expected ErrEdgeHandleNotAllowed, got %v", err)
	}
	if err := validateEdgeHandle(source, nil); err != nil {
		t.Fatalf("expected nil handle from a trigger source to be valid, got %v", err)
	}
}

func TestValidateEdgeHandle_ActionSourceRejectsHandle(t *testing.T) {
	source := newNode(automationdom.KindAction, "add_tag", "{}")
	handle := "x"
	if err := validateEdgeHandle(source, &handle); err != automationdom.ErrEdgeHandleNotAllowed {
		t.Fatalf("expected ErrEdgeHandleNotAllowed, got %v", err)
	}
}

func TestValidateEdgeHandle_ConditionSourceRequiresHandle(t *testing.T) {
	source := newNode(automationdom.KindCondition, automationdom.ConditionNodeType, `{"branches":[{"handle":"bug"}]}`)
	if err := validateEdgeHandle(source, nil); err != automationdom.ErrEdgeHandleRequired {
		t.Fatalf("expected ErrEdgeHandleRequired, got %v", err)
	}
}

func TestValidateEdgeHandle_ConditionSourceAcceptsElse(t *testing.T) {
	source := newNode(automationdom.KindCondition, automationdom.ConditionNodeType, `{"branches":[{"handle":"bug"}]}`)
	elseHandle := automationdom.ElseHandle
	if err := validateEdgeHandle(source, &elseHandle); err != nil {
		t.Fatalf("expected else handle to be valid, got %v", err)
	}
}

func TestValidateEdgeHandle_ConditionSourceAcceptsDeclaredBranch(t *testing.T) {
	source := newNode(automationdom.KindCondition, automationdom.ConditionNodeType, `{"branches":[{"handle":"bug"},{"handle":"feature"}]}`)
	handle := "bug"
	if err := validateEdgeHandle(source, &handle); err != nil {
		t.Fatalf("expected declared branch handle to be valid, got %v", err)
	}
}

func TestValidateEdgeHandle_ConditionSourceRejectsUndeclaredHandle(t *testing.T) {
	source := newNode(automationdom.KindCondition, automationdom.ConditionNodeType, `{"branches":[{"handle":"bug"}]}`)
	handle := "not-a-real-branch"
	if err := validateEdgeHandle(source, &handle); err == nil {
		t.Fatal("expected an error for a handle that isn't a declared branch or else")
	}
}

// TestValidateEdgeHandle_PluginConditionSource covers a plugin-contributed
// condition node (Type != ConditionNodeType): its config is opaque to this
// package (no Branches to check against, unlike the built-in switch), so the
// only two valid handles are PluginConditionTrueHandle ("true") — the handle
// walker.walkPluginCondition follows on a match — and the shared ElseHandle
// fallback. Anything else must still be rejected.
func TestValidateEdgeHandle_PluginConditionSource(t *testing.T) {
	source := newNode(automationdom.KindCondition, "com.acme.github.pr_status", `{"repo":"acme/widgets"}`)
	trueHandle := automationdom.PluginConditionTrueHandle
	if err := validateEdgeHandle(source, &trueHandle); err != nil {
		t.Fatalf("expected the plugin condition's true handle to be valid, got %v", err)
	}
	elseHandle := automationdom.ElseHandle
	if err := validateEdgeHandle(source, &elseHandle); err != nil {
		t.Fatalf("expected else handle to remain valid for a plugin condition node, got %v", err)
	}
	bogusHandle := "not_a_real_handle"
	if err := validateEdgeHandle(source, &bogusHandle); err == nil {
		t.Fatal("expected an arbitrary, undeclared handle to be rejected for a plugin condition node")
	}
}

func TestHandlesEqual(t *testing.T) {
	a, b := "x", "x"
	c := "y"
	if !handlesEqual(&a, &b) {
		t.Fatal("expected equal string pointers to compare equal")
	}
	if handlesEqual(&a, &c) {
		t.Fatal("expected different string pointers to compare unequal")
	}
	if !handlesEqual(nil, nil) {
		t.Fatal("expected both-nil to compare equal")
	}
	if handlesEqual(&a, nil) || handlesEqual(nil, &a) {
		t.Fatal("expected nil vs non-nil to compare unequal")
	}
}

func TestValidateConditionConfig_RejectsElseAsBranchHandle(t *testing.T) {
	cfg, _ := json.Marshal(automationdom.ConditionConfig{
		Branches: []automationdom.ConditionBranch{{Handle: automationdom.ElseHandle, Tree: &automationdom.ConditionLeaf{Field: automationdom.FieldStatus, Operator: automationdom.OpIsEmpty}}},
	})
	if err := (&Service{}).validateConditionConfig(context.Background(), uuid.New(), cfg, true); err == nil {
		t.Fatal("expected an error when a branch uses the reserved 'else' handle")
	}
}

func TestValidateConditionConfig_RejectsDuplicateHandles(t *testing.T) {
	leafNode := &automationdom.ConditionLeaf{Field: automationdom.FieldStatus, Operator: automationdom.OpIsEmpty}
	cfg, _ := json.Marshal(automationdom.ConditionConfig{
		Branches: []automationdom.ConditionBranch{
			{Handle: "a", Tree: leafNode},
			{Handle: "a", Tree: leafNode},
		},
	})
	if err := (&Service{}).validateConditionConfig(context.Background(), uuid.New(), cfg, true); err == nil {
		t.Fatal("expected an error for duplicate branch handles")
	}
}

func TestValidateConditionConfig_AcceptsValidBranches(t *testing.T) {
	leafNode := &automationdom.ConditionLeaf{Field: automationdom.FieldStatus, Operator: automationdom.OpIsEmpty}
	cfg, _ := json.Marshal(automationdom.ConditionConfig{
		Branches: []automationdom.ConditionBranch{
			{Handle: "bug", Tree: leafNode},
			{Handle: "feature", Tree: leafNode},
		},
	})
	if err := (&Service{}).validateConditionConfig(context.Background(), uuid.New(), cfg, true); err != nil {
		t.Fatalf("expected valid branches to pass, got %v", err)
	}
}

// --- Plugin node resolver fallthrough ---------------------------------------

type stubPluginResolver struct {
	triggers, conditions, actions map[string]bool
}

func (s *stubPluginResolver) IsPluginTrigger(t string) bool   { return s.triggers[t] }
func (s *stubPluginResolver) IsPluginCondition(t string) bool { return s.conditions[t] }
func (s *stubPluginResolver) IsPluginAction(t string) bool    { return s.actions[t] }

func TestService_ValidateNodeTypeAndConfig_PluginActionFallthrough(t *testing.T) {
	svc := &Service{}
	svc.WithPluginNodeResolver(&stubPluginResolver{actions: map[string]bool{"com.acme.github.comment_on_pr": true}})

	err := svc.validateNodeTypeAndConfig(context.TODO(), uuid.Nil, automationdom.KindAction, "com.acme.github.comment_on_pr", json.RawMessage(`{"template":"lgtm"}`), true)
	if err != nil {
		t.Fatalf("expected a registered plugin action type to validate, got %v", err)
	}

	err = svc.validateNodeTypeAndConfig(context.TODO(), uuid.Nil, automationdom.KindAction, "com.acme.unregistered.action", json.RawMessage(`{}`), true)
	if err != automationdom.ErrNodeInvalidType {
		t.Fatalf("expected ErrNodeInvalidType for an unregistered action type, got %v", err)
	}
}

// TestService_ValidateNodeTypeAndConfig_PluginConditionFallthrough covers a
// plugin-contributed condition node: its own node type (KindCondition, not
// the built-in ConditionNodeType), validated opaquely once the resolver
// confirms it's registered.
func TestService_ValidateNodeTypeAndConfig_PluginConditionFallthrough(t *testing.T) {
	svc := &Service{}
	svc.WithPluginNodeResolver(&stubPluginResolver{conditions: map[string]bool{"com.acme.github.pr_status": true}})

	err := svc.validateNodeTypeAndConfig(context.TODO(), uuid.Nil, automationdom.KindCondition, "com.acme.github.pr_status", json.RawMessage(`{"status":"open"}`), true)
	if err != nil {
		t.Fatalf("expected a registered plugin condition type to validate, got %v", err)
	}

	err = svc.validateNodeTypeAndConfig(context.TODO(), uuid.Nil, automationdom.KindCondition, "com.acme.unregistered.condition", json.RawMessage(`{}`), true)
	if err != automationdom.ErrNodeInvalidType {
		t.Fatalf("expected ErrNodeInvalidType for an unregistered condition type, got %v", err)
	}
}

func TestService_ValidateNodeTypeAndConfig_NoPluginResolverRejectsUnknownTypes(t *testing.T) {
	svc := &Service{}
	err := svc.validateNodeTypeAndConfig(context.TODO(), uuid.Nil, automationdom.KindAction, "com.acme.github.comment_on_pr", json.RawMessage(`{}`), true)
	if err != automationdom.ErrNodeInvalidType {
		t.Fatalf("expected ErrNodeInvalidType when no plugin resolver is configured, got %v", err)
	}
}

// --- Cron / API trigger / call_api validation -------------------------------

func TestValidateTriggerConfig_Cron_InvalidExpressionRejectedRegardlessOfStrict(t *testing.T) {
	svc := &Service{}
	cfg, _ := json.Marshal(automationdom.TriggerConfig{CronExpression: "not a cron expression"})
	if err := svc.validateTriggerConfig(context.TODO(), uuid.Nil, automationdom.TriggerCron, cfg, false); err == nil {
		t.Fatal("expected an invalid cron expression to be rejected even in non-strict mode")
	}
}

func TestValidateTriggerConfig_Cron_NonStrictAllowsEmptyConfig(t *testing.T) {
	svc := &Service{}
	if err := svc.validateTriggerConfig(context.TODO(), uuid.Nil, automationdom.TriggerCron, json.RawMessage(`{}`), false); err != nil {
		t.Fatalf("expected an empty cron config to pass non-strict (create-time) validation, got %v", err)
	}
}

func TestValidateTriggerConfig_Cron_StrictRequiresExpressionButNotTargetTask(t *testing.T) {
	svc := &Service{}
	if err := svc.validateTriggerConfig(context.TODO(), uuid.Nil, automationdom.TriggerCron, json.RawMessage(`{}`), true); err == nil {
		t.Fatal("expected strict validation to require a cron_expression")
	}
	// target_task_id is optional — a valid cron_expression with no target
	// task passes strict validation; validateTaskReachability (not this
	// function) is what enforces such a node can only reach call_api
	// actions downstream.
	cfg, _ := json.Marshal(automationdom.TriggerConfig{CronExpression: "*/5 * * * *"})
	if err := svc.validateTriggerConfig(context.TODO(), uuid.Nil, automationdom.TriggerCron, cfg, true); err != nil {
		t.Fatalf("expected strict validation to allow an unset target_task_id, got %v", err)
	}
}

func TestValidateTriggerConfig_Cron_ValidExpressionPassesNonStrict(t *testing.T) {
	svc := &Service{}
	for _, expr := range []string{"* * * * *", "*/15 * * * *", "0 9 * * 1-5"} {
		cfg, _ := json.Marshal(automationdom.TriggerConfig{CronExpression: expr})
		if err := svc.validateTriggerConfig(context.TODO(), uuid.Nil, automationdom.TriggerCron, cfg, false); err != nil {
			t.Fatalf("expected %q to be a valid cron expression, got %v", expr, err)
		}
	}
}

func TestValidateTriggerConfig_APITrigger_TargetTaskIDOptionalInBothModes(t *testing.T) {
	svc := &Service{}
	if err := svc.validateTriggerConfig(context.TODO(), uuid.Nil, automationdom.TriggerAPITrigger, json.RawMessage(`{}`), true); err != nil {
		t.Fatalf("expected strict validation to allow an unset target_task_id, got %v", err)
	}
	if err := svc.validateTriggerConfig(context.TODO(), uuid.Nil, automationdom.TriggerAPITrigger, json.RawMessage(`{}`), false); err != nil {
		t.Fatalf("expected non-strict (create-time) validation to allow an empty api_trigger config, got %v", err)
	}
}

func TestValidateActionConfig_CallAPI_FormatChecksApplyRegardlessOfStrict(t *testing.T) {
	svc := &Service{}
	cfg, _ := json.Marshal(automationdom.ActionConfig{Method: "TRACE", URL: "https://example.com"})
	if err := svc.validateActionConfig(context.TODO(), uuid.Nil, automationdom.ActionCallAPI, cfg, false); err == nil {
		t.Fatal("expected an unsupported HTTP method to be rejected even in non-strict mode")
	}

	cfg, _ = json.Marshal(automationdom.ActionConfig{Method: "GET", URL: "ftp://example.com/file"})
	if err := svc.validateActionConfig(context.TODO(), uuid.Nil, automationdom.ActionCallAPI, cfg, false); err == nil {
		t.Fatal("expected a non-http(s) URL scheme to be rejected even in non-strict mode")
	}
}

func TestValidateActionConfig_CallAPI_StrictRequiresMethodAndURL(t *testing.T) {
	svc := &Service{}
	if err := svc.validateActionConfig(context.TODO(), uuid.Nil, automationdom.ActionCallAPI, json.RawMessage(`{}`), false); err != nil {
		t.Fatalf("expected an empty call_api config to pass non-strict (create-time) validation, got %v", err)
	}
	if err := svc.validateActionConfig(context.TODO(), uuid.Nil, automationdom.ActionCallAPI, json.RawMessage(`{}`), true); err == nil {
		t.Fatal("expected strict validation to require method and url")
	}
}

func TestValidateActionConfig_CallAPI_ValidConfigPasses(t *testing.T) {
	svc := &Service{}
	cfg, _ := json.Marshal(automationdom.ActionConfig{Method: "post", URL: "https://example.com/webhook"})
	if err := svc.validateActionConfig(context.TODO(), uuid.Nil, automationdom.ActionCallAPI, cfg, true); err != nil {
		t.Fatalf("expected a valid call_api config to pass, got %v", err)
	}
}

// --- validateTaskReachability ------------------------------------------------

func taskLessTriggerNode(t *testing.T, triggerType automationdom.TriggerType) *automationdom.Node {
	t.Helper()
	cfg, err := json.Marshal(automationdom.TriggerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	return newNode(automationdom.KindTrigger, string(triggerType), string(cfg))
}

func targetedTriggerNode(t *testing.T, triggerType automationdom.TriggerType) *automationdom.Node {
	t.Helper()
	targetTaskID := uuid.New()
	cfg, err := json.Marshal(automationdom.TriggerConfig{TargetTaskID: &targetTaskID})
	if err != nil {
		t.Fatal(err)
	}
	return newNode(automationdom.KindTrigger, string(triggerType), string(cfg))
}

func TestValidateTaskReachability_TaskLessTriggerToCallAPI_Allowed(t *testing.T) {
	trigger := taskLessTriggerNode(t, automationdom.TriggerCron)
	action := newNode(automationdom.KindAction, string(automationdom.ActionCallAPI), "{}")
	nodes := []*automationdom.Node{trigger, action}
	edges := []*automationdom.Edge{newEdge(trigger.ID, action.ID)}
	if err := validateTaskReachability(nodes, edges); err != nil {
		t.Fatalf("expected a task-less cron trigger connected only to call_api to be valid, got %v", err)
	}
}

func TestValidateTaskReachability_TaskLessTriggerToTriggerAIAgent_Allowed(t *testing.T) {
	trigger := taskLessTriggerNode(t, automationdom.TriggerAPITrigger)
	action := newNode(automationdom.KindAction, string(automationdom.ActionTriggerAIAgent), "{}")
	nodes := []*automationdom.Node{trigger, action}
	edges := []*automationdom.Edge{newEdge(trigger.ID, action.ID)}
	if err := validateTaskReachability(nodes, edges); err != nil {
		t.Fatalf("expected a task-less api_trigger connected to trigger_ai_agent to be valid — it fires a direct message instead of assigning a task, got %v", err)
	}
}

func TestValidateTaskReachability_TaskLessTriggerToCondition_Rejected(t *testing.T) {
	trigger := taskLessTriggerNode(t, automationdom.TriggerPredecessorDone)
	condition := newNode(automationdom.KindCondition, automationdom.ConditionNodeType, "{}")
	nodes := []*automationdom.Node{trigger, condition}
	edges := []*automationdom.Edge{newEdge(trigger.ID, condition.ID)}
	if err := validateTaskReachability(nodes, edges); err == nil {
		t.Fatal("expected a task-less predecessor_done trigger reaching a condition node to be rejected")
	}
}

func TestValidateTaskReachability_TaskLessTriggerToNonCallAPIAction_Rejected(t *testing.T) {
	trigger := taskLessTriggerNode(t, automationdom.TriggerAPITrigger)
	action := newNode(automationdom.KindAction, string(automationdom.ActionUpdateTask), "{}")
	nodes := []*automationdom.Node{trigger, action}
	edges := []*automationdom.Edge{newEdge(trigger.ID, action.ID)}
	if err := validateTaskReachability(nodes, edges); err == nil {
		t.Fatal("expected a task-less api_trigger reaching an update_task action to be rejected")
	}
}

func TestValidateTaskReachability_TransitiveThroughCallAPI_Rejected(t *testing.T) {
	trigger := taskLessTriggerNode(t, automationdom.TriggerCron)
	callAPI := newNode(automationdom.KindAction, string(automationdom.ActionCallAPI), "{}")
	condition := newNode(automationdom.KindCondition, automationdom.ConditionNodeType, "{}")
	nodes := []*automationdom.Node{trigger, callAPI, condition}
	edges := []*automationdom.Edge{newEdge(trigger.ID, callAPI.ID), newEdge(callAPI.ID, condition.ID)}
	if err := validateTaskReachability(nodes, edges); err == nil {
		t.Fatal("expected the check to catch a condition reached transitively through call_api")
	}
}

func TestValidateTaskReachability_TriggerWithTargetTask_AllowsAnything(t *testing.T) {
	trigger := targetedTriggerNode(t, automationdom.TriggerCron)
	condition := newNode(automationdom.KindCondition, automationdom.ConditionNodeType, "{}")
	nodes := []*automationdom.Node{trigger, condition}
	edges := []*automationdom.Edge{newEdge(trigger.ID, condition.ID)}
	if err := validateTaskReachability(nodes, edges); err != nil {
		t.Fatalf("expected a trigger with a target task to reach a condition node fine, got %v", err)
	}
}

func TestValidateTaskReachability_EmptyGraph_NoError(t *testing.T) {
	if err := validateTaskReachability(nil, nil); err != nil {
		t.Fatalf("expected an empty graph to be valid, got %v", err)
	}
}

// --- validateTaskTarget ------------------------------------------------------

type fakeTaskLookup struct {
	tasks map[uuid.UUID]*taskdom.Task
}

func (f *fakeTaskLookup) FindTaskByID(_ context.Context, id uuid.UUID) (*taskdom.Task, error) {
	t, ok := f.tasks[id]
	if !ok {
		return nil, automationdom.ErrNotFound
	}
	return t, nil
}

func (f *fakeTaskLookup) FindTaskStatusByID(context.Context, uuid.UUID) (*taskdom.TaskStatus, error) {
	return nil, nil
}

func TestValidateTaskTarget_NilOrSelf_NoError(t *testing.T) {
	svc := &Service{}
	if err := svc.validateTaskTarget(context.Background(), uuid.New(), nil, true); err != nil {
		t.Fatalf("expected nil target to be valid, got %v", err)
	}
	if err := svc.validateTaskTarget(context.Background(), uuid.New(), &automationdom.TaskTarget{}, true); err != nil {
		t.Fatalf("expected an empty-kind target to be valid, got %v", err)
	}
}

func TestValidateTaskTarget_UnknownKind_Rejected(t *testing.T) {
	svc := &Service{}
	err := svc.validateTaskTarget(context.Background(), uuid.New(), &automationdom.TaskTarget{Kind: "bogus"}, true)
	if err == nil {
		t.Fatal("expected an unknown target kind to be rejected")
	}
}

func TestValidateTaskTarget_ParentAndChildren_NoTaskLookupNeeded(t *testing.T) {
	svc := &Service{}
	for _, kind := range []automationdom.TaskTargetKind{automationdom.TaskTargetParent, automationdom.TaskTargetChildren, automationdom.TaskTargetBlocks} {
		if err := svc.validateTaskTarget(context.Background(), uuid.New(), &automationdom.TaskTarget{Kind: kind}, true); err != nil {
			t.Errorf("expected target kind %q to need no further config, got %v", kind, err)
		}
	}
}

func TestValidateTaskTarget_Other_StrictRequiresOtherTaskID(t *testing.T) {
	svc := &Service{}
	if err := svc.validateTaskTarget(context.Background(), uuid.New(), &automationdom.TaskTarget{Kind: automationdom.TaskTargetOther}, true); err == nil {
		t.Fatal("expected strict validation to require other_task_id for target kind \"other\"")
	}
	if err := svc.validateTaskTarget(context.Background(), uuid.New(), &automationdom.TaskTarget{Kind: automationdom.TaskTargetOther}, false); err != nil {
		t.Fatalf("expected non-strict validation to allow an unset other_task_id, got %v", err)
	}
}

func TestValidateTaskTarget_Other_CrossProjectRejected(t *testing.T) {
	projectID := uuid.New()
	otherTaskID := uuid.New()
	svc := &Service{taskRepo: &fakeTaskLookup{tasks: map[uuid.UUID]*taskdom.Task{
		otherTaskID: {ID: otherTaskID, ProjectID: uuid.New()}, // different project
	}}}
	err := svc.validateTaskTarget(context.Background(), projectID, &automationdom.TaskTarget{Kind: automationdom.TaskTargetOther, OtherTaskID: &otherTaskID}, true)
	if err == nil {
		t.Fatal("expected a cross-project other_task_id to be rejected")
	}
}

func TestValidateTaskTarget_Other_SameProjectAccepted(t *testing.T) {
	projectID := uuid.New()
	otherTaskID := uuid.New()
	svc := &Service{taskRepo: &fakeTaskLookup{tasks: map[uuid.UUID]*taskdom.Task{
		otherTaskID: {ID: otherTaskID, ProjectID: projectID},
	}}}
	err := svc.validateTaskTarget(context.Background(), projectID, &automationdom.TaskTarget{Kind: automationdom.TaskTargetOther, OtherTaskID: &otherTaskID}, true)
	if err != nil {
		t.Fatalf("expected a same-project other_task_id to be valid, got %v", err)
	}
}

func TestValidateActionConfig_CallAPI_RejectsTarget(t *testing.T) {
	svc := &Service{}
	cfg, _ := json.Marshal(automationdom.ActionConfig{
		Method: "GET", URL: "https://example.com",
		Target: &automationdom.TaskTarget{Kind: automationdom.TaskTargetParent},
	})
	if err := svc.validateActionConfig(context.Background(), uuid.New(), automationdom.ActionCallAPI, cfg, true); err == nil {
		t.Fatal("expected call_api to reject a target — it doesn't operate on a task")
	}
}

func TestValidateActionConfig_UpdateTask_AcceptsTarget(t *testing.T) {
	svc := &Service{}
	cfg, _ := json.Marshal(automationdom.ActionConfig{
		Update: &automationdom.TaskFieldUpdate{Tags: []string{"x"}},
		Target: &automationdom.TaskTarget{Kind: automationdom.TaskTargetChildren},
	})
	if err := svc.validateActionConfig(context.Background(), uuid.New(), automationdom.ActionUpdateTask, cfg, true); err != nil {
		t.Fatalf("expected update_task to accept a children target, got %v", err)
	}
}

func TestValidateConditionConfig_RejectsBadMatchMode(t *testing.T) {
	svc := &Service{}
	cfg, _ := json.Marshal(automationdom.ConditionConfig{
		Branches: []automationdom.ConditionBranch{
			{Handle: "a", Tree: &automationdom.ConditionLeaf{Field: automationdom.FieldStatus, Operator: automationdom.OpIsEmpty, MatchMode: "sometimes"}},
		},
	})
	if err := svc.validateConditionConfig(context.Background(), uuid.New(), cfg, true); err == nil {
		t.Fatal("expected an invalid match_mode to be rejected")
	}
}
