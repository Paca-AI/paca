package user

import (
	"context"

	"github.com/google/uuid"
)

// CreateInput carries the data needed to register a new user.
type CreateInput struct {
	Email    string
	Password string
	Name     string
}

// UpdateInput carries the fields that may be updated by the user.
type UpdateInput struct {
	Name string
}

// Service defines the user use-case contract.
type Service interface {
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	Create(ctx context.Context, in CreateInput) (*User, error)
	Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*User, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
