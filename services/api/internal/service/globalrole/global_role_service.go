// Package globalrolesvc implements global role application services.
package globalrolesvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
)

// agentRoleCounter is the minimal interface Delete needs to also guard
// against deleting a global role still assigned to a global agent, the same
// way it already guards against users. Satisfied directly by
// *postgres.AgentRepository — this package depends on no agent domain type.
type agentRoleCounter interface {
	CountAgentsWithGlobalRole(ctx context.Context, roleID uuid.UUID) (int64, error)
}

// Service is the concrete implementation of globalrole.Service.
type Service struct {
	repo   globalroledom.Repository
	agents agentRoleCounter
}

// New returns a configured global role service.
func New(repo globalroledom.Repository, agents agentRoleCounter) *Service {
	return &Service{repo: repo, agents: agents}
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

// Delete removes a global role definition. It returns ErrHasAssignedUsers if
// any user or global agent currently references this role via their
// assigned global role (users.role_id or agents.global_role_id).
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	count, err := s.repo.CountUsersWithRole(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return globalroledom.ErrHasAssignedUsers
	}
	if s.agents != nil {
		agentCount, err := s.agents.CountAgentsWithGlobalRole(ctx, id)
		if err != nil {
			return err
		}
		if agentCount > 0 {
			return globalroledom.ErrHasAssignedUsers
		}
	}
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
