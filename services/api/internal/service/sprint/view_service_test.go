// Package sprintsvc_test contains unit tests for the view service.
package sprintsvc_test

import (
	"context"
	"sync"
	"testing"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Fake ViewRepository
// ---------------------------------------------------------------------------

type fakeViewRepo struct {
	mu        sync.RWMutex
	views     map[uuid.UUID]*sprintdom.SprintView
	positions map[string]*sprintdom.ViewTaskPosition // key: viewID+":"+taskID
}

func newFakeViewRepo() *fakeViewRepo {
	return &fakeViewRepo{
		views:     make(map[uuid.UUID]*sprintdom.SprintView),
		positions: make(map[string]*sprintdom.ViewTaskPosition),
	}
}

func posKey(viewID, taskID uuid.UUID) string {
	return viewID.String() + ":" + taskID.String()
}

// uuidPtr returns a pointer to the given uuid value.
func uuidPtr(id uuid.UUID) *uuid.UUID { return &id }

func (r *fakeViewRepo) ListViews(_ context.Context, sprintID uuid.UUID) ([]*sprintdom.SprintView, error) {
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

func (r *fakeViewRepo) ListProjectViews(_ context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext) ([]*sprintdom.SprintView, error) {
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

func (r *fakeViewRepo) FindViewByID(_ context.Context, id uuid.UUID) (*sprintdom.SprintView, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.views[id]
	if !ok {
		return nil, sprintdom.ErrViewNotFound
	}
	cp := *v
	return &cp, nil
}

func (r *fakeViewRepo) CreateView(_ context.Context, v *sprintdom.SprintView) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *v
	r.views[v.ID] = &cp
	return nil
}

func (r *fakeViewRepo) UpdateView(_ context.Context, v *sprintdom.SprintView) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.views[v.ID]; !ok {
		return sprintdom.ErrViewNotFound
	}
	cp := *v
	r.views[v.ID] = &cp
	return nil
}

func (r *fakeViewRepo) DeleteView(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.views, id)
	return nil
}

func (r *fakeViewRepo) CountViews(_ context.Context, sprintID uuid.UUID) (int, error) {
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

func (r *fakeViewRepo) CountProjectViews(_ context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext) (int, error) {
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

func (r *fakeViewRepo) UpsertTaskPosition(_ context.Context, pos *sprintdom.ViewTaskPosition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *pos
	r.positions[posKey(pos.ViewID, pos.TaskID)] = &cp
	return nil
}

func (r *fakeViewRepo) BulkUpsertTaskPositions(_ context.Context, positions []*sprintdom.ViewTaskPosition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, pos := range positions {
		cp := *pos
		r.positions[posKey(pos.ViewID, pos.TaskID)] = &cp
	}
	return nil
}

func (r *fakeViewRepo) ListTaskPositions(_ context.Context, viewID uuid.UUID) ([]*sprintdom.ViewTaskPosition, error) {
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

func (r *fakeViewRepo) ReorderViews(_ context.Context, items []sprintdom.ViewReorderItem) error {
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
// Tests
// ---------------------------------------------------------------------------

func TestViewService_CreateView_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	sprintID := uuid.New()
	v, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    &sprintID,
		Name:        "Backlog",
		ViewType:    sprintdom.ViewTypeTable,
		ViewContext: sprintdom.ViewContextSprint,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Name != "Backlog" {
		t.Errorf("expected name=Backlog, got %q", v.Name)
	}
	if v.ViewType != sprintdom.ViewTypeTable {
		t.Errorf("expected view_type=table, got %q", v.ViewType)
	}
	if v.SprintID == nil || *v.SprintID != sprintID {
		t.Errorf("sprint_id mismatch")
	}
}

func TestViewService_CreateView_DefaultTypeIsTable(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	v, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "My View",
		ViewContext: sprintdom.ViewContextSprint,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.ViewType != sprintdom.ViewTypeTable {
		t.Errorf("expected default type=table, got %q", v.ViewType)
	}
}

func TestViewService_CreateView_EmptyNameReturnsError(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	_, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "   ",
		ViewType:    sprintdom.ViewTypeBoard,
		ViewContext: sprintdom.ViewContextSprint,
	})
	if err != sprintdom.ErrViewNameInvalid {
		t.Errorf("expected ErrViewNameInvalid, got %v", err)
	}
}

func TestViewService_CreateView_InvalidTypeReturnsError(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	_, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "Bad",
		ViewType:    "gantt",
		ViewContext: sprintdom.ViewContextSprint,
	})
	if err != sprintdom.ErrViewTypeInvalid {
		t.Errorf("expected ErrViewTypeInvalid, got %v", err)
	}
}

