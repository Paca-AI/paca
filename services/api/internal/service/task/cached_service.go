package tasksvc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/cache"
)

// CachedService decorates a taskdom.Service with a Valkey/Redis-backed cache.
//
// # What is cached
//
//   - ListTaskTypes       – keyed by project; invalidated on any type mutation.
//   - ListTaskStatuses    – keyed by project; invalidated on any status mutation.
//   - ListCustomFieldDefinitions – keyed by project; invalidated on any field mutation.
//
// All other methods (tasks) are delegated directly to the
// underlying service without caching.
//
// Cache errors are non-fatal: on a read error the decorator falls through to
// the real service; on a write/delete error it logs and continues so that
// mutations always succeed even when the cache is temporarily unavailable.
type CachedService struct {
	svc taskdom.Service
	st  *cache.Store
	ttl time.Duration
	log *slog.Logger
}

// NewCachedService wraps svc with a caching layer backed by st.
// ttl controls how long each cached entry lives; zero disables caching.
// log receives non-fatal cache warnings.
func NewCachedService(svc taskdom.Service, st *cache.Store, ttl time.Duration, log *slog.Logger) *CachedService {
	return &CachedService{svc: svc, st: st, ttl: ttl, log: log}
}

// --- cache key helpers -------------------------------------------------------

func taskTypesKey(projectID uuid.UUID) string {
	return fmt.Sprintf("project:%s:task-types", projectID)
}

func taskStatusesKey(projectID uuid.UUID) string {
	return fmt.Sprintf("project:%s:task-statuses", projectID)
}

func customFieldsKey(projectID uuid.UUID) string {
	return fmt.Sprintf("project:%s:custom-fields", projectID)
}

// --- Task Types --------------------------------------------------------------

// ListTaskTypes returns task types for a project, reading from cache when
// available and populating it on a miss.
func (c *CachedService) ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error) {
	if c.ttl == 0 {
		return c.svc.ListTaskTypes(ctx, projectID)
	}
	key := taskTypesKey(projectID)
	var result []*taskdom.TaskType
	if ok, err := c.st.Get(ctx, key, &result); ok {
		return result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: ListTaskTypes get", "err", err)
	}

	result, err := c.svc.ListTaskTypes(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, result, c.ttl); err != nil {
		c.log.WarnContext(ctx, "cache: ListTaskTypes set", "err", err)
	}
	return result, nil
}

// GetTaskType delegates directly to the underlying service (not cached).
func (c *CachedService) GetTaskType(ctx context.Context, id uuid.UUID) (*taskdom.TaskType, error) {
	return c.svc.GetTaskType(ctx, id)
}

// CreateTaskType delegates to the underlying service and invalidates the task-types cache.
func (c *CachedService) CreateTaskType(ctx context.Context, in taskdom.CreateTaskTypeInput) (*taskdom.TaskType, error) {
	t, err := c.svc.CreateTaskType(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, taskTypesKey(in.ProjectID)); err != nil {
		c.log.WarnContext(ctx, "cache: CreateTaskType delete", "err", err)
	}
	return t, nil
}

// UpdateTaskType delegates to the underlying service and invalidates the task-types cache.
func (c *CachedService) UpdateTaskType(ctx context.Context, projectID, id uuid.UUID, in taskdom.UpdateTaskTypeInput) (*taskdom.TaskType, error) {
	t, err := c.svc.UpdateTaskType(ctx, projectID, id, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, taskTypesKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateTaskType delete", "err", err)
	}
	return t, nil
}

// DeleteTaskType delegates to the underlying service and invalidates the task-types cache.
func (c *CachedService) DeleteTaskType(ctx context.Context, projectID, id uuid.UUID) error {
	if err := c.svc.DeleteTaskType(ctx, projectID, id); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, taskTypesKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: DeleteTaskType delete", "err", err)
	}
	return nil
}

// SetDefaultTaskType delegates to the underlying service and invalidates the task-types cache.
func (c *CachedService) SetDefaultTaskType(ctx context.Context, projectID, typeID uuid.UUID) (*taskdom.TaskType, error) {
	t, err := c.svc.SetDefaultTaskType(ctx, projectID, typeID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, taskTypesKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: SetDefaultTaskType delete", "err", err)
	}
	return t, nil
}

// --- Task Statuses -----------------------------------------------------------

