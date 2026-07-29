package e2e_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/worker"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// startAutomationConsumer wires up and starts a real worker.AutomationConsumer
// against the shared Valkey stream, so that task events made through the
// HTTP API actually get evaluated against active automations — exercising
// the engine itself, not just the automation CRUD surface. Stopped
// automatically at test cleanup.
func startAutomationConsumer(t *testing.T, env *e2eEnv) *worker.AutomationConsumer {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	ac := worker.NewAutomationConsumer(env.redisClient, env.automationRepo, env.taskRepo, env.taskSvc, env.activitySvc, nil, log).
		// The production default is an SSRF-safe client that rejects private
		// IPs — call_api e2e tests need to reach a local httptest.Server
		// (127.0.0.1), which that client would otherwise reject. Safe here
		// since this consumer only ever talks to a test's own local server.
		WithHTTPClient(http.DefaultClient).
		// Same wiring as bootstrap/app.go — lets a task-less trigger_ai_agent
		// node fire a direct message instead of a task assignment.
		WithAgentMessaging(env.projectRepo, env.agentSvc).
		// Every test shares one physical Redis instance and stream (see
		// TestMain), but each gets its own Postgres database — without a
		// consumer group of its own, parallel tests (t.Parallel()) would
		// compete for the SAME group's cursor, and one test's events could
		// be delivered to a different test's consumer, which would fail to
		// find the referenced task in its own isolated database. A unique
		// group per test, created starting from "$" (now), isolates it from
		// both other tests' events and the stream's growing history.
		WithConsumerGroup("e2e." + uuid.NewString())
	ac.Start(env.ctx)
	t.Cleanup(ac.Stop)
	return ac
}

// getTaskViaAPI fetches a task and returns its decoded response body.
func getTaskViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, taskID string) map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projectID, taskID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var e envelope
	decodeJSON(t, resp, &e)
	return assertDataMap(t, e)
}

// setTaskStatusViaAPI changes a task's status through the normal task-update
// endpoint — the same path a human PATCH would take, and the one the
// automation engine listens for.
func setTaskStatusViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, taskID, statusID string) {
	t.Helper()
	body := jsonBody(t, map[string]any{"status_id": statusID})
	req := mustRequest(env.ctx, t, http.MethodPatch,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projectID, taskID), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
}

func createAutomationViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, name string) string {
	t.Helper()
	body := jsonBody(t, map[string]any{"name": name})
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/automations", env.base, projectID), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var e envelope
	decodeJSON(t, resp, &e)
	data := assertDataMap(t, e)
	id, _ := data["id"].(string)
	return id
}

func addAutomationNodeViaAPI(
	t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID, kind, nodeType string, config map[string]any,
) map[string]any {
	t.Helper()
	if config == nil {
		config = map[string]any{}
	}
	body := jsonBody(t, map[string]any{
		"kind": kind, "type": nodeType, "config": config, "pos_x": 0, "pos_y": 0,
	})
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/nodes", env.base, projectID, automationID), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var e envelope
	decodeJSON(t, resp, &e)
	return assertDataMap(t, e)
}

func updateAutomationNodeConfigViaAPIExpect(
	t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID, nodeID string, config map[string]any, wantStatus int,
) {
	t.Helper()
	body := jsonBody(t, map[string]any{"config": config})
	req := mustRequest(env.ctx, t, http.MethodPatch,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/nodes/%s", env.base, projectID, automationID, nodeID), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, wantStatus)
}

func addAutomationEdgeViaAPIExpect(
	t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID, sourceNodeID, targetNodeID string, sourceHandle *string, wantStatus int,
) map[string]any {
	t.Helper()
	payload := map[string]any{"source_node_id": sourceNodeID, "target_node_id": targetNodeID}
	if sourceHandle != nil {
		payload["source_handle"] = *sourceHandle
	}
	body := jsonBody(t, payload)
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/edges", env.base, projectID, automationID), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, wantStatus)
	if wantStatus >= 400 {
		return nil
	}
	var e envelope
	decodeJSON(t, resp, &e)
	return assertDataMap(t, e)
}

func addAutomationEdgeViaAPI(
	t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID, sourceNodeID, targetNodeID string,
) map[string]any {
	t.Helper()
	return addAutomationEdgeViaAPIExpect(t, env, client, token, projectID, automationID, sourceNodeID, targetNodeID, nil, http.StatusCreated)
}

func activateAutomationViaAPIExpect(
	t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID string, wantStatus int,
) map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/activate", env.base, projectID, automationID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, wantStatus)
	if wantStatus >= 400 {
		return nil
	}
	var e envelope
	decodeJSON(t, resp, &e)
	return assertDataMap(t, e)
}

func activateAutomationViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID string) {
	t.Helper()
	activateAutomationViaAPIExpect(t, env, client, token, projectID, automationID, http.StatusOK)
}

func getAutomationGraphViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID string) map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s", env.base, projectID, automationID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var e envelope
	decodeJSON(t, resp, &e)
	return assertDataMap(t, e)
}

func listAutomationRunsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID string) []map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/runs", env.base, projectID, automationID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var e envelope
	decodeJSON(t, resp, &e)
	data := assertDataMap(t, e)
	items, _ := data["items"].([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func listAutomationRunStepsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID, runID string) []map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/runs/%s/steps", env.base, projectID, automationID, runID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var e envelope
	decodeJSON(t, resp, &e)
	data := assertDataMap(t, e)
	items, _ := data["items"].([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func generateWebhookTokenViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID, nodeID string) string {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/nodes/%s/webhook-token", env.base, projectID, automationID, nodeID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var e envelope
	decodeJSON(t, resp, &e)
	data := assertDataMap(t, e)
	rawToken, _ := data["token"].(string)
	if rawToken == "" {
		t.Fatal("expected a non-empty raw token in the webhook-token response")
	}
	return rawToken
}

// postWebhookExpect POSTs to the public webhook receiver with a plain,
// unauthenticated http.Client — no Authorization header at all — to prove
// the endpoint genuinely doesn't require a user session.
func postWebhookExpect(t *testing.T, env *e2eEnv, nodeID, webhookToken string, wantStatus int) {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/webhooks/automations/%s", env.base, nodeID), nil)
	if webhookToken != "" {
		req.Header.Set("X-Webhook-Token", webhookToken)
	}
	resp := mustDo(t, http.DefaultClient, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, wantStatus)
}

// waitForTaskField polls the task until getField(data) returns true, or fails
// the test after timeout. Reassignment/mutation happens asynchronously via a
// Valkey-stream event the AutomationConsumer processes in the background.
func waitForTaskField(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, taskID string, timeout time.Duration, check func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var data map[string]any
	for time.Now().Before(deadline) {
		data = getTaskViaAPI(t, env, client, token, projectID, taskID)
		if check(data) {
			return data
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for task %s to satisfy the expected condition; last observed: %v", taskID, data)
	return nil
}

func waitForAutomationAssignee(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, taskID, wantAssigneeID string, timeout time.Duration) map[string]any {
	t.Helper()
	return waitForTaskField(t, env, client, token, projectID, taskID, timeout, func(data map[string]any) bool {
		assignees, _ := data["assignee_ids"].([]any)
		return len(assignees) == 1 && assignees[0] == wantAssigneeID
	})
}

// memberIDForAgent finds the project_members.id of the member backed by
// agentID (see project_member_dto.go's AgentID field), among members
// already fetched via listProjectMembersViaAPI.
func memberIDForAgent(members []map[string]any, agentID string) string {
	for _, m := range members {
		if aid, _ := m["agent_id"].(string); aid == agentID {
			id, _ := m["id"].(string)
			return id
		}
	}
	return ""
}

func listTaskTypesViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string) []map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/task-types", env.base, projectID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var e envelope
	decodeJSON(t, resp, &e)
	data := assertDataMap(t, e)
	items, _ := data["items"].([]any)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func taskTypeIDByName(types []map[string]any, name string) string {
	for _, tt := range types {
		if n, _ := tt["name"].(string); n == name {
			id, _ := tt["id"].(string)
			return id
		}
	}
	return ""
}

func archiveAutomationViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID string) {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/archive", env.base, projectID, automationID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
}

func revertAutomationToDraftViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID string) {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/revert-to-draft", env.base, projectID, automationID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
}

func removeAutomationEdgeViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, automationID, edgeID string) {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodDelete,
		fmt.Sprintf("%s/api/v1/projects/%s/automations/%s/edges/%s", env.base, projectID, automationID, edgeID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusNoContent)
}

// patchTaskFieldsViaAPI is a generic task PATCH for triggers/fields that
// don't have a dedicated helper (assignee_ids, importance, tags, ...).
func patchTaskFieldsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, taskID string, fields map[string]any) {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPatch,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projectID, taskID), jsonBody(t, fields))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
}

// taskHasTag reports whether a decoded task response's tags include want.
func taskHasTag(data map[string]any, want string) bool {
	tags, _ := data["tags"].([]any)
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Basic CRUD + graph validation
// ---------------------------------------------------------------------------

func TestE2EAutomation_CreateNodesEdgesAndFetchGraph(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	ownerUsername := "automation-crud-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationcrudowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationcrudowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	todoID := statusIDByName(statuses, "Todo")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "CRUD Test Automation")

	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": todoID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "todo-reached"})

	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)

	graph := getAutomationGraphViaAPI(t, env, ownerClient, ownerToken, projID, automationID)
	nodes, _ := graph["nodes"].([]any)
	edges, _ := graph["edges"].([]any)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
}

func TestE2EAutomation_EdgeIntoTriggerRejected(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	ownerUsername := "automation-edge-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationedgeowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationedgeowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Edge Validation Automation")
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, "action", "add_tag", map[string]any{"tag": "x"})
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, "trigger", "task_created", nil)

	actionID, _ := action["id"].(string)
	triggerID, _ := trigger["id"].(string)

	// An edge INTO a trigger node must be rejected.
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, actionID, triggerID, nil, http.StatusBadRequest)
}

func TestE2EAutomation_CycleRejected(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	ownerUsername := "automation-cycle-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationcycleowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationcycleowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Cycle Test Automation")
	a1 := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, "action", "add_tag", map[string]any{"tag": "a"})
	a2 := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, "action", "add_tag", map[string]any{"tag": "b"})
	a1ID, _ := a1["id"].(string)
	a2ID, _ := a2["id"].(string)

	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, a1ID, a2ID)
	// a2 -> a1 would close a cycle.
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, a2ID, a1ID, nil, http.StatusBadRequest)
}

func TestE2EAutomation_ActivateRequiresTriggerAndAction(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	ownerUsername := "automation-activate-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationactivateowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationactivateowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	// No nodes at all — must fail.
	emptyAutomationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Empty Automation")
	activateAutomationViaAPIExpect(t, env, ownerClient, ownerToken, projID, emptyAutomationID, http.StatusBadRequest)

	// Trigger only, no action — must fail.
	triggerOnlyID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Trigger Only Automation")
	addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, triggerOnlyID, "trigger", "task_created", nil)
	activateAutomationViaAPIExpect(t, env, ownerClient, ownerToken, projID, triggerOnlyID, http.StatusBadRequest)

	// Trigger + action, connected — must succeed.
	completeID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Complete Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, completeID, "trigger", "task_created", nil)
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, completeID, "action", "add_tag", map[string]any{"tag": "new"})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, completeID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, completeID)
}

// TestE2EAutomation_CreateEmptyActionNodeThenConfigure exercises the exact
// flow the web canvas uses: an action node is always created with an empty
// config first (dropped from the palette), then configured afterward via a
// separate update. AddNode must not require the type's fields up front, but
// Activate must still refuse to go live while a node is left unconfigured.
func TestE2EAutomation_CreateEmptyActionNodeThenConfigure(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	ownerUsername := "automation-empty-node-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationemptynodeowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationemptynodeowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Empty Then Configure")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, "trigger", "task_created", nil)
	// Creating an add_tag action with no config must succeed — this used to
	// fail with AUTOMATION_NODE_CONFIG_INVALID ("add_tag requires tag"),
	// which made it impossible to ever add this node type from the canvas.
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, "action", "add_tag", nil)
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)

	// Still unconfigured — Activate must refuse to go live.
	activateAutomationViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, http.StatusBadRequest)

	// Configure it for real, then Activate should succeed.
	updateAutomationNodeConfigViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, actionID, map[string]any{"tag": "ready"}, http.StatusOK)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)
}

// ---------------------------------------------------------------------------
// Engine: status_changed trigger -> assign action
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_StatusChangedReassignsTask(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-engine-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationengineowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationengineowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	secondMemberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-engine-member-"+uuid.NewString(), "automationenginemember1")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Engine Test Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Status Reassign Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": secondMemberID})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)

	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, task, secondMemberID, 20*time.Second)
}

