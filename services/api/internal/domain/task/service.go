package taskdom

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Service is the combined task management service contract.
type Service interface {
	TaskTypeService
	TaskStatusService
	TaskService
	TaskLinkService
	CustomFieldDefinitionService
}

// TaskLinkService defines task-link use cases.
type TaskLinkService interface {
	// ListTaskLinks returns all links for taskID, computing inverse display labels.
	ListTaskLinks(ctx context.Context, projectID, taskID uuid.UUID) ([]*TaskLink, error)
	// CreateTaskLink creates a directed link between two tasks in the same project.
	CreateTaskLink(ctx context.Context, in CreateTaskLinkInput) (*TaskLink, error)
	// DeleteTaskLink removes the link identified by linkID, verifying it
	// belongs to a task within projectID.
	DeleteTaskLink(ctx context.Context, projectID, taskID, linkID uuid.UUID) error
}

// CreateTaskLinkInput carries the fields required to create a task link.
type CreateTaskLinkInput struct {
	ProjectID    uuid.UUID
	SourceTaskID uuid.UUID
	TargetTaskID uuid.UUID
	LinkType     LinkType
	CreatedBy    *uuid.UUID
}

// --- Task Type Service -----------------------------------------------------

// TaskTypeService defines task-type use cases.
type TaskTypeService interface {
	ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*TaskType, error)
	GetTaskType(ctx context.Context, id uuid.UUID) (*TaskType, error)
	CreateTaskType(ctx context.Context, in CreateTaskTypeInput) (*TaskType, error)
	// UpdateTaskType updates the task type identified by id, verifying it belongs to projectID.
	UpdateTaskType(ctx context.Context, projectID, id uuid.UUID, in UpdateTaskTypeInput) (*TaskType, error)
	// DeleteTaskType removes the task type identified by id, verifying it belongs to projectID.
	DeleteTaskType(ctx context.Context, projectID, id uuid.UUID) error
	// SetDefaultTaskType marks typeID as the project's default task type,
	// clearing the flag on all other types in the same project.
	SetDefaultTaskType(ctx context.Context, projectID, typeID uuid.UUID) (*TaskType, error)
}

// CreateTaskTypeInput carries fields required to create a task type.
type CreateTaskTypeInput struct {
	ProjectID   uuid.UUID
	Name        string
	Icon        *string
	Color       *string
	Description *string
}

// UpdateTaskTypeInput carries mutable task-type fields for a PATCH operation.
// Name is applied when non-empty. For Icon, Color, and Description, the
// double pointer encodes three states:
//   - nil outer pointer  → field was absent in the request; do NOT overwrite
//   - non-nil outer pointer, inner pointer nil  → explicitly set to null (clear)
//   - non-nil outer pointer, inner pointer non-nil  → set to the given value
type UpdateTaskTypeInput struct {
	Name        string
	Icon        **string
	Color       **string
	Description **string
}

// --- Task Status Service ---------------------------------------------------

// TaskStatusService defines task-status use cases.
type TaskStatusService interface {
	ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*TaskStatus, error)
	GetTaskStatus(ctx context.Context, id uuid.UUID) (*TaskStatus, error)
	CreateTaskStatus(ctx context.Context, in CreateTaskStatusInput) (*TaskStatus, error)
	// UpdateTaskStatus updates the task status identified by id, verifying it belongs to projectID.
	UpdateTaskStatus(ctx context.Context, projectID, id uuid.UUID, in UpdateTaskStatusInput) (*TaskStatus, error)
	// DeleteTaskStatus removes the task status identified by id, verifying it belongs to projectID.
	DeleteTaskStatus(ctx context.Context, projectID, id uuid.UUID) error
	// SetDefaultTaskStatus atomically marks statusID as the default for the project.
	SetDefaultTaskStatus(ctx context.Context, projectID, statusID uuid.UUID) (*TaskStatus, error)
	// ReorderTaskStatuses atomically persists a new display order, assigning
	// position = index in statusIDs to each status.
	ReorderTaskStatuses(ctx context.Context, projectID uuid.UUID, statusIDs []uuid.UUID) error
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
	ListTasks(ctx context.Context, projectID uuid.UUID, filter TaskFilter, pageSize int, sort TaskSort) ([]*Task, bool, error)
	CountTasks(ctx context.Context, projectID uuid.UUID, filter TaskFilter) (int64, error)
	// SumTaskField sums a numeric field across all matching tasks, ignoring pagination.
	// fieldKey is "story_points" or a custom field key.
	SumTaskField(ctx context.Context, projectID uuid.UUID, filter TaskFilter, fieldKey string) (float64, error)
	// GetTask returns the task identified by id, verifying it belongs to projectID.
	GetTask(ctx context.Context, projectID, id uuid.UUID) (*Task, error)
	GetTaskByNumber(ctx context.Context, projectID uuid.UUID, taskNumber int64) (*Task, error)
	CreateTask(ctx context.Context, in CreateTaskInput) (*Task, error)
	// UpdateTask updates the task identified by id, verifying it belongs to projectID.
	UpdateTask(ctx context.Context, projectID, id uuid.UUID, in UpdateTaskInput) (*Task, error)
	// DeleteTask removes the task identified by id, verifying it belongs to projectID.
	DeleteTask(ctx context.Context, projectID, id uuid.UUID) error
	// ListAssignedTasks returns open (non-done) tasks assigned to any of
	// memberIDs, across their respective projects — see
	// Repository.ListAssignedTasks for the keyset-pagination contract.
	ListAssignedTasks(ctx context.Context, memberIDs []uuid.UUID, limit int, cursorAfter *string) ([]*Task, bool, error)
	// CountOpenTasksByProjects returns the number of non-done tasks across all
	// of projectIDs in a single query — see Repository.CountOpenTasksByProjects.
	CountOpenTasksByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error)
}

