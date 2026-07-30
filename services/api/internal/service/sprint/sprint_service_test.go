// Package sprintsvc_test contains unit tests for the sprint service layer.
package sprintsvc_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
)

// ---------------------------------------------------------------------------
// Fake repository
// ---------------------------------------------------------------------------

type fakeSprintRepo struct {
	mu      sync.RWMutex
	sprints map[uuid.UUID]*sprintdom.Sprint
}

func newFakeSprintRepo() *fakeSprintRepo {
	return &fakeSprintRepo{sprints: make(map[uuid.UUID]*sprintdom.Sprint)}
}

func (r *fakeSprintRepo) ListSprints(_ context.Context, projectID uuid.UUID) ([]*sprintdom.Sprint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*sprintdom.Sprint, 0)
	for _, s := range r.sprints {
		if s.ProjectID == projectID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeSprintRepo) FindSprintByID(_ context.Context, id uuid.UUID) (*sprintdom.Sprint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sprints[id]
	if !ok {
		return nil, sprintdom.ErrSprintNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *fakeSprintRepo) CreateSprint(_ context.Context, s *sprintdom.Sprint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *s
	r.sprints[s.ID] = &cp
	return nil
}

func (r *fakeSprintRepo) UpdateSprint(_ context.Context, s *sprintdom.Sprint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sprints[s.ID]; !ok {
		return sprintdom.ErrSprintNotFound
	}
	cp := *s
	r.sprints[s.ID] = &cp
	return nil
}

func (r *fakeSprintRepo) DeleteSprint(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sprints, id)
	return nil
}

// ---------------------------------------------------------------------------
// Fake task repository (subset needed by sprint service)
// ---------------------------------------------------------------------------

type fakeTaskRepo struct {
	mu       sync.RWMutex
	tasks    map[uuid.UUID]*taskdom.Task
	statuses map[uuid.UUID]*taskdom.TaskStatus
}

func newFakeTaskRepo() *fakeTaskRepo {
	return &fakeTaskRepo{
		tasks:    make(map[uuid.UUID]*taskdom.Task),
		statuses: make(map[uuid.UUID]*taskdom.TaskStatus),
	}
}

func (r *fakeTaskRepo) ListTasks(_ context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, _ int, _ taskdom.TaskSort) ([]*taskdom.Task, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*taskdom.Task
	for _, t := range r.tasks {
		if t.ProjectID == projectID && t.DeletedAt == nil {
			cp := *t
			out = append(out, &cp)
		}
	}
	_ = filter
	return out, false, nil
}
func (r *fakeTaskRepo) FindTaskByID(_ context.Context, id uuid.UUID) (*taskdom.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[id]
	if !ok {
		return nil, taskdom.ErrTaskNotFound
	}
	cp := *t
	return &cp, nil
}
func (r *fakeTaskRepo) FindTaskByNumber(_ context.Context, _ uuid.UUID, _ int64) (*taskdom.Task, error) {
	return nil, taskdom.ErrTaskNotFound
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
	cp := *t
	r.tasks[t.ID] = &cp
	return nil
}
func (r *fakeTaskRepo) DeleteTask(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tasks, id)
	return nil
}

// CreateStatus is a test helper to seed a TaskStatus into the fake.
func (r *fakeTaskRepo) CreateStatus(_ context.Context, s *taskdom.TaskStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *s
	r.statuses[s.ID] = &cp
	return nil
}

// BulkMoveSprintTasks moves non-done tasks from sourceSprintID to targetSprintID.
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
		if filter.BacklogOnly && t.SprintID != nil {
			continue
		}
		count++
	}
	return count, nil
}

func (r *fakeTaskRepo) SumTaskField(_ context.Context, _ uuid.UUID, _ taskdom.TaskFilter, _ string) (float64, error) {
	return 0, nil
}

func (r *fakeTaskRepo) ListTaskLinks(_ context.Context, _ uuid.UUID) ([]*taskdom.TaskLink, error) {
	return []*taskdom.TaskLink{}, nil
}

func (r *fakeTaskRepo) FindTaskLinkByID(_ context.Context, _ uuid.UUID) (*taskdom.TaskLink, error) {
	return nil, taskdom.ErrTaskLinkNotFound
}

func (r *fakeTaskRepo) CreateTaskLinkIfNotExists(_ context.Context, _ *taskdom.TaskLink) (bool, error) {
	return true, nil
}

