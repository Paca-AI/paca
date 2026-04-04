package taskdom

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the combined persistence contract for the task aggregate.
type Repository interface {
	TaskTypeRepository
	TaskStatusRepository
	TaskRepository
}

// TaskTypeRepository defines persistence operations for task types.
type TaskTypeRepository interface {
	ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*TaskType, error)
	FindTaskTypeByID(ctx context.Context, id uuid.UUID) (*TaskType, error)
	CreateTaskType(ctx context.Context, t *TaskType) error
	UpdateTaskType(ctx context.Context, t *TaskType) error
	DeleteTaskType(ctx context.Context, id uuid.UUID) error
}

// TaskStatusRepository defines persistence operations for task statuses.
type TaskStatusRepository interface {
	ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*TaskStatus, error)
	FindTaskStatusByID(ctx context.Context, id uuid.UUID) (*TaskStatus, error)
	CreateTaskStatus(ctx context.Context, s *TaskStatus) error
	UpdateTaskStatus(ctx context.Context, s *TaskStatus) error
	DeleteTaskStatus(ctx context.Context, id uuid.UUID) error
}

// TaskRepository defines persistence operations for tasks.
type TaskRepository interface {
	ListTasks(ctx context.Context, projectID uuid.UUID, filter TaskFilter, offset, limit int) ([]*Task, int64, error)
	FindTaskByID(ctx context.Context, id uuid.UUID) (*Task, error)
	CreateTask(ctx context.Context, t *Task) error
	UpdateTask(ctx context.Context, t *Task) error
	DeleteTask(ctx context.Context, id uuid.UUID) error
}

// TaskFilter carries optional criteria for listing tasks.
type TaskFilter struct {
	SprintID    *uuid.UUID
	StatusID    *uuid.UUID
	AssigneeID  *uuid.UUID
	BacklogOnly bool // true → only tasks where sprint_id IS NULL
}
