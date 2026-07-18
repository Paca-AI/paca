package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Fake ViewRepository for integration tests
// ---------------------------------------------------------------------------

type fakeViewRepoIT struct {
	mu        sync.RWMutex
	views     map[uuid.UUID]*sprintdom.SprintView
	positions map[string]*sprintdom.ViewTaskPosition
}

func newFakeViewRepoIT() *fakeViewRepoIT {
	return &fakeViewRepoIT{
		views:     make(map[uuid.UUID]*sprintdom.SprintView),
		positions: make(map[string]*sprintdom.ViewTaskPosition),
	}
}

func viewPosKey(viewID, taskID uuid.UUID) string {
	return viewID.String() + ":" + taskID.String()
}

func (r *fakeViewRepoIT) ListViews(_ context.Context, sprintID uuid.UUID) ([]*sprintdom.SprintView, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*sprintdom.SprintView
	for _, v := range r.views {
		if v.SprintID != nil && *v.SprintID == sprintID {
			cp := *v
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeViewRepoIT) ListProjectViews(_ context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext) ([]*sprintdom.SprintView, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*sprintdom.SprintView
	for _, v := range r.views {
		if v.ViewContext == viewCtx && v.ProjectID == projectID {
			cp := *v
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeViewRepoIT) FindViewByID(_ context.Context, id uuid.UUID) (*sprintdom.SprintView, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.views[id]
	if !ok {
		return nil, sprintdom.ErrViewNotFound
	}
	cp := *v
	return &cp, nil
}

func (r *fakeViewRepoIT) CreateView(_ context.Context, v *sprintdom.SprintView) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *v
	r.views[v.ID] = &cp
	return nil
}

func (r *fakeViewRepoIT) UpdateView(_ context.Context, v *sprintdom.SprintView) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.views[v.ID]; !ok {
		return sprintdom.ErrViewNotFound
	}
	cp := *v
	r.views[v.ID] = &cp
	return nil
}

func (r *fakeViewRepoIT) DeleteView(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.views, id)
	return nil
}

func (r *fakeViewRepoIT) CountViews(_ context.Context, sprintID uuid.UUID) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, v := range r.views {
		if v.SprintID != nil && *v.SprintID == sprintID {
			count++
		}
	}
	return count, nil
}

func (r *fakeViewRepoIT) CountProjectViews(_ context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, v := range r.views {
		if v.ViewContext == viewCtx && v.ProjectID == projectID {
			count++
		}
	}
	return count, nil
}

func (r *fakeViewRepoIT) UpsertTaskPosition(_ context.Context, pos *sprintdom.ViewTaskPosition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *pos
	r.positions[viewPosKey(pos.ViewID, pos.TaskID)] = &cp
	return nil
}

func (r *fakeViewRepoIT) BulkUpsertTaskPositions(_ context.Context, positions []*sprintdom.ViewTaskPosition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, pos := range positions {
		cp := *pos
		r.positions[viewPosKey(pos.ViewID, pos.TaskID)] = &cp
	}
	return nil
}

func (r *fakeViewRepoIT) ListTaskPositions(_ context.Context, viewID uuid.UUID) ([]*sprintdom.ViewTaskPosition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*sprintdom.ViewTaskPosition
	for _, p := range r.positions {
		if p.ViewID == viewID {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeViewRepoIT) ReorderViews(_ context.Context, items []sprintdom.ViewReorderItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range items {
		if v, ok := r.views[item.ID]; ok {
			cp := *v
			cp.Position = item.Position
			r.views[item.ID] = &cp
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Router builder
// ---------------------------------------------------------------------------

func buildViewTestRouter(viewRepo *fakeViewRepoIT, sprintRepo *fakeSprintRepoIT, store *projectPermStore) http.Handler {
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	refreshStore := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, refreshStore, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo)
	projectRepo := newFakeProjectRepo()
	taskRepo := newFakeTaskRepoIT()
	projectService := projectsvc.New(projectRepo, taskRepo)
	taskService := tasksvc.New(taskRepo)
	sprintService := sprintsvc.New(sprintRepo, taskRepo)
	viewService := sprintsvc.NewViewService(viewRepo)
	activityService := tasksvc.NewActivityService(newFakeTaskActivityRepo(), &fakeActivityMemberRepo{}, nil)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager:         tm,
		Authorizer:           authz.NewAuthorizer(store),
		ProjectVisibilitySvc: projectService,
		Health:               handler.NewHealthHandler(),
		Auth:                 handler.NewAuthHandler(authService, testCookieCfg),
		User:                 handler.NewUserHandler(userService),
		GlobalRole:           handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}, authz.NewAuthorizer(store)),
		Project:              handler.NewProjectHandler(projectService, authz.NewAuthorizer(store)),
		Task:                 handler.NewTaskHandler(taskService, viewService, activityService),
		Sprint:               handler.NewSprintHandler(sprintService, viewService),
		View:                 handler.NewViewHandler(viewService),
		Log:                  log,
	})
}

func issueViewToken(t *testing.T, subject string) string {
	t.Helper()
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	tok, err := tm.IssueAccess(subject, "view-user", "USER", "fam-view", false)
	if err != nil {
		t.Fatalf("issue view token: %v", err)
	}
	return tok
}