func (r *fakeTaskRepo) DeleteTaskLink(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (r *fakeTaskRepo) ListAssignedTasks(_ context.Context, _ []uuid.UUID, _ int, _ *string) ([]*taskdom.Task, bool, error) {
	return nil, false, nil
}

func (r *fakeTaskRepo) BulkMoveSprintTasks(_ context.Context, projectID, sourceSprintID uuid.UUID, targetSprintID *uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.tasks {
		if t.ProjectID != projectID || t.DeletedAt != nil {
			continue
		}
		if t.SprintID == nil || *t.SprintID != sourceSprintID {
			continue
		}
		// Skip done-category tasks.
		if t.StatusID != nil {
			if s, ok := r.statuses[*t.StatusID]; ok && s.Category == taskdom.StatusCategoryDone {
				continue
			}
		}
		t.SprintID = targetSprintID
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCreateSprint_OK(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)
	projectID := uuid.New()

	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	goal := "Ship v1"

	sp, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint 1",
		StartDate: &start,
		EndDate:   &end,
		Goal:      &goal,
		Status:    sprintdom.SprintStatusPlanned,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sp.Name != "Sprint 1" {
		t.Errorf("expected Name=Sprint 1, got %q", sp.Name)
	}
	if sp.Status != sprintdom.SprintStatusPlanned {
		t.Errorf("expected status planned, got %q", sp.Status)
	}
}

func TestCreateSprint_DefaultStatus(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)

	sp, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: uuid.New(),
		Name:      "Sprint X",
		// Status omitted — should default to planned
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sp.Status != sprintdom.SprintStatusPlanned {
		t.Errorf("expected default status planned, got %q", sp.Status)
	}
}

func TestCreateSprint_EmptyName(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)

	_, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: uuid.New(),
		Name:      "   ",
	})
	if err != sprintdom.ErrSprintNameInvalid {
		t.Errorf("expected ErrSprintNameInvalid, got %v", err)
	}
}

func TestCreateSprint_InvalidStatus(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)

	_, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: uuid.New(),
		Name:      "Bad Sprint",
		Status:    "unknown",
	})
	if err != sprintdom.ErrSprintStatusInvalid {
		t.Errorf("expected ErrSprintStatusInvalid, got %v", err)
	}
}

func TestUpdateSprint_ActivateSprint(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)
	projectID := uuid.New()

	sp, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint 2",
		Status:    sprintdom.SprintStatusPlanned,
	})

	active := sprintdom.SprintStatusActive
	updated, err := svc.UpdateSprint(ctx, sp.ProjectID, sp.ID, sprintdom.UpdateSprintInput{
		Name:   "Sprint 2",
		Status: &active,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != sprintdom.SprintStatusActive {
		t.Errorf("expected status active, got %q", updated.Status)
	}
}

func TestUpdateSprint_InvalidStatus(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)

	sp, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: uuid.New(),
		Name:      "Sprint 3",
	})

	bad := sprintdom.SprintStatus("flying")
	_, err := svc.UpdateSprint(ctx, sp.ProjectID, sp.ID, sprintdom.UpdateSprintInput{
		Status: &bad,
	})
	if err != sprintdom.ErrSprintStatusInvalid {
		t.Errorf("expected ErrSprintStatusInvalid, got %v", err)
	}
}

