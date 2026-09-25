package e2e_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/jev"
	"github.com/Paca-AI/api/internal/worker"
)

// ---------------------------------------------------------------------------
// Fake Jev server
// ---------------------------------------------------------------------------

// fakeJev is a local stand-in for the Jev System One API. Every request is
// recorded (with its bearer token) and answered by respond, which a test can
// swap at any time. It speaks the same request/response contract as
// platform/jev, so the real client, workers and handlers talk to it
// unmodified — only the project's jev_base_url points here.
type fakeJev struct {
	srv *httptest.Server

	mu       sync.Mutex
	requests []jev.Request
	auths    []string
	respond  func(req jev.Request) (int, jev.Response)
}

func newFakeJev(t *testing.T) *fakeJev {
	t.Helper()
	f := &fakeJev{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jev.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.requests = append(f.requests, req)
		f.auths = append(f.auths, r.Header.Get("Authorization"))
		respond := f.respond
		f.mu.Unlock()

		status, resp := http.StatusOK, jev.Response{Answers: map[string]jev.Answer{}}
		if respond != nil {
			status, resp = respond(req)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeJev) setResponder(fn func(req jev.Request) (int, jev.Response)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.respond = fn
}

func (f *fakeJev) snapshot() ([]jev.Request, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]jev.Request(nil), f.requests...), append([]string(nil), f.auths...)
}

// requestWithQuestion returns the first recorded request that asked
// questionID, or nil.
func (f *fakeJev) requestWithQuestion(questionID string) *jev.Request {
	reqs, _ := f.snapshot()
	for i := range reqs {
		if _, ok := reqs[i].Questions[questionID]; ok {
			return &reqs[i]
		}
	}
	return nil
}

// requestForTitle returns the first recorded request whose state carries
// the given task title, or nil.
func (f *fakeJev) requestForTitle(title string) *jev.Request {
	reqs, _ := f.snapshot()
	for i := range reqs {
		if state, ok := reqs[i].State.(map[string]any); ok && state["title"] == title {
			return &reqs[i]
		}
	}
	return nil
}

func confidence(v float64) *float64 { return &v }

// ---------------------------------------------------------------------------
// Worker + API helpers
// ---------------------------------------------------------------------------

// startJevWorkers starts real TaskAutofillConsumer / TaskAutoAssignConsumer
// instances on the shared Valkey stream, each in a consumer group of its own
// (see startAutomationConsumer for why), with a plain HTTP transport so the
// Jev client can reach the test's loopback fake server.
func startJevWorkers(t *testing.T, env *e2eEnv, autofill, autoAssign bool) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	plain := &http.Client{Timeout: 10 * time.Second}
	if autofill {
		c := worker.NewTaskAutofillConsumer(env.redisClient, env.taskSvc, env.taskRepo, env.projectSvc, env.activitySvc, nil, log).
			WithHTTPClient(plain).
			WithConsumerGroup("e2e.autofill." + uuid.NewString())
		c.Start(env.ctx)
		t.Cleanup(c.Stop)
	}
	if autoAssign {
		c := worker.NewTaskAutoAssignConsumer(env.redisClient, env.taskSvc, env.projectRepo, env.projectSvc, env.activitySvc, nil, log).
			WithHTTPClient(plain).
			WithConsumerGroup("e2e.autoassign." + uuid.NewString())
		c.Start(env.ctx)
		t.Cleanup(c.Stop)
	}
}

func apiJSON(t *testing.T, env *e2eEnv, client *http.Client, token, method, path string, body any, wantStatus int) map[string]any {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = mustRequest(env.ctx, t, method, env.base+path, jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = mustRequest(env.ctx, t, method, env.base+path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	var e envelope
	decodeJSON(t, resp, &e)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: expected HTTP %d, got %d (error_code=%q error=%q)", method, path, wantStatus, resp.StatusCode, e.ErrorCode, e.Error)
	}
	m, _ := e.Data.(map[string]any)
	return m
}

func configureJevViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string, body map[string]any) map[string]any {
	t.Helper()
	return apiJSON(t, env, client, token, http.MethodPatch,
		fmt.Sprintf("/api/v1/projects/%s/jev-config", projectID), body, http.StatusOK)
}

func setProjectJevSettingsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string, jevSettings map[string]any) {
	t.Helper()
	apiJSON(t, env, client, token, http.MethodPatch,
		fmt.Sprintf("/api/v1/projects/%s", projectID),
		map[string]any{"settings": map[string]any{"jev": jevSettings}}, http.StatusOK)
}

// waitForStreamActivity waits for a task activity event for taskID on the
// shared task-activity stream that check accepts. Activities reach the
// task's feed through a separate ActivityConsumer the harness doesn't run,
// so the stream entry is what proves the worker recorded one.
func waitForStreamActivity(t *testing.T, env *e2eEnv, taskID string, timeout time.Duration, check func(payload map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msgs, err := env.redisClient.XRevRangeN(env.ctx, events.StreamActivities, "+", "-", 2000).Result()
		if err != nil {
			t.Fatalf("read task activity stream: %v", err)
		}
		for _, msg := range msgs {
			raw, _ := msg.Values["payload"].(string)
			var payload map[string]any
			if json.Unmarshal([]byte(raw), &payload) != nil || payload["task_id"] != taskID {
				continue
			}
			if check(payload) {
				return payload
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a matching activity event for task %s", taskID)
	return nil
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// newJevProject creates an owner and a project with Jev pointed at fake.
func newJevProject(t *testing.T, env *e2eEnv, prefix string, fake *fakeJev) (*http.Client, string, string) {
	t.Helper()
	username := prefix + "-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "jevowner-password1")
	client, token := taskMemberLogin(t, env, username, "jevowner-password1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	if fake != nil {
		configureJevViaAPI(t, env, client, token, projID, map[string]any{
			"api_key": "e2e-jev-key", "base_url": fake.srv.URL, "model": "e2e-model",
		})
	}
	return client, token, projID
}

// ---------------------------------------------------------------------------
// Per-project Jev configuration
// ---------------------------------------------------------------------------

func TestE2EJev_ConfigLifecycle(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	fake := newFakeJev(t)
	fake.setResponder(func(jev.Request) (int, jev.Response) {
		return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
			"connectivity": {Type: jev.TypeNoul, Noul: 1},
		}}
	})
	client, token, projID := newJevProject(t, env, "jev-config", nil)
	projPath := fmt.Sprintf("/api/v1/projects/%s", projID)
	testPath := projPath + "/jev-config/test"

	// A fresh project has no Jev config, and testing it is a client error.
	if got := apiJSON(t, env, client, token, http.MethodGet, projPath, nil, http.StatusOK); got["jev_configured"] != false {
		t.Fatalf("expected a new project to be unconfigured, got %v", got["jev_configured"])
	}
	apiJSON(t, env, client, token, http.MethodPost, testPath, nil, http.StatusBadRequest)

	// Save: surrounding whitespace is trimmed and the key is never echoed.
	saved := configureJevViaAPI(t, env, client, token, projID, map[string]any{
		"api_key": "  e2e-secret-key  ", "base_url": " " + fake.srv.URL + " ", "model": "e2e-model",
	})
	if saved["configured"] != true || saved["base_url"] != fake.srv.URL || saved["model"] != "e2e-model" {
		t.Fatalf("unexpected save response: %v", saved)
	}
	project := apiJSON(t, env, client, token, http.MethodGet, projPath, nil, http.StatusOK)
	if project["jev_configured"] != true || project["jev_base_url"] != fake.srv.URL || project["jev_model"] != "e2e-model" {
		t.Fatalf("unexpected project read after save: %v", project)
	}
	raw, _ := json.Marshal(project)
	if strings.Contains(string(raw), "e2e-secret-key") {
		t.Fatal("project read leaks the Jev API key")
	}

	// The connection test reaches the configured host with the stored key.
	if got := apiJSON(t, env, client, token, http.MethodPost, testPath, nil, http.StatusOK); got["success"] != true {
		t.Fatalf("expected a successful connection test, got %v", got)
	}
	reqs, auths := fake.snapshot()
	if len(reqs) != 1 || auths[0] != "Bearer e2e-secret-key" || reqs[0].Model != "e2e-model" {
		t.Fatalf("unexpected Jev traffic: %d requests, auths=%v", len(reqs), auths)
	}
	if _, ok := reqs[0].Questions["connectivity"]; !ok {
		t.Fatalf("expected the connectivity question, got %v", reqs[0].Questions)
	}

	// A provider-side failure surfaces as a 400, not a 5xx.
	fake.setResponder(func(jev.Request) (int, jev.Response) { return http.StatusUnauthorized, jev.Response{} })
	apiJSON(t, env, client, token, http.MethodPost, testPath, nil, http.StatusBadRequest)

	// Updating only the model leaves the key and host alone.
	partial := configureJevViaAPI(t, env, client, token, projID, map[string]any{"model": "other-model"})
	if partial["configured"] != true || partial["base_url"] != fake.srv.URL || partial["model"] != "other-model" {
		t.Fatalf("unexpected partial update response: %v", partial)
	}

	// An explicit empty key disables Jev for the project.
	cleared := configureJevViaAPI(t, env, client, token, projID, map[string]any{"api_key": ""})
	if cleared["configured"] != false {
		t.Fatalf("expected an empty key to clear Jev, got %v", cleared)
	}
	apiJSON(t, env, client, token, http.MethodPost, testPath, nil, http.StatusBadRequest)
}

func TestE2EJev_ConfigRequiresProjectWritePermission(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	ownerClient, ownerToken, projID := newJevProject(t, env, "jev-config-authz", nil)

	username := "jev-config-reader-" + uuid.NewString()
	seedUser(t, env, username, "jevreader-password1", username)
	user, err := env.userRepo.FindByUsername(env.ctx, username)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	roleID := createProjectRoleWithPermsViaAPI(t, env, ownerClient, ownerToken, projID, "reader-"+uuid.NewString(),
		map[string]any{"projects.read": true, "tasks.read": true})
	addMemberViaAPI(t, env, ownerClient, ownerToken, projID, user.ID.String(), roleID)
	readerClient, readerToken := taskMemberLogin(t, env, username, "jevreader-password1")

	apiJSON(t, env, readerClient, readerToken, http.MethodPatch,
		fmt.Sprintf("/api/v1/projects/%s/jev-config", projID), map[string]any{"api_key": "k"}, http.StatusForbidden)
	apiJSON(t, env, readerClient, readerToken, http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/jev-config/test", projID), nil, http.StatusForbidden)
}

// ---------------------------------------------------------------------------
// Task field auto-fill
// ---------------------------------------------------------------------------

func TestE2EJev_AutofillFillsBlankFieldsAndRecordsActivity(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startJevWorkers(t, env, true, false)
	fake := newFakeJev(t)
	fake.setResponder(func(jev.Request) (int, jev.Response) {
		return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
			// Score 4 is the top importance bucket (150); score 3 is the
			// fourth story-point bucket (3).
			"importance":   {Type: jev.TypeScore, Score: 4, Confidence: confidence(0.9)},
			"story_points": {Type: jev.TypeScore, Score: 3, Confidence: confidence(0.9)},
		}}
	})
	client, token, projID := newJevProject(t, env, "jev-autofill", fake)

	task := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "Production database is down"})
	taskID := idOf(task)

	waitForTaskField(t, env, client, token, projID, taskID, 20*time.Second, func(d map[string]any) bool {
		imp, _ := d["importance"].(float64)
		sp, _ := d["story_points"].(float64)
		return imp == 150 && sp == 3
	})

	req := fake.requestForTitle("Production database is down")
	if req == nil {
		t.Fatal("expected Jev to be asked about the new task")
	}
	for _, q := range []string{"importance", "story_points"} {
		if _, ok := req.Questions[q]; !ok {
			t.Errorf("expected question %q in the autofill request, got %v", q, req.Questions)
		}
	}

	// Autofilled changes still show up in the task's activity feed.
	waitForStreamActivity(t, env, taskID, 10*time.Second, func(a map[string]any) bool {
		raw, _ := json.Marshal(a)
		return a["activity_type"] == "task.updated" && strings.Contains(string(raw), "importance")
	})
}

