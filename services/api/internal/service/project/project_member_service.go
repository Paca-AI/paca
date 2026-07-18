package projectsvc

import (
	"context"
	"errors"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/google/uuid"
)

// enforceRoleGrantCeiling returns ErrPermissionCeilingExceeded when the target
// project role grants any permission the caller does not itself hold for the
// project. A caller holding "*" (SUPER_ADMIN / project owner via legacy) covers
// everything and always passes. This is the PACA-4 grant ceiling: it stops a
// member with only project.members.write from assigning a broader role (such as
// the shared PROJECT_OWNER template) to escalate privileges.
func enforceRoleGrantCeiling(caller authz.PermissionSet, role *projectdom.ProjectRole) error {
	if caller.HasAll() {
		return nil
	}
	if !caller.Covers(authz.PermissionsFromMap(role.Permissions)) {
		return projectdom.ErrPermissionCeilingExceeded
	}
	return nil
}

// ListMembers returns all members of the given project.
func (s *Service) ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}
	return s.repo.ListMembers(ctx, projectID)
}

// AddMember adds a user to a project with the specified role.
func (s *Service) AddMember(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput, caller authz.PermissionSet) (*projectdom.ProjectMember, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}

	role, err := s.repo.FindRoleByID(ctx, in.ProjectRoleID)
	if err != nil {
		return nil, err
	}
	// Ensure the role belongs to this project (or is a template).
	if role.ProjectID != nil && *role.ProjectID != projectID {
		return nil, projectdom.ErrRoleNotFound
	}

	// Grant ceiling (PACA-4): the caller may not assign a role broader than
	// their own effective permissions for this project.
	if err := enforceRoleGrantCeiling(caller, role); err != nil {
		return nil, err
	}

	_, err = s.repo.FindMember(ctx, projectID, in.UserID)
	if err == nil {
		return nil, projectdom.ErrMemberAlreadyAdded
	}
	if !errors.Is(err, projectdom.ErrMemberNotFound) {
		return nil, err
	}

	m := &projectdom.ProjectMember{
		ID:            uuid.New(),
		ProjectID:     projectID,
		UserID:        in.UserID,
		ProjectRoleID: in.ProjectRoleID,
	}
	if err := s.repo.AddMember(ctx, m); err != nil {
		return nil, err
	}

	// Re-fetch to populate username/role name via JOIN.
	added, err := s.repo.FindMember(ctx, projectID, in.UserID)
	if err != nil {
		return nil, err
	}
	return added, nil
}

// UpdateMemberRole changes the role of an existing project member.
func (s *Service) UpdateMemberRole(ctx context.Context, projectID, userID uuid.UUID, in projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}
	if _, err := s.repo.FindMember(ctx, projectID, userID); err != nil {
		return nil, err
	}

	role, err := s.repo.FindRoleByID(ctx, in.ProjectRoleID)
	if err != nil {
		return nil, err
	}
	// Ensure the role belongs to this project (or is a template).
	if role.ProjectID != nil && *role.ProjectID != projectID {
		return nil, projectdom.ErrRoleNotFound
	}

	if err := s.repo.UpdateMemberRole(ctx, projectID, userID, in.ProjectRoleID); err != nil {
		return nil, err
	}

	return s.repo.FindMember(ctx, projectID, userID)
}

// RemoveMember removes a user from the project.
func (s *Service) RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return err
	}
	if _, err := s.repo.FindMember(ctx, projectID, userID); err != nil {
		return err
	}
	return s.repo.RemoveMember(ctx, projectID, userID)
}

// GetMyProjectPermissions returns the effective permission map of the calling
// user's project role. Returns ErrMemberNotFound when the user is not a member.
// If agentID is provided, looks up the agent's permissions instead.
func (s *Service) GetMyProjectPermissions(ctx context.Context, projectID, userID uuid.UUID, agentID *uuid.UUID) (map[string]any, error) {
	var member *projectdom.ProjectMember
	var err error

	if agentID != nil {
		member, err = s.repo.FindMemberByAgent(ctx, projectID, *agentID)
	} else {
		member, err = s.repo.FindMember(ctx, projectID, userID)
	}

	if err != nil {
		return nil, err
	}
	role, err := s.repo.FindRoleByID(ctx, member.ProjectRoleID)
	if err != nil {
		return nil, err
	}
	perms := role.Permissions
	if perms == nil {
		perms = map[string]any{}
	}
	return perms, nil
}

// AddAgentMember inserts an agent as a project member with the given role.
func (s *Service) AddAgentMember(ctx context.Context, memberID, projectID, agentID, roleID uuid.UUID) error {
	return s.repo.AddAgentMember(ctx, memberID, projectID, agentID, roleID)
}

// RemoveAgentMember soft-deletes the agent's membership record.
func (s *Service) RemoveAgentMember(ctx context.Context, projectID, agentID uuid.UUID) error {
	return s.repo.RemoveAgentMember(ctx, projectID, agentID)
}

// UpdateMemberRoleByMemberID changes the role of an existing project member by member ID.
func (s *Service) UpdateMemberRoleByMemberID(ctx context.Context, projectID, memberID uuid.UUID, in projectdom.UpdateMemberRoleInput, caller authz.PermissionSet) (*projectdom.ProjectMember, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}

	member, err := s.repo.FindMemberByID(ctx, memberID)
	if err != nil {
		return nil, err
	}

	if member.ProjectID != projectID {
		return nil, projectdom.ErrMemberNotFound
	}

	role, err := s.repo.FindRoleByID(ctx, in.ProjectRoleID)
	if err != nil {
		return nil, err
	}
	// Ensure the role belongs to this project (or is a template).
	if role.ProjectID != nil && *role.ProjectID != projectID {
		return nil, projectdom.ErrRoleNotFound
	}

	// Grant ceiling (PACA-4): the caller may not promote a member into a role
	// broader than their own effective permissions for this project.
	if err := enforceRoleGrantCeiling(caller, role); err != nil {
		return nil, err
	}

	if err := s.repo.UpdateMemberRoleByMemberID(ctx, memberID, in.ProjectRoleID); err != nil {
		return nil, err
	}

	return s.repo.FindMemberByID(ctx, memberID)
}

// RemoveMemberByMemberID removes a project member by member ID.
func (s *Service) RemoveMemberByMemberID(ctx context.Context, projectID, memberID uuid.UUID) error {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return err
	}

	member, err := s.repo.FindMemberByID(ctx, memberID)
	if err != nil {
		return err
	}

	if member.ProjectID != projectID {
		return projectdom.ErrMemberNotFound
	}

	return s.repo.RemoveMemberByMemberID(ctx, memberID)
}
