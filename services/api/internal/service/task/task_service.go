// Package tasksvc implements task management application services.
package tasksvc

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

var reservedSystemTypeNames = map[string]bool{
	"Epic": true,
}

// automationStatusChecker is the minimal automation-domain surface the task
// service needs to refuse deleting a status that automation still depends on.
type automationStatusChecker interface {
	StatusUsedByAutomation(ctx context.Context, statusID uuid.UUID) (bool, error)
}

// Service is the concrete implementation of taskdom.Service.
type Service struct {
	repo              taskdom.Repository
	automationChecker automationStatusChecker
}

// New returns a configured task service.
func New(repo taskdom.Repository) *Service {
	return &Service{repo: repo}
}

// WithAutomationStatusChecker configures a check that refuses to delete a
// task status still referenced by an automation's node config. Without it,
// DeleteTaskStatus does not guard against this (e.g. in tests).
func (s *Service) WithAutomationStatusChecker(checker automationStatusChecker) *Service {
	s.automationChecker = checker
	return s
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
	if reservedSystemTypeNames[name] {
		return nil, taskdom.ErrTypeNameReserved
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
func (s *Service) UpdateTaskType(ctx context.Context, projectID, id uuid.UUID, in taskdom.UpdateTaskTypeInput) (*taskdom.TaskType, error) {
	t, err := s.repo.FindTaskTypeByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.ProjectID != projectID {
		return nil, taskdom.ErrTypeNotFound
	}
	if t.IsSystem {
		return nil, taskdom.ErrTypeIsSystem
	}

	if name := strings.TrimSpace(in.Name); name != "" {
		t.Name = name
	}
	if in.Icon != nil {
		t.Icon = *in.Icon
	}
	if in.Color != nil {
		t.Color = *in.Color
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	t.UpdatedAt = time.Now()

	if err := s.repo.UpdateTaskType(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// DeleteTaskType removes a task type by ID.
func (s *Service) DeleteTaskType(ctx context.Context, projectID, id uuid.UUID) error {
	t, err := s.repo.FindTaskTypeByID(ctx, id)
	if err != nil {
		return err
	}
	if t.ProjectID != projectID {
		return taskdom.ErrTypeNotFound
	}
	if t.IsSystem {
		return taskdom.ErrTypeIsSystem
	}
	return s.repo.DeleteTaskType(ctx, id)
}

// SetDefaultTaskType marks typeID as the project's default task type,
// clearing the flag on all other types in the same project.
func (s *Service) SetDefaultTaskType(ctx context.Context, projectID, typeID uuid.UUID) (*taskdom.TaskType, error) {
	if err := s.repo.SetDefaultTaskType(ctx, projectID, typeID); err != nil {
		return nil, err
	}
	return s.repo.FindTaskTypeByID(ctx, typeID)
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
func (s *Service) UpdateTaskStatus(ctx context.Context, projectID, id uuid.UUID, in taskdom.UpdateTaskStatusInput) (*taskdom.TaskStatus, error) {
	st, err := s.repo.FindTaskStatusByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if st.ProjectID != projectID {
		return nil, taskdom.ErrStatusNotFound
	}

	if name := strings.TrimSpace(in.Name); name != "" {
		st.Name = name
	}
	if in.Color != nil {
		st.Color = in.Color
	}
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
func (s *Service) DeleteTaskStatus(ctx context.Context, projectID, id uuid.UUID) error {
	st, err := s.repo.FindTaskStatusByID(ctx, id)
	if err != nil {
		return err
	}
	if st.ProjectID != projectID {
		return taskdom.ErrStatusNotFound
	}
	if s.automationChecker != nil {
		used, err := s.automationChecker.StatusUsedByAutomation(ctx, id)
		if err != nil {
			return err
		}
		if used {
			return taskdom.ErrStatusInUseByAutomation
		}
	}
	return s.repo.DeleteTaskStatus(ctx, id)
}

// SetDefaultTaskStatus marks statusID as the project's default task status,
// returning the updated status.
func (s *Service) SetDefaultTaskStatus(ctx context.Context, projectID, statusID uuid.UUID) (*taskdom.TaskStatus, error) {
	if err := s.repo.SetDefaultTaskStatus(ctx, projectID, statusID); err != nil {
		return nil, err
	}
	return s.repo.FindTaskStatusByID(ctx, statusID)
}

// ReorderTaskStatuses persists a new display order for the project's task
// statuses, assigning position = index in statusIDs to each status.
func (s *Service) ReorderTaskStatuses(ctx context.Context, projectID uuid.UUID, statusIDs []uuid.UUID) error {
	if len(statusIDs) == 0 {
		return taskdom.ErrStatusReorderInvalid
	}
	return s.repo.ReorderTaskStatuses(ctx, projectID, statusIDs)
}

// isEpicTaskType returns whether typeID belongs to the system Epic type.
func (s *Service) isEpicTaskType(ctx context.Context, typeID *uuid.UUID) (bool, error) {
	if typeID == nil {
		return false, nil
	}
	t, err := s.repo.FindTaskTypeByID(ctx, *typeID)
	if err != nil {
		return false, err
	}
	return t.IsSystem && t.Name == "Epic", nil
}

// wouldCreateCycle reports whether making proposedParentID the parent of taskID
// would introduce a directed cycle in the task hierarchy.
func (s *Service) wouldCreateCycle(ctx context.Context, taskID, proposedParentID uuid.UUID) bool {
	current := proposedParentID
	const maxDepth = 50
	for range maxDepth {
		if current == taskID {
			return true
		}
		t, err := s.repo.FindTaskByID(ctx, current)
		if err != nil || t.ParentTaskID == nil {
			return false
		}
		current = *t.ParentTaskID
	}
	return false
}

// --- Tasks ------------------------------------------------------------------

// ListTasks returns a page of tasks. When filter.CursorAfter is nil, returns from
// the beginning. When set, returns tasks after the cursor position.
// Returns hasMore=true when a next page exists.
func (s *Service) ListTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, pageSize int, sort taskdom.TaskSort) ([]*taskdom.Task, bool, error) {
	if pageSize < 1 {
		pageSize = 20
	}
	return s.repo.ListTasks(ctx, projectID, filter, pageSize, sort)
}

// CountTasks returns the number of tasks in a project matching the given filter.
func (s *Service) CountTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter) (int64, error) {
	return s.repo.CountTasks(ctx, projectID, filter)
}

// CountOpenTasksByProjects returns the number of non-done tasks across all of
// projectIDs in a single query.
func (s *Service) CountOpenTasksByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error) {
	return s.repo.CountOpenTasksByProjects(ctx, projectIDs)
}

// SumTaskField sums a numeric field across all matching tasks, ignoring pagination.
func (s *Service) SumTaskField(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, fieldKey string) (float64, error) {
	return s.repo.SumTaskField(ctx, projectID, filter, fieldKey)
}

// ListAssignedTasks returns open tasks assigned to any of memberIDs, across
// their respective projects — see taskdom.Repository.ListAssignedTasks.
func (s *Service) ListAssignedTasks(ctx context.Context, memberIDs []uuid.UUID, limit int, cursorAfter *string) ([]*taskdom.Task, bool, error) {
	if limit < 1 {
		limit = 10
	}
	return s.repo.ListAssignedTasks(ctx, memberIDs, limit, cursorAfter)
}

// GetTask returns the task with the given ID, verifying it belongs to projectID.
func (s *Service) GetTask(ctx context.Context, projectID, id uuid.UUID) (*taskdom.Task, error) {
	t, err := s.repo.FindTaskByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.ProjectID != projectID {
		return nil, taskdom.ErrTaskNotFound
	}
	return t, nil
}

// GetTaskByNumber returns the task with the given project-scoped sequential number.
func (s *Service) GetTaskByNumber(ctx context.Context, projectID uuid.UUID, taskNumber int64) (*taskdom.Task, error) {
	return s.repo.FindTaskByNumber(ctx, projectID, taskNumber)
}

// CreateTask creates a new task. When TaskTypeID or StatusID are not provided,
// the project's default task type / status is resolved automatically.
func (s *Service) CreateTask(ctx context.Context, in taskdom.CreateTaskInput) (*taskdom.Task, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, taskdom.ErrTaskTitleInvalid
	}

	if in.ParentTaskID != nil {
		isEpic, err := s.isEpicTaskType(ctx, in.TaskTypeID)
		if err != nil {
			return nil, err
		}
		if isEpic {
			return nil, taskdom.ErrEpicCannotHaveParent
		}
	}

	taskTypeID := in.TaskTypeID
	if taskTypeID == nil {
		if dt, err := s.repo.FindDefaultTaskType(ctx, in.ProjectID); err != nil {
			return nil, err
		} else if dt != nil {
			taskTypeID = &dt.ID
		}
	}

	statusID := in.StatusID
	if statusID == nil {
		if ds, err := s.repo.FindDefaultTaskStatus(ctx, in.ProjectID); err != nil {
			return nil, err
		} else if ds != nil {
			statusID = &ds.ID
		}
	}

	cf := in.CustomFields
	if cf == nil {
		cf = map[string]any{}
	}
	tags := in.Tags
	if tags == nil {
		tags = []string{}
	}
	assigneeIDs := in.AssigneeIDs
	if assigneeIDs == nil {
		assigneeIDs = []uuid.UUID{}
	}

	now := time.Now()
	t := &taskdom.Task{
		ID:           uuid.New(),
		ProjectID:    in.ProjectID,
		TaskTypeID:   taskTypeID,
		StatusID:     statusID,
		SprintID:     in.SprintID,
		ParentTaskID: in.ParentTaskID,
		Title:        title,
		Description:  in.Description,
		Importance:   in.Importance,
		StoryPoints:  in.StoryPoints,
		AssigneeIDs:  assigneeIDs,
		ReporterID:   in.ReporterID,
		CustomFields: cf,
		StartDate:    in.StartDate,
		DueDate:      in.DueDate,
		Tags:         tags,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.repo.CreateTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// UpdateTask updates the mutable fields of an existing task.
func (s *Service) UpdateTask(ctx context.Context, projectID, id uuid.UUID, in taskdom.UpdateTaskInput) (*taskdom.Task, error) {
	t, err := s.repo.FindTaskByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.ProjectID != projectID {
		return nil, taskdom.ErrTaskNotFound
	}

	if title := strings.TrimSpace(in.Title); title != "" {
		t.Title = title
	}

	// Compute the effective parent and type IDs after the update to validate constraints.
	effectiveParentID := t.ParentTaskID
	if in.ParentTaskID != nil {
		effectiveParentID = *in.ParentTaskID
	}
	effectiveTypeID := t.TaskTypeID
	if in.TaskTypeID != nil {
		effectiveTypeID = *in.TaskTypeID
	}
	// Validate parent constraints using the post-update effective values.
	if effectiveParentID != nil {
		if *effectiveParentID == t.ID {
			return nil, taskdom.ErrTaskCannotBeOwnParent
		}
		if s.wouldCreateCycle(ctx, t.ID, *effectiveParentID) {
			return nil, taskdom.ErrTaskParentCycleDetected
		}
		isEpic, err := s.isEpicTaskType(ctx, effectiveTypeID)
		if err != nil {
			return nil, err
		}
		if isEpic {
			return nil, taskdom.ErrEpicCannotHaveParent
		}
	}

	if in.TaskTypeID != nil {
		t.TaskTypeID = *in.TaskTypeID
	}
	if in.StatusID != nil {
		t.StatusID = *in.StatusID
	}
	if in.SprintID != nil {
		t.SprintID = *in.SprintID
	}
	if in.ParentTaskID != nil {
		t.ParentTaskID = *in.ParentTaskID
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	if in.Importance != nil {
		t.Importance = *in.Importance
	}
	if in.StoryPoints != nil {
		t.StoryPoints = *in.StoryPoints
	}
	if in.AssigneeIDs != nil {
		t.AssigneeIDs = *in.AssigneeIDs
	}
	if in.ReporterID != nil {
		t.ReporterID = *in.ReporterID
	}
	if in.CustomFields != nil {
		t.CustomFields = *in.CustomFields
	}
	if in.StartDate != nil {
		t.StartDate = *in.StartDate
	}
	if in.DueDate != nil {
		t.DueDate = *in.DueDate
	}
	if in.Tags != nil {
		t.Tags = *in.Tags
	}
	t.UpdatedAt = time.Now()

	if err := s.repo.UpdateTask(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// DeleteTask soft-deletes a task by ID, verifying it belongs to projectID.
func (s *Service) DeleteTask(ctx context.Context, projectID, id uuid.UUID) error {
	t, err := s.repo.FindTaskByID(ctx, id)
	if err != nil {
		return err
	}
	if t.ProjectID != projectID {
		return taskdom.ErrTaskNotFound
	}
	return s.repo.DeleteTask(ctx, id)
}

// --- Custom Field Definitions -----------------------------------------------

// ListCustomFieldDefinitions returns all custom field definitions for a project.
func (s *Service) ListCustomFieldDefinitions(ctx context.Context, projectID uuid.UUID) ([]*taskdom.CustomFieldDefinition, error) {
	return s.repo.ListCustomFieldDefinitions(ctx, projectID)
}

// GetCustomFieldDefinition returns the custom field definition with the given ID,
// verifying it belongs to projectID.
func (s *Service) GetCustomFieldDefinition(ctx context.Context, projectID, id uuid.UUID) (*taskdom.CustomFieldDefinition, error) {
	f, err := s.repo.FindCustomFieldDefinitionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.ProjectID != projectID {
		return nil, taskdom.ErrCustomFieldNotFound
	}
	return f, nil
}

// CreateCustomFieldDefinition creates a new custom field definition.
func (s *Service) CreateCustomFieldDefinition(ctx context.Context, in taskdom.CreateCustomFieldDefinitionInput) (*taskdom.CustomFieldDefinition, error) {
	fieldKey := strings.TrimSpace(in.FieldKey)
	if fieldKey == "" {
		return nil, taskdom.ErrCustomFieldKeyInvalid
	}
	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		return nil, taskdom.ErrCustomFieldNameInvalid
	}
	if !taskdom.ValidFieldTypes[in.FieldType] {
		return nil, taskdom.ErrCustomFieldTypeInvalid
	}

	opts := in.Options
	if opts == nil {
		opts = []taskdom.CustomFieldOption{}
	}

	now := time.Now()
	f := &taskdom.CustomFieldDefinition{
		ID:          uuid.New(),
		ProjectID:   in.ProjectID,
		FieldKey:    fieldKey,
		DisplayName: displayName,
		FieldType:   in.FieldType,
		Options:     opts,
		IsRequired:  in.IsRequired,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.CreateCustomFieldDefinition(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// UpdateCustomFieldDefinition updates the mutable fields of a custom field
// definition. The field_key is immutable after creation.
func (s *Service) UpdateCustomFieldDefinition(ctx context.Context, projectID, id uuid.UUID, in taskdom.UpdateCustomFieldDefinitionInput) (*taskdom.CustomFieldDefinition, error) {
	f, err := s.repo.FindCustomFieldDefinitionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if f.ProjectID != projectID {
		return nil, taskdom.ErrCustomFieldNotFound
	}

	if displayName := strings.TrimSpace(in.DisplayName); displayName != "" {
		f.DisplayName = displayName
	}
	if in.FieldType != nil {
		if !taskdom.ValidFieldTypes[*in.FieldType] {
			return nil, taskdom.ErrCustomFieldTypeInvalid
		}
		f.FieldType = *in.FieldType
	}
	if in.Options != nil {
		f.Options = in.Options
	}
	if in.IsRequired != nil {
		f.IsRequired = *in.IsRequired
	}
	f.UpdatedAt = time.Now()

	if err := s.repo.UpdateCustomFieldDefinition(ctx, f); err != nil {
		return nil, err
	}
	return f, nil
}

// DeleteCustomFieldDefinition removes a custom field definition by ID,
// verifying it belongs to projectID.
func (s *Service) DeleteCustomFieldDefinition(ctx context.Context, projectID, id uuid.UUID) error {
	f, err := s.repo.FindCustomFieldDefinitionByID(ctx, id)
	if err != nil {
		return err
	}
	if f.ProjectID != projectID {
		return taskdom.ErrCustomFieldNotFound
	}
	return s.repo.DeleteCustomFieldDefinition(ctx, id)
}
