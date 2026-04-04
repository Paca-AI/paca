// Package tasksvc implements task management application services.
package tasksvc

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	taskdom "github.com/paca/api/internal/domain/task"
)

// Service is the concrete implementation of taskdom.Service.
type Service struct {
	repo taskdom.Repository
}

// New returns a configured task service.
func New(repo taskdom.Repository) *Service {
	return &Service{repo: repo}
}

// --- Task Types -------------------------------------------------------------

// ListTaskTypes returns all task types for a project.
func (s *Service) ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error) {
	return s.repo.ListTaskTypes(ctx, projectID)
}

// GetTaskType returns the task type with the given ID.
func (s *Service) GetTaskType(ctx context.Context, id uuid.UUID) (*taskdom.TaskType, error) {
	return s.repo.FindTaskTypeByID(ctx, id)
}

// CreateTaskType creates a new task type for the given project.
func (s *Service) CreateTaskType(ctx context.Context, in taskdom.CreateTaskTypeInput) (*taskdom.TaskType, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, taskdom.ErrTypeNameInvalid
	}

	now := time.Now()
	t := &taskdom.TaskType{
		ID:          uuid.New(),
		ProjectID:   in.ProjectID,
		Name:        name,
		Icon:        in.Icon,
		Color:       in.Color,
		Description: in.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.CreateTaskType(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// UpdateTaskType updates the mutable fields of an existing task type.
func (s *Service) UpdateTaskType(ctx context.Context, id uuid.UUID, in taskdom.UpdateTaskTypeInput) (*taskdom.TaskType, error) {
	t, err := s.repo.FindTaskTypeByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if name := strings.TrimSpace(in.Name); name != "" {
		t.Name = name
	}
	t.Icon = in.Icon
	t.Color = in.Color
	t.Description = in.Description
	t.UpdatedAt = time.Now()

	if err := s.repo.UpdateTaskType(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// DeleteTaskType removes a task type by ID.
func (s *Service) DeleteTaskType(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.FindTaskTypeByID(ctx, id); err != nil {
		return err
	}
	return s.repo.DeleteTaskType(ctx, id)
}

// --- Task Statuses ----------------------------------------------------------

// ListTaskStatuses returns all task statuses for a project.
func (s *Service) ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskStatus, error) {
	return s.repo.ListTaskStatuses(ctx, projectID)
}

// GetTaskStatus returns the task status with the given ID.
func (s *Service) GetTaskStatus(ctx context.Context, id uuid.UUID) (*taskdom.TaskStatus, error) {
	return s.repo.FindTaskStatusByID(ctx, id)
}

// CreateTaskStatus creates a new task status for the given project.
func (s *Service) CreateTaskStatus(ctx context.Context, in taskdom.CreateTaskStatusInput) (*taskdom.TaskStatus, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, taskdom.ErrStatusNameInvalid
	}
	if !taskdom.ValidStatusCategories[in.Category] {
		return nil, taskdom.ErrStatusCategoryInvalid
	}

	now := time.Now()
	st := &taskdom.TaskStatus{
		ID:        uuid.New(),
		ProjectID: in.ProjectID,
		Name:      name,
		Color:     in.Color,
		Position:  in.Position,
		Category:  in.Category,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.CreateTaskStatus(ctx, st); err != nil {
		return nil, err
	}
	return st, nil
}

// UpdateTaskStatus updates the mutable fields of an existing task status.
func (s *Service) UpdateTaskStatus(ctx context.Context, id uuid.UUID, in taskdom.UpdateTaskStatusInput) (*taskdom.TaskStatus, error) {
	st, err := s.repo.FindTaskStatusByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if name := strings.TrimSpace(in.Name); name != "" {
		st.Name = name
	}
	st.Color = in.Color
	if in.Position != nil {
		st.Position = *in.Position
	}
	if in.Category != nil {
		if !taskdom.ValidStatusCategories[*in.Category] {
			return nil, taskdom.ErrStatusCategoryInvalid
		}
		st.Category = *in.Category
	}
	st.UpdatedAt = time.Now()

	if err := s.repo.UpdateTaskStatus(ctx, st); err != nil {
		return nil, err
	}
	return st, nil
}

// DeleteTaskStatus removes a task status by ID.
func (s *Service) DeleteTaskStatus(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.FindTaskStatusByID(ctx, id); err != nil {
		return err
	}
	return s.repo.DeleteTaskStatus(ctx, id)
}

// --- Tasks ------------------------------------------------------------------

// ListTasks returns a page of tasks for a project with optional filters.
func (s *Service) ListTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, page, pageSize int) ([]*taskdom.Task, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	return s.repo.ListTasks(ctx, projectID, filter, offset, pageSize)
}

// GetTask returns the task with the given ID.
func (s *Service) GetTask(ctx context.Context, id uuid.UUID) (*taskdom.Task, error) {
	return s.repo.FindTaskByID(ctx, id)
}

// CreateTask creates a new task.
func (s *Service) CreateTask(ctx context.Context, in taskdom.CreateTaskInput) (*taskdom.Task, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, taskdom.ErrTaskTitleInvalid
	}

	cf := in.CustomFields
	if cf == nil {
		cf = map[string]any{}
	}

	now := time.Now()
	t := &taskdom.Task{
		ID:            uuid.New(),
		ProjectID:     in.ProjectID,
		TaskTypeID:    in.TaskTypeID,
		StatusID:      in.StatusID,
		SprintID:      in.SprintID,
		ParentTaskID:  in.ParentTaskID,
		Title:         title,
		Description:   in.Description,
		Importance:    in.Importance,
		BoardPosition: in.BoardPosition,
		AssigneeID:    in.AssigneeID,
		ReporterID:    in.ReporterID,
		CustomFields:  cf,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := s.repo.CreateTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// UpdateTask updates the mutable fields of an existing task.
func (s *Service) UpdateTask(ctx context.Context, id uuid.UUID, in taskdom.UpdateTaskInput) (*taskdom.Task, error) {
	t, err := s.repo.FindTaskByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if title := strings.TrimSpace(in.Title); title != "" {
		t.Title = title
	}
	t.TaskTypeID = in.TaskTypeID
	t.StatusID = in.StatusID
	t.SprintID = in.SprintID
	t.ParentTaskID = in.ParentTaskID
	t.Description = in.Description
	if in.Importance != nil {
		t.Importance = *in.Importance
	}
	if in.BoardPosition != nil {
		t.BoardPosition = *in.BoardPosition
	}
	t.AssigneeID = in.AssigneeID
	t.ReporterID = in.ReporterID
	if in.CustomFields != nil {
		t.CustomFields = in.CustomFields
	}
	t.UpdatedAt = time.Now()

	if err := s.repo.UpdateTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// DeleteTask soft-deletes a task by ID.
func (s *Service) DeleteTask(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteTask(ctx, id)
}