func TestE2EJev_AutofillNeverOverwritesHumanSetFields(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startJevWorkers(t, env, true, false)
	fake := newFakeJev(t)
	fake.setResponder(func(jev.Request) (int, jev.Response) {
		// Even if Jev were (wrongly) asked about importance, it would say 150.
		return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
			"importance":   {Type: jev.TypeScore, Score: 4, Confidence: confidence(0.95)},
			"story_points": {Type: jev.TypeScore, Score: 5, Confidence: confidence(0.95)},
		}}
	})
	client, token, projID := newJevProject(t, env, "jev-autofill-human", fake)

	task := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Update the README", "importance": 10,
	})
	taskID := idOf(task)

	// story_points (left blank) gets filled; importance (set by a human) stays.
	data := waitForTaskField(t, env, client, token, projID, taskID, 20*time.Second, func(d map[string]any) bool {
		sp, _ := d["story_points"].(float64)
		return sp == 8 // score 5 is the sixth story-point bucket
	})
	if imp, _ := data["importance"].(float64); imp != 10 {
		t.Fatalf("expected the human-set importance to be kept, got %v", data["importance"])
	}
	req := fake.requestForTitle("Update the README")
	if req == nil {
		t.Fatal("expected Jev to be asked about the new task")
	}
	if _, asked := req.Questions["importance"]; asked {
		t.Error("Jev must not be asked about a field the human already set")
	}
}

