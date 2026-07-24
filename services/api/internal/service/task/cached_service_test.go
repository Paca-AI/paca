// Package tasksvc_test contains unit tests for the task service layer,
// including the CachedService decorator.
package tasksvc_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/cache"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newCacheStore(t *testing.T) *cache.Store {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return cache.NewStore(client, "paca:")
}

// ---------------------------------------------------------------------------
// Stub task service (implements taskdom.Service)
// ---------------------------------------------------------------------------

type stubTaskSvc struct {
	listTaskTypes  func(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error)
	createTaskType func(ctx context.Context, in taskdom.CreateTaskTypeInput) (*taskdom.TaskType, error)
	deleteTaskType func(ctx context.Context, projectID, id uuid.UUID) error

	listTaskStatuses func(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskStatus, error)
	createTaskStatus func(ctx context.Context, in taskdom.CreateTaskStatusInput) (*taskdom.TaskStatus, error)
	deleteTaskStatus func(ctx context.Context, projectID, id uuid.UUID) error

	listCustomFields  func(ctx context.Context, projectID uuid.UUID) ([]*taskdom.CustomFieldDefinition, error)
	createCustomField func(ctx context.Context, in taskdom.CreateCustomFieldDefinitionInput) (*taskdom.CustomFieldDefinition, error)
	deleteCustomField func(ctx context.Context, projectID, id uuid.UUID) error

	countTasksFn   func(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter) (int64, error)
	sumTaskFieldFn func(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, fieldKey string) (float64, error)

	listTypesCalls  int
	listStatusCalls int
	listFieldsCalls int
}

// TaskType methods

func (s *stubTaskSvc) ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error) {
	s.listTypesCalls++
	if s.listTaskTypes != nil {
		return s.listTaskTypes(ctx, projectID)
	}
	return []*taskdom.TaskType{{ID: uuid.New(), ProjectID: projectID, Name: "Bug"}}, nil
}

func (s *stubTaskSvc) GetTaskType(_ context.Context, id uuid.UUID) (*taskdom.TaskType, error) {
	return &taskdom.TaskType{ID: id}, nil
}

func (s *stubTaskSvc) CreateTaskType(ctx context.Context, in taskdom.CreateTaskTypeInput) (*taskdom.TaskType, error) {
	if s.createTaskType != nil {
		return s.createTaskType(ctx, in)
	}
	return &taskdom.TaskType{ID: uuid.New(), ProjectID: in.ProjectID, Name: in.Name}, nil
}

func (s *stubTaskSvc) UpdateTaskType(_ context.Context, projectID, id uuid.UUID, in taskdom.UpdateTaskTypeInput) (*taskdom.TaskType, error) {
	return &taskdom.TaskType{ID: id, ProjectID: projectID, Name: in.Name}, nil
}

func (s *stubTaskSvc) DeleteTaskType(ctx context.Context, projectID, id uuid.UUID) error {
	if s.deleteTaskType != nil {
		return s.deleteTaskType(ctx, projectID, id)
	}
	return nil
}

func (s *stubTaskSvc) SetDefaultTaskType(_ context.Context, projectID, typeID uuid.UUID) (*taskdom.TaskType, error) {
	return &taskdom.TaskType{ID: typeID, ProjectID: projectID, IsDefault: true}, nil
}

// TaskStatus methods

func (s *stubTaskSvc) ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskStatus, error) {
	s.listStatusCalls++
	if s.listTaskStatuses != nil {
		return s.listTaskStatuses(ctx, projectID)
	}
	return []*taskdom.TaskStatus{{ID: uuid.New(), ProjectID: projectID, Name: "Todo"}}, nil
}

func (s *stubTaskSvc) GetTaskStatus(_ context.Context, id uuid.UUID) (*taskdom.TaskStatus, error) {
	return &taskdom.TaskStatus{ID: id}, nil
}

func (s *stubTaskSvc) CreateTaskStatus(ctx context.Context, in taskdom.CreateTaskStatusInput) (*taskdom.TaskStatus, error) {
	if s.createTaskStatus != nil {
		return s.createTaskStatus(ctx, in)
	}
	return &taskdom.TaskStatus{ID: uuid.New(), ProjectID: in.ProjectID, Name: in.Name}, nil
}

func (s *stubTaskSvc) UpdateTaskStatus(_ context.Context, projectID, id uuid.UUID, in taskdom.UpdateTaskStatusInput) (*taskdom.TaskStatus, error) {
	return &taskdom.TaskStatus{ID: id, ProjectID: projectID, Name: in.Name}, nil
}

