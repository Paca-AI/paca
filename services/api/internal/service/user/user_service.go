// Package user implements the user use-case service.
package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/paca/api/internal/domain/user"
	"golang.org/x/crypto/bcrypt"
)

// Service is the concrete implementation of domain/user.Service.
type Service struct {
	repo user.Repository
}

// New returns a configured user Service.
func New(repo user.Repository) *Service {
	return &Service{repo: repo}
}

// GetByID returns a user by primary key.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	return s.repo.FindByID(ctx, id)
}

// Create registers a new user with a hashed password.
func (s *Service) Create(ctx context.Context, in user.CreateInput) (*user.User, error) {
	_, err := s.repo.FindByEmail(ctx, in.Email)
	if err == nil {
		return nil, user.ErrEmailTaken
	}
	if !errors.Is(err, user.ErrNotFound) {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("user svc: hash password: %w", err)
	}

	now := time.Now()
	u := &user.User{
		ID:           uuid.New(),
		Email:        in.Email,
		PasswordHash: string(hash),
		Name:         in.Name,
		Role:         user.RoleUser,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// Update applies mutable profile changes to an existing user.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in user.UpdateInput) (*user.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	u.Name = in.Name
	u.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// Delete soft-deletes a user.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}