// viewIDFrom decodes data.id from a handler JSON response.
func viewIDFrom(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode view response: %v", err)
	}
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatalf("missing id in view response: %s", string(body))
	}
	return id
}

// viewListCount decodes data.items and returns its length.
func viewListCount(t *testing.T, body []byte) int {
	t.Helper()
	var env struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode view list response: %v", err)
	}
	return len(env.Data.Items)
}

// ---------------------------------------------------------------------------
// Helper — seed a sprint into the fake repo
// ---------------------------------------------------------------------------

func seedSprintIT(t *testing.T, repo *fakeSprintRepoIT, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ctx := context.Background()
	if err := repo.CreateSprint(ctx, &sprintdom.Sprint{
		ID:        id,
		ProjectID: projectID,
		Name:      "Sprint IT",
		Status:    sprintdom.SprintStatusPlanned,
	}); err != nil {
		t.Fatalf("seed sprint: %v", err)
	}
	return id
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestIntegrationViews_CRUD(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionSprintsRead,
				authz.PermissionSprintsWrite,
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
			},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)

	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	// Create
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Board View",
		"view_type": "board",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create view: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	// List
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list views: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if count := viewListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 view, got %d", count)
	}

	// Get single
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+viewID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get view: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}

	// Update name
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, itemBase+"/"+viewID, tok, map[string]any{
		"name": "Renamed Board",
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update view: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	// Create a second view so deletion of first is allowed
	create2W := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Table View",
		"view_type": "table",
	}))
	if create2W.Code != http.StatusCreated {
		t.Fatalf("create second view: expected 201, got %d (%s)", create2W.Code, create2W.Body.String())
	}

	// Delete first view
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, itemBase+"/"+viewID, tok, nil))
	if delW.Code != http.StatusNoContent {
		t.Fatalf("delete view: expected 204, got %d (%s)", delW.Code, delW.Body.String())
	}

	// Verify removed
	getDeleted := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+viewID, tok, nil))
	if getDeleted.Code != http.StatusNotFound {
		t.Fatalf("get deleted: expected 404, got %d", getDeleted.Code)
	}
	if code := decodeErrorCode(t, getDeleted); code != "VIEW_NOT_FOUND" {
		t.Errorf("expected VIEW_NOT_FOUND, got %q", code)
	}
}

func TestIntegrationViews_DeleteLastViewRejected(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Only View",
		"view_type": "table",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create view: expected 201, got %d", createW.Code)
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, itemBase+"/"+viewID, tok, nil))
	if delW.Code != http.StatusConflict {
		t.Fatalf("delete last view: expected 409, got %d (%s)", delW.Code, delW.Body.String())
	}
	if code := decodeErrorCode(t, delW); code != "VIEW_IS_LAST_VIEW" {
		t.Errorf("expected VIEW_IS_LAST_VIEW, got %q", code)
	}
}

func TestIntegrationViews_CreateInvalidTypeReturns400(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Bad",
		"view_type": "gantt",
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "VIEW_TYPE_INVALID" {
		t.Errorf("expected VIEW_TYPE_INVALID, got %q", code)
	}
}

func TestIntegrationViews_TaskPositions(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionSprintsRead,
				authz.PermissionSprintsWrite,
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
			},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	// Create view
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Table",
		"view_type": "table",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create view: expected 201, got %d", createW.Code)
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	taskID := uuid.NewString()
	posURL := fmt.Sprintf("%s/%s/task-positions/%s", itemBase, viewID, taskID)

	// Move task
	moveW := serve(r, authedJSONReq(t.Context(), http.MethodPut, posURL, tok, map[string]any{
		"position":  5,
		"group_key": "in-progress",
	}))
	if moveW.Code != http.StatusNoContent {
		t.Fatalf("move task: expected 204, got %d (%s)", moveW.Code, moveW.Body.String())
	}

	// List positions
	listPosURL := fmt.Sprintf("%s/%s/task-positions", itemBase, viewID)
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, listPosURL, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list positions: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}

	var env struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&env); err != nil {
		t.Fatalf("decode positions: %v", err)
	}
	if len(env.Data.Items) != 1 {
		t.Fatalf("expected 1 position, got %d", len(env.Data.Items))
	}
	pos := env.Data.Items[0]
	if pos["task_id"] != taskID {
		t.Errorf("task_id mismatch: %v", pos["task_id"])
	}
}

