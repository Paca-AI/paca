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
	ID                 uuid.UUID
	Username           string
	PasswordHash       string
	FullName           string
	MustChangePassword bool
	// RoleID is the foreign-key reference to the global_roles table.
	RoleID uuid.UUID
	// Role holds the role name populated by a JOIN on global_roles; it is not
	// stored directly in the users table.
	Role      string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}
