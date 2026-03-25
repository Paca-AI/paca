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
	Role         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}
