package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// ---------------------------------------------------------------------------
// Minimal fakes
// ---------------------------------------------------------------------------

type fakeSprintSvcH struct {
	created []*sprintdom.Sprint
}

func (f *fakeSprintSvcH) ListSprints(_ context.Context, _ uuid.UUID) ([]*sprintdom.Sprint, error) {
	return nil, nil
}
func (f *fakeSprintSvcH) GetSprint(_ context.Context, _, _ uuid.UUID) (*sprintdom.Sprint, error) {
	return nil, sprintdom.ErrSprintNotFound
}
func (f *fakeSprintSvcH) CreateSprint(_ context.Context, in sprintdom.CreateSprintInput) (*sprintdom.Sprint, error) {
	now := time.Now()
	sp := &sprintdom.Sprint{
		ID:        uuid.New(),
		ProjectID: in.ProjectID,
		Name:      in.Name,
		Status:    sprintdom.SprintStatusPlanned,
		CreatedAt: now,
		UpdatedAt: now,
	}
	f.created = append(f.created, sp)
	return sp, nil
}
func (f *fakeSprintSvcH) UpdateSprint(_ context.Context, _, _ uuid.UUID, _ sprintdom.UpdateSprintInput) (*sprintdom.Sprint, error) {
	return nil, sprintdom.ErrSprintNotFound
}
func (f *fakeSprintSvcH) DeleteSprint(_ context.Context, _, _ uuid.UUID) error {
	return nil
}
func (f *fakeSprintSvcH) CompleteSprint(_ context.Context, _, id uuid.UUID, _ sprintdom.CompleteSprintInput) (*sprintdom.Sprint, error) {
	for _, sp := range f.created {
		if sp.ID == id {
			sp.Status = sprintdom.SprintStatusCompleted
			cp := *sp
			return &cp, nil
		}
	}
	return nil, sprintdom.ErrSprintNotFound
}

type fakeViewSvcH struct {
	mu      sync.Mutex
	created []*sprintdom.SprintView
}

type fakeTaskTypeSvcH struct {
	taskTypes []*taskdom.TaskType
}

func (f *fakeTaskTypeSvcH) ListTaskTypes(_ context.Context, _ uuid.UUID) ([]*taskdom.TaskType, error) {
	return f.taskTypes, nil
}

