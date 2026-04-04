package taskdom

import (
	"context"

	"github.com/google/uuid"
)

// Service is the combined task management service contract.
type Service interface {
	TaskTypeService
	TaskStatusService
	TaskService
}

// --- Task Type Service -----------------------------------------------------

// TaskTypeService defines task-type use cases.
type TaskTypeService interface {
	ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*TaskType, error)
	GetTaskType(ctx context.Context, id uuid.UUID) (*TaskType, error)
	CreateTaskType(ctx context.Context, in CreateTaskTypeInput) (*TaskType, error)
	UpdateTaskType(ctx context.Context, id uuid.UUID, in UpdateTaskTypeInput) (*TaskType, error)
	DeleteTaskType(ctx context.Context, id uuid.UUID) error
}

// CreateTaskTypeInput carries fields required to create a task type.
type CreateTaskTypeInput struct {
	ProjectID   uuid.UUID
	Name        string
	Icon        *string
	Color       *string
	Description *string
}

// UpdateTaskTypeInput carries mutable task-type fields.
type UpdateTaskTypeInput struct {
	Name        string
	Icon        *string
	Color       *string
	Description *string
}

// --- Task Status Service ---------------------------------------------------

// TaskStatusService defines task-status use cases.
type TaskStatusService interface {
	ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*TaskStatus, error)
	GetTaskStatus(ctx context.Context, id uuid.UUID) (*TaskStatus, error)
	CreateTaskStatus(ctx context.Context, in CreateTaskStatusInput) (*TaskStatus, error)
	UpdateTaskStatus(ctx context.Context, id uuid.UUID, in UpdateTaskStatusInput) (*TaskStatus, error)
	DeleteTaskStatus(ctx context.Context, id uuid.UUID) error
}

// CreateTaskStatusInput carries fields required to create a task status.
type CreateTaskStatusInput struct {
	ProjectID uuid.UUID
	Name      string
	Color     *string
	Position  int
	Category  StatusCategory
}

// UpdateTaskStatusInput carries mutable task-status fields.
type UpdateTaskStatusInput struct {
	Name     string
	Color    *string
	Position *int
	Category *StatusCategory
}

// --- Task Service ----------------------------------------------------------

// TaskService defines task use cases.
type TaskService interface {
	ListTasks(ctx context.Context, projectID uuid.UUID, filter TaskFilter, page, pageSize int) ([]*Task, int64, error)
	GetTask(ctx context.Context, id uuid.UUID) (*Task, error)
	CreateTask(ctx context.Context, in CreateTaskInput) (*Task, error)
	UpdateTask(ctx context.Context, id uuid.UUID, in UpdateTaskInput) (*Task, error)
	DeleteTask(ctx context.Context, id uuid.UUID) error
}

// CreateTaskInput carries fields required to create a task.
type CreateTaskInput struct {
	ProjectID     uuid.UUID
	TaskTypeID    *uuid.UUID
	StatusID      *uuid.UUID
	SprintID      *uuid.UUID
	ParentTaskID  *uuid.UUID
	Title         string
	Description   *string
	Importance    int
	BoardPosition int
	AssigneeID    *uuid.UUID
	ReporterID    *uuid.UUID
	CustomFields  map[string]any
}

// UpdateTaskInput carries mutable task fields.
// String fields are applied when non-empty; pointer fields always replace the
// current value (use nil to clear a nullable reference).
type UpdateTaskInput struct {
	TaskTypeID    *uuid.UUID
	StatusID      *uuid.UUID
	SprintID      *uuid.UUID
	ParentTaskID  *uuid.UUID
	Title         string
	Description   *string
	Importance    *int
	BoardPosition *int
	AssigneeID    *uuid.UUID
	ReporterID    *uuid.UUID
	CustomFields  map[string]any
}
