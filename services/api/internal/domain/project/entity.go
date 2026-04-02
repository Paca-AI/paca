// Package projectdom defines the project aggregate and its domain contracts.
package projectdom

import (
	"time"

	"github.com/google/uuid"
)

// Project is the core project aggregate.
type Project struct {
	ID          uuid.UUID
	Name        string
	Description string
	Settings    map[string]any
	CreatedBy   *uuid.UUID
	CreatedAt   time.Time
}