func TestViewService_CreateView_PluginWithoutConfigReturnsError(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	_, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "Dashboard",
		ViewType:    sprintdom.ViewTypePlugin,
		ViewContext: sprintdom.ViewContextSprint,
	})
	if err != sprintdom.ErrViewPluginConfigRequired {
		t.Errorf("expected ErrViewPluginConfigRequired, got %v", err)
	}
}

func TestViewService_CreateView_PluginWithPartialConfigReturnsError(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	_, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "Dashboard",
		ViewType:    sprintdom.ViewTypePlugin,
		Config:      sprintdom.ViewConfig{PluginID: "com.paca.dashboard"}, // missing PluginComponent
		ViewContext: sprintdom.ViewContextSprint,
	})
	if err != sprintdom.ErrViewPluginConfigRequired {
		t.Errorf("expected ErrViewPluginConfigRequired, got %v", err)
	}
}

func TestViewService_CreateView_PluginWithWhitespaceOnlyConfigReturnsError(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	_, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID: uuidPtr(uuid.New()),
		Name:     "Dashboard",
		ViewType: sprintdom.ViewTypePlugin,
		Config: sprintdom.ViewConfig{
			PluginID:        "  ",
			PluginComponent: "  ",
		},
		ViewContext: sprintdom.ViewContextSprint,
	})
	if err != sprintdom.ErrViewPluginConfigRequired {
		t.Errorf("expected ErrViewPluginConfigRequired, got %v", err)
	}
}

func TestViewService_CreateView_PluginWithConfig_OK(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	v, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID: uuidPtr(uuid.New()),
		Name:     "Dashboard",
		ViewType: sprintdom.ViewTypePlugin,
		Config: sprintdom.ViewConfig{
			PluginID:        "com.paca.dashboard",
			PluginComponent: "DashboardIntegrationView",
		},
		ViewContext: sprintdom.ViewContextSprint,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Config.PluginID != "com.paca.dashboard" || v.Config.PluginComponent != "DashboardIntegrationView" {
		t.Errorf("plugin config not persisted: %+v", v.Config)
	}
}

func TestViewService_UpdateView_ChangeToPluginWithoutConfigReturnsError(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	created, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "Table",
		ViewType:    sprintdom.ViewTypeTable,
		ViewContext: sprintdom.ViewContextSprint,
	})

	newType := sprintdom.ViewTypePlugin
	_, err := svc.UpdateView(ctx, created.ProjectID, created.ID, sprintdom.UpdateViewInput{ViewType: &newType})
	if err != sprintdom.ErrViewPluginConfigRequired {
		t.Errorf("expected ErrViewPluginConfigRequired, got %v", err)
	}
}

func TestViewService_UpdateView_ClearingPluginConfigWithoutTypeChangeReturnsError(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	created, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID: uuidPtr(uuid.New()),
		Name:     "Dashboard",
		ViewType: sprintdom.ViewTypePlugin,
		Config: sprintdom.ViewConfig{
			PluginID:        "com.paca.dashboard",
			PluginComponent: "DashboardIntegrationView",
		},
		ViewContext: sprintdom.ViewContextSprint,
	})

	emptyCfg := sprintdom.ViewConfig{}
	_, err := svc.UpdateView(ctx, created.ProjectID, created.ID, sprintdom.UpdateViewInput{Config: &emptyCfg})
	if err != sprintdom.ErrViewPluginConfigRequired {
		t.Errorf("expected ErrViewPluginConfigRequired, got %v", err)
	}
}

