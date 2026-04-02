package projectsvc

import (
	"context"
	"errors"

	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
)

// ListMembers returns all members of the given project.
func (s *Service) ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}
	return s.repo.ListMembers(ctx, projectID)
}

// AddMember adds a user to a project with the specified role.
func (s *Service) AddMember(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput) (*projectdom.ProjectMember, error) {
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
		return m, nil
	}
	return added, nil
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