func (f *fakeViewSvcH) ListViews(_ context.Context, _, _ uuid.UUID) ([]*sprintdom.SprintView, error) {
	return nil, nil
}
func (f *fakeViewSvcH) ListProjectViews(_ context.Context, _ uuid.UUID, _ sprintdom.ViewContext) ([]*sprintdom.SprintView, error) {
	return nil, nil
}
func (f *fakeViewSvcH) GetView(_ context.Context, _, _ uuid.UUID) (*sprintdom.SprintView, error) {
	return nil, sprintdom.ErrViewNotFound
}
func (f *fakeViewSvcH) CreateView(_ context.Context, in sprintdom.CreateViewInput) (*sprintdom.SprintView, error) {
	now := time.Now()
	v := &sprintdom.SprintView{
		ID:          uuid.New(),
		SprintID:    in.SprintID,
		ProjectID:   in.ProjectID,
		Name:        in.Name,
		ViewType:    in.ViewType,
		Config:      in.Config,
		Position:    in.Position,
		ViewContext: in.ViewContext,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	f.mu.Lock()
	f.created = append(f.created, v)
	f.mu.Unlock()
	return v, nil
}
func (f *fakeViewSvcH) UpdateView(_ context.Context, _, _ uuid.UUID, _ sprintdom.UpdateViewInput) (*sprintdom.SprintView, error) {
	return nil, sprintdom.ErrViewNotFound
}
func (f *fakeViewSvcH) DeleteView(_ context.Context, _, _ uuid.UUID) error { return nil }
func (f *fakeViewSvcH) MoveTask(_ context.Context, _, _ uuid.UUID, _ sprintdom.MoveTaskInput) error {
	return nil
}
func (f *fakeViewSvcH) BulkMoveTasks(_ context.Context, _, _ uuid.UUID, _ []sprintdom.MoveTaskInput) error {
	return nil
}
func (f *fakeViewSvcH) ListTaskPositions(_ context.Context, _, _ uuid.UUID) ([]*sprintdom.ViewTaskPosition, error) {
	return nil, nil
}
func (f *fakeViewSvcH) ReorderViews(_ context.Context, _, _ uuid.UUID, _ []uuid.UUID) error {
	return nil
}
func (f *fakeViewSvcH) ReorderProjectViews(_ context.Context, _ uuid.UUID, _ sprintdom.ViewContext, _ []uuid.UUID) error {
	return nil
}

func (f *fakeViewSvcH) SetUserViewConfig(_ context.Context, _, _, _ uuid.UUID, _ sprintdom.ViewConfig) (*sprintdom.SprintView, error) {
	return nil, nil
}

func (f *fakeViewSvcH) OverlayUserConfigs(_ context.Context, _ uuid.UUID, _ []*sprintdom.SprintView) error {
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCreateSprint_SeedsDefaultViews(t *testing.T) {

	sprintSvc := &fakeSprintSvcH{}
	viewSvc := &fakeViewSvcH{}
	taskTypeSvc := &fakeTaskTypeSvcH{taskTypes: []*taskdom.TaskType{
		{ID: uuid.New(), Name: "Task"},
		{ID: uuid.New(), Name: "Bug"},
		{ID: uuid.New(), Name: "Epic", IsSystem: true},
		{ID: uuid.New(), Name: "Subtask", IsSystem: true},
	}}

	h := handler.NewSprintHandler(sprintSvc, viewSvc, handler.WithSprintDefaultTaskTypes(taskTypeSvc))

	r := chi.NewRouter()
	r.Post("/projects/{projectId}/sprints", h.CreateSprint)

	body, _ := json.Marshal(map[string]any{"name": "Sprint 1"})
	projectID := uuid.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/projects/"+projectID.String()+"/sprints", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if len(viewSvc.created) != 2 {
		t.Fatalf("expected 2 default views, got %d", len(viewSvc.created))
	}

	viewTypes := map[sprintdom.ViewType]bool{}
	for _, v := range viewSvc.created {
		viewTypes[v.ViewType] = true
	}
	if !viewTypes[sprintdom.ViewTypeBoard] {
		t.Error("expected a board view to be created")
	}
	if !viewTypes[sprintdom.ViewTypeTable] {
		t.Error("expected a table view to be created")
	}

	// Both views must belong to the newly created sprint.
	sprintID := sprintSvc.created[0].ID
	for _, v := range viewSvc.created {
		if v.SprintID == nil || *v.SprintID != sprintID {
			t.Errorf("view %s has wrong sprint ID: want %s, got %v", v.ID, sprintID, v.SprintID)
		}
		if v.ViewContext != sprintdom.ViewContextSprint {
			t.Errorf("expected sprint view context, got %q", v.ViewContext)
		}
	}

	var boardView, tableView *sprintdom.SprintView
	for _, v := range viewSvc.created {
		switch v.ViewType {
		case sprintdom.ViewTypeBoard:
			boardView = v
		case sprintdom.ViewTypeTable:
			tableView = v
		case sprintdom.ViewTypeRoadmap, sprintdom.ViewTypePlugin:
			// roadmap and plugin views are not seeded for sprint defaults
		}
	}
	if boardView == nil || tableView == nil {
		t.Fatal("expected seeded board and table views")
	}
	if boardView.Config.ColumnBy != "status" {
		t.Errorf("expected board column_by=status, got %q", boardView.Config.ColumnBy)
	}
	// Board view must use the "normal" virtual group to include all non-system types.
	if boardView.Config.Filters == nil || boardView.Config.Filters.TaskTypes == nil {
		t.Error("expected board view to have a task type filter")
	} else {
		normalEntry, ok := boardView.Config.Filters.TaskTypes.Items["normal"]
		if !ok || !normalEntry.IsNested() || !normalEntry.Config().All {
			t.Error("expected board view task types to use the all-normal group")
		}
	}
	// Board view must include the sprint in its sprints filter.
	if boardView.Config.Filters == nil || boardView.Config.Filters.Sprints == nil {
		t.Errorf("expected board view to have a sprint filter")
	} else {
		sprintEntry, ok := boardView.Config.Filters.Sprints.Items[sprintID.String()]
		if !ok || sprintEntry.IsNested() || !sprintEntry.Flag() {
			t.Errorf("expected board sprint filter to include %s, got %+v", sprintID, boardView.Config.Filters.Sprints)
		}
	}
	if tableView.Config.ColumnBy != "status" {
		t.Errorf("expected table column_by=status, got %q", tableView.Config.ColumnBy)
	}
	// Table view must also include the sprint in its sprints filter.
	if tableView.Config.Filters == nil || tableView.Config.Filters.Sprints == nil {
		t.Errorf("expected table view to have a sprint filter")
	} else {
		sprintEntry, ok := tableView.Config.Filters.Sprints.Items[sprintID.String()]
		if !ok || sprintEntry.IsNested() || !sprintEntry.Flag() {
			t.Errorf("expected table sprint filter to include %s, got %+v", sprintID, tableView.Config.Filters.Sprints)
		}
	}
}

func TestCompleteSprint_OK(t *testing.T) {

	sprintSvc := &fakeSprintSvcH{}
	viewSvc := &fakeViewSvcH{}

	// Pre-seed an active sprint into the fake service.
	projectID := uuid.New()
	activeSprint := &sprintdom.Sprint{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      "Sprint 1",
		Status:    sprintdom.SprintStatusActive,
	}
	sprintSvc.created = append(sprintSvc.created, activeSprint)

	h := handler.NewSprintHandler(sprintSvc, viewSvc)
	r := chi.NewRouter()
	r.Post("/projects/{projectId}/sprints/{sprintId}/complete", h.CompleteSprint)

	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/projects/"+projectID.String()+"/sprints/"+activeSprint.ID.String()+"/complete",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Data.Status != string(sprintdom.SprintStatusCompleted) {
		t.Errorf("expected status=completed, got %q", resp.Data.Status)
	}
}