// ListTaskStatuses returns task statuses for a project, reading from cache
// when available and populating it on a miss.
func (c *CachedService) ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskStatus, error) {
	if c.ttl == 0 {
		return c.svc.ListTaskStatuses(ctx, projectID)
	}
	key := taskStatusesKey(projectID)
	var result []*taskdom.TaskStatus
	if ok, err := c.st.Get(ctx, key, &result); ok {
		return result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: ListTaskStatuses get", "err", err)
	}

	result, err := c.svc.ListTaskStatuses(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, result, c.ttl); err != nil {
		c.log.WarnContext(ctx, "cache: ListTaskStatuses set", "err", err)
	}
	return result, nil
}

// GetTaskStatus delegates directly to the underlying service (not cached).
func (c *CachedService) GetTaskStatus(ctx context.Context, id uuid.UUID) (*taskdom.TaskStatus, error) {
	return c.svc.GetTaskStatus(ctx, id)
}

// CreateTaskStatus delegates to the underlying service and invalidates the task-statuses cache.
func (c *CachedService) CreateTaskStatus(ctx context.Context, in taskdom.CreateTaskStatusInput) (*taskdom.TaskStatus, error) {
	st, err := c.svc.CreateTaskStatus(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, taskStatusesKey(in.ProjectID)); err != nil {
		c.log.WarnContext(ctx, "cache: CreateTaskStatus delete", "err", err)
	}
	return st, nil
}

// UpdateTaskStatus delegates to the underlying service and invalidates the task-statuses cache.
func (c *CachedService) UpdateTaskStatus(ctx context.Context, projectID, id uuid.UUID, in taskdom.UpdateTaskStatusInput) (*taskdom.TaskStatus, error) {
	st, err := c.svc.UpdateTaskStatus(ctx, projectID, id, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, taskStatusesKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateTaskStatus delete", "err", err)
	}
	return st, nil
}

// DeleteTaskStatus delegates to the underlying service and invalidates the task-statuses cache.
func (c *CachedService) DeleteTaskStatus(ctx context.Context, projectID, id uuid.UUID) error {
	if err := c.svc.DeleteTaskStatus(ctx, projectID, id); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, taskStatusesKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: DeleteTaskStatus delete", "err", err)
	}
	return nil
}

// SetDefaultTaskStatus delegates to the underlying service and invalidates the task-statuses cache.
func (c *CachedService) SetDefaultTaskStatus(ctx context.Context, projectID, statusID uuid.UUID) (*taskdom.TaskStatus, error) {
	st, err := c.svc.SetDefaultTaskStatus(ctx, projectID, statusID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, taskStatusesKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: SetDefaultTaskStatus delete", "err", err)
	}
	return st, nil
}

// ReorderTaskStatuses delegates to the underlying service and invalidates the task-statuses cache.
func (c *CachedService) ReorderTaskStatuses(ctx context.Context, projectID uuid.UUID, statusIDs []uuid.UUID) error {
	if err := c.svc.ReorderTaskStatuses(ctx, projectID, statusIDs); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, taskStatusesKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: ReorderTaskStatuses delete", "err", err)
	}
	return nil
}

// --- Tasks (pass-through) ----------------------------------------------------
// Tasks are not cached because they are highly mutable and queries are filtered
// in many different ways, making cache key cardinality prohibitively large.

// ListTasks delegates directly to the underlying service (not cached).
func (c *CachedService) ListTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, pageSize int, sort taskdom.TaskSort) ([]*taskdom.Task, bool, error) {
	return c.svc.ListTasks(ctx, projectID, filter, pageSize, sort)
}

// CountTasks delegates to the underlying service without caching.
func (c *CachedService) CountTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter) (int64, error) {
	return c.svc.CountTasks(ctx, projectID, filter)
}

// CountOpenTasksByProjects delegates to the underlying service without caching.
func (c *CachedService) CountOpenTasksByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error) {
	return c.svc.CountOpenTasksByProjects(ctx, projectIDs)
}

// SumTaskField delegates to the underlying service without caching.
func (c *CachedService) SumTaskField(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, fieldKey string) (float64, error) {
	return c.svc.SumTaskField(ctx, projectID, filter, fieldKey)
}

// ListAssignedTasks delegates directly to the underlying service (not cached).
func (c *CachedService) ListAssignedTasks(ctx context.Context, memberIDs []uuid.UUID, limit int, cursorAfter *string) ([]*taskdom.Task, bool, error) {
	return c.svc.ListAssignedTasks(ctx, memberIDs, limit, cursorAfter)
}

// GetTask delegates directly to the underlying service (not cached).
func (c *CachedService) GetTask(ctx context.Context, projectID, id uuid.UUID) (*taskdom.Task, error) {
	return c.svc.GetTask(ctx, projectID, id)
}

