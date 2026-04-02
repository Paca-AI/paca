// Package projectsvc implements project management application services.
package projectsvc

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
)

// Service is the concrete implementation of projectdom.Service.
type Service struct {
	repo projectdom.Repository
}

// New returns a configured project service.
func New(repo projectdom.Repository) *Service {
	return &Service{repo: repo}
}

// List returns a page of projects and the total count.
func (s *Service) List(ctx context.Context, page, pageSize int) ([]*projectdom.Project, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	return s.repo.List(ctx, offset, pageSize)
}

// GetByID returns the project with the given ID.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (*projectdom.Project, error) {
	return s.repo.FindByID(ctx, id)
}

// Create defines and persists a new project, bootstraps the three default
// project-scoped roles (admin, editor, viewer), and adds the creator as the
// project admin.
func (s *Service) Create(ctx context.Context, in projectdom.CreateProjectInput) (*projectdom.Project, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, projectdom.ErrNameInvalid
	}

	now := time.Now()
	p := &projectdom.Project{
		ID:          uuid.New(),
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		Settings:    cloneSettings(in.Settings),
		CreatedBy:   in.CreatedBy,
		CreatedAt:   now,
	}

	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}

	// Bootstrap the three default project-scoped roles.
	defaultRoles := []*projectdom.ProjectRole{
		{
			ID:        uuid.New(),
			ProjectID: &p.ID,
			RoleName:  "Admin",
			Permissions: map[string]any{
				"projects.*":        true,
				"project.members.*": true,
				"project.roles.*":   true,
				"tasks.*":           true,
				"sprints.*":         true,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        uuid.New(),
			ProjectID: &p.ID,
			RoleName:  "Editor",
			Permissions: map[string]any{
				"projects.read": true,
				"tasks.read":    true,
				"tasks.write":   true,
				"sprints.read":  true,
				"sprints.write": true,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        uuid.New(),
			ProjectID: &p.ID,
			RoleName:  "Viewer",
			Permissions: map[string]any{
				"projects.read": true,
				"tasks.read":    true,
				"sprints.read":  true,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
	}

	var adminRoleID uuid.UUID
	for _, r := range defaultRoles {
		if err := s.repo.CreateRole(ctx, r); err != nil {
			return nil, err
		}
		if r.RoleName == "Admin" {
			adminRoleID = r.ID
		}
	}

	// Add the creator as a project admin.
	if in.CreatedBy != nil {
		m := &projectdom.ProjectMember{
			ID:            uuid.New(),
			ProjectID:     p.ID,
			UserID:        *in.CreatedBy,
			ProjectRoleID: adminRoleID,
		}
		if err := s.repo.AddMember(ctx, m); err != nil {
			return nil, err
		}
	}

	return p, nil
}

// Update modifies an existing project's mutable fields.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in projectdom.UpdateProjectInput) (*projectdom.Project, error) {
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(in.Name)
	if name != "" {
		p.Name = name
	}
	desc := strings.TrimSpace(in.Description)
	if desc != "" {
		p.Description = desc
	}
	if in.Settings != nil {
		p.Settings = cloneSettings(in.Settings)
	}

	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Delete removes a project and all cascading records defined in the DB schema.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, id)
}

func cloneSettings(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
