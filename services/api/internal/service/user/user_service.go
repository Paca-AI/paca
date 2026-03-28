// Package usersvc implements the user use-case service.
package usersvc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/platform/authz"
	"golang.org/x/crypto/bcrypt"
)

// GlobalPermissionReader resolves global permissions for a user.
type GlobalPermissionReader interface {
	ListGlobalPermissions(ctx context.Context, userID uuid.UUID) ([]authz.Permission, error)
}

// Service is the concrete implementation of domain/user.Service.
type Service struct {
	repo                   userdom.Repository
	globalPermissionReader GlobalPermissionReader
}

// New returns a configured user Service.
func New(repo userdom.Repository, globalPermissionReaders ...GlobalPermissionReader) *Service {
	var reader GlobalPermissionReader
	if len(globalPermissionReaders) > 0 {
		reader = globalPermissionReaders[0]
	}
	return &Service{repo: repo, globalPermissionReader: reader}
}

// GetByID returns a user by primary key.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*userdom.User, error) {
	return s.repo.FindByID(ctx, id)
}

// ListGlobalPermissions returns effective global permissions for the user.
func (s *Service) ListGlobalPermissions(ctx context.Context, id uuid.UUID) ([]string, error) {
	u, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	for _, p := range authz.LegacyPermissionsForRole(u.Role) {
		seen[string(p)] = struct{}{}
	}

	if s.globalPermissionReader != nil {
		perms, err := s.globalPermissionReader.ListGlobalPermissions(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, p := range perms {
			seen[string(p)] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)

	return out, nil
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