// GetTaskByNumber delegates directly to the underlying service (not cached).
func (c *CachedService) GetTaskByNumber(ctx context.Context, projectID uuid.UUID, taskNumber int64) (*taskdom.Task, error) {
	return c.svc.GetTaskByNumber(ctx, projectID, taskNumber)
}

// CreateTask delegates directly to the underlying service (not cached).
func (c *CachedService) CreateTask(ctx context.Context, in taskdom.CreateTaskInput) (*taskdom.Task, error) {
	return c.svc.CreateTask(ctx, in)
}

// UpdateTask delegates directly to the underlying service (not cached).
func (c *CachedService) UpdateTask(ctx context.Context, projectID, id uuid.UUID, in taskdom.UpdateTaskInput) (*taskdom.Task, error) {
	return c.svc.UpdateTask(ctx, projectID, id, in)
}

// DeleteTask delegates directly to the underlying service (not cached).
func (c *CachedService) DeleteTask(ctx context.Context, projectID, id uuid.UUID) error {
	return c.svc.DeleteTask(ctx, projectID, id)
}

// --- Custom Field Definitions ------------------------------------------------

// ListCustomFieldDefinitions returns custom field definitions for a project,
// reading from cache when available and populating it on a miss.
func (c *CachedService) ListCustomFieldDefinitions(ctx context.Context, projectID uuid.UUID) ([]*taskdom.CustomFieldDefinition, error) {
	if c.ttl == 0 {
		return c.svc.ListCustomFieldDefinitions(ctx, projectID)
	}
	key := customFieldsKey(projectID)
	var result []*taskdom.CustomFieldDefinition
	if ok, err := c.st.Get(ctx, key, &result); ok {
		return result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: ListCustomFieldDefinitions get", "err", err)
	}

	result, err := c.svc.ListCustomFieldDefinitions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, result, c.ttl); err != nil {
		c.log.WarnContext(ctx, "cache: ListCustomFieldDefinitions set", "err", err)
	}
	return result, nil
}

// GetCustomFieldDefinition delegates directly to the underlying service (not cached).
func (c *CachedService) GetCustomFieldDefinition(ctx context.Context, projectID, id uuid.UUID) (*taskdom.CustomFieldDefinition, error) {
	return c.svc.GetCustomFieldDefinition(ctx, projectID, id)
}

// CreateCustomFieldDefinition delegates to the underlying service and invalidates the custom-fields cache.
func (c *CachedService) CreateCustomFieldDefinition(ctx context.Context, in taskdom.CreateCustomFieldDefinitionInput) (*taskdom.CustomFieldDefinition, error) {
	f, err := c.svc.CreateCustomFieldDefinition(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, customFieldsKey(in.ProjectID)); err != nil {
		c.log.WarnContext(ctx, "cache: CreateCustomFieldDefinition delete", "err", err)
	}
	return f, nil
}

// UpdateCustomFieldDefinition delegates to the underlying service and invalidates the custom-fields cache.
func (c *CachedService) UpdateCustomFieldDefinition(ctx context.Context, projectID, id uuid.UUID, in taskdom.UpdateCustomFieldDefinitionInput) (*taskdom.CustomFieldDefinition, error) {
	f, err := c.svc.UpdateCustomFieldDefinition(ctx, projectID, id, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, customFieldsKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateCustomFieldDefinition delete", "err", err)
	}
	return f, nil
}

// DeleteCustomFieldDefinition delegates to the underlying service and invalidates the custom-fields cache.
func (c *CachedService) DeleteCustomFieldDefinition(ctx context.Context, projectID, id uuid.UUID) error {
	if err := c.svc.DeleteCustomFieldDefinition(ctx, projectID, id); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, customFieldsKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: DeleteCustomFieldDefinition delete", "err", err)
	}
	return nil
}

// --- Task Links (pass-through) -----------------------------------------------

// ListTaskLinks delegates directly to the underlying service (not cached).
func (c *CachedService) ListTaskLinks(ctx context.Context, projectID, taskID uuid.UUID) ([]*taskdom.TaskLink, error) {
	return c.svc.ListTaskLinks(ctx, projectID, taskID)
}

// CreateTaskLink delegates directly to the underlying service (not cached).
func (c *CachedService) CreateTaskLink(ctx context.Context, in taskdom.CreateTaskLinkInput) (*taskdom.TaskLink, error) {
	return c.svc.CreateTaskLink(ctx, in)
}

// DeleteTaskLink delegates directly to the underlying service (not cached).
func (c *CachedService) DeleteTaskLink(ctx context.Context, projectID, taskID, linkID uuid.UUID) error {
	return c.svc.DeleteTaskLink(ctx, projectID, taskID, linkID)
}