func TestViewService_UpdateView_RenameKeepsExistingPluginConfig(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	created, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID: uuidPtr(uuid.New()),
		Name:     "Dashboard",
		ViewType: sprintdom.ViewTypePlugin,
		Config: sprintdom.ViewConfig{
			PluginID:        "com.paca.dashboard",
			PluginComponent: "DashboardIntegrationView",
		},
		ViewContext: sprintdom.ViewContextSprint,
	})
	if err != nil {
		t.Fatalf("unexpected error creating view: %v", err)
	}

	newName := "Dashboard (renamed)"
	updated, err := svc.UpdateView(ctx, created.ProjectID, created.ID, sprintdom.UpdateViewInput{Name: &newName})
	if err != nil {
		t.Fatalf("unexpected error updating unrelated field on valid plugin view: %v", err)
	}
	if updated.Name != newName {
		t.Errorf("Name: want %q, got %q", newName, updated.Name)
	}
	if updated.Config.PluginID != "com.paca.dashboard" || updated.Config.PluginComponent != "DashboardIntegrationView" {
		t.Errorf("plugin config should be preserved untouched, got %+v", updated.Config)
	}
}

func TestViewService_GetView_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	created, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "Sprint View",
		ViewType:    sprintdom.ViewTypeBoard,
		ViewContext: sprintdom.ViewContextSprint,
	})

	got, err := svc.GetView(ctx, created.ProjectID, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("id mismatch")
	}
}

func TestViewService_GetView_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	_, err := svc.GetView(ctx, uuid.New(), uuid.New())
	if err != sprintdom.ErrViewNotFound {
		t.Errorf("expected ErrViewNotFound, got %v", err)
	}
}

func TestViewService_UpdateView_Name(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	created, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "Old Name",
		ViewType:    sprintdom.ViewTypeTable,
		ViewContext: sprintdom.ViewContextSprint,
	})

	newName := "New Name"
	updated, err := svc.UpdateView(ctx, created.ProjectID, created.ID, sprintdom.UpdateViewInput{Name: &newName})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("expected name=New Name, got %q", updated.Name)
	}
}

func TestViewService_UpdateView_Config(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	created, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "Board View",
		ViewType:    sprintdom.ViewTypeBoard,
		ViewContext: sprintdom.ViewContextSprint,
	})

	cfg := sprintdom.ViewConfig{ColumnBy: "status", Swimlanes: "assignee"}
	updated, err := svc.UpdateView(ctx, created.ProjectID, created.ID, sprintdom.UpdateViewInput{Config: &cfg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Config.ColumnBy != "status" {
		t.Errorf("expected column_by=status, got %q", updated.Config.ColumnBy)
	}
}

func TestViewService_UpdateView_Config_PageSize(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	created, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "Table View",
		ViewType:    sprintdom.ViewTypeTable,
		ViewContext: sprintdom.ViewContextSprint,
	})

	cfg := sprintdom.ViewConfig{PageSize: 50, InitialPageSize: 10}
	updated, err := svc.UpdateView(ctx, created.ProjectID, created.ID, sprintdom.UpdateViewInput{Config: &cfg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Config.PageSize != 50 {
		t.Errorf("expected page_size=50, got %d", updated.Config.PageSize)
	}
	if updated.Config.InitialPageSize != 10 {
		t.Errorf("expected initial_page_size=10, got %d", updated.Config.InitialPageSize)
	}
}

func TestViewService_UpdateView_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	name := "Does not matter"
	_, err := svc.UpdateView(ctx, uuid.New(), uuid.New(), sprintdom.UpdateViewInput{Name: &name})
	if err != sprintdom.ErrViewNotFound {
		t.Errorf("expected ErrViewNotFound, got %v", err)
	}
}

func TestViewService_DeleteView_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	sprintID := uuid.New()
	v1, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: &sprintID, Name: "V1", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextSprint})
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: &sprintID, Name: "V2", ViewType: sprintdom.ViewTypeBoard, ViewContext: sprintdom.ViewContextSprint})

	if err := svc.DeleteView(ctx, v1.ProjectID, v1.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.GetView(ctx, v1.ProjectID, v1.ID)
	if err != sprintdom.ErrViewNotFound {
		t.Errorf("expected ErrViewNotFound after deletion, got %v", err)
	}
}