func TestCompleteSprint_NotFound(t *testing.T) {

	sprintSvc := &fakeSprintSvcH{}
	viewSvc := &fakeViewSvcH{}
	h := handler.NewSprintHandler(sprintSvc, viewSvc)

	r := chi.NewRouter()
	r.Post("/projects/{projectId}/sprints/{sprintId}/complete", h.CompleteSprint)

	body, _ := json.Marshal(map[string]any{})
	projectID := uuid.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/projects/"+projectID.String()+"/sprints/"+uuid.New().String()+"/complete",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateSprint_EmptyName_Returns400(t *testing.T) {
	sprintSvc := &fakeSprintSvcH{}
	viewSvc := &fakeViewSvcH{}
	h := handler.NewSprintHandler(sprintSvc, viewSvc)

	r := chi.NewRouter()
	r.Post("/projects/{projectId}/sprints", h.CreateSprint)

	body, _ := json.Marshal(map[string]any{"name": ""})
	projectID := uuid.New()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/projects/"+projectID.String()+"/sprints", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty sprint name, got %d: %s", w.Code, w.Body.String())
	}
}

func newViewRouter(svc sprintdom.ViewService) chi.Router {
	h := handler.NewViewHandler(svc)
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Get("/views", h.ListViews)
		r.Post("/views", h.CreateView)
		r.Put("/views/positions", h.ReorderViews)
		r.Get("/views/{viewId}", h.GetView)
		r.Patch("/views/{viewId}", h.UpdateView)
		r.Delete("/views/{viewId}", h.DeleteView)
	})
	return r
}

func TestCreateView_EmptyName_Returns400(t *testing.T) {
	r := newViewRouter(&fakeViewSvcH{})
	projectID := uuid.New()
	sprintID := uuid.New()

	body, _ := json.Marshal(map[string]any{"name": ""})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/projects/"+projectID.String()+"/views?context=sprint&sprint_id="+sprintID.String(),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty view name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReorderViews_EmptyViewIDs_Returns400(t *testing.T) {
	r := newViewRouter(&fakeViewSvcH{})
	projectID := uuid.New()
	sprintID := uuid.New()

	body, _ := json.Marshal(map[string]any{"view_ids": []string{}})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPut,
		"/projects/"+projectID.String()+"/views/positions?context=sprint&sprint_id="+sprintID.String(),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty view_ids, got %d: %s", w.Code, w.Body.String())
	}
}