// TestUpdateSprint_OmittedFieldsUnchanged guards against regressing to
// unconditionally overwriting StartDate/EndDate/Goal on every PATCH, even
// when the request omits them (nil outer pointer = absent, not "clear").
func TestUpdateSprint_OmittedFieldsUnchanged(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)
	projectID := uuid.New()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	goal := "Ship the thing"
	sp, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint 1",
		StartDate: &start,
		EndDate:   &end,
		Goal:      &goal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Name is set; StartDate/EndDate/Goal are absent (nil outer pointer)
	// and must survive the update untouched.
	updated, err := svc.UpdateSprint(ctx, projectID, sp.ID, sprintdom.UpdateSprintInput{
		Name: "Renamed Sprint",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "Renamed Sprint" {
		t.Errorf("expected Name to update, got %q", updated.Name)
	}
	if updated.StartDate == nil || !updated.StartDate.Equal(start) {
		t.Errorf("expected StartDate to remain %v, got %v", start, updated.StartDate)
	}
	if updated.EndDate == nil || !updated.EndDate.Equal(end) {
		t.Errorf("expected EndDate to remain %v, got %v", end, updated.EndDate)
	}
	if updated.Goal == nil || *updated.Goal != goal {
		t.Errorf("expected Goal to remain %q, got %v", goal, updated.Goal)
	}
}

// TestUpdateSprint_ExplicitNullClearsGoal verifies the other half of the
// three-state contract: an explicit null (non-nil outer pointer, nil inner
// pointer) clears the field rather than being ignored.
func TestUpdateSprint_ExplicitNullClearsGoal(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)
	projectID := uuid.New()

	goal := "Ship the thing"
	sp, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint 1",
		Goal:      &goal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var clearedGoal *string
	updated, err := svc.UpdateSprint(ctx, projectID, sp.ID, sprintdom.UpdateSprintInput{
		Goal: &clearedGoal,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Goal != nil {
		t.Errorf("expected Goal to be cleared, got %q", *updated.Goal)
	}
}

func TestDeleteSprint_OK(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)

	sp, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: uuid.New(),
		Name:      "To Delete",
	})
	if err := svc.DeleteSprint(ctx, sp.ProjectID, sp.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err := svc.GetSprint(ctx, sp.ProjectID, sp.ID)
	if err != sprintdom.ErrSprintNotFound {
		t.Errorf("expected ErrSprintNotFound after delete, got %v", err)
	}
}

func TestDeleteSprint_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)

	err := svc.DeleteSprint(ctx, uuid.New(), uuid.New())
	if err != sprintdom.ErrSprintNotFound {
		t.Errorf("expected ErrSprintNotFound, got %v", err)
	}
}

func TestGetSprint_OK(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)
	projectID := uuid.New()

	sp, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint Alpha",
		Status:    sprintdom.SprintStatusPlanned,
	})
	if err != nil {
		t.Fatalf("unexpected error creating sprint: %v", err)
	}

	got, err := svc.GetSprint(ctx, sp.ProjectID, sp.ID)
	if err != nil {
		t.Fatalf("unexpected error getting sprint: %v", err)
	}
	if got.ID != sp.ID {
		t.Errorf("expected ID %v, got %v", sp.ID, got.ID)
	}
	if got.Name != "Sprint Alpha" {
		t.Errorf("expected Name=Sprint Alpha, got %q", got.Name)
	}
}

func TestGetSprint_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)

	_, err := svc.GetSprint(ctx, uuid.New(), uuid.New())
	if err != sprintdom.ErrSprintNotFound {
		t.Errorf("expected ErrSprintNotFound, got %v", err)
	}
}

func TestListSprints_ReturnsProjectSprints(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)
	projectID := uuid.New()
	otherProjectID := uuid.New()

	// Create sprints in the target project
	for i := range 3 {
		_, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
			ProjectID: projectID,
			Name:      fmt.Sprintf("Sprint %d", i+1),
		})
		if err != nil {
			t.Fatalf("create sprint: %v", err)
		}
	}
	// Create a sprint in another project — should not appear
	_, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: otherProjectID,
		Name:      "Other Sprint",
	})
	if err != nil {
		t.Fatalf("create other sprint: %v", err)
	}

	sprints, err := svc.ListSprints(ctx, projectID)
	if err != nil {
		t.Fatalf("list sprints: %v", err)
	}
	if len(sprints) != 3 {
		t.Errorf("expected 3 sprints for project, got %d", len(sprints))
	}
}

// ---------------------------------------------------------------------------
// CompleteSprint tests
// ---------------------------------------------------------------------------