func TestViewService_DeleteView_LastViewRejected(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	v, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "Only View",
		ViewType:    sprintdom.ViewTypeTable,
		ViewContext: sprintdom.ViewContextSprint,
	})

	err := svc.DeleteView(ctx, v.ProjectID, v.ID)
	if err != sprintdom.ErrViewIsLastView {
		t.Errorf("expected ErrViewIsLastView, got %v", err)
	}
}

func TestViewService_DeleteView_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	err := svc.DeleteView(ctx, uuid.New(), uuid.New())
	if err != sprintdom.ErrViewNotFound {
		t.Errorf("expected ErrViewNotFound, got %v", err)
	}
}

func TestViewService_MoveTask_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	v, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		SprintID:    uuidPtr(uuid.New()),
		Name:        "V",
		ViewType:    sprintdom.ViewTypeTable,
		ViewContext: sprintdom.ViewContextSprint,
	})

	taskID := uuid.New()
	grp := "todo"
	if err := svc.MoveTask(ctx, v.ProjectID, v.ID, sprintdom.MoveTaskInput{
		TaskID:   taskID,
		Position: 3,
		GroupKey: &grp,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	positions, err := svc.ListTaskPositions(ctx, v.ProjectID, v.ID)
	if err != nil {
		t.Fatalf("list positions: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	if positions[0].TaskID != taskID {
		t.Errorf("task_id mismatch")
	}
	if positions[0].Position != 3 {
		t.Errorf("expected position=3, got %g", positions[0].Position)
	}
}

func TestViewService_MoveTask_ViewNotFound(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	err := svc.MoveTask(ctx, uuid.New(), uuid.New(), sprintdom.MoveTaskInput{
		TaskID:   uuid.New(),
		Position: 0,
	})
	if err != sprintdom.ErrViewNotFound {
		t.Errorf("expected ErrViewNotFound, got %v", err)
	}
}

func TestViewService_ListViews_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	sprintID := uuid.New()
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: &sprintID, Name: "A", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextSprint})
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: &sprintID, Name: "B", ViewType: sprintdom.ViewTypeRoadmap, ViewContext: sprintdom.ViewContextSprint})

	views, err := svc.ListViews(ctx, sprintID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(views) != 2 {
		t.Errorf("expected 2 views, got %d", len(views))
	}
}

// ---------------------------------------------------------------------------
// Product-backlog view tests
// ---------------------------------------------------------------------------

func TestViewService_ListBacklogViews_Empty(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	views, err := svc.ListProjectViews(ctx, uuid.New(), sprintdom.ViewContextBacklog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(views) != 0 {
		t.Errorf("expected 0 views, got %d", len(views))
	}
}

func TestViewService_ListBacklogViews_ReturnsOnlyBacklogViews(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	otherProjectID := uuid.New()
	sprintID := uuid.New()

	// backlog view for our project
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "Backlog Table", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextBacklog})
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "Backlog Board", ViewType: sprintdom.ViewTypeBoard, ViewContext: sprintdom.ViewContextBacklog})
	// sprint view for same project — should NOT appear in backlog list
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: &sprintID, ProjectID: projectID, Name: "Sprint View", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextSprint})
	// backlog view for a different project — should NOT appear
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: otherProjectID, Name: "Other Backlog", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextBacklog})

	views, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(views) != 2 {
		t.Errorf("expected 2 backlog views, got %d", len(views))
	}
	for _, v := range views {
		if v.SprintID != nil {
			t.Errorf("backlog view should have nil SprintID, got %v", v.SprintID)
		}
		if v.ProjectID != projectID {
			t.Errorf("backlog view has wrong project_id: %v", v.ProjectID)
		}
	}
}

func TestViewService_CreateBacklogView_NilSprintID(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	projectID := uuid.New()
	v, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		ProjectID:   projectID,
		Name:        "My Backlog",
		ViewType:    sprintdom.ViewTypeBoard,
		ViewContext: sprintdom.ViewContextBacklog,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.SprintID != nil {
		t.Errorf("expected SprintID=nil for backlog view, got %v", v.SprintID)
	}
	if v.ProjectID != projectID {
		t.Errorf("expected project_id=%s, got %s", projectID, v.ProjectID)
	}
}

