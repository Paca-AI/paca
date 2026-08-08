// Package projectdom defines the project aggregate and its domain contracts.
package projectdom

import (
	"time"

	"github.com/google/uuid"
)

// Project is the core project aggregate.
type Project struct {
	ID           uuid.UUID
	Name         string
	Description  string
	TaskIDPrefix string
	IsPublic     bool
	Settings     map[string]any
	// AvatarKey and AvatarThumbKey are object-storage keys for the two
	// server-generated avatar variants (256x256 full, 64x64 thumb). Both nil
	// when no avatar has been uploaded. See attachmentdom.AvatarService.
	AvatarKey      *string
	AvatarThumbKey *string
	CreatedBy      *uuid.UUID
	CreatedAt      time.Time
	DeletedAt      *time.Time // non-nil = soft-deleted
}
