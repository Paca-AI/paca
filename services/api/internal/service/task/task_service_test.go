// Package tasksvc_test contains unit tests for the task service layer.
// Tests use in-memory fake repositories and do not require any infrastructure.
package tasksvc_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	taskdom "github.com/paca/api/internal/domain/task"
	tasksvc "github.com/paca/api/internal/service/task"
)

// ---------------------------------------------------------------------------
// Fake repository
// ---------------------------------------------------------------------------

type fakeTaskRepo struct {
	mu       sync.RWMutex
	types    map[uuid.UUID]*taskdom.TaskType
	statuses map[uuid.UUID]*taskdom.TaskStatus
	tasks    map[uuid.UUID]*taskdom.Task
}

func newFakeTaskRepo() *fakeTaskRepo {
	return &fakeTaskRepo{
		types:    make(map[uuid.UUID]*taskdom.TaskType),
		statuses: make(map[uuid.UUID]*taskdom.TaskStatus),
		tasks:    make(map[uuid.UUID]*taskdom.Task),
	}
}

// -- TaskType methods --

func (r *fakeTaskRepo) ListTaskTypes(_ context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*taskdom.TaskType, 0)
	for _, t := range r.types {
		if t.ProjectID == projectID {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeTaskRepo) FindTaskTypeByID(_ context.Context, id uuid.UUID) (*taskdom.TaskType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.types[id]
	if !ok {
		return nil, taskdom.ErrTypeNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *fakeTaskRepo) CreateTaskType(_ context.Context, t *taskdom.TaskType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *t
	r.types[t.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) UpdateTaskType(_ context.Context, t *taskdom.TaskType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.types[t.ID]; !ok {
		return taskdom.ErrTypeNotFound
	}
	cp := *t
	r.types[t.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) DeleteTaskType(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.types, id)
	return nil
}

// -- TaskStatus methods --

func (r *fakeTaskRepo) ListTaskStatuses(_ context.Context, projectID uuid.UUID) ([]*taskdom.TaskStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*taskdom.TaskStatus, 0)
	for _, s := range r.statuses {
		if s.ProjectID == projectID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeTaskRepo) FindTaskStatusByID(_ context.Context, id uuid.UUID) (*taskdom.TaskStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.statuses[id]
	if !ok {
		return nil, taskdom.ErrStatusNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *fakeTaskRepo) CreateTaskStatus(_ context.Context, s *taskdom.TaskStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *s
	r.statuses[s.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) UpdateTaskStatus(_ context.Context, s *taskdom.TaskStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.statuses[s.ID]; !ok {
		return taskdom.ErrStatusNotFound
	}
	cp := *s
	r.statuses[s.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) DeleteTaskStatus(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.statuses, id)
	return nil
}

// -- Task methods --

func (r *fakeTaskRepo) ListTasks(_ context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, offset, limit int) ([]*taskdom.Task, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]*taskdom.Task, 0)
	for _, t := range r.tasks {
		if t.ProjectID != projectID || t.DeletedAt != nil {
			continue
		}
		if filter.SprintID != nil && (t.SprintID == nil || *t.SprintID != *filter.SprintID) {
			continue
		}
		if filter.StatusID != nil && (t.StatusID == nil || *t.StatusID != *filter.StatusID) {
			continue
		}
		if filter.AssigneeID != nil && (t.AssigneeID == nil || *t.AssigneeID != *filter.AssigneeID) {
			continue
		}
		cp := *t
		all = append(all, &cp)
	}
	total := int64(len(all))
	if offset >= len(all) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}

func (r *fakeTaskRepo) FindTaskByID(_ context.Context, id uuid.UUID) (*taskdom.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[id]
	if !ok || t.DeletedAt != nil {
		return nil, taskdom.ErrTaskNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *fakeTaskRepo) CreateTask(_ context.Context, t *taskdom.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *t
	r.tasks[t.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) UpdateTask(_ context.Context, t *taskdom.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tasks[t.ID]; !ok {
		return taskdom.ErrTaskNotFound
	}
	cp := *t
	r.tasks[t.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) DeleteTask(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok || t.DeletedAt != nil {
		return taskdom.ErrTaskNotFound
	}
	now := time.Now()
	t.DeletedAt = &now
	return nil
}

// ---------------------------------------------------------------------------
// Task Type tests
// ---------------------------------------------------------------------------

func TestCreateTaskType_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	icon := "bug"
	got, err := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{
		ProjectID: projectID,
		Name:      "Bug",
		Icon:      &icon,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Bug" {
		t.Errorf("expected Name=Bug, got %q", got.Name)
	}
	if got.ProjectID != projectID {
		t.Errorf("expected ProjectID=%v, got %v", projectID, got.ProjectID)
	}
	if got.Icon == nil || *got.Icon != "bug" {
		t.Errorf("expected Icon=bug, got %v", got.Icon)
	}
}

func TestCreateTaskType_EmptyName(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{
		ProjectID: uuid.New(),
		Name:      "   ",
	})
	if err != taskdom.ErrTypeNameInvalid {
		t.Errorf("expected ErrTypeNameInvalid, got %v", err)
	}
}

func TestUpdateTaskType_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	existing, _ := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{
		ProjectID: projectID,
		Name:      "Feature",
	})

	updated, err := svc.UpdateTaskType(ctx, existing.ID, taskdom.UpdateTaskTypeInput{
		Name: "Feature Request",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "Feature Request" {
		t.Errorf("expected Name=Feature Request, got %q", updated.Name)
	}
}

func TestUpdateTaskType_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.UpdateTaskType(ctx, uuid.New(), taskdom.UpdateTaskTypeInput{Name: "X"})
	if err != taskdom.ErrTypeNotFound {
		t.Errorf("expected ErrTypeNotFound, got %v", err)
	}
}

func TestDeleteTaskType_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	tt, _ := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{ProjectID: projectID, Name: "Chore"})
	if err := svc.DeleteTaskType(ctx, tt.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.GetTaskType(ctx, tt.ID)
	if err != taskdom.ErrTypeNotFound {
		t.Errorf("expected ErrTypeNotFound after delete, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task Status tests
// ---------------------------------------------------------------------------

func TestCreateTaskStatus_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	color := "#FF0000"
	got, err := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{
		ProjectID: projectID,
		Name:      "In Progress",
		Color:     &color,
		Position:  2,
		Category:  taskdom.StatusCategoryInProgress,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Category != taskdom.StatusCategoryInProgress {
		t.Errorf("expected category inprogress, got %q", got.Category)
	}
	if got.Position != 2 {
		t.Errorf("expected position 2, got %d", got.Position)
	}
}

func TestCreateTaskStatus_EmptyName(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{
		ProjectID: uuid.New(),
		Name:      "",
		Category:  taskdom.StatusCategoryTodo,
	})
	if err != taskdom.ErrStatusNameInvalid {
		t.Errorf("expected ErrStatusNameInvalid, got %v", err)
	}
}

func TestCreateTaskStatus_InvalidCategory(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{
		ProjectID: uuid.New(),
		Name:      "Weird",
		Category:  "invalid-category",
	})
	if err != taskdom.ErrStatusCategoryInvalid {
		t.Errorf("expected ErrStatusCategoryInvalid, got %v", err)
	}
}

func TestUpdateTaskStatus_PositionUpdate(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	st, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{
		ProjectID: projectID,
		Name:      "Todo",
		Position:  0,
		Category:  taskdom.StatusCategoryTodo,
	})

	newPos := 5
	updated, err := svc.UpdateTaskStatus(ctx, st.ID, taskdom.UpdateTaskStatusInput{
		Position: &newPos,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Position != 5 {
		t.Errorf("expected position 5, got %d", updated.Position)
	}
}

// ---------------------------------------------------------------------------
// Task tests
// ---------------------------------------------------------------------------

func TestCreateTask_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	desc := "Implement the feature"
	task, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Implement login",
		Description: &desc,
		Importance:  3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.Title != "Implement login" {
		t.Errorf("expected Title=Implement login, got %q", task.Title)
	}
	if task.Importance != 3 {
		t.Errorf("expected Importance=3, got %d", task.Importance)
	}
	if task.CustomFields == nil {
		t.Error("expected non-nil CustomFields map")
	}
}

func TestCreateTask_EmptyTitle(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: uuid.New(),
		Title:     "   ",
	})
	if err != taskdom.ErrTaskTitleInvalid {
		t.Errorf("expected ErrTaskTitleInvalid, got %v", err)
	}
}

func TestUpdateTask_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	task, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID:  projectID,
		Title:      "Old Title",
		Importance: 1,
	})

	newImportance := 5
	updated, err := svc.UpdateTask(ctx, task.ID, taskdom.UpdateTaskInput{
		Title:      "New Title",
		Importance: &newImportance,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Title != "New Title" {
		t.Errorf("expected Title=New Title, got %q", updated.Title)
	}
	if updated.Importance != 5 {
		t.Errorf("expected Importance=5, got %d", updated.Importance)
	}
}

func TestUpdateTask_TitleUnchangedWhenEmpty(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	task, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: projectID,
		Title:     "Keep This Title",
	})

	// Update with empty title — original title should be preserved
	updated, err := svc.UpdateTask(ctx, task.ID, taskdom.UpdateTaskInput{
		Title: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Title != "Keep This Title" {
		t.Errorf("expected original title preserved, got %q", updated.Title)
	}
}

func TestDeleteTask_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	task, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: projectID,
		Title:     "To Delete",
	})

	if err := svc.DeleteTask(ctx, task.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.GetTask(ctx, task.ID)
	if err != taskdom.ErrTaskNotFound {
		t.Errorf("expected ErrTaskNotFound after delete, got %v", err)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	err := svc.DeleteTask(ctx, uuid.New())
	if err != taskdom.ErrTaskNotFound {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestListTasks_FilterBySprint(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()
	sprintID := uuid.New()

	// Create two tasks — one with sprint, one without
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: projectID,
		Title:     "In Sprint",
		SprintID:  &sprintID,
	})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: projectID,
		Title:     "No Sprint",
	})

	tasks, total, err := svc.ListTasks(ctx, projectID, taskdom.TaskFilter{SprintID: &sprintID}, 1, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(tasks) != 1 || tasks[0].Title != "In Sprint" {
		t.Errorf("expected filtered task In Sprint, got %v", tasks)
	}
}

func TestListTasks_Pagination(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	for i := 0; i < 5; i++ {
		_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{
			ProjectID: projectID,
			Title:     "Task",
		})
	}

	_, total, err := svc.ListTasks(ctx, projectID, taskdom.TaskFilter{}, 1, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
}