func (s *stubTaskSvc) DeleteTaskStatus(ctx context.Context, projectID, id uuid.UUID) error {
	if s.deleteTaskStatus != nil {
		return s.deleteTaskStatus(ctx, projectID, id)
	}
	return nil
}

func (s *stubTaskSvc) SetDefaultTaskStatus(_ context.Context, projectID, statusID uuid.UUID) (*taskdom.TaskStatus, error) {
	return &taskdom.TaskStatus{ID: statusID, ProjectID: projectID, IsDefault: true}, nil
}

func (s *stubTaskSvc) ReorderTaskStatuses(_ context.Context, _ uuid.UUID, _ []uuid.UUID) error {
	return nil
}

// TaskService methods (pass-through in CachedService)

func (s *stubTaskSvc) ListTasks(_ context.Context, _ uuid.UUID, _ taskdom.TaskFilter, _ int, _ taskdom.TaskSort) ([]*taskdom.Task, bool, error) {
	return nil, false, nil
}

func (s *stubTaskSvc) CountTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter) (int64, error) {
	if s.countTasksFn != nil {
		return s.countTasksFn(ctx, projectID, filter)
	}
	return 0, nil
}

func (s *stubTaskSvc) SumTaskField(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, fieldKey string) (float64, error) {
	if s.sumTaskFieldFn != nil {
		return s.sumTaskFieldFn(ctx, projectID, filter, fieldKey)
	}
	return 0, nil
}

func (s *stubTaskSvc) ListAssignedTasks(_ context.Context, _ []uuid.UUID, _ int, _ *string) ([]*taskdom.Task, bool, error) {
	return nil, false, nil
}

func (s *stubTaskSvc) GetTask(_ context.Context, _, id uuid.UUID) (*taskdom.Task, error) {
	return &taskdom.Task{ID: id}, nil
}

func (s *stubTaskSvc) GetTaskByNumber(_ context.Context, projectID uuid.UUID, n int64) (*taskdom.Task, error) {
	return &taskdom.Task{ProjectID: projectID, TaskNumber: n}, nil
}

func (s *stubTaskSvc) CreateTask(_ context.Context, in taskdom.CreateTaskInput) (*taskdom.Task, error) {
	return &taskdom.Task{ID: uuid.New(), ProjectID: in.ProjectID, Title: in.Title}, nil
}

func (s *stubTaskSvc) UpdateTask(_ context.Context, _, id uuid.UUID, _ taskdom.UpdateTaskInput) (*taskdom.Task, error) {
	return &taskdom.Task{ID: id}, nil
}

func (s *stubTaskSvc) DeleteTask(_ context.Context, _, _ uuid.UUID) error { return nil }

// CustomFieldDefinition methods

func (s *stubTaskSvc) ListCustomFieldDefinitions(ctx context.Context, projectID uuid.UUID) ([]*taskdom.CustomFieldDefinition, error) {
	s.listFieldsCalls++
	if s.listCustomFields != nil {
		return s.listCustomFields(ctx, projectID)
	}
	return []*taskdom.CustomFieldDefinition{{ID: uuid.New(), ProjectID: projectID}}, nil
}

func (s *stubTaskSvc) GetCustomFieldDefinition(_ context.Context, _, id uuid.UUID) (*taskdom.CustomFieldDefinition, error) {
	return &taskdom.CustomFieldDefinition{ID: id}, nil
}

func (s *stubTaskSvc) CreateCustomFieldDefinition(ctx context.Context, in taskdom.CreateCustomFieldDefinitionInput) (*taskdom.CustomFieldDefinition, error) {
	if s.createCustomField != nil {
		return s.createCustomField(ctx, in)
	}
	return &taskdom.CustomFieldDefinition{ID: uuid.New(), ProjectID: in.ProjectID}, nil
}

func (s *stubTaskSvc) UpdateCustomFieldDefinition(_ context.Context, projectID, id uuid.UUID, _ taskdom.UpdateCustomFieldDefinitionInput) (*taskdom.CustomFieldDefinition, error) {
	return &taskdom.CustomFieldDefinition{ID: id, ProjectID: projectID}, nil
}

func (s *stubTaskSvc) DeleteCustomFieldDefinition(ctx context.Context, projectID, id uuid.UUID) error {
	if s.deleteCustomField != nil {
		return s.deleteCustomField(ctx, projectID, id)
	}
	return nil
}