func TestIntegrationViews_BulkMoveTasks(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionSprintsRead,
				authz.PermissionSprintsWrite,
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
			},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	// Create a view to work with
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Bulk Test",
		"view_type": "table",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create view: expected 201, got %d", createW.Code)
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())
	bulkURL := fmt.Sprintf("%s/%s/task-positions", itemBase, viewID)
	listURL := bulkURL

	task1 := uuid.NewString()
	task2 := uuid.NewString()
	task3 := uuid.NewString()

	// --- happy path: bulk upsert three tasks ---
	w := serve(r, authedJSONReq(t.Context(), http.MethodPut, bulkURL, tok, map[string]any{
		"items": []map[string]any{
			{"task_id": task1, "position": 65536, "group_key": "todo"},
			{"task_id": task2, "position": 131072, "group_key": "todo"},
			{"task_id": task3, "position": 196608, "group_key": "in-progress"},
		},
	}))
	if w.Code != http.StatusNoContent {
		t.Fatalf("bulk move: expected 204, got %d (%s)", w.Code, w.Body.String())
	}

	// Verify all three are stored
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, listURL, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list positions: expected 200, got %d", listW.Code)
	}
	var env struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&env); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(env.Data.Items) != 3 {
		t.Fatalf("expected 3 positions, got %d", len(env.Data.Items))
	}

	// --- upsert overwrites existing positions ---
	w2 := serve(r, authedJSONReq(t.Context(), http.MethodPut, bulkURL, tok, map[string]any{
		"items": []map[string]any{
			{"task_id": task1, "position": 32768, "group_key": "done"},
		},
	}))
	if w2.Code != http.StatusNoContent {
		t.Fatalf("upsert existing: expected 204, got %d (%s)", w2.Code, w2.Body.String())
	}

	// task1 updated + task2/task3 still present = 3 total
	listW2 := serve(r, authedJSONReq(t.Context(), http.MethodGet, listURL, tok, nil))
	var env2 struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listW2.Body).Decode(&env2); err != nil {
		t.Fatalf("decode list2: %v", err)
	}
	if len(env2.Data.Items) != 3 {
		t.Fatalf("expected 3 positions after upsert, got %d", len(env2.Data.Items))
	}
	// Find task1 and confirm its group_key was overwritten
	for _, item := range env2.Data.Items {
		if item["task_id"] == task1 {
			if item["group_key"] != "done" {
				t.Errorf("task1 group_key: expected done, got %v", item["group_key"])
			}
		}
	}
}

func TestIntegrationViews_BulkMoveTasks_EmptyItems(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite, authz.PermissionTasksWrite},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name": "V", "view_type": "table",
	}))
	viewID := viewIDFrom(t, createW.Body.Bytes())
	bulkURL := fmt.Sprintf("%s/%s/task-positions", itemBase, viewID)

	w := serve(r, authedJSONReq(t.Context(), http.MethodPut, bulkURL, tok, map[string]any{
		"items": []map[string]any{},
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty items: expected 400, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationViews_BulkMoveTasks_AuthzGuard(t *testing.T) {
	projectID := uuid.New()
	// TasksRead only — no TasksWrite
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionTasksRead},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	_ = fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID) // context URL not used in this test

	bulkURL := fmt.Sprintf("%s/%s/task-positions", itemBase, uuid.NewString())
	w := serve(r, authedJSONReq(t.Context(), http.MethodPut, bulkURL, tok, map[string]any{
		"items": []map[string]any{
			{"task_id": uuid.NewString(), "position": 1000},
		},
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("authz guard: expected 403, got %d", w.Code)
	}
}

func TestIntegrationBacklogViews_BulkMoveTasks(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionSprintsRead,
				authz.PermissionSprintsWrite,
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
			},
		},
	}
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, newFakeSprintRepoIT(), store)
	tok := issueViewToken(t, uuid.NewString())
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=backlog"

	// Create a backlog view
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Bulk Backlog",
		"view_type": "table",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create backlog view: expected 201, got %d", createW.Code)
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())
	bulkURL := fmt.Sprintf("%s/%s/task-positions", itemBase, viewID)

	task1 := uuid.NewString()
	task2 := uuid.NewString()

	w := serve(r, authedJSONReq(t.Context(), http.MethodPut, bulkURL, tok, map[string]any{
		"items": []map[string]any{
			{"task_id": task1, "position": 65536, "group_key": "todo"},
			{"task_id": task2, "position": 131072},
		},
	}))
	if w.Code != http.StatusNoContent {
		t.Fatalf("bulk backlog move: expected 204, got %d (%s)", w.Code, w.Body.String())
	}

	// List and verify both positions were recorded
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, bulkURL, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list backlog positions: expected 200, got %d", listW.Code)
	}
	var env struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&env); err != nil {
		t.Fatalf("decode backlog list: %v", err)
	}
	if len(env.Data.Items) != 2 {
		t.Fatalf("expected 2 backlog positions, got %d", len(env.Data.Items))
	}
}

func TestIntegrationViews_AuthzGuard(t *testing.T) {
	projectID := uuid.New()
	// No permissions at all
	store := &projectPermStore{}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := uuid.New()
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Unauthorized",
		"view_type": "table",
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Product-backlog view integration tests
// ---------------------------------------------------------------------------

func newBacklogViewPerms(projectID uuid.UUID) *projectPermStore {
	return &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionSprintsRead,
				authz.PermissionSprintsWrite,
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
			},
		},
	}
}

