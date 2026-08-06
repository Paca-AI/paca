// Package projectsvc implements project management application services.
package projectsvc

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/authz"
)

// prefixRe validates that a task ID prefix contains only uppercase letters and digits.
var prefixRe = regexp.MustCompile(`^[A-Z0-9]{1,10}$`)

// validatePrefix returns ErrPrefixInvalid if the provided prefix is non-empty
// but does not match the allowed pattern.
func validatePrefix(p string) error {
	if p != "" && !prefixRe.MatchString(p) {
		return projectdom.ErrPrefixInvalid
	}
	return nil
}

// suggestPrefix derives a short uppercase identifier from the project name.
// Rules (matches JIRA-style behavior):
//  1. Split by whitespace/hyphens/underscores.
//  2. If single word: first 4 letters (or all if shorter), stripped of non-alpha.
//  3. If multiple words: first letter of each word, up to 4, uppercase.
//  4. Remove non-alphanumeric characters and return uppercase.
func suggestPrefix(name string) string {
	// Strip non-alphanumeric/space characters (keep letters, digits, spaces).
	var sb strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			sb.WriteRune(r)
		}
	}
	clean := strings.TrimSpace(sb.String())
	words := strings.Fields(clean)
	if len(words) == 0 {
		return "PROJ"
	}
	var prefix string
	if len(words) == 1 {
		// Single word: up to 4 leading letters/digits.
		count := 0
		for _, r := range words[0] {
			if count >= 4 {
				break
			}
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				prefix += string(r)
				count++
			}
		}
	} else {
		// Multiple words: first letter of each, up to 4 words.
		for i, w := range words {
			if i >= 4 {
				break
			}
			for _, r := range w {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					prefix += string(r)
					break
				}
			}
		}
	}
	return strings.ToUpper(prefix)
}

// taskBootstrapper is the minimal persistence interface the project service
// needs to seed default task types and statuses at project creation time.
type taskBootstrapper interface {
	CreateTaskType(ctx context.Context, t *taskdom.TaskType) error
	CreateTaskStatus(ctx context.Context, s *taskdom.TaskStatus) error
}

// agentLookup is the minimal interface AddMember needs to validate and
// resolve an agent being invited into a project. Satisfied directly by
// *postgres.AgentRepository — this package depends only on the small
// agentdom domain type, not on the agentsvc service package, so there is no
// import cycle with agentsvc (which itself depends on projectsvc for member
// cache invalidation).
type agentLookup interface {
	FindAgentByID(ctx context.Context, id uuid.UUID) (*agentdom.Agent, error)
	FindAgentByHandle(ctx context.Context, projectID uuid.UUID, handle string) (*agentdom.Agent, error)
}

// Service is the concrete implementation of projectdom.Service.
type Service struct {
	repo     projectdom.Repository
	taskRepo taskBootstrapper
	agents   agentLookup
}

// New returns a configured project service.
func New(repo projectdom.Repository, taskRepo taskBootstrapper, agents agentLookup) *Service {
	return &Service{repo: repo, taskRepo: taskRepo, agents: agents}
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

	prefix := strings.ToUpper(strings.TrimSpace(in.TaskIDPrefix))
	if prefix == "" {
		prefix = suggestPrefix(name)
	}
	if err := validatePrefix(prefix); err != nil {
		return nil, err
	}

	now := time.Now()
	p := &projectdom.Project{
		ID:           uuid.New(),
		Name:         name,
		Description:  strings.TrimSpace(in.Description),
		TaskIDPrefix: prefix,
		IsPublic:     in.IsPublic,
		Settings:     cloneSettings(in.Settings),
		CreatedBy:    in.CreatedBy,
		CreatedAt:    now,
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
				string(authz.PermissionProjectsAll):       true,
				string(authz.PermissionProjectMembersAll): true,
				string(authz.PermissionProjectRolesAll):   true,
				string(authz.PermissionTasksAll):          true,
				string(authz.PermissionSprintsAll):        true,
				string(authz.PermissionDocsAll):           true,
				string(authz.PermissionAgentsAll):         true,
				string(authz.PermissionWorkflowsAll):      true,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        uuid.New(),
			ProjectID: &p.ID,
			RoleName:  "Editor",
			Permissions: map[string]any{
				string(authz.PermissionProjectsRead):       true,
				string(authz.PermissionProjectMembersRead): true,
				string(authz.PermissionProjectRolesRead):   true,
				string(authz.PermissionTasksRead):          true,
				string(authz.PermissionTasksWrite):         true,
				string(authz.PermissionSprintsRead):        true,
				string(authz.PermissionSprintsWrite):       true,
				string(authz.PermissionDocsRead):           true,
				string(authz.PermissionDocsWrite):          true,
				string(authz.PermissionAgentsRead):         true,
				string(authz.PermissionAgentsWrite):        true,
				string(authz.PermissionWorkflowsRead):      true,
				string(authz.PermissionWorkflowsWrite):     true,
			},
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        uuid.New(),
			ProjectID: &p.ID,
			RoleName:  "Viewer",
			Permissions: map[string]any{
				string(authz.PermissionProjectsRead):       true,
				string(authz.PermissionProjectMembersRead): true,
				string(authz.PermissionProjectRolesRead):   true,
				string(authz.PermissionTasksRead):          true,
				string(authz.PermissionSprintsRead):        true,
				string(authz.PermissionDocsRead):           true,
				string(authz.PermissionAgentsRead):         true,
				string(authz.PermissionWorkflowsRead):      true,
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

// seedDefaultTaskTypes creates the built-in task types for a new project.
// The "Task" type is the default. Epic is system-managed and read-only.
func (s *Service) seedDefaultTaskTypes(ctx context.Context, projectID uuid.UUID, now time.Time) error {
	defaults := []*taskdom.TaskType{
		{ID: uuid.New(), ProjectID: projectID, Name: "Task", Icon: ptr("CheckSquare"), Color: ptr("#3b82f6"), Description: ptr("A general work item that needs to be completed"), IsDefault: true, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), ProjectID: projectID, Name: "Bug", Icon: ptr("Bug"), Color: ptr("#ef4444"), Description: ptr("An issue or defect that needs to be fixed"), CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), ProjectID: projectID, Name: "Story", Icon: ptr("BookOpen"), Color: ptr("#22c55e"), Description: ptr("A user-facing feature or requirement"), CreatedAt: now, UpdatedAt: now},
		{ID: uuid.New(), ProjectID: projectID, Name: "Epic", Icon: ptr("Layers"), Color: ptr("#a855f7"), Description: ptr("A large body of work that can be broken down into smaller tasks"), IsSystem: true, CreatedAt: now, UpdatedAt: now},
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
		{ID: uuid.New(), ProjectID: projectID, Name: "Backlog", Color: ptr("#64748b"), Position: 1, Category: taskdom.StatusCategoryBacklog, IsDefault: true, CreatedAt: now, UpdatedAt: now},
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
	if rawPrefix := strings.ToUpper(strings.TrimSpace(in.TaskIDPrefix)); rawPrefix != "" {
		if err := validatePrefix(rawPrefix); err != nil {
			return nil, err
		}
		p.TaskIDPrefix = rawPrefix
	}
	if in.IsPublic != nil {
		p.IsPublic = *in.IsPublic
	}
	if in.Settings != nil {
		p.Settings = cloneSettings(in.Settings)
	}

	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// IsProjectPublic returns true when the project exists and has is_public set.
func (s *Service) IsProjectPublic(ctx context.Context, id uuid.UUID) (bool, error) {
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return false, err
	}
	return p.IsPublic, nil
}

// Delete soft-deletes a project by setting deleted_at.
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
