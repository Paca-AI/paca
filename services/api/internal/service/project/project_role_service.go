package projectsvc

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
)

// ListRoles returns all roles defined for a project.
func (s *Service) ListRoles(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectRole, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}
	return s.repo.ListRoles(ctx, projectID)
}

// CreateRole adds a new role definition to a project.
func (s *Service) CreateRole(ctx context.Context, projectID uuid.UUID, in projectdom.CreateRoleInput) (*projectdom.ProjectRole, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}

	name := strings.TrimSpace(in.RoleName)
	if name == "" {
		return nil, projectdom.ErrRoleNameInvalid
	}

	if _, err := s.repo.FindRoleByName(ctx, projectID, name); err == nil {
		return nil, projectdom.ErrRoleNameTaken
	} else if !errors.Is(err, projectdom.ErrRoleNotFound) {
		return nil, err
	}

	now := time.Now()
	r := &projectdom.ProjectRole{
		ID:          uuid.New(),
		ProjectID:   &projectID,
		RoleName:    name,
		Permissions: cloneSettings(in.Permissions),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.repo.CreateRole(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// UpdateRole modifies a project-scoped role.
func (s *Service) UpdateRole(ctx context.Context, projectID, roleID uuid.UUID, in projectdom.UpdateRoleInput) (*projectdom.ProjectRole, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}

	r, err := s.repo.FindRoleByID(ctx, roleID)
	if err != nil {
		return nil, err
	}
	// Ensure the role belongs to this project.
	if r.ProjectID == nil || *r.ProjectID != projectID {
		return nil, projectdom.ErrRoleNotFound
	}

	name := strings.TrimSpace(in.RoleName)
	if name == "" {
		return nil, projectdom.ErrRoleNameInvalid
	}
	if !strings.EqualFold(name, r.RoleName) {
		existing, err := s.repo.FindRoleByName(ctx, projectID, name)
		if err == nil && existing.ID != r.ID {
			return nil, projectdom.ErrRoleNameTaken
		}
		if err != nil && !errors.Is(err, projectdom.ErrRoleNotFound) {
			return nil, err
		}
	}

	r.RoleName = name
	if in.Permissions != nil {
		r.Permissions = cloneSettings(in.Permissions)
	}
	r.UpdatedAt = time.Now()

	if err := s.repo.UpdateRole(ctx, r); err != nil {
		return nil, err
	}
	return r, nil
}

// DeleteRole removes a project-scoped role. It fails if members still have this role.
func (s *Service) DeleteRole(ctx context.Context, projectID, roleID uuid.UUID) error {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return err
	}

	r, err := s.repo.FindRoleByID(ctx, roleID)
	if err != nil {
		return err
	}
	if r.ProjectID == nil || *r.ProjectID != projectID {
		return projectdom.ErrRoleNotFound
	}

	count, err := s.repo.CountMembersWithRole(ctx, roleID)
	if err != nil {
		return err
	}
	if count > 0 {
		return projectdom.ErrRoleHasMembers
	}
	return s.repo.DeleteRole(ctx, roleID)
}