func TestIntegrationBacklogViews_CRUD(t *testing.T) {
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, newBacklogViewPerms(projectID))
	tok := issueViewToken(t, uuid.NewString())

	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=backlog"

	// Create first backlog view
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Backlog Board",
		"view_type": "board",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create backlog view: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	// Verify sprint_id is absent / null
	var createResp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(createW.Body.Bytes(), &createResp)
	if sid, ok := createResp.Data["sprint_id"]; ok && sid != nil {
		t.Errorf("expected sprint_id to be absent/null for backlog view, got %v", sid)
	}
	if pid, _ := createResp.Data["project_id"].(string); pid != projectID.String() {
		t.Errorf("expected project_id=%s, got %q", projectID, pid)
	}

	// List — should see the created view
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list backlog views: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if count := viewListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 backlog view, got %d", count)
	}

	// Get single
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+viewID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get backlog view: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}
	var getResp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(getW.Body.Bytes(), &getResp)
	if getResp.Data["id"] != viewID {
		t.Errorf("get view id mismatch: want %s, got %v", viewID, getResp.Data["id"])
	}

	// Update name
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, itemBase+"/"+viewID, tok, map[string]any{
		"name": "Renamed Backlog Board",
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("patch backlog view: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}
	var patchResp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(patchW.Body.Bytes(), &patchResp)
	if patchResp.Data["name"] != "Renamed Backlog Board" {
		t.Errorf("expected name=Renamed Backlog Board, got %v", patchResp.Data["name"])
	}

	// Create second view so we can delete the first
	create2W := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Backlog Table",
		"view_type": "table",
	}))
	if create2W.Code != http.StatusCreated {
		t.Fatalf("create second backlog view: expected 201, got %d", create2W.Code)
	}

	// Delete first view
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, itemBase+"/"+viewID, tok, nil))
	if delW.Code != http.StatusNoContent {
		t.Fatalf("delete backlog view: expected 204, got %d (%s)", delW.Code, delW.Body.String())
	}

	// Verify removed
	getDeletedW := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+viewID, tok, nil))
	if getDeletedW.Code != http.StatusNotFound {
		t.Fatalf("deleted backlog view: expected 404, got %d", getDeletedW.Code)
	}
}

func TestIntegrationBacklogViews_DeleteLastViewRejected(t *testing.T) {
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, newBacklogViewPerms(projectID))
	tok := issueViewToken(t, uuid.NewString())
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=backlog"

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Only Backlog View",
		"view_type": "table",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create backlog view: expected 201, got %d", createW.Code)
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, itemBase+"/"+viewID, tok, nil))
	if delW.Code != http.StatusConflict {
		t.Fatalf("delete last backlog view: expected 409, got %d (%s)", delW.Code, delW.Body.String())
	}
	if code := decodeErrorCode(t, delW); code != "VIEW_IS_LAST_VIEW" {
		t.Errorf("expected VIEW_IS_LAST_VIEW, got %q", code)
	}
}

func TestIntegrationBacklogViews_SprintViewsNotIncluded(t *testing.T) {
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, newBacklogViewPerms(projectID))
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)

	viewsBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	backlogBase := viewsBase + "?context=backlog"
	sprintBase := fmt.Sprintf("%s?context=sprint&sprint_id=%s", viewsBase, sprintID)

	// Create a sprint view and a backlog view
	w1 := serve(r, authedJSONReq(t.Context(), http.MethodPost, sprintBase, tok, map[string]any{
		"name":      "Sprint View",
		"view_type": "board",
	}))
	if w1.Code != http.StatusCreated {
		t.Fatalf("create sprint view: expected 201, got %d", w1.Code)
	}
	w2 := serve(r, authedJSONReq(t.Context(), http.MethodPost, backlogBase, tok, map[string]any{
		"name":      "Backlog View",
		"view_type": "table",
	}))
	if w2.Code != http.StatusCreated {
		t.Fatalf("create backlog view: expected 201, got %d", w2.Code)
	}

	// Backlog list must contain exactly 1 item (the backlog view)
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, backlogBase, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list backlog views: expected 200, got %d", listW.Code)
	}
	if count := viewListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 backlog view, got %d", count)
	}

	// Sprint list must also contain exactly 1 item (the sprint view)
	sprintListW := serve(r, authedJSONReq(t.Context(), http.MethodGet, sprintBase, tok, nil))
	if sprintListW.Code != http.StatusOK {
		t.Fatalf("list sprint views: expected 200, got %d", sprintListW.Code)
	}
	if count := viewListCount(t, sprintListW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 sprint view, got %d", count)
	}
}

func TestIntegrationBacklogViews_TaskPositions(t *testing.T) {
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, newBacklogViewPerms(projectID))
	tok := issueViewToken(t, uuid.NewString())
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=backlog"

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Backlog Table",
		"view_type": "table",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create backlog view: expected 201, got %d", createW.Code)
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())
	taskID := uuid.NewString()

	// Move task
	moveW := serve(r, authedJSONReq(t.Context(), http.MethodPut,
		fmt.Sprintf("%s/%s/task-positions/%s", itemBase, viewID, taskID), tok,
		map[string]any{"position": 3, "group_key": "todo"},
	))
	if moveW.Code != http.StatusNoContent {
		t.Fatalf("move task: expected 204, got %d (%s)", moveW.Code, moveW.Body.String())
	}

	// List positions
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("%s/%s/task-positions", itemBase, viewID), tok, nil,
	))
	if listW.Code != http.StatusOK {
		t.Fatalf("list positions: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	var env struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&env); err != nil {
		t.Fatalf("decode positions: %v", err)
	}
	if len(env.Data.Items) != 1 {
		t.Fatalf("expected 1 position, got %d", len(env.Data.Items))
	}
	if env.Data.Items[0]["task_id"] != taskID {
		t.Errorf("task_id mismatch: %v", env.Data.Items[0]["task_id"])
	}
}