// CreateTaskInput carries fields required to create a task.
type CreateTaskInput struct {
	ProjectID    uuid.UUID
	TaskTypeID   *uuid.UUID
	StatusID     *uuid.UUID
	SprintID     *uuid.UUID
	ParentTaskID *uuid.UUID
	Title        string
	Description  json.RawMessage
	Importance   int
	StoryPoints  *int
	AssigneeIDs  []uuid.UUID
	ReporterID   *uuid.UUID
	CustomFields map[string]any
	StartDate    *time.Time
	DueDate      *time.Time
	Tags         []string
}

// UpdateTaskInput carries mutable task fields for a PATCH operation.
// Title is applied when non-empty.
// For nullable reference fields, the double-pointer encodes three states:
//   - nil outer pointer  → field was absent in the request; do NOT overwrite
//   - non-nil outer pointer, inner pointer nil  → explicitly set to null (clear)
//   - non-nil outer pointer, inner pointer non-nil  → set to the given value
//
// For slice/map fields (Tags, CustomFields, AssigneeIDs), a nil pointer means
// the field was absent and should not be overwritten; a non-nil pointer (even
// to an empty slice/map) means the field was explicitly set and replaces the
// stored value in full.
type UpdateTaskInput struct {
	TaskTypeID   **uuid.UUID
	StatusID     **uuid.UUID
	SprintID     **uuid.UUID
	ParentTaskID **uuid.UUID
	Title        string
	Description  *json.RawMessage
	Importance   *int
	StoryPoints  **int
	AssigneeIDs  *[]uuid.UUID
	ReporterID   **uuid.UUID
	CustomFields *map[string]any
	StartDate    **time.Time
	DueDate      **time.Time
	Tags         *[]string
}

// --- Custom Field Definition Service --------------------------------------

// CustomFieldDefinitionService defines custom field definition use cases.
type CustomFieldDefinitionService interface {
	ListCustomFieldDefinitions(ctx context.Context, projectID uuid.UUID) ([]*CustomFieldDefinition, error)
	// GetCustomFieldDefinition returns the field definition identified by id, verifying it belongs to projectID.
	GetCustomFieldDefinition(ctx context.Context, projectID, id uuid.UUID) (*CustomFieldDefinition, error)
	CreateCustomFieldDefinition(ctx context.Context, in CreateCustomFieldDefinitionInput) (*CustomFieldDefinition, error)
	// UpdateCustomFieldDefinition updates the field definition identified by id, verifying it belongs to projectID.
	UpdateCustomFieldDefinition(ctx context.Context, projectID, id uuid.UUID, in UpdateCustomFieldDefinitionInput) (*CustomFieldDefinition, error)
	// DeleteCustomFieldDefinition removes the field definition identified by id, verifying it belongs to projectID.
	DeleteCustomFieldDefinition(ctx context.Context, projectID, id uuid.UUID) error
}

// CreateCustomFieldDefinitionInput carries fields required to create a custom
// field definition.
type CreateCustomFieldDefinitionInput struct {
	ProjectID   uuid.UUID
	FieldKey    string
	DisplayName string
	FieldType   FieldType
	Options     []CustomFieldOption
	IsRequired  bool
}

// UpdateCustomFieldDefinitionInput carries mutable custom field definition
// fields.
type UpdateCustomFieldDefinitionInput struct {
	DisplayName string
	FieldType   *FieldType
	Options     []CustomFieldOption
	IsRequired  *bool
}
