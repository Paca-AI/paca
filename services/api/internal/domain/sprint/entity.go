// Package sprintdom defines the sprint aggregate and its domain contracts.
package sprintdom

import (
	"time"

	"github.com/google/uuid"
)

// SprintStatus describes the lifecycle state of a Sprint.
type SprintStatus string

// SprintStatus constants for planned, active, and completed sprints.
const (
	SprintStatusPlanned   SprintStatus = "planned"
	SprintStatusActive    SprintStatus = "active"
	SprintStatusCompleted SprintStatus = "completed"
)

// ValidSprintStatuses is the set of allowed sprint status values.
var ValidSprintStatuses = map[SprintStatus]bool{
	SprintStatusPlanned:   true,
	SprintStatusActive:    true,
	SprintStatusCompleted: true,
}

// Sprint is a time-boxed iteration containing a set of tasks.
type Sprint struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Name      string
	StartDate *time.Time
	EndDate   *time.Time
	Goal      *string
	Status    SprintStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}