func TestIntegrationBacklogViews_AuthzGuard(t *testing.T) {
	projectID := uuid.New()
	// No permissions at all
	store := &projectPermStore{}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=backlog"

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Unauthorized",
		"view_type": "table",
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Reorder sprint views
// ---------------------------------------------------------------------------

func TestIntegrationViews_Reorder(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)

	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	// Create three views
	w1 := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": "Alpha", "view_type": "table"}))
	if w1.Code != http.StatusCreated {
		t.Fatalf("create v1: expected 201, got %d", w1.Code)
	}
	id1 := viewIDFrom(t, w1.Body.Bytes())

	w2 := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": "Beta", "view_type": "board"}))
	if w2.Code != http.StatusCreated {
		t.Fatalf("create v2: expected 201, got %d", w2.Code)
	}
	id2 := viewIDFrom(t, w2.Body.Bytes())

	w3 := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": "Gamma", "view_type": "roadmap"}))
	if w3.Code != http.StatusCreated {
		t.Fatalf("create v3: expected 201, got %d", w3.Code)
	}
	id3 := viewIDFrom(t, w3.Body.Bytes())

	// Reorder: Gamma(id3), Alpha(id1), Beta(id2)
	reorderW := serve(r, authedJSONReq(t.Context(), http.MethodPut, strings.Replace(base, "?", "/positions?", 1), tok, map[string]any{
		"view_ids": []string{id3, id1, id2},
	}))
	if reorderW.Code != http.StatusNoContent {
		t.Fatalf("reorder: expected 204, got %d (%s)", reorderW.Code, reorderW.Body.String())
	}

	// Verify positions via individual GETs
	decodePosition := func(body []byte) int {
		var env struct {
			Data map[string]any `json:"data"`
		}
		_ = json.Unmarshal(body, &env)
		pos, _ := env.Data["position"].(float64)
		return int(pos)
	}

	g1 := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+id1, tok, nil))
	g2 := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+id2, tok, nil))
	g3 := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+id3, tok, nil))

	if p := decodePosition(g3.Body.Bytes()); p != 0 {
		t.Errorf("Gamma: expected position=0, got %d", p)
	}
	if p := decodePosition(g1.Body.Bytes()); p != 1 {
		t.Errorf("Alpha: expected position=1, got %d", p)
	}
	if p := decodePosition(g2.Body.Bytes()); p != 2 {
		t.Errorf("Beta: expected position=2, got %d", p)
	}
}

func TestIntegrationViews_Reorder_MismatchReturns400(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	w1 := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": "Only", "view_type": "table"}))
	id1 := viewIDFrom(t, w1.Body.Bytes())

	// Send two IDs when only one exists
	reorderW := serve(r, authedJSONReq(t.Context(), http.MethodPut, strings.Replace(base, "?", "/positions?", 1), tok, map[string]any{
		"view_ids": []string{id1, uuid.NewString()},
	}))
	if reorderW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", reorderW.Code, reorderW.Body.String())
	}
	if code := decodeErrorCode(t, reorderW); code != "VIEW_REORDER_INVALID" {
		t.Errorf("expected VIEW_REORDER_INVALID, got %q", code)
	}
}

func TestIntegrationViews_Reorder_AuthzGuard(t *testing.T) {
	projectID := uuid.New()
	// SprintsRead only — no SprintsWrite
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	reorderW := serve(r, authedJSONReq(t.Context(), http.MethodPut, strings.Replace(base, "?", "/positions?", 1), tok, map[string]any{
		"view_ids": []string{uuid.NewString()},
	}))
	if reorderW.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", reorderW.Code)
	}
}

// ---------------------------------------------------------------------------
// Reorder backlog views
// ---------------------------------------------------------------------------

func TestIntegrationBacklogViews_Reorder(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())

	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=backlog"

	w1 := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": "P", "view_type": "table"}))
	if w1.Code != http.StatusCreated {
		t.Fatalf("create backlog v1: expected 201, got %d", w1.Code)
	}
	bid1 := viewIDFrom(t, w1.Body.Bytes())

	w2 := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": "Q", "view_type": "board"}))
	if w2.Code != http.StatusCreated {
		t.Fatalf("create backlog v2: expected 201, got %d", w2.Code)
	}
	bid2 := viewIDFrom(t, w2.Body.Bytes())

	// Reorder: Q before P
	reorderW := serve(r, authedJSONReq(t.Context(), http.MethodPut, strings.Replace(base, "?", "/positions?", 1), tok, map[string]any{
		"view_ids": []string{bid2, bid1},
	}))
	if reorderW.Code != http.StatusNoContent {
		t.Fatalf("reorder backlog: expected 204, got %d (%s)", reorderW.Code, reorderW.Body.String())
	}

	decodePosition := func(body []byte) int {
		var env struct {
			Data map[string]any `json:"data"`
		}
		_ = json.Unmarshal(body, &env)
		pos, _ := env.Data["position"].(float64)
		return int(pos)
	}

	gP := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+bid1, tok, nil))
	gQ := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+bid2, tok, nil))

	if p := decodePosition(gQ.Body.Bytes()); p != 0 {
		t.Errorf("Q: expected position=0, got %d", p)
	}
	if p := decodePosition(gP.Body.Bytes()); p != 1 {
		t.Errorf("P: expected position=1, got %d", p)
	}
}