// --- TaskLinkService stubs --------------------------------------------------

func (s *stubTaskSvc) ListTaskLinks(_ context.Context, _, _ uuid.UUID) ([]*taskdom.TaskLink, error) {
	return []*taskdom.TaskLink{}, nil
}

func (s *stubTaskSvc) CreateTaskLink(_ context.Context, in taskdom.CreateTaskLinkInput) (*taskdom.TaskLink, error) {
	return &taskdom.TaskLink{
		ID:           uuid.New(),
		SourceTaskID: in.SourceTaskID,
		TargetTaskID: in.TargetTaskID,
		LinkType:     in.LinkType,
	}, nil
}

func (s *stubTaskSvc) DeleteTaskLink(_ context.Context, _, _, _ uuid.UUID) error { return nil }

// ---------------------------------------------------------------------------
// ListTaskTypes
// ---------------------------------------------------------------------------

func TestCachedTask_ListTaskTypes_CacheMissPopulatesCache(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	// First call: miss.
	types, err := svc.ListTaskTypes(ctx, projectID)
	if err != nil {
		t.Fatalf("ListTaskTypes (miss): %v", err)
	}
	if len(types) == 0 {
		t.Fatal("expected at least one task type")
	}
	if stub.listTypesCalls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.listTypesCalls)
	}

	// Second call: hit.
	if _, err := svc.ListTaskTypes(ctx, projectID); err != nil {
		t.Fatalf("ListTaskTypes (hit): %v", err)
	}
	if stub.listTypesCalls != 1 {
		t.Fatalf("cache hit: stub called again; got %d calls", stub.listTypesCalls)
	}
}

func TestCachedTask_ListTaskTypes_ZeroTTLBypassesCache(t *testing.T) {
	ctx := context.Background()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 0, discardLogger())

	for i := 0; i < 3; i++ {
		if _, err := svc.ListTaskTypes(ctx, uuid.New()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if stub.listTypesCalls != 3 {
		t.Fatalf("TTL=0 should bypass cache; want 3 calls, got %d", stub.listTypesCalls)
	}
}

func TestCachedTask_CreateTaskType_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListTaskTypes(ctx, projectID); err != nil {
		t.Fatalf("ListTaskTypes: %v", err)
	}
	if _, err := svc.CreateTaskType(ctx, taskdom.CreateTaskTypeInput{ProjectID: projectID, Name: "Epic"}); err != nil {
		t.Fatalf("CreateTaskType: %v", err)
	}
	if _, err := svc.ListTaskTypes(ctx, projectID); err != nil {
		t.Fatalf("ListTaskTypes after Create: %v", err)
	}
	if stub.listTypesCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listTypesCalls)
	}
}

func TestCachedTask_UpdateTaskType_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListTaskTypes(ctx, projectID); err != nil {
		t.Fatalf("ListTaskTypes: %v", err)
	}
	if _, err := svc.UpdateTaskType(ctx, projectID, uuid.New(), taskdom.UpdateTaskTypeInput{Name: "Story"}); err != nil {
		t.Fatalf("UpdateTaskType: %v", err)
	}
	if _, err := svc.ListTaskTypes(ctx, projectID); err != nil {
		t.Fatalf("ListTaskTypes after Update: %v", err)
	}
	if stub.listTypesCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listTypesCalls)
	}
}

func TestCachedTask_DeleteTaskType_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListTaskTypes(ctx, projectID); err != nil {
		t.Fatalf("ListTaskTypes: %v", err)
	}
	if err := svc.DeleteTaskType(ctx, projectID, uuid.New()); err != nil {
		t.Fatalf("DeleteTaskType: %v", err)
	}
	if _, err := svc.ListTaskTypes(ctx, projectID); err != nil {
		t.Fatalf("ListTaskTypes after Delete: %v", err)
	}
	if stub.listTypesCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listTypesCalls)
	}
}

func TestCachedTask_SetDefaultTaskType_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListTaskTypes(ctx, projectID); err != nil {
		t.Fatalf("ListTaskTypes: %v", err)
	}
	if _, err := svc.SetDefaultTaskType(ctx, projectID, uuid.New()); err != nil {
		t.Fatalf("SetDefaultTaskType: %v", err)
	}
	if _, err := svc.ListTaskTypes(ctx, projectID); err != nil {
		t.Fatalf("ListTaskTypes after SetDefault: %v", err)
	}
	if stub.listTypesCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listTypesCalls)
	}
}

