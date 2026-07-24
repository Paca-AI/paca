// Package tasksvc_test contains unit tests for the task service layer.
// Tests use in-memory fake repositories and do not require any infrastructure.
package tasksvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Fake repository
// ---------------------------------------------------------------------------

type fakeTaskRepo struct {
	mu                   sync.RWMutex
	types                map[uuid.UUID]*taskdom.TaskType
	statuses             map[uuid.UUID]*taskdom.TaskStatus
	tasks                map[uuid.UUID]*taskdom.Task
	customFields         map[uuid.UUID]*taskdom.CustomFieldDefinition
	counters             map[uuid.UUID]int64 // project-scoped task number counters
	findDefaultTypeErr   error               // injected error for FindDefaultTaskType
	findDefaultStatusErr error               // injected error for FindDefaultTaskStatus
	setDefaultStatusErr  error               // injected error for SetDefaultTaskStatus
}

// taskMatchesSearch mirrors the postgres repo's title / "#<task_number>" ILIKE
// matching so the fake exercises the same filter contract as production.
func taskMatchesSearch(t *taskdom.Task, filter taskdom.TaskFilter) bool {
	if filter.Search == nil {
		return true
	}
	q := strings.ToLower(strings.TrimSpace(*filter.Search))
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(t.Title), q) ||
		strings.Contains(fmt.Sprintf("#%d", t.TaskNumber), q)
}