// ---------------------------------------------------------------------------
// Timeline view integration tests
// ---------------------------------------------------------------------------

func newTimelineViewPerms(projectID uuid.UUID) *projectPermStore {
	return &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionSprintsRead,
				authz.PermissionSprintsWrite,
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
			},
		},
	}
}

func TestIntegrationTimelineViews_CRUD(t *testing.T) {
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, newTimelineViewPerms(projectID))
	tok := issueViewToken(t, uuid.NewString())

	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=timeline"

	// Create first timeline view
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Roadmap",
		"view_type": "roadmap",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create timeline view: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	var createResp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(createW.Body.Bytes(), &createResp)
	if sid, ok := createResp.Data["sprint_id"]; ok && sid != nil {
		t.Errorf("expected sprint_id to be absent/null for timeline view, got %v", sid)
	}
	if pid, _ := createResp.Data["project_id"].(string); pid != projectID.String() {
		t.Errorf("expected project_id=%s, got %q", projectID, pid)
	}

	// List — should see the created view
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list timeline views: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if count := viewListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 timeline view, got %d", count)
	}

	// Get single
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+viewID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get timeline view: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}
	var getResp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(getW.Body.Bytes(), &getResp)
	if getResp.Data["id"] != viewID {
		t.Errorf("get view id mismatch: want %s, got %v", viewID, getResp.Data["id"])
	}

	// Update name
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, itemBase+"/"+viewID, tok, map[string]any{
		"name": "Renamed Roadmap",
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("patch timeline view: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}
	var patchResp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(patchW.Body.Bytes(), &patchResp)
	if patchResp.Data["name"] != "Renamed Roadmap" {
		t.Errorf("expected name=Renamed Roadmap, got %v", patchResp.Data["name"])
	}

	// Create second view so we can delete the first
	create2W := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Timeline Table",
		"view_type": "table",
	}))
	if create2W.Code != http.StatusCreated {
		t.Fatalf("create second timeline view: expected 201, got %d", create2W.Code)
	}

	// Delete first view
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, itemBase+"/"+viewID, tok, nil))
	if delW.Code != http.StatusNoContent {
		t.Fatalf("delete timeline view: expected 204, got %d (%s)", delW.Code, delW.Body.String())
	}

	// Verify removed
	getDeletedW := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+viewID, tok, nil))
	if getDeletedW.Code != http.StatusNotFound {
		t.Fatalf("deleted timeline view: expected 404, got %d", getDeletedW.Code)
	}
}

func TestIntegrationTimelineViews_DeleteLastViewRejected(t *testing.T) {
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, newTimelineViewPerms(projectID))
	tok := issueViewToken(t, uuid.NewString())
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=timeline"

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Only Timeline View",
		"view_type": "roadmap",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create timeline view: expected 201, got %d", createW.Code)
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, itemBase+"/"+viewID, tok, nil))
	if delW.Code != http.StatusConflict {
		t.Fatalf("delete last timeline view: expected 409, got %d (%s)", delW.Code, delW.Body.String())
	}
	if code := decodeErrorCode(t, delW); code != "VIEW_IS_LAST_VIEW" {
		t.Errorf("expected VIEW_IS_LAST_VIEW, got %q", code)
	}
}

func TestIntegrationTimelineViews_BacklogViewsNotReturned(t *testing.T) {
	// Ensures timeline endpoint only returns timeline-context views.
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, newTimelineViewPerms(projectID))
	tok := issueViewToken(t, uuid.NewString())

	viewsBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	timelineBase := viewsBase + "?context=timeline"
	backlogBase := viewsBase + "?context=backlog"

	// Create a timeline view and a backlog view
	w1 := serve(r, authedJSONReq(t.Context(), http.MethodPost, timelineBase, tok, map[string]any{
		"name":      "Timeline View",
		"view_type": "roadmap",
	}))
	if w1.Code != http.StatusCreated {
		t.Fatalf("create timeline view: expected 201, got %d", w1.Code)
	}
	w2 := serve(r, authedJSONReq(t.Context(), http.MethodPost, backlogBase, tok, map[string]any{
		"name":      "Backlog View",
		"view_type": "table",
	}))
	if w2.Code != http.StatusCreated {
		t.Fatalf("create backlog view: expected 201, got %d", w2.Code)
	}

	// Timeline endpoint returns only the timeline view
	tlListW := serve(r, authedJSONReq(t.Context(), http.MethodGet, timelineBase, tok, nil))
	if tlListW.Code != http.StatusOK {
		t.Fatalf("list timeline views: expected 200, got %d", tlListW.Code)
	}
	if count := viewListCount(t, tlListW.Body.Bytes()); count != 1 {
		t.Errorf("timeline list: expected 1, got %d", count)
	}

	// Backlog endpoint also returns only 1
	blListW := serve(r, authedJSONReq(t.Context(), http.MethodGet, backlogBase, tok, nil))
	if blListW.Code != http.StatusOK {
		t.Fatalf("list backlog views: expected 200, got %d", blListW.Code)
	}
	if count := viewListCount(t, blListW.Body.Bytes()); count != 1 {
		t.Errorf("backlog list: expected 1, got %d", count)
	}
}