func TestE2EJev_AutofillIgnoresLowConfidenceAnswers(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startJevWorkers(t, env, true, false)
	fake := newFakeJev(t)
	fake.setResponder(func(jev.Request) (int, jev.Response) {
		return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
			"importance":   {Type: jev.TypeScore, Score: 4, Confidence: confidence(0.2)},
			"story_points": {Type: jev.TypeScore, Score: 7, Confidence: confidence(0.9)},
		}}
	})
	client, token, projID := newJevProject(t, env, "jev-autofill-lowconf", fake)

	task := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "Vague task"})
	taskID := idOf(task)
	originalImportance := task["importance"]

	data := waitForTaskField(t, env, client, token, projID, taskID, 20*time.Second, func(d map[string]any) bool {
		sp, _ := d["story_points"].(float64)
		return sp == 21
	})
	if data["importance"] != originalImportance {
		t.Fatalf("a low-confidence answer must not be applied: importance %v -> %v", originalImportance, data["importance"])
	}
}

func TestE2EJev_AutofillRespectsProjectSettings(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startJevWorkers(t, env, true, false)
	fake := newFakeJev(t)
	fake.setResponder(func(jev.Request) (int, jev.Response) {
		return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
			"importance":   {Type: jev.TypeScore, Score: 4, Confidence: confidence(0.9)},
			"story_points": {Type: jev.TypeScore, Score: 2, Confidence: confidence(0.9)},
		}}
	})
	client, token, projID := newJevProject(t, env, "jev-autofill-settings", fake)

	// Excluding importance keeps it out of the question set entirely.
	setProjectJevSettingsViaAPI(t, env, client, token, projID, map[string]any{
		"autofill_enabled": true, "autofill_excluded_fields": []string{"importance"},
	})
	task := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "Excluded importance"})
	data := waitForTaskField(t, env, client, token, projID, idOf(task), 20*time.Second, func(d map[string]any) bool {
		sp, _ := d["story_points"].(float64)
		return sp == 2
	})
	if data["importance"] != task["importance"] {
		t.Fatalf("an excluded field must not be autofilled: %v -> %v", task["importance"], data["importance"])
	}
	if req := fake.requestForTitle("Excluded importance"); req == nil {
		t.Fatal("expected Jev to be asked about the task")
	} else if _, asked := req.Questions["importance"]; asked {
		t.Error("an excluded field must not be asked about")
	}

	// Disabling autofill stops Jev calls for new tasks altogether. A
	// follow-up task, created after the disabled one, proves the consumer
	// has already processed (and skipped) it by the time we look.
	setProjectJevSettingsViaAPI(t, env, client, token, projID, map[string]any{"autofill_enabled": false})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "Autofill disabled"})
	setProjectJevSettingsViaAPI(t, env, client, token, projID, map[string]any{"autofill_enabled": true})
	sentinel := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "Sentinel"})
	waitForTaskField(t, env, client, token, projID, idOf(sentinel), 20*time.Second, func(d map[string]any) bool {
		imp, _ := d["importance"].(float64)
		return imp == 150
	})
	if fake.requestForTitle("Autofill disabled") != nil {
		t.Fatal("Jev must not be called while autofill is disabled")
	}
}

