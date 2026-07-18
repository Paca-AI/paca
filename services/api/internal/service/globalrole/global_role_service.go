// Package globalrolesvc implements global role application services.
package globalrolesvc

import (
	"context"
	"errors"
	"strings"
	"time"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/google/uuid"
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
//
// Grant ceiling (PACA-3): the new role may not grant any permission the caller
// does not itself hold. This prevents a caller with global_roles.write (but
// less than full authority) from minting a role — e.g. one carrying "*" — that
// exceeds their own permissions and then escalating through it.
func (s *Service) Create(ctx context.Context, in globalroledom.CreateInput, caller authz.PermissionSet) (*globalroledom.GlobalRole, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, globalroledom.ErrInvalidName
	}

	if err := enforceGrantCeiling(caller, in.Permissions); err != nil {
		return nil, err
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
//
// Grant ceiling (PACA-3): when the update sets a new permission map, the
// caller may not grant any permission they do not themselves hold.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in globalroledom.UpdateInput, caller authz.PermissionSet) (*globalroledom.GlobalRole, error) {
	role, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, globalroledom.ErrInvalidName
	}

	if in.Permissions != nil {
		if err := enforceGrantCeiling(caller, in.Permissions); err != nil {
			return nil, err
		}
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
// any user currently references this role via their assigned global role
// (for example, through users.role_id).
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	count, err := s.repo.CountUsersWithRole(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return globalroledom.ErrHasAssignedUsers
	}
	return s.repo.Delete(ctx, id)
}

// ReplaceUserRoles replaces all global-role assignments for the target user.
//
// Grant ceiling (PACA-3): the caller may only assign roles whose combined
// permission set is a subset of the caller's own effective permissions.
// Otherwise a caller could hand another user (or themselves) a role broader
// than their own authority — a self-escalation. Each target role is resolved
// so its permissions can be checked before anything is written.
func (s *Service) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID, caller authz.PermissionSet) ([]*globalroledom.GlobalRole, error) {
	if !caller.HasAll() {
		for _, roleID := range roleIDs {
			role, err := s.repo.FindByID(ctx, roleID)
			if err != nil {
				return nil, err
			}
			if err := enforceGrantCeiling(caller, role.Permissions); err != nil {
				return nil, err
			}
		}
	}

	if err := s.repo.ReplaceUserRoles(ctx, userID, roleIDs); err != nil {
		return nil, err
	}
	return s.repo.ListUserRoles(ctx, userID)
}

// enforceGrantCeiling returns ErrPermissionCeilingExceeded when the given role
// permission map grants any permission the caller does not itself hold. A
// caller holding "*" (SUPER_ADMIN) covers everything and always passes.
func enforceGrantCeiling(caller authz.PermissionSet, rolePerms map[string]any) error {
	if caller.HasAll() {
		return nil
	}
	if !caller.Covers(authz.PermissionsFromMap(rolePerms)) {
		return globalroledom.ErrPermissionCeilingExceeded
	}
	return nil
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