// TestE2EAutomationEngine_MultipleTriggersShareOneChain verifies the
// n8n-style semantics that didn't exist in the old model: a graph can have
// more than one trigger node, and EITHER firing starts the same downstream
// chain.
func TestE2EAutomationEngine_MultipleTriggersShareOneChain(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-multitrigger-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationmultitrigger1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationmultitrigger1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-multitrigger-member-"+uuid.NewString(), "automationmultitriggerm1")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Multi-trigger Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Multi Trigger Automation")
	priorityTrigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "priority_changed", nil)
	tagTrigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "tag_added", map[string]any{"tag": "urgent"})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": memberID})

	priorityTriggerID, _ := priorityTrigger["id"].(string)
	tagTriggerID, _ := tagTrigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, priorityTriggerID, actionID)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, tagTriggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	// Fire via the SECOND trigger (tag_added), not the first — proving either
	// entry point reaches the same shared action.
	req := mustRequest(env.ctx, t, http.MethodPatch,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, task), jsonBody(t, map[string]any{"tags": []string{"urgent"}}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ownerToken)
	resp := mustDo(t, ownerClient, req)
	_ = resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, task, memberID, 20*time.Second)
}

// ---------------------------------------------------------------------------
// Engine: Condition node — N-branch switch
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_ConditionBranchesToCorrectAction(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-condition-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationconditionowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationconditionowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	bugMemberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-condition-bug-"+uuid.NewString(), "automationconditionbug1")
	elseMemberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-condition-else-"+uuid.NewString(), "automationconditionelse1")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{
		"title": "Condition Test Task", "importance": 9,
	})
	taskID, _ := task["id"].(string)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Condition Branch Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	condition := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"condition", "condition", map[string]any{
			"branches": []map[string]any{
				{
					"handle": "high_priority",
					"tree":   map[string]any{"field": "importance", "operator": "greater_than", "value": 5},
				},
			},
		})
	highPriorityAction := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": bugMemberID})
	elseAction := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": elseMemberID})

	triggerID, _ := trigger["id"].(string)
	conditionID, _ := condition["id"].(string)
	highPriorityActionID, _ := highPriorityAction["id"].(string)
	elseActionID, _ := elseAction["id"].(string)

	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, conditionID)
	highPriorityHandle := "high_priority"
	elseHandle := "else"
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, highPriorityActionID, &highPriorityHandle, http.StatusCreated)
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, elseActionID, &elseHandle, http.StatusCreated)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	// importance=9 > 5, so the "high_priority" branch should fire, not else.
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskID, inProgressID)
	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, taskID, bugMemberID, 20*time.Second)
}

// ---------------------------------------------------------------------------
// Engine: predecessor_done trigger — stateless AND-join
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_PredecessorDoneWaitsForAllWatchedTasks(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-predecessor-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationpredecessorowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationpredecessorowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-predecessor-member-"+uuid.NewString(), "automationpredecessormember1")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	doneID := statusIDByName(statuses, "Done")

	taskA := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Predecessor A")
	taskB := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Predecessor B")
	taskC := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Successor C")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Predecessor Done Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "predecessor_done", map[string]any{"watched_task_ids": []string{taskA, taskB}, "target_task_id": taskC})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": memberID})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	// Only taskA done — the AND-join must NOT fire yet (taskC stays unassigned).
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskA, doneID)
	time.Sleep(2 * time.Second)
	dataC := getTaskViaAPI(t, env, ownerClient, ownerToken, projID, taskC)
	if assignees, _ := dataC["assignee_ids"].([]any); len(assignees) != 0 {
		t.Fatalf("expected taskC to remain unassigned with only one of two predecessors done, got assignee_ids=%v", assignees)
	}

	// Now taskB also reaches done — ALL watched tasks are done, so the
	// trigger should fire and assign taskC.
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskB, doneID)
	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, taskC, memberID, 20*time.Second)
}

// ---------------------------------------------------------------------------
// Engine: cron trigger — recurring schedule fires against a fixed task
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_CronTriggerFiresOnSchedule(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	consumer := startAutomationConsumer(t, env)

	ownerUsername := "automation-cron-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationcronowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationcronowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-cron-assignee-"+uuid.NewString(), "automationcronassignee1")

	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{"title": "Cron Target Task"})
	taskID, _ := task["id"].(string)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Cron Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "cron", map[string]any{"cron_expression": "* * * * *", "target_task_id": taskID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": memberID})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	// Seed a far-in-the-past last-fired-at directly so the very first tick
	// already considers this node overdue, instead of waiting on the real
	// 1-minute cadence — the same technique DueDateScheduler tests would use
	// via WithInterval, applied here to the "is it due" state instead of the
	// tick cadence itself (cron's due-check is schedule-relative, not just
	// interval-relative).
	triggerUUID, err := uuid.Parse(triggerID)
	if err != nil {
		t.Fatalf("parse trigger id: %v", err)
	}
	automationUUID, err := uuid.Parse(automationID)
	if err != nil {
		t.Fatalf("parse automation id: %v", err)
	}
	if err := env.automationRepo.RecordCronFire(env.ctx, automationUUID, triggerUUID, time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatalf("seed cron fire: %v", err)
	}

	scheduler := worker.NewCronScheduler(env.redisClient, consumer, slog.New(slog.NewTextHandler(os.Stdout, nil))).
		WithInterval(200 * time.Millisecond).
		WithLeaderKey("e2e." + uuid.NewString())
	scheduler.Start(env.ctx)
	t.Cleanup(scheduler.Stop)

	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, taskID, memberID, 20*time.Second)
}

// ---------------------------------------------------------------------------
// Engine: api_trigger — webhook token lifecycle and dispatch
// ---------------------------------------------------------------------------

func TestE2EAutomation_WebhookTriggerTokenLifecycle(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-webhook-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationwebhookowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationwebhookowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-webhook-assignee-"+uuid.NewString(), "automationwebhookassignee1")

	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{"title": "Webhook Target Task"})
	taskID, _ := task["id"].(string)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Webhook Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "api_trigger", map[string]any{"target_task_id": taskID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": memberID})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)

	// Token generation doesn't require the automation to be active — but the
	// webhook call itself must still be rejected while it's in draft.
	rawToken := generateWebhookTokenViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID)
	postWebhookExpect(t, env, triggerID, rawToken, http.StatusNotFound)

	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	// Wrong / missing token rejected.
	postWebhookExpect(t, env, triggerID, "wrong-token", http.StatusUnauthorized)
	postWebhookExpect(t, env, triggerID, "", http.StatusUnauthorized)

	// Correct token accepted; the run fires asynchronously via the
	// external-triggers stream.
	postWebhookExpect(t, env, triggerID, rawToken, http.StatusAccepted)
	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, taskID, memberID, 20*time.Second)

	// Regenerating the token revokes the old one atomically.
	newToken := generateWebhookTokenViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID)
	postWebhookExpect(t, env, triggerID, rawToken, http.StatusUnauthorized)
	postWebhookExpect(t, env, triggerID, newToken, http.StatusAccepted)
}