func TestE2EJev_UnconfiguredProjectNeverCallsJev(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startJevWorkers(t, env, true, true)
	fake := newFakeJev(t)
	fake.setResponder(func(jev.Request) (int, jev.Response) {
		return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
			"importance": {Type: jev.TypeScore, Score: 4, Confidence: confidence(0.9)},
		}}
	})

	// Project A has no Jev key; project B (same workers) does. Once B's task
	// has been processed, A's earlier task has been through the same stream.
	clientA, tokenA, projA := newJevProject(t, env, "jev-unconfigured", nil)
	taskA := createTaskViaAPIWithBody(t, env, clientA, tokenA, projA, map[string]any{
		"title": "No Jev here", "assignment_mode": "auto",
	})
	clientB, tokenB, projB := newJevProject(t, env, "jev-configured", fake)
	taskB := createTaskViaAPIWithBody(t, env, clientB, tokenB, projB, map[string]any{"title": "Jev here"})
	waitForTaskField(t, env, clientB, tokenB, projB, idOf(taskB), 20*time.Second, func(d map[string]any) bool {
		imp, _ := d["importance"].(float64)
		return imp == 150
	})

	if fake.requestForTitle("No Jev here") != nil {
		t.Fatal("a project without a Jev key must never reach Jev")
	}
	data := getTaskViaAPI(t, env, clientA, tokenA, projA, idOf(taskA))
	if data["importance"] != taskA["importance"] {
		t.Errorf("unconfigured project's task was modified: %v -> %v", taskA["importance"], data["importance"])
	}
	if data["assignment_mode"] != "auto" {
		t.Errorf("unconfigured project's auto task should be left pending, got %v", data["assignment_mode"])
	}
}

// ---------------------------------------------------------------------------
// Task auto-assign
// ---------------------------------------------------------------------------

func TestE2EJev_AutoAssignPicksConfidentCandidate(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startJevWorkers(t, env, false, true)
	fake := newFakeJev(t)
	client, token, projID := newJevProject(t, env, "jev-autoassign", fake)

	backendMemberID := addProjectMemberWithAutomationPerms(t, env, client, token, projID,
		"jev-backend-"+uuid.NewString(), "jevbackend-password1")
	apiJSON(t, env, client, token, http.MethodPatch,
		fmt.Sprintf("/api/v1/projects/%s/members/%s", projID, backendMemberID),
		map[string]any{"description": "Backend engineer; owns the database and APIs"}, http.StatusOK)

	// Pick whichever candidate's criteria mention the database.
	fake.setResponder(func(req jev.Request) (int, jev.Response) {
		q := req.Questions["assignee"]
		criteria, _ := q.Criteria.(map[string]any)
		for id, desc := range criteria {
			if s, _ := desc.(string); strings.Contains(s, "database") {
				return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
					"assignee": {Type: jev.TypeChoice, Choice: id, Confidence: confidence(0.85)},
				}}
			}
		}
		return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
			"assignee": {Type: jev.TypeChoice, Choice: "__none__", Confidence: confidence(0.9)},
		}}
	})

	task := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Slow queries on the orders table", "assignment_mode": "auto",
	})
	taskID := idOf(task)
	if task["assignment_mode"] != "auto" {
		t.Fatalf("expected assignment_mode=auto on create, got %v", task["assignment_mode"])
	}

	data := waitForAutomationAssignee(t, env, client, token, projID, taskID, backendMemberID, 20*time.Second)
	if data["assignment_mode"] != "auto" {
		t.Errorf("a Jev-made assignment should keep assignment_mode=auto, got %v", data["assignment_mode"])
	}

	req := fake.requestWithQuestion("assignee")
	if req == nil {
		t.Fatal("expected an assignee question")
	}
	criteria, _ := req.Questions["assignee"].Criteria.(map[string]any)
	if _, ok := criteria[backendMemberID]; !ok {
		t.Errorf("expected the backend member among candidates, got %v", criteria)
	}
	if _, ok := criteria["__none__"]; !ok {
		t.Error("expected a 'nobody' option among candidates")
	}
}