// ---------------------------------------------------------------------------
// ListTaskStatuses
// ---------------------------------------------------------------------------

func TestCachedTask_ListTaskStatuses_CacheHit(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListTaskStatuses(ctx, projectID); err != nil {
		t.Fatalf("ListTaskStatuses (miss): %v", err)
	}
	if _, err := svc.ListTaskStatuses(ctx, projectID); err != nil {
		t.Fatalf("ListTaskStatuses (hit): %v", err)
	}
	if stub.listStatusCalls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.listStatusCalls)
	}
}

func TestCachedTask_CreateTaskStatus_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListTaskStatuses(ctx, projectID); err != nil {
		t.Fatalf("ListTaskStatuses: %v", err)
	}
	in := taskdom.CreateTaskStatusInput{
		ProjectID: projectID, Name: "In Review", Category: taskdom.StatusCategoryInProgress, // = "inprogress"
	}
	if _, err := svc.CreateTaskStatus(ctx, in); err != nil {
		t.Fatalf("CreateTaskStatus: %v", err)
	}
	if _, err := svc.ListTaskStatuses(ctx, projectID); err != nil {
		t.Fatalf("ListTaskStatuses after Create: %v", err)
	}
	if stub.listStatusCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listStatusCalls)
	}
}

func TestCachedTask_DeleteTaskStatus_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListTaskStatuses(ctx, projectID); err != nil {
		t.Fatalf("ListTaskStatuses: %v", err)
	}
	if err := svc.DeleteTaskStatus(ctx, projectID, uuid.New()); err != nil {
		t.Fatalf("DeleteTaskStatus: %v", err)
	}
	if _, err := svc.ListTaskStatuses(ctx, projectID); err != nil {
		t.Fatalf("ListTaskStatuses after Delete: %v", err)
	}
	if stub.listStatusCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listStatusCalls)
	}
}

func TestCachedTask_ReorderTaskStatuses_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListTaskStatuses(ctx, projectID); err != nil {
		t.Fatalf("ListTaskStatuses: %v", err)
	}
	if err := svc.ReorderTaskStatuses(ctx, projectID, []uuid.UUID{uuid.New()}); err != nil {
		t.Fatalf("ReorderTaskStatuses: %v", err)
	}
	if _, err := svc.ListTaskStatuses(ctx, projectID); err != nil {
		t.Fatalf("ListTaskStatuses after Reorder: %v", err)
	}
	if stub.listStatusCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listStatusCalls)
	}
}

// ---------------------------------------------------------------------------
// ListCustomFieldDefinitions
// ---------------------------------------------------------------------------

func TestCachedTask_ListCustomFields_CacheHit(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListCustomFieldDefinitions(ctx, projectID); err != nil {
		t.Fatalf("ListCustomFields (miss): %v", err)
	}
	if _, err := svc.ListCustomFieldDefinitions(ctx, projectID); err != nil {
		t.Fatalf("ListCustomFields (hit): %v", err)
	}
	if stub.listFieldsCalls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.listFieldsCalls)
	}
}

func TestCachedTask_CreateCustomField_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListCustomFieldDefinitions(ctx, projectID); err != nil {
		t.Fatalf("ListCustomFields: %v", err)
	}
	in := taskdom.CreateCustomFieldDefinitionInput{
		ProjectID: projectID, FieldKey: "priority", DisplayName: "Priority", FieldType: taskdom.FieldTypeSelect,
	}
	if _, err := svc.CreateCustomFieldDefinition(ctx, in); err != nil {
		t.Fatalf("CreateCustomField: %v", err)
	}
	if _, err := svc.ListCustomFieldDefinitions(ctx, projectID); err != nil {
		t.Fatalf("ListCustomFields after Create: %v", err)
	}
	if stub.listFieldsCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listFieldsCalls)
	}
}

func TestCachedTask_UpdateCustomField_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListCustomFieldDefinitions(ctx, projectID); err != nil {
		t.Fatalf("ListCustomFields: %v", err)
	}
	if _, err := svc.UpdateCustomFieldDefinition(ctx, projectID, uuid.New(), taskdom.UpdateCustomFieldDefinitionInput{DisplayName: "Updated"}); err != nil {
		t.Fatalf("UpdateCustomField: %v", err)
	}
	if _, err := svc.ListCustomFieldDefinitions(ctx, projectID); err != nil {
		t.Fatalf("ListCustomFields after Update: %v", err)
	}
	if stub.listFieldsCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listFieldsCalls)
	}
}

