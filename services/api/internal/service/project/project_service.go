// Package projectsvc implements project management application services.
package projectsvc

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
	taskdom "github.com/paca/api/internal/domain/task"
)

// taskBootstrapper is the minimal persistence interface the project service
// needs to seed default task types and statuses at project creation time.
type taskBootstrapper interface {
	CreateTaskType(ctx context.Context, t *taskdom.TaskType) error
	CreateTaskStatus(ctx context.Context, s *taskdom.TaskStatus) error
}

// Service is the concrete implementation of projectdom.Service.
type Service struct {
	repo     projectdom.Repository
	taskRepo taskBootstrapper
}

// New returns a configured project service.
func New(repo projectdom.Repository, taskRepo taskBootstrapper) *Service {
	return &Service{repo: repo, taskRepo: taskRepo}
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

// ListAccessible returns only the projects the given user is a member of.
func (s *Service) ListAccessible(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]*projectdom.Project, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	return s.repo.ListAccessible(ctx, userID, offset, pageSize)
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

	// Bootstrap default task types and statuses.
	if s.taskRepo != nil {
		if err := s.seedDefaultTaskTypes(ctx, p.ID, now); err != nil {
			return nil, err
		}
		if err := s.seedDefaultTaskStatuses(ctx, p.ID, now); err != nil {
			return nil, err
		}
	}

	return p, nil
}

func ptr[T any](v T) *T { return &v }

// seedDefaultTaskTypes creates the three built-in task types for a new project.
func (s *Service) seedDefaultTaskTypes(ctx context.Context, projectID uuid.UUID, now time.Time) error {
	defaults := []*taskdom.TaskType{
		{ID: uuid.New(), ProjectID: projectID, Name: "Task", Icon: ptr("CheckSquare"), Color: ptr("#3b82f6"), CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), ProjectID: projectID, Name: "Bug", Icon: ptr("Bug"), Color: ptr("#ef4444"), CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), ProjectID: projectID, Name: "Story", Icon: ptr("BookOpen"), Color: ptr("#22c55e"), CreatedAt: now, UpdatedAt: now},
	}
	for _, tt := range defaults {
		if err := s.taskRepo.CreateTaskType(ctx, tt); err != nil {
			return err
		}
	}
	return nil
}

// seedDefaultTaskStatuses creates the four built-in task statuses for a new project.
func (s *Service) seedDefaultTaskStatuses(ctx context.Context, projectID uuid.UUID, now time.Time) error {
	defaults := []*taskdom.TaskStatus{
		{ID: uuid.New(), ProjectID: projectID, Name: "Backlog", Color: ptr("#64748b"), Position: 1, Category: taskdom.StatusCategoryBacklog, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), ProjectID: projectID, Name: "Todo", Color: ptr("#eab308"), Position: 2, Category: taskdom.StatusCategoryTodo, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), ProjectID: projectID, Name: "In Progress", Color: ptr("#3b82f6"), Position: 3, Category: taskdom.StatusCategoryInProgress, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), ProjectID: projectID, Name: "Done", Color: ptr("#22c55e"), Position: 4, Category: taskdom.StatusCategoryDone, CreatedAt: now, UpdatedAt: now},
	}
	for _, ts := range defaults {
		if err := s.taskRepo.CreateTaskStatus(ctx, ts); err != nil {
			return err
		}
	}
	return nil
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
