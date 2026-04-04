// Package taskdom defines the task aggregate and its domain contracts.
package taskdom

import (
	"time"

	"github.com/google/uuid"
)

// StatusCategory describes which phase of the workflow a TaskStatus belongs to.
type StatusCategory string

// StatusCategory constants for task workflow phases.
const (
	StatusCategoryBacklog    StatusCategory = "backlog"
	StatusCategoryRefinement StatusCategory = "refinement"
	StatusCategoryReady      StatusCategory = "ready"
	StatusCategoryTodo       StatusCategory = "todo"
	StatusCategoryInProgress StatusCategory = "inprogress"
	StatusCategoryDone       StatusCategory = "done"
)

// ValidStatusCategories is the set of allowed status category values.
var ValidStatusCategories = map[StatusCategory]bool{
	StatusCategoryBacklog:    true,
	StatusCategoryRefinement: true,
	StatusCategoryReady:      true,
	StatusCategoryTodo:       true,
	StatusCategoryInProgress: true,
	StatusCategoryDone:       true,
}

// TaskType categorises tasks within a project.
type TaskType struct {
	ID          uuid.UUID
	ProjectID   uuid.UUID
	Name        string
	Icon        *string
	Color       *string
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TaskStatus represents a column in the project's status workflow.
type TaskStatus struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	Color     *string
	Position  int
	Category  StatusCategory
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Task is the core work item aggregate.
type Task struct {
	ID            uuid.UUID
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
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
}