func TestViewService_DeleteBacklogView_LastViewRejected(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	v, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		ProjectID:   projectID,
		Name:        "Only Backlog View",
		ViewType:    sprintdom.ViewTypeTable,
		ViewContext: sprintdom.ViewContextBacklog,
	})

	err := svc.DeleteView(ctx, v.ProjectID, v.ID)
	if err != sprintdom.ErrViewIsLastView {
		t.Errorf("expected ErrViewIsLastView, got %v", err)
	}
}

func TestViewService_DeleteBacklogView_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	v1, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "BL1", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextBacklog})
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "BL2", ViewType: sprintdom.ViewTypeBoard, ViewContext: sprintdom.ViewContextBacklog})

	if err := svc.DeleteView(ctx, v1.ProjectID, v1.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err := svc.GetView(ctx, v1.ProjectID, v1.ID)
	if err != sprintdom.ErrViewNotFound {
		t.Errorf("expected ErrViewNotFound after deletion, got %v", err)
	}
}

func TestViewService_BacklogAndSprintViewsDontInterfere(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	sprintID := uuid.New()

	// Create one sprint view and one backlog view for the same project
	sv, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: &sprintID, ProjectID: projectID, Name: "Sprint Board", ViewType: sprintdom.ViewTypeBoard, ViewContext: sprintdom.ViewContextSprint})
	bv, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "Backlog Table", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextBacklog})

	// ListViews should only return sprint view
	sprintViews, _ := svc.ListViews(ctx, sprintID)
	if len(sprintViews) != 1 || sprintViews[0].ID != sv.ID {
		t.Errorf("ListViews returned wrong results: %v", sprintViews)
	}

	// ListProjectViews(backlog) should only return backlog view
	backlogViews, _ := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog)
	if len(backlogViews) != 1 || backlogViews[0].ID != bv.ID {
		t.Errorf("ListBacklogViews returned wrong results: %v", backlogViews)
	}

	// Deleting the sprint view should use sprint-scoped count; backlog view is not counted
	// so deleting sv (1 sprint view) → ErrViewIsLastView
	if err := svc.DeleteView(ctx, sv.ProjectID, sv.ID); err != sprintdom.ErrViewIsLastView {
		t.Errorf("expected ErrViewIsLastView for sole sprint view, got %v", err)
	}

	// Deleting backlog view (1 backlog view) → ErrViewIsLastView
	if err := svc.DeleteView(ctx, bv.ProjectID, bv.ID); err != sprintdom.ErrViewIsLastView {
		t.Errorf("expected ErrViewIsLastView for sole backlog view, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ReorderViews tests
// ---------------------------------------------------------------------------

func TestViewService_ReorderViews_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	sprintID := uuid.New()
	v1, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: uuidPtr(sprintID), Name: "A", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextSprint})
	v2, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: uuidPtr(sprintID), Name: "B", ViewType: sprintdom.ViewTypeBoard, ViewContext: sprintdom.ViewContextSprint})
	v3, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: uuidPtr(sprintID), Name: "C", ViewType: sprintdom.ViewTypeRoadmap, ViewContext: sprintdom.ViewContextSprint})

	// Reorder: C, A, B
	if err := svc.ReorderViews(ctx, sprintID, []uuid.UUID{v3.ID, v1.ID, v2.ID}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated1, _ := svc.GetView(ctx, v1.ProjectID, v1.ID)
	updated2, _ := svc.GetView(ctx, v2.ProjectID, v2.ID)
	updated3, _ := svc.GetView(ctx, v3.ProjectID, v3.ID)

	if updated3.Position != 0 {
		t.Errorf("C: expected position=0, got %g", updated3.Position)
	}
	if updated1.Position != 1 {
		t.Errorf("A: expected position=1, got %g", updated1.Position)
	}
	if updated2.Position != 2 {
		t.Errorf("B: expected position=2, got %g", updated2.Position)
	}
}

func TestViewService_ReorderViews_CountMismatch(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	sprintID := uuid.New()
	v1, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: uuidPtr(sprintID), Name: "A", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextSprint})
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: uuidPtr(sprintID), Name: "B", ViewType: sprintdom.ViewTypeBoard, ViewContext: sprintdom.ViewContextSprint})

	// Only one ID provided for two views
	err := svc.ReorderViews(ctx, sprintID, []uuid.UUID{v1.ID})
	if err != sprintdom.ErrViewReorderInvalid {
		t.Errorf("expected ErrViewReorderInvalid, got %v", err)
	}
}

