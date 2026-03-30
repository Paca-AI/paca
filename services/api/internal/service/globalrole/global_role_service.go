// Package globalrolesvc implements global role application services.
package globalrolesvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
)

// Service is the concrete implementation of globalrole.Service.
type Service struct {
	repo globalroledom.Repository
}

// New returns a configured global role service.
func New(repo globalroledom.Repository) *Service {
	return &Service{repo: repo}
}

// List returns all global role definitions.
func (s *Service) List(ctx context.Context) ([]*globalroledom.GlobalRole, error) {
	return s.repo.List(ctx)
}

// Create defines and persists a new global role.
func (s *Service) Create(ctx context.Context, in globalroledom.CreateInput) (*globalroledom.GlobalRole, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, globalroledom.ErrInvalidName
	}

	_, err := s.repo.FindByName(ctx, name)
	if err == nil {
		return nil, globalroledom.ErrNameTaken
	}
	if !errors.Is(err, globalroledom.ErrNotFound) {
		return nil, err
	}

	now := time.Now()
	role := &globalroledom.GlobalRole{
		ID:          uuid.New(),
		Name:        name,
		Permissions: clonePermissions(in.Permissions),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.Create(ctx, role); err != nil {
		return nil, err
	}
	return role, nil
}

// Update modifies an existing global role.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in globalroledom.UpdateInput) (*globalroledom.GlobalRole, error) {
	role, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, globalroledom.ErrInvalidName
	}
	if !strings.EqualFold(name, role.Name) {
		existing, err := s.repo.FindByName(ctx, name)
		if err == nil && existing.ID != role.ID {
			return nil, globalroledom.ErrNameTaken
		}
		if err != nil && !errors.Is(err, globalroledom.ErrNotFound) {
			return nil, err
		}
	}

	role.Name = name
	if in.Permissions != nil {
		role.Permissions = clonePermissions(in.Permissions)
	}
	role.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, role); err != nil {
		return nil, err
	}
	return role, nil
}

// Delete removes a global role definition.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

// ReplaceUserRoles replaces all global-role assignments for the target user.
func (s *Service) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) ([]*globalroledom.GlobalRole, error) {
	if err := s.repo.ReplaceUserRoles(ctx, userID, roleIDs); err != nil {
		return nil, err
	}
	return s.repo.ListUserRoles(ctx, userID)
}

func clonePermissions(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
