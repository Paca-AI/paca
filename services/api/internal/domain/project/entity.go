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
	// JevAPIKeySecret is this project's Jev (AI decision API) API key,
	// encrypted at rest (see 000060_add_jev_integration.sql) — empty
	// means the project hasn't configured Jev. JevBaseURL/JevModel are
	// plain and optional; empty means "use platform/jev's TypeSafe
	// defaults". See service/project's encryptJevKey/decryptJevKey and
	// platform/jev.New's doc comment for how a third-party
	// provider (e.g. OpenJev, https://openjev.sh/docs) overrides these.
	JevAPIKeySecret string
	JevBaseURL      string
	JevModel        string
	// AvatarKey and AvatarThumbKey are object-storage keys for the two
	// server-generated avatar variants (256x256 full, 64x64 thumb). Both nil
	// when no avatar has been uploaded. See attachmentdom.AvatarService.
	AvatarKey      *string
	AvatarThumbKey *string
	CreatedBy      *uuid.UUID
	CreatedAt      time.Time
	DeletedAt      *time.Time // non-nil = soft-deleted
}
