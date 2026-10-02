package userdom

import (
	"context"

	"github.com/google/uuid"
)

// ListFilter narrows a user listing. The zero value matches every user.
type ListFilter struct {
	// Search is split on whitespace; every word must appear (case-insensitive)
	// in the username, full name or email.
	Search string
	// Role, when non-empty, is the exact global role name to match.
	Role string
}

// Repository defines persistence operations for the user aggregate.
type Repository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	FindByUsername(ctx context.Context, username string) (*User, error)
	// FindByEmail returns a user by email, scoped to active (non-deleted)
	// users, matching the uni_users_email_active unique index. The match
	// ignores case.
	FindByEmail(ctx context.Context, email string) (*User, error)
	// FindByUsernameIncludingDeleted returns a user by username even when
	// the row is soft-deleted.
	FindByUsernameIncludingDeleted(ctx context.Context, username string) (*User, error)
	// List returns a page of users matching filter, sorted by name, and the
	// total count of users matching filter (not just this page).
	List(ctx context.Context, offset, limit int, filter ListFilter) ([]*User, int64, error)
	// CountUsers returns the total count of active, non-system users — the
	// same count List returns as its total, without paginating any rows.
	// Used by the home page's workspace stats widget for team-member count.
	CountUsers(ctx context.Context) (int64, error)
	// CountUsersMustChangePassword returns the total count of active,
	// non-system users with MustChangePassword set — the same filter as
	// CountUsers, plus the must-change-password condition. Used by the admin
	// users page's stats bar so the count reflects every user, not just
	// whichever page is currently displayed.
	CountUsersMustChangePassword(ctx context.Context) (int64, error)
	Create(ctx context.Context, u *User) error
	Update(ctx context.Context, u *User) error
	Delete(ctx context.Context, id uuid.UUID) error
}