func TestE2EJev_AutoAssignLowConfidenceRevertsToManual(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startJevWorkers(t, env, false, true)
	fake := newFakeJev(t)
	fake.setResponder(func(req jev.Request) (int, jev.Response) {
		criteria, _ := req.Questions["assignee"].Criteria.(map[string]any)
		for id := range criteria {
			if id != "__none__" {
				return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
					"assignee": {Type: jev.TypeChoice, Choice: id, Confidence: confidence(0.3)},
				}}
			}
		}
		return http.StatusOK, jev.Response{}
	})
	client, token, projID := newJevProject(t, env, "jev-autoassign-low", fake)

	task := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Ambiguous work item", "assignment_mode": "auto",
	})
	taskID := idOf(task)

	data := waitForTaskField(t, env, client, token, projID, taskID, 20*time.Second, func(d map[string]any) bool {
		return d["assignment_mode"] == "manual"
	})
	if assignees, _ := data["assignee_ids"].([]any); len(assignees) != 0 {
		t.Fatalf("a low-confidence pick must not assign anyone, got %v", assignees)
	}
	waitForStreamActivity(t, env, taskID, 10*time.Second, func(a map[string]any) bool {
		return a["activity_type"] == "task.auto_assign.skipped"
	})
}

func TestE2EJev_AutoAssignSkipsManualTasks(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	startJevWorkers(t, env, false, true)
	fake := newFakeJev(t)
	client, token, projID := newJevProject(t, env, "jev-autoassign-manual", fake)

	manual := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "Manual task"})
	// A later auto task that does reach Jev proves the manual one was seen first.
	fake.setResponder(func(jev.Request) (int, jev.Response) {
		return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
			"assignee": {Type: jev.TypeChoice, Choice: "__none__", Confidence: confidence(0.9)},
		}}
	})
	auto := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Auto task", "assignment_mode": "auto",
	})
	waitForTaskField(t, env, client, token, projID, idOf(auto), 20*time.Second, func(d map[string]any) bool {
		return d["assignment_mode"] == "manual"
	})
	if fake.requestForTitle("Manual task") != nil {
		t.Fatal("a manual-mode task must never be sent to Jev for assignment")
	}
	if data := getTaskViaAPI(t, env, client, token, projID, idOf(manual)); data["assignment_mode"] != "manual" {
		t.Errorf("expected manual task to stay manual, got %v", data["assignment_mode"])
	}
}

// ---------------------------------------------------------------------------
// Jev condition nodes in automations
// ---------------------------------------------------------------------------

// setupJevConditionAutomation builds: task_created -> jev_condition(choice
// urgent/routine) -> urgent: assign urgentMember, else: assign elseMember.
func setupJevConditionAutomation(t *testing.T, env *e2eEnv, fake *fakeJev, prefix string) (client *http.Client, token, projID, urgentMemberID, elseMemberID string) {
	t.Helper()
	startAutomationConsumer(t, env)
	client, token, projID = newJevProject(t, env, prefix, fake)

	urgentMemberID = addProjectMemberWithAutomationPerms(t, env, client, token, projID,
		prefix+"-urgent-"+uuid.NewString(), "jevurgent-password1")
	elseMemberID = addProjectMemberWithAutomationPerms(t, env, client, token, projID,
		prefix+"-else-"+uuid.NewString(), "jevelse-password1")

	automationID := createAutomationViaAPI(t, env, client, token, projID, "Jev routing")
	trigger := addAutomationNodeViaAPI(t, env, client, token, projID, automationID, "trigger", "task_created", nil)
	// Created empty first, like the canvas does, then configured.
	condition := addAutomationNodeViaAPI(t, env, client, token, projID, automationID, "condition", "jev_condition", nil)
	updateAutomationNodeConfigViaAPIExpect(t, env, client, token, projID, automationID, idOf(condition), map[string]any{
		"answer_type":  "choice",
		"instructions": "How urgent is this task?",
		"options": map[string]any{
			"urgent":  "Production is broken or customers are blocked",
			"routine": "Everything else",
		},
	}, http.StatusOK)
	urgentAction := addAutomationNodeViaAPI(t, env, client, token, projID, automationID,
		"action", "update_task", map[string]any{"update": map[string]any{"assignee_ids": []string{urgentMemberID}}})
	elseAction := addAutomationNodeViaAPI(t, env, client, token, projID, automationID,
		"action", "update_task", map[string]any{"update": map[string]any{"assignee_ids": []string{elseMemberID}}})

	addAutomationEdgeViaAPI(t, env, client, token, projID, automationID, idOf(trigger), idOf(condition))
	urgent, elseHandle := "urgent", "else"
	addAutomationEdgeViaAPIExpect(t, env, client, token, projID, automationID, idOf(condition), idOf(urgentAction), &urgent, http.StatusCreated)
	addAutomationEdgeViaAPIExpect(t, env, client, token, projID, automationID, idOf(condition), idOf(elseAction), &elseHandle, http.StatusCreated)
	activateAutomationViaAPI(t, env, client, token, projID, automationID)
	return client, token, projID, urgentMemberID, elseMemberID
}