func TestViewService_ReorderViews_UnknownID(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	sprintID := uuid.New()
	v1, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: uuidPtr(sprintID), Name: "A", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextSprint})

	err := svc.ReorderViews(ctx, sprintID, []uuid.UUID{v1.ID, uuid.New()})
	if err != sprintdom.ErrViewReorderInvalid {
		t.Errorf("expected ErrViewReorderInvalid, got %v", err)
	}
}

func TestViewService_ReorderViews_EmptyList(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	sprintID := uuid.New()
	// No views exist; empty list should succeed (0 == 0)
	if err := svc.ReorderViews(ctx, sprintID, []uuid.UUID{}); err != nil {
		t.Errorf("expected nil for empty+empty, got %v", err)
	}
}

func TestViewService_ReorderBacklogViews_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	b1, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "X", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextBacklog})
	b2, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "Y", ViewType: sprintdom.ViewTypeBoard, ViewContext: sprintdom.ViewContextBacklog})

	if err := svc.ReorderProjectViews(ctx, projectID, sprintdom.ViewContextBacklog, []uuid.UUID{b2.ID, b1.ID}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updB1, _ := svc.GetView(ctx, b1.ProjectID, b1.ID)
	updB2, _ := svc.GetView(ctx, b2.ProjectID, b2.ID)
	if updB2.Position != 0 {
		t.Errorf("Y: expected position=0, got %g", updB2.Position)
	}
	if updB1.Position != 1 {
		t.Errorf("X: expected position=1, got %g", updB1.Position)
	}
}

// ---------------------------------------------------------------------------
// Timeline view tests
// ---------------------------------------------------------------------------

func TestViewService_ListTimelineViews_Empty(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	views, err := svc.ListProjectViews(ctx, uuid.New(), sprintdom.ViewContextTimeline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(views) != 0 {
		t.Errorf("expected 0 views, got %d", len(views))
	}
}

func TestViewService_ListTimelineViews_ReturnsOnlyTimelineViews(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	otherID := uuid.New()
	sprintID := uuid.New()

	// Two timeline views for our project.
	tv1, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "Roadmap", ViewType: sprintdom.ViewTypeRoadmap, ViewContext: sprintdom.ViewContextTimeline})
	tv2, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "Timeline Table", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextTimeline})
	// A backlog view for the same project — must NOT appear.
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "Backlog", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextBacklog})
	// A sprint view for the same project — must NOT appear.
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{SprintID: &sprintID, ProjectID: projectID, Name: "Sprint", ViewType: sprintdom.ViewTypeBoard, ViewContext: sprintdom.ViewContextSprint})
	// A timeline view for a different project — must NOT appear.
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: otherID, Name: "Other TL", ViewType: sprintdom.ViewTypeRoadmap, ViewContext: sprintdom.ViewContextTimeline})

	views, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextTimeline)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(views) != 2 {
		t.Errorf("expected 2 timeline views, got %d", len(views))
	}
	ids := map[uuid.UUID]bool{tv1.ID: true, tv2.ID: true}
	for _, v := range views {
		if !ids[v.ID] {
			t.Errorf("unexpected view id in result: %v", v.ID)
		}
		if v.ViewContext != sprintdom.ViewContextTimeline {
			t.Errorf("expected ViewContext=timeline, got %q", v.ViewContext)
		}
	}
}

func TestViewService_CreateTimelineView_HasCorrectContext(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.NewViewService(newFakeViewRepo())

	projectID := uuid.New()
	v, err := svc.CreateView(ctx, sprintdom.CreateViewInput{
		ProjectID:   projectID,
		Name:        "Roadmap",
		ViewType:    sprintdom.ViewTypeRoadmap,
		ViewContext: sprintdom.ViewContextTimeline,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.ViewContext != sprintdom.ViewContextTimeline {
		t.Errorf("expected ViewContext=timeline, got %q", v.ViewContext)
	}
	if v.SprintID != nil {
		t.Errorf("expected SprintID=nil for timeline view, got %v", v.SprintID)
	}
}

func TestViewService_DeleteTimelineView_LastViewRejected(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	v, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		ProjectID:   projectID,
		Name:        "Only Timeline",
		ViewType:    sprintdom.ViewTypeRoadmap,
		ViewContext: sprintdom.ViewContextTimeline,
	})

	if err := svc.DeleteView(ctx, v.ProjectID, v.ID); err != sprintdom.ErrViewIsLastView {
		t.Errorf("expected ErrViewIsLastView, got %v", err)
	}
}

