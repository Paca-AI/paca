// Package usersvc implements the user use-case service.
package usersvc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
	"golang.org/x/crypto/bcrypt"
)

// Service is the concrete implementation of domain/user.Service.
type Service struct {
	repo userdom.Repository
}

// New returns a configured user Service.
func New(repo userdom.Repository) *Service {
	return &Service{repo: repo}
}

// GetByID returns a user by primary key.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*userdom.User, error) {
	return s.repo.FindByID(ctx, id)
}

// Create registers a new user with a hashed password.
func (s *Service) Create(ctx context.Context, in userdom.CreateInput) (*userdom.User, error) {
	// Check username uniqueness.
	_, err := s.repo.FindByUsername(ctx, in.Username)
	if err == nil {
		return nil, userdom.ErrUsernameTaken
	}
	if !errors.Is(err, userdom.ErrNotFound) {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("user svc: hash password: %w", err)
	}

	now := time.Now()
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     in.Username,
		PasswordHash: string(hash),
		FullName:     in.FullName,
		Role:         userdom.RoleUser,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.repo.Create(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// Update applies mutable profile changes to an existing user.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in userdom.UpdateInput) (*userdom.User, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	u.FullName = in.FullName
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
