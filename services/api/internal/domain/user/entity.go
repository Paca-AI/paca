// Package userdom holds the user aggregate and its domain contracts.
package userdom

import (
	"strings"
	"time"

	"github.com/google/uuid"

	roledom "github.com/Paca-AI/api/internal/domain/role"
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
	// Roles are the platform roles attached to the user (platform-wide
	// attachments, sorted by name), populated by the repository on reads.
	//
	// On Create they are the roles to attach to the new account; left empty,
	// the account starts with the default role (roledom.ErrNoDefault when
	// none is set). They are never written by Update: roles change only
	// through the attachment endpoints.
	Roles []roledom.Summary
	// AvatarKey and AvatarThumbKey are object-storage keys for the two
	// server-generated avatar variants (256x256 full, 64x64 thumb). Both nil
	// when no avatar has been uploaded. See attachmentdom.AvatarService.
	AvatarKey      *string
	AvatarThumbKey *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

// RoleNames returns the names of the user's platform roles.
func (u *User) RoleNames() []string {
	out := make([]string, 0, len(u.Roles))
	for _, r := range u.Roles {
		out = append(out, r.Name)
	}
	return out
}

// RoleClaim is the value of the "role" claim in the user's tokens (and of the
// caller role a plugin sees): the names of the platform roles, comma-joined.
// It is informational only; every authorization decision reads the stored
// role attachments, never the token.
func (u *User) RoleClaim() string {
	return strings.Join(u.RoleNames(), ",")
}