func TestViewService_DeleteTimelineView_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	v1, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "TL1", ViewType: sprintdom.ViewTypeRoadmap, ViewContext: sprintdom.ViewContextTimeline})
	_, _ = svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "TL2", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextTimeline})

	if err := svc.DeleteView(ctx, v1.ProjectID, v1.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.GetView(ctx, v1.ProjectID, v1.ID); err != sprintdom.ErrViewNotFound {
		t.Errorf("expected ErrViewNotFound after deletion, got %v", err)
	}
}

func TestViewService_TimelineAndBacklogViewsDontInterfere(t *testing.T) {
	// A timeline view and a backlog view for the same project should be
	// counted independently; deleting the "last" one of each context is
	// correctly blocked.
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	tv, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "Roadmap", ViewType: sprintdom.ViewTypeRoadmap, ViewContext: sprintdom.ViewContextTimeline})
	bv, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "Backlog", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextBacklog})

	// ListProjectViews(timeline) only returns timeline view.
	tlViews, _ := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextTimeline)
	if len(tlViews) != 1 || tlViews[0].ID != tv.ID {
		t.Errorf("ListProjectViews(timeline) wrong: %v", tlViews)
	}
	// ListProjectViews(backlog) only returns backlog view.
	blViews, _ := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog)
	if len(blViews) != 1 || blViews[0].ID != bv.ID {
		t.Errorf("ListBacklogViews wrong: %v", blViews)
	}
	// Deleting the only timeline view is blocked.
	if err := svc.DeleteView(ctx, tv.ProjectID, tv.ID); err != sprintdom.ErrViewIsLastView {
		t.Errorf("expected ErrViewIsLastView for sole timeline view, got %v", err)
	}
	// Deleting the only backlog view is also blocked.
	if err := svc.DeleteView(ctx, bv.ProjectID, bv.ID); err != sprintdom.ErrViewIsLastView {
		t.Errorf("expected ErrViewIsLastView for sole backlog view, got %v", err)
	}
}

func TestViewService_ReorderTimelineViews_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	t1, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "A", ViewType: sprintdom.ViewTypeRoadmap, ViewContext: sprintdom.ViewContextTimeline})
	t2, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{ProjectID: projectID, Name: "B", ViewType: sprintdom.ViewTypeTable, ViewContext: sprintdom.ViewContextTimeline})

	// Swap order: B, A
	if err := svc.ReorderProjectViews(ctx, projectID, sprintdom.ViewContextTimeline, []uuid.UUID{t2.ID, t1.ID}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updT1, _ := svc.GetView(ctx, t1.ProjectID, t1.ID)
	updT2, _ := svc.GetView(ctx, t2.ProjectID, t2.ID)
	if updT2.Position != 0 {
		t.Errorf("B: expected position=0, got %g", updT2.Position)
	}
	if updT1.Position != 1 {
		t.Errorf("A: expected position=1, got %g", updT1.Position)
	}
}

func TestViewService_ViewContextPreservedAfterUpdate(t *testing.T) {
	ctx := context.Background()
	repo := newFakeViewRepo()
	svc := sprintsvc.NewViewService(repo)

	projectID := uuid.New()
	v, _ := svc.CreateView(ctx, sprintdom.CreateViewInput{
		ProjectID:   projectID,
		Name:        "Roadmap",
		ViewType:    sprintdom.ViewTypeRoadmap,
		ViewContext: sprintdom.ViewContextTimeline,
	})

	newName := "Renamed Roadmap"
	updated, err := svc.UpdateView(ctx, v.ProjectID, v.ID, sprintdom.UpdateViewInput{Name: &newName})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	// ViewContext must survive an update since UpdateView only touches name/type/config/position.
	if updated.ViewContext != sprintdom.ViewContextTimeline {
		t.Errorf("ViewContext changed after update: got %q", updated.ViewContext)
	}
}
