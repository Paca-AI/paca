package userdom

import (
	"context"

	"github.com/google/uuid"
)

// Repository defines persistence operations for the user aggregate.
type Repository interface {
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	FindByUsername(ctx context.Context, username string) (*User, error)
	// FindByUsernameIncludingDeleted returns a user by username even when
	// the row is soft-deleted.
	FindByUsernameIncludingDeleted(ctx context.Context, username string) (*User, error)
	// List returns a page of users and the total count of all users.
	List(ctx context.Context, offset, limit int) ([]*User, int64, error)
	Create(ctx context.Context, u *User) error
	Update(ctx context.Context, u *User) error
	Delete(ctx context.Context, id uuid.UUID) error
}