func TestCachedTask_DeleteCustomField_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubTaskSvc{}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ListCustomFieldDefinitions(ctx, projectID); err != nil {
		t.Fatalf("ListCustomFields: %v", err)
	}
	if err := svc.DeleteCustomFieldDefinition(ctx, projectID, uuid.New()); err != nil {
		t.Fatalf("DeleteCustomField: %v", err)
	}
	if _, err := svc.ListCustomFieldDefinitions(ctx, projectID); err != nil {
		t.Fatalf("ListCustomFields after Delete: %v", err)
	}
	if stub.listFieldsCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listFieldsCalls)
	}
}

// ---------------------------------------------------------------------------
// Error propagation
// ---------------------------------------------------------------------------

func TestCachedTask_ListTaskTypes_ServiceErrorPropagated(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("repo failure")
	stub := &stubTaskSvc{
		listTaskTypes: func(_ context.Context, _ uuid.UUID) ([]*taskdom.TaskType, error) {
			return nil, sentinel
		},
	}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	_, err := svc.ListTaskTypes(ctx, uuid.New())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Per-project cache isolation
// ---------------------------------------------------------------------------

func TestCachedTask_ListTaskTypes_PerProjectCacheIsolation(t *testing.T) {
	ctx := context.Background()
	projectA := uuid.New()
	projectB := uuid.New()

	callsA := 0
	callsB := 0
	stub := &stubTaskSvc{
		listTaskTypes: func(_ context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error) {
			if projectID == projectA {
				callsA++
			} else {
				callsB++
			}
			return []*taskdom.TaskType{{ID: uuid.New(), ProjectID: projectID}}, nil
		},
	}
	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	// Populate both caches.
	if _, err := svc.ListTaskTypes(ctx, projectA); err != nil {
		t.Fatalf("ListTaskTypes(A): %v", err)
	}
	if _, err := svc.ListTaskTypes(ctx, projectB); err != nil {
		t.Fatalf("ListTaskTypes(B): %v", err)
	}

	// Invalidate only project A.
	if err := svc.DeleteTaskType(ctx, projectA, uuid.New()); err != nil {
		t.Fatalf("DeleteTaskType(A): %v", err)
	}

	// Project A cache is evicted; project B cache is intact.
	if _, err := svc.ListTaskTypes(ctx, projectA); err != nil {
		t.Fatalf("ListTaskTypes(A) after invalidation: %v", err)
	}
	if _, err := svc.ListTaskTypes(ctx, projectB); err != nil {
		t.Fatalf("ListTaskTypes(B) after A invalidation: %v", err)
	}

	if callsA != 2 {
		t.Fatalf("projectA: expected 2 stub calls, got %d", callsA)
	}
	if callsB != 1 {
		t.Fatalf("projectB: expected 1 stub call (no invalidation), got %d", callsB)
	}
}

// ---------------------------------------------------------------------------
// CountTasks delegation
// ---------------------------------------------------------------------------

func TestCachedTask_CountTasks_DelegatesToUnderlying(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()

	calls := 0
	stub := &stubTaskSvc{}
	stub.countTasksFn = func(_ context.Context, _ uuid.UUID, _ taskdom.TaskFilter) (int64, error) {
		calls++
		return 42, nil
	}

	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	count, err := svc.CountTasks(ctx, projectID, taskdom.TaskFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 42 {
		t.Errorf("expected delegated count=42, got %d", count)
	}
	if calls != 1 {
		t.Errorf("expected underlying CountTasks called once, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// SumTaskField delegation
// ---------------------------------------------------------------------------

func TestCachedTask_SumTaskField_DelegatesToUnderlying(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()

	calls := 0
	stub := &stubTaskSvc{}
	stub.sumTaskFieldFn = func(_ context.Context, _ uuid.UUID, _ taskdom.TaskFilter, _ string) (float64, error) {
		calls++
		return 99.5, nil
	}

	svc := tasksvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	sum, err := svc.SumTaskField(ctx, projectID, taskdom.TaskFilter{}, "story_points")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum != 99.5 {
		t.Errorf("expected delegated sum=99.5, got %v", sum)
	}
	if calls != 1 {
		t.Errorf("expected underlying SumTaskField called once, got %d", calls)
	}
}