func TestIntegrationTimelineViews_Reorder(t *testing.T) {
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, newTimelineViewPerms(projectID))
	tok := issueViewToken(t, uuid.NewString())

	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=timeline"

	w1 := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": "Alpha", "view_type": "roadmap"}))
	if w1.Code != http.StatusCreated {
		t.Fatalf("create tl v1: expected 201, got %d", w1.Code)
	}
	id1 := viewIDFrom(t, w1.Body.Bytes())

	w2 := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": "Beta", "view_type": "table"}))
	if w2.Code != http.StatusCreated {
		t.Fatalf("create tl v2: expected 201, got %d", w2.Code)
	}
	id2 := viewIDFrom(t, w2.Body.Bytes())

	// Reorder: Beta first, then Alpha
	reorderW := serve(r, authedJSONReq(t.Context(), http.MethodPut, strings.Replace(base, "?", "/positions?", 1), tok, map[string]any{
		"view_ids": []string{id2, id1},
	}))
	if reorderW.Code != http.StatusNoContent {
		t.Fatalf("reorder timeline: expected 204, got %d (%s)", reorderW.Code, reorderW.Body.String())
	}

	decodePosition := func(body []byte) int {
		var env struct {
			Data map[string]any `json:"data"`
		}
		_ = json.Unmarshal(body, &env)
		pos, _ := env.Data["position"].(float64)
		return int(pos)
	}

	gAlpha := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+id1, tok, nil))
	gBeta := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+id2, tok, nil))

	if p := decodePosition(gBeta.Body.Bytes()); p != 0 {
		t.Errorf("Beta: expected position=0, got %d", p)
	}
	if p := decodePosition(gAlpha.Body.Bytes()); p != 1 {
		t.Errorf("Alpha: expected position=1, got %d", p)
	}
}

func TestIntegrationTimelineViews_TaskPositions(t *testing.T) {
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, newTimelineViewPerms(projectID))
	tok := issueViewToken(t, uuid.NewString())
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=timeline"

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Timeline Table",
		"view_type": "table",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create timeline view: expected 201, got %d", createW.Code)
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())
	taskID := uuid.NewString()

	// Move task
	moveW := serve(r, authedJSONReq(t.Context(), http.MethodPut,
		fmt.Sprintf("%s/%s/task-positions/%s", itemBase, viewID, taskID), tok,
		map[string]any{"position": 2, "group_key": "in-progress"},
	))
	if moveW.Code != http.StatusNoContent {
		t.Fatalf("move task: expected 204, got %d (%s)", moveW.Code, moveW.Body.String())
	}

	// List positions
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("%s/%s/task-positions", itemBase, viewID), tok, nil,
	))
	if listW.Code != http.StatusOK {
		t.Fatalf("list positions: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	var env struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listW.Body).Decode(&env); err != nil {
		t.Fatalf("decode positions: %v", err)
	}
	if len(env.Data.Items) != 1 {
		t.Fatalf("expected 1 position, got %d", len(env.Data.Items))
	}
	if env.Data.Items[0]["task_id"] != taskID {
		t.Errorf("task_id mismatch: %v", env.Data.Items[0]["task_id"])
	}
}

func TestIntegrationTimelineViews_AuthzGuard(t *testing.T) {
	projectID := uuid.New()
	// No permissions at all
	store := &projectPermStore{}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := itemBase + "?context=timeline"

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Unauthorized",
		"view_type": "roadmap",
	}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// ViewFilters roundtrip tests
// ---------------------------------------------------------------------------

// TestIntegrationViews_FiltersRoundtrip_AllNormalMode creates a sprint view
// with a task_types FilterConfig using the "normal" virtual group plus an
// explicit Epic ID, then GETs the view back and asserts the filter fields are
// preserved.
func TestIntegrationViews_FiltersRoundtrip_AllNormalMode(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)

	epicID := strings.ToLower(uuid.NewString())
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Board View",
		"view_type": "board",
		"config": map[string]any{
			"column_by": "status",
			"filters": map[string]any{
				"task_types": map[string]any{
					"all": false,
					"items": map[string]any{
						"normal": map[string]any{"all": true},
						epicID:   true,
					},
				},
				"sprints": map[string]any{
					"all":   false,
					"items": map[string]any{sprintID.String(): true},
				},
			},
		},
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create view: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	// GET the view and check filter fields are present and unchanged.
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+viewID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get view: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}

	var getResp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}

	cfg, _ := getResp.Data["config"].(map[string]any)
	if cfg == nil {
		t.Fatal("expected config in response")
	}
	filters, _ := cfg["filters"].(map[string]any)
	if filters == nil {
		t.Fatal("expected filters in config")
	}

	// task_types must round-trip as an object with "normal" group and explicit epic ID.
	taskTypes, _ := filters["task_types"].(map[string]any)
	if taskTypes == nil {
		t.Fatal("expected task_types in filters")
	}
	items, _ := taskTypes["items"].(map[string]any)
	if items == nil {
		t.Fatal("expected task_types.items in filters")
	}
	normalGroup, _ := items["normal"].(map[string]any)
	if normalGroup == nil {
		t.Fatalf("expected task_types.items.normal group, got %v", items)
	}
	if normalGroup["all"] != true {
		t.Errorf("task_types.items.normal.all: want true, got %v", normalGroup["all"])
	}
	if items[epicID] != true {
		t.Errorf("task_types.items[epicID]: want true, got %v", items[epicID])
	}

	// sprints must round-trip.
	sprintsFilter, _ := filters["sprints"].(map[string]any)
	if sprintsFilter == nil {
		t.Fatal("expected sprints in filters")
	}
	sprintItems, _ := sprintsFilter["items"].(map[string]any)
	if sprintItems == nil || sprintItems[sprintID.String()] != true {
		t.Errorf("sprints.items[sprintID]: want true, got %v", sprintItems)
	}
}