func TestCompleteSprint_MovesToBacklog(t *testing.T) {
	ctx := context.Background()
	taskRepo := newFakeTaskRepo()
	svc := sprintsvc.New(newFakeSprintRepo(), taskRepo, nil)
	projectID := uuid.New()

	sp, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint Active",
		Status:    sprintdom.SprintStatusActive,
	})

	// Seed two tasks in the sprint.
	for range 2 {
		id := uuid.New()
		_ = taskRepo.CreateTask(ctx, &taskdom.Task{
			ID:        id,
			ProjectID: projectID,
			SprintID:  &sp.ID,
			Title:     "task",
		})
	}

	completed, err := svc.CompleteSprint(ctx, sp.ProjectID, sp.ID, sprintdom.CompleteSprintInput{
		MoveToSprintID: nil, // move to backlog
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if completed.Status != sprintdom.SprintStatusCompleted {
		t.Errorf("expected status completed, got %q", completed.Status)
	}

	// All tasks should now have no sprint (backlog).
	tasks, _, _ := taskRepo.ListTasks(ctx, projectID, taskdom.TaskFilter{}, 100, taskdom.TaskSort{})
	for _, task := range tasks {
		if task.SprintID != nil {
			t.Errorf("expected task %s to be in backlog, still has sprint %s", task.ID, *task.SprintID)
		}
	}
}

func TestCompleteSprint_MovesToOtherSprint(t *testing.T) {
	ctx := context.Background()
	taskRepo := newFakeTaskRepo()
	repo := newFakeSprintRepo()
	svc := sprintsvc.New(repo, taskRepo, nil)
	projectID := uuid.New()

	source, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint Source",
		Status:    sprintdom.SprintStatusActive,
	})
	dest, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint Dest",
		Status:    sprintdom.SprintStatusPlanned,
	})

	taskID := uuid.New()
	_ = taskRepo.CreateTask(ctx, &taskdom.Task{
		ID:        taskID,
		ProjectID: projectID,
		SprintID:  &source.ID,
		Title:     "incomplete task",
	})

	_, err := svc.CompleteSprint(ctx, source.ProjectID, source.ID, sprintdom.CompleteSprintInput{
		MoveToSprintID: &dest.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	moved, _ := taskRepo.FindTaskByID(ctx, taskID)
	if moved.SprintID == nil || *moved.SprintID != dest.ID {
		t.Errorf("expected task to be in dest sprint %s, got %v", dest.ID, moved.SprintID)
	}
}

func TestCompleteSprint_SkipsDoneTasks(t *testing.T) {
	ctx := context.Background()
	taskRepo := newFakeTaskRepo()
	svc := sprintsvc.New(newFakeSprintRepo(), taskRepo, nil)
	projectID := uuid.New()

	sp, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint With Done",
		Status:    sprintdom.SprintStatusActive,
	})

	// Create a done status and seed it into the fake task repo.
	doneStatusID := uuid.New()
	_ = taskRepo.CreateStatus(ctx, &taskdom.TaskStatus{
		ID:        doneStatusID,
		ProjectID: projectID,
		Name:      "Done",
		Category:  taskdom.StatusCategoryDone,
	})

	doneTaskID := uuid.New()
	_ = taskRepo.CreateTask(ctx, &taskdom.Task{
		ID:        doneTaskID,
		ProjectID: projectID,
		SprintID:  &sp.ID,
		StatusID:  &doneStatusID,
		Title:     "done task",
	})
	incompleteTaskID := uuid.New()
	_ = taskRepo.CreateTask(ctx, &taskdom.Task{
		ID:        incompleteTaskID,
		ProjectID: projectID,
		SprintID:  &sp.ID,
		Title:     "incomplete task",
	})

	_, err := svc.CompleteSprint(ctx, sp.ProjectID, sp.ID, sprintdom.CompleteSprintInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Done task stays in the sprint.
	done, _ := taskRepo.FindTaskByID(ctx, doneTaskID)
	if done.SprintID == nil || *done.SprintID != sp.ID {
		t.Errorf("expected done task to remain in sprint, got sprint=%v", done.SprintID)
	}

	// Incomplete task is moved to backlog.
	incomplete, _ := taskRepo.FindTaskByID(ctx, incompleteTaskID)
	if incomplete.SprintID != nil {
		t.Errorf("expected incomplete task to be in backlog, got sprint=%v", incomplete.SprintID)
	}
}

func TestCompleteSprint_AlreadyCompleted(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)
	projectID := uuid.New()

	sp, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Already Done",
		Status:    sprintdom.SprintStatusCompleted,
	})

	_, err := svc.CompleteSprint(ctx, sp.ProjectID, sp.ID, sprintdom.CompleteSprintInput{})
	if err != sprintdom.ErrSprintAlreadyComplete {
		t.Errorf("expected ErrSprintAlreadyComplete, got %v", err)
	}
}

func TestCompleteSprint_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo(), newFakeTaskRepo(), nil)

	_, err := svc.CompleteSprint(ctx, uuid.New(), uuid.New(), sprintdom.CompleteSprintInput{})
	if err != sprintdom.ErrSprintNotFound {
		t.Errorf("expected ErrSprintNotFound, got %v", err)
	}
}