// ---------------------------------------------------------------------------
// Engine: call_api action — outbound HTTP request
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_CallAPIActionMakesOutboundRequest(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	var mu sync.Mutex
	var gotMethod, gotHeader, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Test-Header")
		gotBody = string(body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"received":true}`))
	}))
	defer server.Close()

	ownerUsername := "automation-callapi-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationcallapiowner1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationcallapiowner1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{"title": "Call API Task"})
	taskID, _ := task["id"].(string)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Call API Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "call_api", map[string]any{
			"method":  "POST",
			"url":     server.URL,
			"headers": map[string]any{"X-Test-Header": "e2e"},
			"body":    `{"hello":"world"}`,
		})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskID, inProgressID)

	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		method := gotMethod
		mu.Unlock()
		if method != "" || time.Now().After(deadline) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotMethod != http.MethodPost {
		t.Fatalf("expected the automation to POST to the test server, got method %q", gotMethod)
	}
	if gotHeader != "e2e" {
		t.Fatalf("expected the configured header to reach the server, got %q", gotHeader)
	}
	if gotBody != `{"hello":"world"}` {
		t.Fatalf("expected the configured body to reach the server, got %q", gotBody)
	}

	runs := listAutomationRunsViaAPI(t, env, ownerClient, ownerToken, projID, automationID)
	if len(runs) == 0 {
		t.Fatal("expected at least one recorded run")
	}
	runID, _ := runs[0]["id"].(string)
	steps := listAutomationRunStepsViaAPI(t, env, ownerClient, ownerToken, projID, automationID, runID)
	found := false
	for _, step := range steps {
		output, ok := step["output_snapshot"].(map[string]any)
		if !ok {
			continue
		}
		if output["status_code"] == float64(http.StatusOK) {
			found = true
			if _, ok := output["response_body"].(string); !ok {
				t.Fatal("expected response_body to be recorded in the run step's output_snapshot")
			}
		}
	}
	if !found {
		t.Fatalf("expected a run step recording status_code=200, got steps: %v", steps)
	}
}

// ---------------------------------------------------------------------------
// Engine: optional target_task_id — a task-less trigger may only reach
// call_api actions downstream
// ---------------------------------------------------------------------------

func TestE2EAutomation_AddEdge_TaskLessTriggerToConditionRejected(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	ownerUsername := "automation-nulltask-cond-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationnulltaskcond1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationnulltaskcond1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Task-less Cron To Condition")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "cron", map[string]any{"cron_expression": "* * * * *"})
	condition := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"condition", "condition", map[string]any{"branches": []any{}})
	triggerID, _ := trigger["id"].(string)
	conditionID, _ := condition["id"].(string)

	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, triggerID, conditionID, nil, http.StatusBadRequest)
}

func TestE2EAutomation_AddEdge_TaskLessTriggerToCallAPIAllowedAndActivatable(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	ownerUsername := "automation-nulltask-callapi-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationnulltaskcallapi1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationnulltaskcallapi1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Task-less Cron To Call API")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "cron", map[string]any{"cron_expression": "* * * * *"})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "call_api", map[string]any{"method": "GET", "url": "https://example.com/hook"})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)

	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)
}

func TestE2EAutomation_AddEdge_TaskLessTriggerToTriggerAIAgentAllowedAndActivatable(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	ownerUsername := "automation-nulltask-aiagent-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationnulltaskaiagent1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationnulltaskaiagent1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-nulltask-aiagent-member-"+uuid.NewString(), "automationnulltaskaiagentmember1")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Task-less Cron To Trigger AI Agent")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "cron", map[string]any{"cron_expression": "* * * * *"})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "trigger_ai_agent", map[string]any{"member_id": memberID, "message": "do the thing"})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)

	// The connection is allowed regardless of whether memberID actually
	// resolves to an agent — that's a runtime concern (applyDirectAgentMessage
	// checks it when the walk reaches the node), not a graph-shape one.
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)
}

func TestE2EAutomation_UpdateNode_ClearingTargetTaskRejectedWhenDownstreamNeedsTask(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	ownerUsername := "automation-nulltask-update-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationnulltaskupdate1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationnulltaskupdate1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{"title": "Update Node Target Task"})
	taskID, _ := task["id"].(string)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Clear Target Task Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "cron", map[string]any{"cron_expression": "* * * * *", "target_task_id": taskID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "x"})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)

	// add_tag needs a task — clearing this trigger's target_task_id now
	// (omitting it from the replacement config entirely) would leave that
	// edge unrunnable, so it must be rejected.
	updateAutomationNodeConfigViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, triggerID,
		map[string]any{"cron_expression": "* * * * *"}, http.StatusBadRequest)
}

func TestE2EAutomationEngine_CronTriggerWithNoTargetTask_FiresCallAPIOnlyWithNilRunTask(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	consumer := startAutomationConsumer(t, env)

	var mu sync.Mutex
	var hit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hit = true
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ownerUsername := "automation-nulltask-cron-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationnulltaskcron1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationnulltaskcron1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Task-less Cron Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "cron", map[string]any{"cron_expression": "* * * * *"})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "call_api", map[string]any{"method": "GET", "url": server.URL})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	triggerUUID, err := uuid.Parse(triggerID)
	if err != nil {
		t.Fatalf("parse trigger id: %v", err)
	}
	automationUUID, err := uuid.Parse(automationID)
	if err != nil {
		t.Fatalf("parse automation id: %v", err)
	}
	if err := env.automationRepo.RecordCronFire(env.ctx, automationUUID, triggerUUID, time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatalf("seed cron fire: %v", err)
	}

	scheduler := worker.NewCronScheduler(env.redisClient, consumer, slog.New(slog.NewTextHandler(os.Stdout, nil))).
		WithInterval(200 * time.Millisecond).
		WithLeaderKey("e2e." + uuid.NewString())
	scheduler.Start(env.ctx)
	t.Cleanup(scheduler.Stop)

	deadline := time.Now().Add(20 * time.Second)
	for {
		mu.Lock()
		got := hit
		mu.Unlock()
		if got || time.Now().After(deadline) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	mu.Lock()
	got := hit
	mu.Unlock()
	if !got {
		t.Fatal("expected the task-less cron trigger to fire and call the test server")
	}

	runs := listAutomationRunsViaAPI(t, env, ownerClient, ownerToken, projID, automationID)
	if len(runs) == 0 {
		t.Fatal("expected at least one recorded run")
	}
	if runs[0]["task_id"] != nil {
		t.Fatalf("expected the run's task_id to be nil for a task-less trigger, got %v", runs[0]["task_id"])
	}
}

// ---------------------------------------------------------------------------
// Engine: retargeting a condition/action onto a related task
// (parent/children/linked) instead of the walk's own bound task
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_ActionRetargetedToChildren_FansOutToEveryChild(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-target-children-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationtargetchildren1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationtargetchildren1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")
	doneID := statusIDByName(statuses, "Done")

	parent := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{"title": "Parent Task"})
	parentID := idOf(parent)
	child1 := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{"title": "Child 1", "parent_task_id": parentID})
	child2 := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{"title": "Child 2", "parent_task_id": parentID})
	child1ID, child2ID := idOf(child1), idOf(child2)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Retarget To Children")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "set_status", map[string]any{
			"status_id": doneID,
			"target":    map[string]any{"kind": "children"},
		})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, parentID, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, child1ID, 20*time.Second, func(data map[string]any) bool {
		return data["status_id"] == doneID
	})
	waitForTaskField(t, env, ownerClient, ownerToken, projID, child2ID, 20*time.Second, func(data map[string]any) bool {
		return data["status_id"] == doneID
	})

	// The parent itself must NOT have been changed — only its children.
	parentData := getTaskViaAPI(t, env, ownerClient, ownerToken, projID, parentID)
	if parentData["status_id"] == doneID {
		t.Fatal("expected the retargeted action to leave the parent's own status untouched")
	}
}

func TestE2EAutomationEngine_ConditionRetargetedToBlockingTask(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-target-blocks-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationtargetblocks1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationtargetblocks1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")
	doneID := statusIDByName(statuses, "Done")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Blocked Task")
	blocker := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Blocking Task")

	// task "is_blocked_by" blocker, i.e. blocker "blocks" task.
	linkBody := jsonBody(t, map[string]any{"target_task_id": task, "link_type": "blocks"})
	linkReq := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, blocker), linkBody)
	linkReq.Header.Set("Content-Type", "application/json")
	linkReq.Header.Set("Authorization", "Bearer "+ownerToken)
	linkResp := mustDo(t, ownerClient, linkReq)
	defer func() { _ = linkResp.Body.Close() }()
	assertStatus(t, linkResp, http.StatusCreated)

	// Mark the blocker Done first so the condition (checked against the
	// blocking task, not `task` itself) is already satisfied when the walk
	// runs, then trigger the walk off `task`'s own status change.
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, blocker, doneID)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Retarget To Blocking Task")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	condition := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"condition", "condition", map[string]any{
			"branches": []any{
				map[string]any{
					"handle": "blocker_done",
					"tree": map[string]any{
						"field": "status_id", "operator": "equals", "value": doneID,
						"target": map[string]any{"kind": "is_blocked_by"},
					},
				},
			},
		})
	tagAction := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "unblocked"})
	triggerID, _ := trigger["id"].(string)
	conditionID, _ := condition["id"].(string)
	tagActionID, _ := tagAction["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, conditionID)
	handle := "blocker_done"
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, tagActionID, &handle, http.StatusCreated)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, task, 20*time.Second, func(data map[string]any) bool {
		tags, _ := data["tags"].([]any)
		for _, tag := range tags {
			if tag == "unblocked" {
				return true
			}
		}
		return false
	})
}

// ---------------------------------------------------------------------------
// Engine: every remaining built-in trigger type actually fires
// (status_changed, predecessor_done, cron, and api_trigger are covered
// above; this fills in task_created, assignee_changed, priority_changed,
// and due_date_reached)
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_TaskCreatedTriggerFires(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-taskcreated-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationtaskcreated1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationtaskcreated1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-taskcreated-member-"+uuid.NewString(), "automationtaskcreatedm1")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Task Created Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, "trigger", "task_created", nil)
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": memberID})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Fresh Task")
	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, task, memberID, 20*time.Second)
}

func TestE2EAutomationEngine_AssigneeChangedTriggerFires(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-assigneechanged-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationassigneechanged1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationassigneechanged1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-assigneechanged-member-"+uuid.NewString(), "automationassigneechangedm1")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Assignee Changed Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Assignee Changed Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, "trigger", "assignee_changed", nil)
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "reassigned"})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	patchTaskFieldsViaAPI(t, env, ownerClient, ownerToken, projID, task, map[string]any{"assignee_ids": []string{memberID}})

	waitForTaskField(t, env, ownerClient, ownerToken, projID, task, 20*time.Second, func(data map[string]any) bool {
		return taskHasTag(data, "reassigned")
	})
}

func TestE2EAutomationEngine_PriorityChangedTriggerFiresOnItsOwn(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-prioritychanged-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationprioritychanged1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationprioritychanged1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Priority Changed Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Priority Changed Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, "trigger", "priority_changed", nil)
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "priority-touched"})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	patchTaskFieldsViaAPI(t, env, ownerClient, ownerToken, projID, task, map[string]any{"importance": 8})

	waitForTaskField(t, env, ownerClient, ownerToken, projID, task, 20*time.Second, func(data map[string]any) bool {
		return taskHasTag(data, "priority-touched")
	})
}

func TestE2EAutomationEngine_DueDateReachedTriggerFires(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	consumer := startAutomationConsumer(t, env)

	ownerUsername := "automation-duedate-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationduedate1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationduedate1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-duedate-member-"+uuid.NewString(), "automationduedatem1")

	pastDue := time.Now().Add(-1 * time.Hour)
	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{
		"title": "Due Date Task", "due_date": pastDue.Format(time.RFC3339),
	})
	taskID := idOf(task)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Due Date Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "due_date_reached", map[string]any{"due_date_offset_minutes": 0})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": memberID})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	scheduler := worker.NewDueDateScheduler(env.redisClient, consumer, slog.New(slog.NewTextHandler(os.Stdout, nil))).
		WithInterval(200 * time.Millisecond).
		WithLeaderKey("e2e." + uuid.NewString())
	scheduler.Start(env.ctx)
	t.Cleanup(scheduler.Stop)

	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, taskID, memberID, 20*time.Second)
}

// ---------------------------------------------------------------------------
// Engine: every remaining built-in action type actually applies
// (assign, add_tag, and call_api are covered above; this fills in
// set_priority, set_custom_field, and trigger_ai_agent's task-bound path)
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_SetPriorityActionApplies(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-setpriority-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationsetpriority1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationsetpriority1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{"title": "Set Priority Task", "importance": 1})
	taskID := idOf(task)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Set Priority Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "set_priority", map[string]any{"importance": 9})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskID, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, taskID, 20*time.Second, func(data map[string]any) bool {
		imp, _ := data["importance"].(float64)
		return imp == 9
	})
}

func TestE2EAutomationEngine_SetCustomFieldActionApplies(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-setcustomfield-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationsetcustomfield1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationsetcustomfield1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	createCustomFieldViaAPI(t, env, ownerClient, ownerToken, projID, map[string]any{
		"field_key": "triage", "display_name": "Triage", "field_type": "text",
	})

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Set Custom Field Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Set Custom Field Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "set_custom_field", map[string]any{"field_key": "triage", "value": "reviewed"})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, task, 20*time.Second, func(data map[string]any) bool {
		cf, _ := data["custom_fields"].(map[string]any)
		return cf["triage"] == "reviewed"
	})
}

func TestE2EAutomationEngine_TriggerAIAgentAssignsTaskWhenTaskIsPresent(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-aiagent-task-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationaiagenttask1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationaiagenttask1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	roleID := projectRoleIDByName(t, env, ownerClient, ownerToken, projID, "Editor")
	_, createEnv := createAgentRequest(t, env, ownerClient, ownerToken, projID,
		llmAgentBody(roleID, "aiagent-"+uuid.NewString(), nil))
	agentID, _ := assertDataMap(t, createEnv)["id"].(string)

	members := listProjectMembersViaAPI(t, env, ownerClient, ownerToken, projID)
	agentMemberID := memberIDForAgent(members, agentID)
	if agentMemberID == "" {
		t.Fatalf("expected agent %q to resolve to a project member", agentID)
	}

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")
	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "AI Agent Assign Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Trigger AI Agent Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "trigger_ai_agent", map[string]any{"member_id": agentMemberID, "message": "please pick this up"})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)

	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, task, agentMemberID, 20*time.Second)
}

// ---------------------------------------------------------------------------
// Engine: condition fields/operators beyond importance greater_than
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_ConditionOnTaskType(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-tasktype-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationtasktype1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationtasktype1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	taskTypes := listTaskTypesViaAPI(t, env, ownerClient, ownerToken, projID)
	bugTypeID := taskTypeIDByName(taskTypes, "Bug")
	if bugTypeID == "" {
		t.Fatal("expected a default 'Bug' task type")
	}

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{
		"title": "Bug Task", "task_type_id": bugTypeID,
	})
	taskID := idOf(task)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Task Type Condition Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	condition := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"condition", "condition", map[string]any{
			"branches": []map[string]any{
				{"handle": "is_bug", "tree": map[string]any{"field": "task_type_id", "operator": "equals", "value": bugTypeID}},
			},
		})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "bug-flagged"})
	triggerID, _ := trigger["id"].(string)
	conditionID, _ := condition["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, conditionID)
	handle := "is_bug"
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, actionID, &handle, http.StatusCreated)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskID, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, taskID, 20*time.Second, func(data map[string]any) bool {
		return taskHasTag(data, "bug-flagged")
	})
}

func TestE2EAutomationEngine_ConditionOnAssigneeContains(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-assigneecond-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationassigneecond1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationassigneecond1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	watchedMemberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-assigneecond-watched-"+uuid.NewString(), "automationassigneecondw1")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{
		"title": "Assignee Condition Task", "assignee_ids": []string{watchedMemberID},
	})
	taskID := idOf(task)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Assignee Condition Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	condition := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"condition", "condition", map[string]any{
			"branches": []map[string]any{
				{"handle": "is_watched", "tree": map[string]any{"field": "assignee_ids", "operator": "contains", "value": watchedMemberID}},
			},
		})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "watched-assignee"})
	triggerID, _ := trigger["id"].(string)
	conditionID, _ := condition["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, conditionID)
	handle := "is_watched"
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, actionID, &handle, http.StatusCreated)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskID, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, taskID, 20*time.Second, func(data map[string]any) bool {
		return taskHasTag(data, "watched-assignee")
	})
}

func TestE2EAutomationEngine_ConditionOnTagsContains(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-tagscond-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationtagscond1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationtagscond1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{
		"title": "Tags Condition Task", "tags": []string{"security"},
	})
	taskID := idOf(task)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Tags Condition Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	condition := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"condition", "condition", map[string]any{
			"branches": []map[string]any{
				{"handle": "is_security", "tree": map[string]any{"field": "tags", "operator": "contains", "value": "security"}},
			},
		})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "set_priority", map[string]any{"importance": 10})
	triggerID, _ := trigger["id"].(string)
	conditionID, _ := condition["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, conditionID)
	handle := "is_security"
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, actionID, &handle, http.StatusCreated)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskID, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, taskID, 20*time.Second, func(data map[string]any) bool {
		imp, _ := data["importance"].(float64)
		return imp == 10
	})
}

func TestE2EAutomationEngine_ConditionOnCustomFieldEquals(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-customfieldcond-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationcustomfieldcond1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationcustomfieldcond1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	createCustomFieldViaAPI(t, env, ownerClient, ownerToken, projID, map[string]any{
		"field_key": "severity", "display_name": "Severity", "field_type": "text",
	})

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{
		"title": "Custom Field Condition Task", "custom_fields": map[string]any{"severity": "critical"},
	})
	taskID := idOf(task)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Custom Field Condition Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	condition := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"condition", "condition", map[string]any{
			"branches": []map[string]any{
				{"handle": "is_critical", "tree": map[string]any{"field": "custom_field", "field_key": "severity", "operator": "equals", "value": "critical"}},
			},
		})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "critical-flagged"})
	triggerID, _ := trigger["id"].(string)
	conditionID, _ := condition["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, conditionID)
	handle := "is_critical"
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, actionID, &handle, http.StatusCreated)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskID, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, taskID, 20*time.Second, func(data map[string]any) bool {
		return taskHasTag(data, "critical-flagged")
	})
}

func TestE2EAutomationEngine_ConditionIsEmptyOperator(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-isempty-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationisempty1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationisempty1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Is Empty Condition Task") // no assignees

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Is Empty Condition Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	condition := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"condition", "condition", map[string]any{
			"branches": []map[string]any{
				{"handle": "no_assignee", "tree": map[string]any{"field": "assignee_ids", "operator": "is_empty"}},
			},
		})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "needs-owner"})
	triggerID, _ := trigger["id"].(string)
	conditionID, _ := condition["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, conditionID)
	handle := "no_assignee"
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, actionID, &handle, http.StatusCreated)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, task, 20*time.Second, func(data map[string]any) bool {
		return taskHasTag(data, "needs-owner")
	})
}

func TestE2EAutomationEngine_ConditionElseBranchFiresWhenNoBranchMatches(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-elsebranch-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationelsebranch1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationelsebranch1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	bugMemberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-elsebranch-bug-"+uuid.NewString(), "automationelsebranchbug1")
	elseMemberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-elsebranch-else-"+uuid.NewString(), "automationelsebranchelse1")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	// importance=1, NOT > 5, so this time the ELSE branch must fire.
	task := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{
		"title": "Else Branch Task", "importance": 1,
	})
	taskID := idOf(task)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Else Branch Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	condition := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"condition", "condition", map[string]any{
			"branches": []map[string]any{
				{"handle": "high_priority", "tree": map[string]any{"field": "importance", "operator": "greater_than", "value": 5}},
			},
		})
	highPriorityAction := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": bugMemberID})
	elseAction := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": elseMemberID})

	triggerID, _ := trigger["id"].(string)
	conditionID, _ := condition["id"].(string)
	highPriorityActionID, _ := highPriorityAction["id"].(string)
	elseActionID, _ := elseAction["id"].(string)

	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, conditionID)
	highPriorityHandle := "high_priority"
	elseHandle := "else"
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, highPriorityActionID, &highPriorityHandle, http.StatusCreated)
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, elseActionID, &elseHandle, http.StatusCreated)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, taskID, inProgressID)
	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, taskID, elseMemberID, 20*time.Second)
}

// ---------------------------------------------------------------------------
// Engine: graph structure — chaining, idempotency, and lifecycle after
// activation (archive/revert-to-draft/live edits)
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_ActionChainContinuesPastFirstAction(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-actionchain-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationactionchain1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationactionchain1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Action Chain Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Action Chain Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	firstAction := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "step1"})
	secondAction := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "set_priority", map[string]any{"importance": 10})
	triggerID, _ := trigger["id"].(string)
	firstActionID, _ := firstAction["id"].(string)
	secondActionID, _ := secondAction["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, firstActionID)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, firstActionID, secondActionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, task, 20*time.Second, func(data map[string]any) bool {
		imp, _ := data["importance"].(float64)
		return taskHasTag(data, "step1") && imp == 10
	})
}

func TestE2EAutomationEngine_IdempotentAssignSkipsRunStepOnSecondFire(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-idempotent-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationidempotent1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationidempotent1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-idempotent-member-"+uuid.NewString(), "automationidempotentm1")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Idempotent Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Idempotent Automation")
	statusTrigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	priorityTrigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "priority_changed", nil)
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": memberID})
	statusTriggerID, _ := statusTrigger["id"].(string)
	priorityTriggerID, _ := priorityTrigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, statusTriggerID, actionID)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, priorityTriggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	// First fire (status_changed): applies the assignment.
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)
	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, task, memberID, 20*time.Second)

	// Second fire (priority_changed): the member is already assigned, so
	// this visit of the SAME action node must be a no-op, recorded as
	// "skipped" rather than silently doing nothing unobserved.
	patchTaskFieldsViaAPI(t, env, ownerClient, ownerToken, projID, task, map[string]any{"importance": 7})

	var skippedStepFound bool
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		runs := listAutomationRunsViaAPI(t, env, ownerClient, ownerToken, projID, automationID)
		for _, run := range runs {
			if run["trigger_node_id"] != priorityTriggerID {
				continue
			}
			runID, _ := run["id"].(string)
			steps := listAutomationRunStepsViaAPI(t, env, ownerClient, ownerToken, projID, automationID, runID)
			for _, step := range steps {
				if step["node_id"] == actionID && step["status"] == "skipped" {
					skippedStepFound = true
				}
			}
		}
		if skippedStepFound {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !skippedStepFound {
		t.Fatal("expected the second (priority_changed-triggered) run to record a 'skipped' step for the already-satisfied assign action")
	}
}

func TestE2EAutomation_ArchivedAutomationDoesNotFire(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-archived-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationarchived1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationarchived1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-archived-member-"+uuid.NewString(), "automationarchivedm1")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")
	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Archived Automation Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Archived Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": memberID})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)
	archiveAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)

	// Give the engine ample time to (not) act, then assert nothing happened.
	time.Sleep(3 * time.Second)
	data := getTaskViaAPI(t, env, ownerClient, ownerToken, projID, task)
	if assignees, _ := data["assignee_ids"].([]any); len(assignees) != 0 {
		t.Fatalf("expected an archived automation to never fire, got assignee_ids=%v", assignees)
	}
}

func TestE2EAutomation_RevertToDraftStopsFiringThenReactivateResumes(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-revertdraft-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationrevertdraft1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationrevertdraft1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	memberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-revertdraft-member-"+uuid.NewString(), "automationrevertdraftm1")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")
	doneID := statusIDByName(statuses, "Done")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Revert Draft Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Revert Draft Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": memberID})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)
	revertAutomationToDraftViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	// Draft: firing the matching event must NOT assign anyone.
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)
	time.Sleep(3 * time.Second)
	data := getTaskViaAPI(t, env, ownerClient, ownerToken, projID, task)
	if assignees, _ := data["assignee_ids"].([]any); len(assignees) != 0 {
		t.Fatalf("expected a draft (reverted) automation to not fire, got assignee_ids=%v", assignees)
	}

	// Re-activate: a FRESH status transition into In Progress must now fire.
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, doneID)
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)
	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, task, memberID, 20*time.Second)
}

func TestE2EAutomation_DeletingEdgeStopsDownstreamFiring(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-deleteedge-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationdeleteedge1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationdeleteedge1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Delete Edge Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Delete Edge Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "should-not-appear"})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	edge := addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	edgeID, _ := edge["id"].(string)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	// Live-editing an active automation's graph is allowed — remove the
	// only edge, severing the trigger from its action.
	removeAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, edgeID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)
	time.Sleep(3 * time.Second)
	data := getTaskViaAPI(t, env, ownerClient, ownerToken, projID, task)
	if taskHasTag(data, "should-not-appear") {
		t.Fatal("expected removing the only edge to stop the action from firing")
	}
}

func TestE2EAutomation_UpdatingActiveActionConfigAffectsNextFire(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-updateconfig-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationupdateconfig1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationupdateconfig1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	firstMemberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-updateconfig-first-"+uuid.NewString(), "automationupdateconfigf1")
	secondMemberID := addProjectMemberWithAutomationPerms(t, env, ownerClient, ownerToken, projID,
		"automation-updateconfig-second-"+uuid.NewString(), "automationupdateconfigs1")

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")
	doneID := statusIDByName(statuses, "Done")

	task := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Update Config Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Update Config Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "assign", map[string]any{"member_id": firstMemberID})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)
	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, task, firstMemberID, 20*time.Second)

	// Edit the action's config WHILE the automation stays active.
	updateAutomationNodeConfigViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, actionID,
		map[string]any{"member_id": secondMemberID}, http.StatusOK)

	// A fresh status transition (Done -> In Progress again) must now assign
	// the NEW member, proving the live edit took effect.
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, doneID)
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, task, inProgressID)
	waitForAutomationAssignee(t, env, ownerClient, ownerToken, projID, task, secondMemberID, 20*time.Second)
}

// ---------------------------------------------------------------------------
// Engine: retargeting — parent, an explicit other task, and match_mode=all
// (children fan-out and is_blocked_by are covered above)
// ---------------------------------------------------------------------------

func TestE2EAutomationEngine_ActionRetargetedToParent(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-target-parent-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationtargetparent1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationtargetparent1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	parent := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Parent Task For Retarget")
	child := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{
		"title": "Child Task For Retarget", "parent_task_id": parent,
	})
	childID := idOf(child)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Retarget To Parent")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{
			"tag": "child-touched-parent", "target": map[string]any{"kind": "parent"},
		})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	// Fire off the CHILD's own status change — the action should touch the
	// PARENT, not the child itself.
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, childID, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, parent, 20*time.Second, func(data map[string]any) bool {
		return taskHasTag(data, "child-touched-parent")
	})

	childData := getTaskViaAPI(t, env, ownerClient, ownerToken, projID, childID)
	if taskHasTag(childData, "child-touched-parent") {
		t.Fatal("expected the retargeted action to leave the child's own tags untouched")
	}
}

func TestE2EAutomationEngine_ActionRetargetedToOtherExplicitTask(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-target-other-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationtargetother1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationtargetother1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")

	triggerTask := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Trigger Task For Other Retarget")
	otherTask := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Other Explicit Target Task")

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Retarget To Other")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	action := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{
			"tag":    "touched-via-other",
			"target": map[string]any{"kind": "other", "other_task_id": otherTask},
		})
	triggerID, _ := trigger["id"].(string)
	actionID, _ := action["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, actionID)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, triggerTask, inProgressID)

	waitForTaskField(t, env, ownerClient, ownerToken, projID, otherTask, 20*time.Second, func(data map[string]any) bool {
		return taskHasTag(data, "touched-via-other")
	})

	triggerTaskData := getTaskViaAPI(t, env, ownerClient, ownerToken, projID, triggerTask)
	if taskHasTag(triggerTaskData, "touched-via-other") {
		t.Fatal("expected the retargeted action to leave the triggering task's own tags untouched")
	}
}

func TestE2EAutomationEngine_ConditionMatchModeAllRequiresEveryChildToMatch(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startAutomationConsumer(t, env)

	ownerUsername := "automation-matchall-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "automationmatchall1")
	ownerClient, ownerToken := taskMemberLogin(t, env, ownerUsername, "automationmatchall1")
	projID := createProjectForTasksViaAPI(t, env, ownerClient, ownerToken)

	statuses := listTaskStatusesViaAPI(t, env, ownerClient, ownerToken, projID)
	inProgressID := statusIDByName(statuses, "In Progress")
	doneID := statusIDByName(statuses, "Done")

	parentID := createTaskViaAPI(t, env, ownerClient, ownerToken, projID, "Match All Parent")
	child1 := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{"title": "Match All Child 1", "parent_task_id": parentID})
	child2 := createTaskViaAPIWithBody(t, env, ownerClient, ownerToken, projID, map[string]any{"title": "Match All Child 2", "parent_task_id": parentID})
	child1ID, child2ID := idOf(child1), idOf(child2)

	automationID := createAutomationViaAPI(t, env, ownerClient, ownerToken, projID, "Match All Automation")
	trigger := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"trigger", "status_changed", map[string]any{"status_id": inProgressID})
	condition := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"condition", "condition", map[string]any{
			"branches": []map[string]any{
				{
					"handle": "all_children_done",
					"tree": map[string]any{
						"field": "status_id", "operator": "equals", "value": doneID,
						"target": map[string]any{"kind": "children"}, "match_mode": "all",
					},
				},
			},
		})
	allDoneAction := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "all-done"})
	notAllAction := addAutomationNodeViaAPI(t, env, ownerClient, ownerToken, projID, automationID,
		"action", "add_tag", map[string]any{"tag": "not-all-done"})
	triggerID, _ := trigger["id"].(string)
	conditionID, _ := condition["id"].(string)
	allDoneActionID, _ := allDoneAction["id"].(string)
	notAllActionID, _ := notAllAction["id"].(string)
	addAutomationEdgeViaAPI(t, env, ownerClient, ownerToken, projID, automationID, triggerID, conditionID)
	allDoneHandle := "all_children_done"
	elseHandle := "else"
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, allDoneActionID, &allDoneHandle, http.StatusCreated)
	addAutomationEdgeViaAPIExpect(t, env, ownerClient, ownerToken, projID, automationID, conditionID, notAllActionID, &elseHandle, http.StatusCreated)
	activateAutomationViaAPI(t, env, ownerClient, ownerToken, projID, automationID)

	// Only child1 done — match_mode=all must NOT be satisfied yet.
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, child1ID, doneID)
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, parentID, inProgressID)
	waitForTaskField(t, env, ownerClient, ownerToken, projID, parentID, 20*time.Second, func(data map[string]any) bool {
		return taskHasTag(data, "not-all-done")
	})

	// Now both children are done — a FRESH status transition must take the
	// "all_children_done" branch instead.
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, child2ID, doneID)
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, parentID, doneID)
	setTaskStatusViaAPI(t, env, ownerClient, ownerToken, projID, parentID, inProgressID)
	waitForTaskField(t, env, ownerClient, ownerToken, projID, parentID, 20*time.Second, func(data map[string]any) bool {
		return taskHasTag(data, "all-done")
	})
}