// TestIntegrationViews_FiltersRoundtrip_SprintIDs verifies that a sprints
// FilterConfig with multiple explicit IDs is persisted and returned unchanged.
func TestIntegrationViews_FiltersRoundtrip_SprintIDs(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	sprint2ID := uuid.New()

	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Multi-sprint View",
		"view_type": "table",
		"config": map[string]any{
			"filters": map[string]any{
				"sprints": map[string]any{
					"all": false,
					"items": map[string]any{
						sprintID.String():  true,
						sprint2ID.String(): true,
					},
				},
			},
		},
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+viewID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}

	var getResp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cfg, _ := getResp.Data["config"].(map[string]any)
	filters, _ := cfg["filters"].(map[string]any)
	if filters == nil {
		t.Fatal("expected filters")
	}
	sprintsFilter, _ := filters["sprints"].(map[string]any)
	if sprintsFilter == nil {
		t.Fatal("expected sprints filter")
	}
	sprintItems, _ := sprintsFilter["items"].(map[string]any)
	if len(sprintItems) != 2 {
		t.Errorf("sprints.items: want 2 entries, got %d: %v", len(sprintItems), sprintItems)
	}
}

// TestIntegrationViews_UpdateConfig_TaskTypes verifies that PATCHing a view
// with a task_types FilterConfig persists the new config and returns it.
func TestIntegrationViews_UpdateConfig_TaskTypes(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Table View",
		"view_type": "table",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createW.Code)
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	// PATCH to add a task_types FilterConfig using the "normal" group.
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, itemBase+"/"+viewID, tok, map[string]any{
		"config": map[string]any{
			"filters": map[string]any{
				"task_types": map[string]any{
					"all":   false,
					"items": map[string]any{"normal": map[string]any{"all": true}},
				},
			},
		},
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	var patchResp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(patchW.Body.Bytes(), &patchResp); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	cfg, _ := patchResp.Data["config"].(map[string]any)
	filters, _ := cfg["filters"].(map[string]any)
	if filters == nil {
		t.Fatal("expected filters after patch")
	}
	taskTypes, _ := filters["task_types"].(map[string]any)
	if taskTypes == nil {
		t.Fatal("expected task_types in filters after patch")
	}
	items, _ := taskTypes["items"].(map[string]any)
	normalGroup, _ := items["normal"].(map[string]any)
	if normalGroup == nil || normalGroup["all"] != true {
		t.Errorf("task_types.items.normal after patch: want {all:true}, got %v", items["normal"])
	}
}

// TestIntegrationViews_UpdateConfig_PageSize verifies that PATCHing a view
// with page_size and initial_page_size persists both independently and
// returns them on a subsequent GET.
func TestIntegrationViews_UpdateConfig_PageSize(t *testing.T) {
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	sprintRepo := newFakeSprintRepoIT()
	viewRepo := newFakeViewRepoIT()
	r := buildViewTestRouter(viewRepo, sprintRepo, store)
	tok := issueViewToken(t, uuid.NewString())
	sprintID := seedSprintIT(t, sprintRepo, projectID)
	itemBase := fmt.Sprintf("/api/v1/projects/%s/views", projectID)
	base := fmt.Sprintf("%s?context=sprint&sprint_id=%s", itemBase, sprintID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":      "Board View",
		"view_type": "board",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", createW.Code)
	}
	viewID := viewIDFrom(t, createW.Body.Bytes())

	// PATCH page_size and initial_page_size independently.
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, itemBase+"/"+viewID, tok, map[string]any{
		"config": map[string]any{
			"page_size":         50,
			"initial_page_size": 10,
		},
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	var patchResp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(patchW.Body.Bytes(), &patchResp); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	cfg, _ := patchResp.Data["config"].(map[string]any)
	if cfg == nil {
		t.Fatal("expected config in patch response")
	}
	if pageSize, _ := cfg["page_size"].(float64); pageSize != 50 {
		t.Errorf("page_size: want 50, got %v", cfg["page_size"])
	}
	if initialPageSize, _ := cfg["initial_page_size"].(float64); initialPageSize != 10 {
		t.Errorf("initial_page_size: want 10, got %v", cfg["initial_page_size"])
	}

	// GET the view back and verify both fields survive the round trip.
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, itemBase+"/"+viewID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}
	var getResp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	getCfg, _ := getResp.Data["config"].(map[string]any)
	if getCfg == nil {
		t.Fatal("expected config in get response")
	}
	if pageSize, _ := getCfg["page_size"].(float64); pageSize != 50 {
		t.Errorf("after GET, page_size: want 50, got %v", getCfg["page_size"])
	}
	if initialPageSize, _ := getCfg["initial_page_size"].(float64); initialPageSize != 10 {
		t.Errorf("after GET, initial_page_size: want 10, got %v", getCfg["initial_page_size"])
	}
}