func TestE2EJev_ConditionNodeRoutesOnConfidentAnswer(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	fake := newFakeJev(t)
	fake.setResponder(func(req jev.Request) (int, jev.Response) {
		state, _ := req.State.(map[string]any)
		choice := "routine"
		if title, _ := state["title"].(string); strings.Contains(title, "outage") {
			choice = "urgent"
		}
		return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
			"answer": {Type: jev.TypeChoice, Choice: choice, Confidence: confidence(0.9)},
		}}
	})
	client, token, projID, urgentMemberID, _ := setupJevConditionAutomation(t, env, fake, "jev-cond")

	taskID := createTaskViaAPI(t, env, client, token, projID, "Checkout outage in EU")
	waitForAutomationAssignee(t, env, client, token, projID, taskID, urgentMemberID, 20*time.Second)

	req := fake.requestForTitle("Checkout outage in EU")
	if req == nil {
		t.Fatal("expected the condition node to call Jev")
	}
	q := req.Questions["answer"]
	if q.Type != jev.TypeChoice || q.Instructions != "How urgent is this task?" {
		t.Errorf("unexpected question: %+v", q)
	}
}

func TestE2EJev_ConditionNodeFallsBackToElseOnJevFailure(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	fake := newFakeJev(t)
	fake.setResponder(func(jev.Request) (int, jev.Response) {
		return http.StatusInternalServerError, jev.Response{}
	})
	client, token, projID, _, elseMemberID := setupJevConditionAutomation(t, env, fake, "jev-cond-fail")

	taskID := createTaskViaAPI(t, env, client, token, projID, "Checkout outage in US")
	waitForAutomationAssignee(t, env, client, token, projID, taskID, elseMemberID, 20*time.Second)
}

func TestE2EJev_ConditionNodeFallsBackToElseOnLowConfidence(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	fake := newFakeJev(t)
	fake.setResponder(func(jev.Request) (int, jev.Response) {
		return http.StatusOK, jev.Response{Answers: map[string]jev.Answer{
			"answer": {Type: jev.TypeChoice, Choice: "urgent", Confidence: confidence(0.4)},
		}}
	})
	client, token, projID, _, elseMemberID := setupJevConditionAutomation(t, env, fake, "jev-cond-low")

	taskID := createTaskViaAPI(t, env, client, token, projID, "Maybe an outage?")
	waitForAutomationAssignee(t, env, client, token, projID, taskID, elseMemberID, 20*time.Second)
}

func TestE2EJev_ConditionNodeRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token, projID := newJevProject(t, env, "jev-cond-invalid", nil)

	automationID := createAutomationViaAPI(t, env, client, token, projID, "Invalid Jev node")
	condition := addAutomationNodeViaAPI(t, env, client, token, projID, automationID, "condition", "jev_condition", nil)
	// A choice question with no options can't route anywhere.
	updateAutomationNodeConfigViaAPIExpect(t, env, client, token, projID, automationID, idOf(condition), map[string]any{
		"answer_type": "choice", "instructions": "Which team?",
	}, http.StatusBadRequest)
	// Unknown answer types are rejected.
	updateAutomationNodeConfigViaAPIExpect(t, env, client, token, projID, automationID, idOf(condition), map[string]any{
		"answer_type": "essay", "instructions": "Write a poem",
	}, http.StatusBadRequest)
}