func newFakeTaskRepo() *fakeTaskRepo {
	return &fakeTaskRepo{
		types:        make(map[uuid.UUID]*taskdom.TaskType),
		statuses:     make(map[uuid.UUID]*taskdom.TaskStatus),
		tasks:        make(map[uuid.UUID]*taskdom.Task),
		customFields: make(map[uuid.UUID]*taskdom.CustomFieldDefinition),
		counters:     make(map[uuid.UUID]int64),
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

func (r *fakeTaskRepo) SetDefaultTaskType(_ context.Context, projectID, typeID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	found := false
	for _, t := range r.types {
		if t.ProjectID == projectID {
			if t.ID == typeID {
				t.IsDefault = true
				found = true
			} else {
				t.IsDefault = false
			}
		}
	}
	if !found {
		return taskdom.ErrTypeNotFound
	}
	return nil
}

func (r *fakeTaskRepo) FindDefaultTaskType(_ context.Context, projectID uuid.UUID) (*taskdom.TaskType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.findDefaultTypeErr != nil {
		return nil, r.findDefaultTypeErr
	}
	for _, t := range r.types {
		if t.ProjectID == projectID && t.IsDefault {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
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

func (r *fakeTaskRepo) SetDefaultTaskStatus(_ context.Context, projectID, statusID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setDefaultStatusErr != nil {
		return r.setDefaultStatusErr
	}
	found := false
	for _, s := range r.statuses {
		if s.ProjectID == projectID {
			if s.ID == statusID {
				s.IsDefault = true
				found = true
			} else {
				s.IsDefault = false
			}
		}
	}
	if !found {
		return taskdom.ErrStatusNotFound
	}
	return nil
}

func (r *fakeTaskRepo) FindDefaultTaskStatus(_ context.Context, projectID uuid.UUID) (*taskdom.TaskStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.findDefaultStatusErr != nil {
		return nil, r.findDefaultStatusErr
	}
	for _, s := range r.statuses {
		if s.ProjectID == projectID && s.IsDefault {
			cp := *s
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *fakeTaskRepo) ReorderTaskStatuses(_ context.Context, projectID uuid.UUID, statusIDs []uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing := make(map[uuid.UUID]struct{})
	for _, s := range r.statuses {
		if s.ProjectID == projectID {
			existing[s.ID] = struct{}{}
		}
	}
	if len(existing) != len(statusIDs) {
		return taskdom.ErrStatusReorderInvalid
	}
	for _, id := range statusIDs {
		if _, ok := existing[id]; !ok {
			return taskdom.ErrStatusReorderInvalid
		}
	}
	for i, id := range statusIDs {
		r.statuses[id].Position = i
	}
	return nil
}

// -- Task methods --

func (r *fakeTaskRepo) ListTasks(_ context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, limit int, _ taskdom.TaskSort) ([]*taskdom.Task, bool, error) {
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
		if filter.AssigneeID != nil && !slices.Contains(t.AssigneeIDs, *filter.AssigneeID) {
			continue
		}
		if !taskMatchesSearch(t, filter) {
			continue
		}
		cp := *t
		all = append(all, &cp)
	}
	// Stable sort matching the real ORDER BY created_at ASC, id ASC.
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID.String() < all[j].ID.String()
		}
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})
	// Apply cursor filter when provided.
	if filter.CursorAfter != nil {
		cur, err := taskdom.DecodeTaskCursor(*filter.CursorAfter)
		if err != nil {
			return nil, false, err
		}
		pos := -1
		for i, t := range all {
			if t.CreatedAt.UTC().Equal(cur.CreatedAt) && t.ID.String() == cur.ID {
				pos = i
				break
			}
		}
		if pos >= 0 {
			all = all[pos+1:]
		}
	}
	hasMore := len(all) > limit
	if hasMore {
		all = all[:limit]
	}
	return all, hasMore, nil
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

func (r *fakeTaskRepo) FindTaskByNumber(_ context.Context, projectID uuid.UUID, taskNumber int64) (*taskdom.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tasks {
		if t.ProjectID == projectID && t.TaskNumber == taskNumber && t.DeletedAt == nil {
			cp := *t
			return &cp, nil
		}
	}
	return nil, taskdom.ErrTaskNotFound
}

func (r *fakeTaskRepo) CreateTask(_ context.Context, t *taskdom.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[t.ProjectID]++
	t.TaskNumber = r.counters[t.ProjectID]
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

// CountTasks returns the total number of matching tasks without cursor/limit.
func (r *fakeTaskRepo) CountTasks(_ context.Context, projectID uuid.UUID, filter taskdom.TaskFilter) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var count int64
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
		if filter.AssigneeID != nil && !slices.Contains(t.AssigneeIDs, *filter.AssigneeID) {
			continue
		}
		if filter.BacklogOnly && t.SprintID != nil {
			continue
		}
		if !taskMatchesSearch(t, filter) {
			continue
		}
		count++
	}
	return count, nil
}

// SumTaskField sums a numeric field across matching tasks without cursor/limit.
func (r *fakeTaskRepo) SumTaskField(_ context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, fieldKey string) (float64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var sum float64
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
		if filter.AssigneeID != nil && !slices.Contains(t.AssigneeIDs, *filter.AssigneeID) {
			continue
		}
		if filter.BacklogOnly && t.SprintID != nil {
			continue
		}
		if !taskMatchesSearch(t, filter) {
			continue
		}
		if fieldKey == "story_points" {
			if t.StoryPoints != nil {
				sum += float64(*t.StoryPoints)
			}
		} else {
			if v, ok := t.CustomFields[fieldKey]; ok {
				switch n := v.(type) {
				case float64:
					sum += n
				case int:
					sum += float64(n)
				}
			}
		}
	}
	return sum, nil
}

// BulkMoveSprintTasks is not exercised by task service tests but must satisfy
// the taskdom.TaskRepository interface.
func (r *fakeTaskRepo) BulkMoveSprintTasks(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID) error {
	return nil
}

// ListAssignedTasks is not exercised by task service tests but must satisfy
// the taskdom.TaskRepository interface.
func (r *fakeTaskRepo) ListAssignedTasks(_ context.Context, _ []uuid.UUID, _ int, _ *string) ([]*taskdom.Task, bool, error) {
	return nil, false, nil
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

	updated, err := svc.UpdateTaskType(ctx, existing.ProjectID, existing.ID, taskdom.UpdateTaskTypeInput{
		Name: "Feature Request",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "Feature Request" {
		t.Errorf("expected Name=Feature Request, got %q", updated.Name)
	}
}

// TestUpdateTaskType_OmittedFieldsUnchanged guards against regressing to
// unconditionally overwriting Icon/Color/Description on every PATCH, even
// when the request omits them (nil outer pointer = absent, not "clear").
func TestUpdateTaskType_OmittedFieldsUnchanged(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	icon := "rocket"
	color := "#ff0000"
	desc := "A feature request"
	existing, err := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{
		ProjectID:   projectID,
		Name:        "Feature",
		Icon:        &icon,
		Color:       &color,
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Name is set; Icon/Color/Description are absent (nil outer pointer)
	// and must survive the update untouched.
	updated, err := svc.UpdateTaskType(ctx, existing.ProjectID, existing.ID, taskdom.UpdateTaskTypeInput{
		Name: "Feature Request",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "Feature Request" {
		t.Errorf("expected Name=Feature Request, got %q", updated.Name)
	}
	if updated.Icon == nil || *updated.Icon != icon {
		t.Errorf("expected Icon to remain %q, got %v", icon, updated.Icon)
	}
	if updated.Color == nil || *updated.Color != color {
		t.Errorf("expected Color to remain %q, got %v", color, updated.Color)
	}
	if updated.Description == nil || *updated.Description != desc {
		t.Errorf("expected Description to remain %q, got %v", desc, updated.Description)
	}
}

// TestUpdateTaskType_ExplicitNullClearsIcon verifies the other half of the
// three-state contract: an explicit null (non-nil outer pointer, nil inner
// pointer) clears the field rather than being ignored.
func TestUpdateTaskType_ExplicitNullClearsIcon(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	icon := "rocket"
	existing, err := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{
		ProjectID: projectID,
		Name:      "Feature",
		Icon:      &icon,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var clearedIcon *string
	updated, err := svc.UpdateTaskType(ctx, existing.ProjectID, existing.ID, taskdom.UpdateTaskTypeInput{
		Icon: &clearedIcon,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Icon != nil {
		t.Errorf("expected Icon to be cleared, got %q", *updated.Icon)
	}
}

func TestUpdateTaskType_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.UpdateTaskType(ctx, uuid.New(), uuid.New(), taskdom.UpdateTaskTypeInput{Name: "X"})
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
	if err := svc.DeleteTaskType(ctx, tt.ProjectID, tt.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.GetTaskType(ctx, tt.ID)
	if err != taskdom.ErrTypeNotFound {
		t.Errorf("expected ErrTypeNotFound after delete, got %v", err)
	}
}

func TestSetDefaultTaskType_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	tt1, _ := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{ProjectID: projectID, Name: "Task"})
	tt2, _ := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{ProjectID: projectID, Name: "Bug"})

	got, err := svc.SetDefaultTaskType(ctx, projectID, tt1.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsDefault {
		t.Errorf("expected returned type to have IsDefault=true")
	}

	// The other type should have IsDefault=false.
	other, _ := svc.GetTaskType(ctx, tt2.ID)
	if other.IsDefault {
		t.Errorf("expected %q to have IsDefault=false after SetDefaultTaskType", other.Name)
	}
}

func TestSetDefaultTaskType_SwitchDefault(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	tt1, _ := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{ProjectID: projectID, Name: "Task"})
	tt2, _ := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{ProjectID: projectID, Name: "Bug"})

	// Set tt1 as default first.
	_, _ = svc.SetDefaultTaskType(ctx, projectID, tt1.ID)

	// Then switch to tt2.
	got, err := svc.SetDefaultTaskType(ctx, projectID, tt2.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsDefault {
		t.Errorf("expected Bug to now be default")
	}

	// tt1 must no longer be default.
	prev, _ := svc.GetTaskType(ctx, tt1.ID)
	if prev.IsDefault {
		t.Errorf("expected Task to no longer be default after switching to Bug")
	}
}

func TestSetDefaultTaskType_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.SetDefaultTaskType(ctx, uuid.New(), uuid.New())
	if err != taskdom.ErrTypeNotFound {
		t.Errorf("expected ErrTypeNotFound, got %v", err)
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
	updated, err := svc.UpdateTaskStatus(ctx, st.ProjectID, st.ID, taskdom.UpdateTaskStatusInput{
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

	desc := json.RawMessage(`[{"id":"1","type":"paragraph","props":{"textColor":"default","backgroundColor":"default","textAlignment":"left"},"content":[{"type":"text","text":"Implement the feature","styles":{}}],"children":[]}]`)
	task, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Implement login",
		Description: desc,
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

func TestCreateTask_UsesDefaultTaskTypeAndStatus(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	tt, _ := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{ProjectID: projectID, Name: "Task"})
	st, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{
		ProjectID: projectID, Name: "Backlog", Category: taskdom.StatusCategoryTodo,
	})
	_, _ = svc.SetDefaultTaskType(ctx, projectID, tt.ID)
	_, _ = svc.SetDefaultTaskStatus(ctx, projectID, st.ID)

	task, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: projectID,
		Title:     "My Task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.TaskTypeID == nil || *task.TaskTypeID != tt.ID {
		t.Errorf("expected TaskTypeID=%v (default), got %v", tt.ID, task.TaskTypeID)
	}
	if task.StatusID == nil || *task.StatusID != st.ID {
		t.Errorf("expected StatusID=%v (default), got %v", st.ID, task.StatusID)
	}
}

func TestCreateTask_ExplicitIDsBypassDefaultLookup(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	defaultType, _ := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{ProjectID: projectID, Name: "Task"})
	defaultStatus, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{
		ProjectID: projectID, Name: "Backlog", Category: taskdom.StatusCategoryTodo,
	})
	_, _ = svc.SetDefaultTaskType(ctx, projectID, defaultType.ID)
	_, _ = svc.SetDefaultTaskStatus(ctx, projectID, defaultStatus.ID)

	explicitTypeID := uuid.New()
	explicitStatusID := uuid.New()
	task, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID:  projectID,
		Title:      "Explicit IDs Task",
		TaskTypeID: &explicitTypeID,
		StatusID:   &explicitStatusID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.TaskTypeID == nil || *task.TaskTypeID != explicitTypeID {
		t.Errorf("expected explicit TaskTypeID=%v, got %v", explicitTypeID, task.TaskTypeID)
	}
	if task.StatusID == nil || *task.StatusID != explicitStatusID {
		t.Errorf("expected explicit StatusID=%v, got %v", explicitStatusID, task.StatusID)
	}
}

func TestCreateTask_DefaultTaskTypeErrorPropagates(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	repo.findDefaultTypeErr = errors.New("db unavailable")

	_, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: uuid.New(),
		Title:     "Task",
	})
	if err == nil || err.Error() != "db unavailable" {
		t.Errorf("expected db unavailable error, got %v", err)
	}
}

func TestCreateTask_DefaultTaskStatusErrorPropagates(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	repo.findDefaultStatusErr = errors.New("db timeout")

	_, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: uuid.New(),
		Title:     "Task",
	})
	if err == nil || err.Error() != "db timeout" {
		t.Errorf("expected db timeout error, got %v", err)
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
	updated, err := svc.UpdateTask(ctx, task.ProjectID, task.ID, taskdom.UpdateTaskInput{
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
	updated, err := svc.UpdateTask(ctx, task.ProjectID, task.ID, taskdom.UpdateTaskInput{
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

	if err := svc.DeleteTask(ctx, task.ProjectID, task.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.GetTask(ctx, task.ProjectID, task.ID)
	if err != taskdom.ErrTaskNotFound {
		t.Errorf("expected ErrTaskNotFound after delete, got %v", err)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	err := svc.DeleteTask(ctx, uuid.New(), uuid.New())
	if err != taskdom.ErrTaskNotFound {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestCountTasks_Total(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	for i := 0; i < 7; i++ {
		_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Task"})
	}

	count, err := svc.CountTasks(ctx, projectID, taskdom.TaskFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 7 {
		t.Errorf("expected count=7, got %d", count)
	}
}

func TestCountTasks_FilterBySprint(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()
	sprintID := uuid.New()

	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Sprint Task", SprintID: &sprintID})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Sprint Task 2", SprintID: &sprintID})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Backlog Task"})

	count, err := svc.CountTasks(ctx, projectID, taskdom.TaskFilter{SprintID: &sprintID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
	}
}

func TestCountTasks_IgnoresCursor(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	for i := 0; i < 5; i++ {
		_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: fmt.Sprintf("Task %d", i)})
	}

	// Fetch page 1 to get a cursor.
	page1, _, _ := svc.ListTasks(ctx, projectID, taskdom.TaskFilter{}, 2, taskdom.TaskSort{})
	last := page1[len(page1)-1]
	cursor := taskdom.EncodeTaskCursor(last, taskdom.TaskSort{})

	// CountTasks with a cursor filter should still return the full total (cursor is stripped).
	count, err := svc.CountTasks(ctx, projectID, taskdom.TaskFilter{CursorAfter: &cursor})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected full count=5 (cursor ignored), got %d", count)
	}
}

func TestCountTasks_BacklogOnly(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()
	sprintID := uuid.New()

	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Backlog 1"})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Backlog 2"})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "In Sprint", SprintID: &sprintID})

	count, err := svc.CountTasks(ctx, projectID, taskdom.TaskFilter{BacklogOnly: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
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

	tasks, _, err := svc.ListTasks(ctx, projectID, taskdom.TaskFilter{SprintID: &sprintID}, 20, taskdom.TaskSort{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "In Sprint" {
		t.Errorf("expected 1 filtered task In Sprint, got %v", tasks)
	}
}

func TestListTasks_FilterBySearch_MatchesTitleCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Fix login bug"})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Add signup flow"})

	search := "LOGIN"
	tasks, _, err := svc.ListTasks(ctx, projectID, taskdom.TaskFilter{Search: &search}, 20, taskdom.TaskSort{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "Fix login bug" {
		t.Errorf("expected 1 filtered task 'Fix login bug', got %v", tasks)
	}
}

func TestListTasks_FilterBySearch_MatchesTaskNumber(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	first, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "First task"})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Second task"})

	search := fmt.Sprintf("#%d", first.TaskNumber)
	tasks, _, err := svc.ListTasks(ctx, projectID, taskdom.TaskFilter{Search: &search}, 20, taskdom.TaskSort{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != first.ID {
		t.Errorf("expected 1 filtered task matching %s, got %v", search, tasks)
	}
}

func TestListTasks_FilterBySearch_BlankIsNoOp(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Task one"})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Task two"})

	blank := "   "
	tasks, _, err := svc.ListTasks(ctx, projectID, taskdom.TaskFilter{Search: &blank}, 20, taskdom.TaskSort{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected blank search to be a no-op returning 2 tasks, got %d", len(tasks))
	}
}

func TestCountTasks_FilterBySearch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Fix login bug"})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Fix logout bug"})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Add signup flow"})

	search := "fix"
	count, err := svc.CountTasks(ctx, projectID, taskdom.TaskFilter{Search: &search})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
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

	tasks, hasMore, err := svc.ListTasks(ctx, projectID, taskdom.TaskFilter{}, 3, taskdom.TaskSort{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks in first page, got %d", len(tasks))
	}
	if !hasMore {
		t.Errorf("expected hasMore=true for page of 3 from 5")
	}
}

func TestListTasks_CursorPagination(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	for i := 0; i < 5; i++ {
		_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{
			ProjectID: projectID,
			Title:     fmt.Sprintf("Task %d", i),
		})
	}

	// Page 1: first 3 tasks
	page1, hasMore, err := svc.ListTasks(ctx, projectID, taskdom.TaskFilter{}, 3, taskdom.TaskSort{})
	if err != nil {
		t.Fatalf("page 1 error: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("expected 3 tasks on page 1, got %d", len(page1))
	}
	if !hasMore {
		t.Fatal("expected hasMore=true after page 1")
	}

	// Encode cursor from the last item on page 1.
	last := page1[len(page1)-1]
	cursor := taskdom.EncodeTaskCursor(last, taskdom.TaskSort{})

	// Page 2: tasks after the cursor.
	page2, hasMore2, err := svc.ListTasks(ctx, projectID, taskdom.TaskFilter{CursorAfter: &cursor}, 3, taskdom.TaskSort{})
	if err != nil {
		t.Fatalf("page 2 error: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("expected 2 tasks on page 2, got %d", len(page2))
	}
	if hasMore2 {
		t.Error("expected hasMore=false after page 2")
	}
	// Verify no overlap between pages.
	p1IDs := make(map[uuid.UUID]bool, len(page1))
	for _, task := range page1 {
		p1IDs[task.ID] = true
	}
	for _, task := range page2 {
		if p1IDs[task.ID] {
			t.Errorf("task %v appeared in both page 1 and page 2", task.ID)
		}
	}
}

func TestGetTask_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	created, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: projectID,
		Title:     "Fetch me",
	})

	got, err := svc.GetTask(ctx, created.ProjectID, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID=%v, got %v", created.ID, got.ID)
	}
	if got.Title != "Fetch me" {
		t.Errorf("expected Title=Fetch me, got %q", got.Title)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.GetTask(ctx, uuid.New(), uuid.New())
	if err != taskdom.ErrTaskNotFound {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestUpdateTask_AbsentFieldsPreserveValues(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()
	sprintID := uuid.New()
	desc := json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"original desc","styles":{}}],"children":[]}]`)

	task, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "My Task",
		SprintID:    &sprintID,
		Description: desc,
		Importance:  2,
		Tags:        []string{"alpha"},
	})

	// Only update Title — all other fields must remain unchanged.
	updated, err := svc.UpdateTask(ctx, task.ProjectID, task.ID, taskdom.UpdateTaskInput{
		Title: "My Updated Task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Title != "My Updated Task" {
		t.Errorf("expected updated title, got %q", updated.Title)
	}
	if updated.SprintID == nil || *updated.SprintID != sprintID {
		t.Errorf("expected SprintID=%v to be preserved, got %v", sprintID, updated.SprintID)
	}
	if string(updated.Description) != string(desc) {
		t.Errorf("expected Description to be preserved, got %v", updated.Description)
	}
	if updated.Importance != 2 {
		t.Errorf("expected Importance=2 to be preserved, got %d", updated.Importance)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "alpha" {
		t.Errorf("expected Tags to be preserved, got %v", updated.Tags)
	}
}

func TestUpdateTask_NullSprintIDClearsField(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()
	sprintID := uuid.New()

	task, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: projectID,
		Title:     "Sprint Task",
		SprintID:  &sprintID,
	})

	// Explicitly set sprint_id to nil (remove from sprint → backlog).
	nilPtr := (*uuid.UUID)(nil)
	updated, err := svc.UpdateTask(ctx, task.ProjectID, task.ID, taskdom.UpdateTaskInput{
		SprintID: &nilPtr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.SprintID != nil {
		t.Errorf("expected SprintID=nil after explicit clear, got %v", updated.SprintID)
	}
}

func TestUpdateTask_StatusChangePreservesSprintID(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()
	sprintID := uuid.New()
	statusID := uuid.New()

	task, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: projectID,
		Title:     "Sprint Task",
		SprintID:  &sprintID,
	})

	// Simulate a drag-to-change-status: only StatusID is supplied.
	statusIDPtr := &statusID
	updated, err := svc.UpdateTask(ctx, task.ProjectID, task.ID, taskdom.UpdateTaskInput{
		StatusID: &statusIDPtr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.StatusID == nil || *updated.StatusID != statusID {
		t.Errorf("expected StatusID=%v, got %v", statusID, updated.StatusID)
	}
	if updated.SprintID == nil || *updated.SprintID != sprintID {
		t.Errorf("expected SprintID=%v to be preserved, got %v", sprintID, updated.SprintID)
	}
}

func TestCreateTask_StoryPoints(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	sp := 5
	task, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Implement login",
		Importance:  3,
		StoryPoints: &sp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.StoryPoints == nil || *task.StoryPoints != 5 {
		t.Errorf("expected StoryPoints=5, got %v", task.StoryPoints)
	}
}

func TestCreateTask_StoryPoints_Nil(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	task, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: projectID,
		Title:     "No estimates task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.StoryPoints != nil {
		t.Errorf("expected StoryPoints=nil, got %v", task.StoryPoints)
	}
}

func TestUpdateTask_StoryPoints(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	task, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID: projectID,
		Title:     "Task",
	})

	sp := 8
	spPtr := &sp
	updated, err := svc.UpdateTask(ctx, task.ProjectID, task.ID, taskdom.UpdateTaskInput{
		StoryPoints: &spPtr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.StoryPoints == nil || *updated.StoryPoints != 8 {
		t.Errorf("expected StoryPoints=8, got %v", updated.StoryPoints)
	}
}

func TestUpdateTask_StoryPointsClearedToNil(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	sp := 3
	task, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task with SP",
		StoryPoints: &sp,
	})

	// Explicitly set story_points to null.
	nilPtr := (*int)(nil)
	updated, err := svc.UpdateTask(ctx, task.ProjectID, task.ID, taskdom.UpdateTaskInput{
		StoryPoints: &nilPtr,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.StoryPoints != nil {
		t.Errorf("expected StoryPoints=nil after clear, got %v", updated.StoryPoints)
	}
}

func TestUpdateTask_AbsentStoryPointsPreservesValue(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	sp := 13
	task, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{
		ProjectID:   projectID,
		Title:       "Task",
		StoryPoints: &sp,
	})

	// Update importance only — story_points must remain unchanged.
	newImportance := 2
	updated, err := svc.UpdateTask(ctx, task.ProjectID, task.ID, taskdom.UpdateTaskInput{
		Importance: &newImportance,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.StoryPoints == nil || *updated.StoryPoints != 13 {
		t.Errorf("expected StoryPoints=13 to be preserved, got %v", updated.StoryPoints)
	}
}

func TestUpdateTaskStatus_NameUpdate(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	st, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{
		ProjectID: projectID,
		Name:      "To Do",
		Position:  0,
		Category:  taskdom.StatusCategoryTodo,
	})

	updated, err := svc.UpdateTaskStatus(ctx, st.ProjectID, st.ID, taskdom.UpdateTaskStatusInput{
		Name: "Backlog",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "Backlog" {
		t.Errorf("expected Name=Backlog, got %q", updated.Name)
	}
}

func TestUpdateTaskStatus_CategoryUpdate(t *testing.T) {
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

	newCat := taskdom.StatusCategoryInProgress
	updated, err := svc.UpdateTaskStatus(ctx, st.ProjectID, st.ID, taskdom.UpdateTaskStatusInput{
		Category: &newCat,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Category != taskdom.StatusCategoryInProgress {
		t.Errorf("expected category inprogress, got %q", updated.Category)
	}
}

func TestUpdateTaskStatus_InvalidCategoryReturnsError(t *testing.T) {
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

	badCat := taskdom.StatusCategory("not-real")
	_, err := svc.UpdateTaskStatus(ctx, st.ProjectID, st.ID, taskdom.UpdateTaskStatusInput{
		Category: &badCat,
	})
	if err != taskdom.ErrStatusCategoryInvalid {
		t.Errorf("expected ErrStatusCategoryInvalid, got %v", err)
	}
}

func TestUpdateTaskStatus_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	newPos := 1
	_, err := svc.UpdateTaskStatus(ctx, uuid.New(), uuid.New(), taskdom.UpdateTaskStatusInput{Position: &newPos})
	if err != taskdom.ErrStatusNotFound {
		t.Errorf("expected ErrStatusNotFound, got %v", err)
	}
}

func TestDeleteTaskStatus_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	st, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{
		ProjectID: projectID,
		Name:      "Done",
		Position:  10,
		Category:  taskdom.StatusCategoryDone,
	})

	if err := svc.DeleteTaskStatus(ctx, st.ProjectID, st.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.GetTaskStatus(ctx, st.ID)
	if err != taskdom.ErrStatusNotFound {
		t.Errorf("expected ErrStatusNotFound after delete, got %v", err)
	}
}

func TestDeleteTaskStatus_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	err := svc.DeleteTaskStatus(ctx, uuid.New(), uuid.New())
	if err != taskdom.ErrStatusNotFound {
		t.Errorf("expected ErrStatusNotFound, got %v", err)
	}
}

// stubWorkflowStatusChecker lets tests control whether a status looks
// referenced by a workflow without depending on the workflow package.
type stubWorkflowStatusChecker struct {
	used bool
	err  error
}

func (s *stubWorkflowStatusChecker) StatusUsedByWorkflow(_ context.Context, _ uuid.UUID) (bool, error) {
	return s.used, s.err
}

func TestDeleteTaskStatus_InUseByWorkflow_Blocked(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo).WithWorkflowStatusChecker(&stubWorkflowStatusChecker{used: true})
	projectID := uuid.New()

	st, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{
		ProjectID: projectID,
		Name:      "Done",
		Position:  10,
		Category:  taskdom.StatusCategoryDone,
	})

	err := svc.DeleteTaskStatus(ctx, st.ProjectID, st.ID)
	if err != taskdom.ErrStatusInUseByWorkflow {
		t.Fatalf("expected ErrStatusInUseByWorkflow, got %v", err)
	}

	if _, getErr := svc.GetTaskStatus(ctx, st.ID); getErr != nil {
		t.Errorf("status should not have been deleted, got GetTaskStatus error: %v", getErr)
	}
}

func TestDeleteTaskStatus_NotUsedByWorkflow_Allowed(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo).WithWorkflowStatusChecker(&stubWorkflowStatusChecker{used: false})
	projectID := uuid.New()

	st, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{
		ProjectID: projectID,
		Name:      "Done",
		Position:  10,
		Category:  taskdom.StatusCategoryDone,
	})

	if err := svc.DeleteTaskStatus(ctx, st.ProjectID, st.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.GetTaskStatus(ctx, st.ID); err != taskdom.ErrStatusNotFound {
		t.Errorf("expected ErrStatusNotFound after delete, got %v", err)
	}
}

func TestListTaskStatuses_MultiProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	p1 := uuid.New()
	p2 := uuid.New()

	_, _ = svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: p1, Name: "A", Category: taskdom.StatusCategoryTodo})
	_, _ = svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: p1, Name: "B", Category: taskdom.StatusCategoryDone})
	_, _ = svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: p2, Name: "C", Category: taskdom.StatusCategoryTodo})

	p1Statuses, err := svc.ListTaskStatuses(ctx, p1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p1Statuses) != 2 {
		t.Errorf("expected 2 statuses for p1, got %d", len(p1Statuses))
	}

	p2Statuses, _ := svc.ListTaskStatuses(ctx, p2)
	if len(p2Statuses) != 1 {
		t.Errorf("expected 1 status for p2, got %d", len(p2Statuses))
	}
}

func TestSetDefaultTaskStatus_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	st1, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: projectID, Name: "Backlog", Category: taskdom.StatusCategoryTodo})
	st2, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: projectID, Name: "Done", Category: taskdom.StatusCategoryDone})

	got, err := svc.SetDefaultTaskStatus(ctx, projectID, st1.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsDefault {
		t.Errorf("expected returned status to have IsDefault=true")
	}

	other, _ := svc.GetTaskStatus(ctx, st2.ID)
	if other.IsDefault {
		t.Errorf("expected %q to have IsDefault=false after SetDefaultTaskStatus", other.Name)
	}
}

func TestSetDefaultTaskStatus_SwitchDefault(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	st1, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: projectID, Name: "Backlog", Category: taskdom.StatusCategoryTodo})
	st2, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: projectID, Name: "Done", Category: taskdom.StatusCategoryDone})

	_, _ = svc.SetDefaultTaskStatus(ctx, projectID, st1.ID)

	got, err := svc.SetDefaultTaskStatus(ctx, projectID, st2.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsDefault {
		t.Errorf("expected Done to now be default")
	}

	prev, _ := svc.GetTaskStatus(ctx, st1.ID)
	if prev.IsDefault {
		t.Errorf("expected Backlog to no longer be default after switching to Done")
	}
}

func TestSetDefaultTaskStatus_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.SetDefaultTaskStatus(ctx, uuid.New(), uuid.New())
	if err != taskdom.ErrStatusNotFound {
		t.Errorf("expected ErrStatusNotFound, got %v", err)
	}
}

func TestSetDefaultTaskStatus_RepoErrorPropagates(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	repo.setDefaultStatusErr = errors.New("db unavailable")

	_, err := svc.SetDefaultTaskStatus(ctx, uuid.New(), uuid.New())
	if err == nil || err.Error() != "db unavailable" {
		t.Errorf("expected db unavailable error, got %v", err)
	}
}

func TestReorderTaskStatuses_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	st1, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: projectID, Name: "A", Category: taskdom.StatusCategoryTodo, Position: 0})
	st2, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: projectID, Name: "B", Category: taskdom.StatusCategoryDone, Position: 1})
	st3, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: projectID, Name: "C", Category: taskdom.StatusCategoryTodo, Position: 2})

	if err := svc.ReorderTaskStatuses(ctx, projectID, []uuid.UUID{st3.ID, st1.ID, st2.ID}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got1, _ := svc.GetTaskStatus(ctx, st3.ID)
	got2, _ := svc.GetTaskStatus(ctx, st1.ID)
	got3, _ := svc.GetTaskStatus(ctx, st2.ID)
	if got1.Position != 0 || got2.Position != 1 || got3.Position != 2 {
		t.Errorf("expected positions 0,1,2 for C,A,B, got %d,%d,%d", got1.Position, got2.Position, got3.Position)
	}
}

func TestReorderTaskStatuses_InvalidSet(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	st1, _ := svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: projectID, Name: "A", Category: taskdom.StatusCategoryTodo})
	_, _ = svc.CreateTaskStatus(ctx, taskdom.CreateTaskStatusInput{ProjectID: projectID, Name: "B", Category: taskdom.StatusCategoryDone})

	// Missing st2 from the submitted set.
	if err := svc.ReorderTaskStatuses(ctx, projectID, []uuid.UUID{st1.ID}); err != taskdom.ErrStatusReorderInvalid {
		t.Errorf("expected ErrStatusReorderInvalid for incomplete set, got %v", err)
	}

	// Unknown ID included.
	if err := svc.ReorderTaskStatuses(ctx, projectID, []uuid.UUID{st1.ID, uuid.New()}); err != taskdom.ErrStatusReorderInvalid {
		t.Errorf("expected ErrStatusReorderInvalid for unknown ID, got %v", err)
	}
}

func TestReorderTaskStatuses_Empty(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	if err := svc.ReorderTaskStatuses(ctx, uuid.New(), nil); err != taskdom.ErrStatusReorderInvalid {
		t.Errorf("expected ErrStatusReorderInvalid for empty input, got %v", err)
	}
}

func (r *fakeTaskRepo) ListCustomFieldDefinitions(_ context.Context, projectID uuid.UUID) ([]*taskdom.CustomFieldDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*taskdom.CustomFieldDefinition, 0)
	for _, f := range r.customFields {
		if f.ProjectID == projectID {
			cp := *f
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeTaskRepo) FindCustomFieldDefinitionByID(_ context.Context, id uuid.UUID) (*taskdom.CustomFieldDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.customFields[id]
	if !ok {
		return nil, taskdom.ErrCustomFieldNotFound
	}
	cp := *f
	return &cp, nil
}

func (r *fakeTaskRepo) CreateCustomFieldDefinition(_ context.Context, f *taskdom.CustomFieldDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.customFields {
		if existing.ProjectID == f.ProjectID && existing.FieldKey == f.FieldKey {
			return taskdom.ErrCustomFieldKeyTaken
		}
	}
	cp := *f
	r.customFields[f.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) UpdateCustomFieldDefinition(_ context.Context, f *taskdom.CustomFieldDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.customFields[f.ID]; !ok {
		return taskdom.ErrCustomFieldNotFound
	}
	cp := *f
	r.customFields[f.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) DeleteCustomFieldDefinition(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.customFields, id)
	return nil
}

// --- TaskLinkRepository stubs -----------------------------------------------

func (r *fakeTaskRepo) ListTaskLinks(_ context.Context, _ uuid.UUID) ([]*taskdom.TaskLink, error) {
	return []*taskdom.TaskLink{}, nil
}

func (r *fakeTaskRepo) FindTaskLinkByID(_ context.Context, _ uuid.UUID) (*taskdom.TaskLink, error) {
	return nil, taskdom.ErrTaskLinkNotFound
}

func (r *fakeTaskRepo) CreateTaskLinkIfNotExists(_ context.Context, _ *taskdom.TaskLink) (bool, error) {
	return true, nil
}

func (r *fakeTaskRepo) DeleteTaskLink(_ context.Context, _ uuid.UUID) error { return nil }

// ---------------------------------------------------------------------------
// Task Number tests
// ---------------------------------------------------------------------------

func TestCreateTask_TaskNumberIncrementsPerProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	t1, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "First"})
	if err != nil {
		t.Fatalf("unexpected error creating first task: %v", err)
	}
	if t1.TaskNumber != 1 {
		t.Errorf("expected first task TaskNumber=1, got %d", t1.TaskNumber)
	}

	t2, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Second"})
	if err != nil {
		t.Fatalf("unexpected error creating second task: %v", err)
	}
	if t2.TaskNumber != 2 {
		t.Errorf("expected second task TaskNumber=2, got %d", t2.TaskNumber)
	}

	t3, err := svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Third"})
	if err != nil {
		t.Fatalf("unexpected error creating third task: %v", err)
	}
	if t3.TaskNumber != 3 {
		t.Errorf("expected third task TaskNumber=3, got %d", t3.TaskNumber)
	}
}

func TestCreateTask_TaskNumberScopedToProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projA := uuid.New()
	projB := uuid.New()

	a1, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projA, Title: "A-1"})
	a2, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projA, Title: "A-2"})
	b1, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projB, Title: "B-1"})

	if a1.TaskNumber != 1 {
		t.Errorf("projA first task: expected TaskNumber=1, got %d", a1.TaskNumber)
	}
	if a2.TaskNumber != 2 {
		t.Errorf("projA second task: expected TaskNumber=2, got %d", a2.TaskNumber)
	}
	if b1.TaskNumber != 1 {
		t.Errorf("projB first task: expected TaskNumber=1 (independent counter), got %d", b1.TaskNumber)
	}
}

func TestGetTaskByNumber_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	created, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Lookup me"})

	got, err := svc.GetTaskByNumber(ctx, projectID, created.TaskNumber)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID=%v, got %v", created.ID, got.ID)
	}
	if got.TaskNumber != created.TaskNumber {
		t.Errorf("expected TaskNumber=%d, got %d", created.TaskNumber, got.TaskNumber)
	}
}

func TestGetTaskByNumber_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.GetTaskByNumber(ctx, uuid.New(), 999)
	if err != taskdom.ErrTaskNotFound {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestGetTaskByNumber_DeletedTask(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	created, _ := svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Gone"})
	_ = svc.DeleteTask(ctx, created.ProjectID, created.ID)

	_, err := svc.GetTaskByNumber(ctx, projectID, created.TaskNumber)
	if err != taskdom.ErrTaskNotFound {
		t.Errorf("expected ErrTaskNotFound for deleted task, got %v", err)
	}
}

func TestGetTaskByNumber_CrossProjectIsolation(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projA := uuid.New()
	projB := uuid.New()

	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projA, Title: "A-1"}) // task_number=1 in projA

	// Looking up task_number=1 from projB should fail — different project.
	_, err := svc.GetTaskByNumber(ctx, projB, 1)
	if err != taskdom.ErrTaskNotFound {
		t.Errorf("expected ErrTaskNotFound across projects, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Custom Field Definition tests
// ---------------------------------------------------------------------------

func TestCreateCustomFieldDefinition_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	got, err := svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID:   projectID,
		FieldKey:    "priority",
		DisplayName: "Priority",
		FieldType:   taskdom.FieldTypeSelect,
		Options:     []string{"low", "medium", "high"},
		IsRequired:  true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.FieldKey != "priority" {
		t.Errorf("expected FieldKey=priority, got %q", got.FieldKey)
	}
	if got.FieldType != taskdom.FieldTypeSelect {
		t.Errorf("expected FieldType=select, got %q", got.FieldType)
	}
	if len(got.Options) != 3 {
		t.Errorf("expected 3 options, got %d", len(got.Options))
	}
	if !got.IsRequired {
		t.Error("expected IsRequired=true")
	}
}

func TestCreateCustomFieldDefinition_EmptyKey(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID:   uuid.New(),
		FieldKey:    "   ",
		DisplayName: "Priority",
		FieldType:   taskdom.FieldTypeText,
	})
	if err != taskdom.ErrCustomFieldKeyInvalid {
		t.Errorf("expected ErrCustomFieldKeyInvalid, got %v", err)
	}
}

func TestCreateCustomFieldDefinition_EmptyDisplayName(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID:   uuid.New(),
		FieldKey:    "cost",
		DisplayName: "",
		FieldType:   taskdom.FieldTypeNumber,
	})
	if err != taskdom.ErrCustomFieldNameInvalid {
		t.Errorf("expected ErrCustomFieldNameInvalid, got %v", err)
	}
}

func TestCreateCustomFieldDefinition_InvalidType(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID:   uuid.New(),
		FieldKey:    "x",
		DisplayName: "X",
		FieldType:   "not_valid",
	})
	if err != taskdom.ErrCustomFieldTypeInvalid {
		t.Errorf("expected ErrCustomFieldTypeInvalid, got %v", err)
	}
}

func TestCreateCustomFieldDefinition_DuplicateKey(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	_, err := svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID:   projectID,
		FieldKey:    "story_points",
		DisplayName: "Story Points",
		FieldType:   taskdom.FieldTypeNumber,
	})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	_, err = svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID:   projectID,
		FieldKey:    "story_points",
		DisplayName: "Story Points Again",
		FieldType:   taskdom.FieldTypeNumber,
	})
	if err != taskdom.ErrCustomFieldKeyTaken {
		t.Errorf("expected ErrCustomFieldKeyTaken, got %v", err)
	}
}

func TestUpdateCustomFieldDefinition_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	f, _ := svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID:   projectID,
		FieldKey:    "status_reason",
		DisplayName: "Status Reason",
		FieldType:   taskdom.FieldTypeText,
	})

	newType := taskdom.FieldTypeSelect
	required := true
	updated, err := svc.UpdateCustomFieldDefinition(ctx, f.ProjectID, f.ID, taskdom.UpdateCustomFieldDefinitionInput{
		DisplayName: "Reason",
		FieldType:   &newType,
		Options:     []string{"blocked", "waiting"},
		IsRequired:  &required,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.DisplayName != "Reason" {
		t.Errorf("expected DisplayName=Reason, got %q", updated.DisplayName)
	}
	if updated.FieldType != taskdom.FieldTypeSelect {
		t.Errorf("expected FieldType=select, got %q", updated.FieldType)
	}
	if len(updated.Options) != 2 {
		t.Errorf("expected 2 options, got %d", len(updated.Options))
	}
	if !updated.IsRequired {
		t.Error("expected IsRequired=true")
	}
}

func TestUpdateCustomFieldDefinition_InvalidType(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	f, _ := svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID:   projectID,
		FieldKey:    "notes",
		DisplayName: "Notes",
		FieldType:   taskdom.FieldTypeText,
	})

	bad := taskdom.FieldType("bad_type")
	_, err := svc.UpdateCustomFieldDefinition(ctx, f.ProjectID, f.ID, taskdom.UpdateCustomFieldDefinitionInput{
		FieldType: &bad,
	})
	if err != taskdom.ErrCustomFieldTypeInvalid {
		t.Errorf("expected ErrCustomFieldTypeInvalid, got %v", err)
	}
}

func TestUpdateCustomFieldDefinition_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	_, err := svc.UpdateCustomFieldDefinition(ctx, uuid.New(), uuid.New(), taskdom.UpdateCustomFieldDefinitionInput{
		DisplayName: "X",
	})
	if err != taskdom.ErrCustomFieldNotFound {
		t.Errorf("expected ErrCustomFieldNotFound, got %v", err)
	}
}

func TestDeleteCustomFieldDefinition_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	f, _ := svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID:   projectID,
		FieldKey:    "to_delete",
		DisplayName: "To Delete",
		FieldType:   taskdom.FieldTypeBoolean,
	})

	if err := svc.DeleteCustomFieldDefinition(ctx, f.ProjectID, f.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err := svc.GetCustomFieldDefinition(ctx, f.ProjectID, f.ID)
	if err != taskdom.ErrCustomFieldNotFound {
		t.Errorf("expected ErrCustomFieldNotFound after delete, got %v", err)
	}
}

func TestDeleteCustomFieldDefinition_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)

	err := svc.DeleteCustomFieldDefinition(ctx, uuid.New(), uuid.New())
	if err != taskdom.ErrCustomFieldNotFound {
		t.Errorf("expected ErrCustomFieldNotFound, got %v", err)
	}
}

func TestSumTaskField_StoryPoints(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	sp1, sp2, sp3 := 3, 5, 10
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "A", StoryPoints: &sp1})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "B", StoryPoints: &sp2})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "C", StoryPoints: &sp3})

	sum, err := svc.SumTaskField(ctx, projectID, taskdom.TaskFilter{}, "story_points")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum != 18 {
		t.Errorf("expected sum=18, got %v", sum)
	}
}

func TestSumTaskField_FilterBySprint(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()
	sprintID := uuid.New()

	sp1, sp2 := 5, 8
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Sprint", SprintID: &sprintID, StoryPoints: &sp1})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "Backlog", StoryPoints: &sp2})

	sum, err := svc.SumTaskField(ctx, projectID, taskdom.TaskFilter{SprintID: &sprintID}, "story_points")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum != 5 {
		t.Errorf("expected sum=5 (sprint only), got %v", sum)
	}
}

func TestSumTaskField_NilStoryPoints(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projectID := uuid.New()

	sp := 4
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "With points", StoryPoints: &sp})
	_, _ = svc.CreateTask(ctx, taskdom.CreateTaskInput{ProjectID: projectID, Title: "No points"})

	sum, err := svc.SumTaskField(ctx, projectID, taskdom.TaskFilter{}, "story_points")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum != 4 {
		t.Errorf("expected sum=4 (nil treated as 0), got %v", sum)
	}
}

func TestListCustomFieldDefinitions_MultiProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeTaskRepo()
	svc := tasksvc.New(repo)
	projA := uuid.New()
	projB := uuid.New()

	_, _ = svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID: projA, FieldKey: "field_a", DisplayName: "Field A", FieldType: taskdom.FieldTypeText,
	})
	_, _ = svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID: projA, FieldKey: "field_b", DisplayName: "Field B", FieldType: taskdom.FieldTypeNumber,
	})
	_, _ = svc.CreateCustomFieldDefinition(ctx, taskdom.CreateCustomFieldDefinitionInput{
		ProjectID: projB, FieldKey: "other", DisplayName: "Other", FieldType: taskdom.FieldTypeDate,
	})

	fields, err := svc.ListCustomFieldDefinitions(ctx, projA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fields) != 2 {
		t.Errorf("expected 2 fields for projA, got %d", len(fields))
	}
}
