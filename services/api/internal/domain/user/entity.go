// Package userdom holds the user aggregate and its domain contracts.
package userdom

import (
	"time"

	"github.com/google/uuid"
)

// Role constants for user roles.
const (
	RoleUser  = "USER"
	RoleAdmin = "ADMIN"
)

// User is the core user aggregate.  PasswordHash must never leave the domain
// boundary; the transport layer uses DTOs without this field.
type User struct {
	ID           uuid.UUID
	Username     string
	PasswordHash string
	FullName     string
	// Email is optional — used for notification delivery (e.g. a mail
	// plugin's welcome/invite email), not for login.
	Email              *string
	MustChangePassword bool
	// RoleID is the foreign-key reference to the global_roles table.
	RoleID uuid.UUID
	// Role holds the role name populated by a JOIN on global_roles; it is not
	// stored directly in the users table.
	Role string
	// AvatarKey and AvatarThumbKey are object-storage keys for the two
	// server-generated avatar variants (256x256 full, 64x64 thumb). Both nil
	// when no avatar has been uploaded. See attachmentdom.AvatarService.
	AvatarKey      *string
	AvatarThumbKey *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}
