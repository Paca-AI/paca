// Package postgres — GORM implementation of taskdom.Repository.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	taskdom "github.com/paca/api/internal/domain/task"
	"gorm.io/gorm"
)

// --- GORM models ------------------------------------------------------------

type taskTypeRecord struct {
	ID          string  `gorm:"primarykey;type:uuid"`
	ProjectID   string  `gorm:"type:uuid;not null;column:project_id"`
	Name        string  `gorm:"not null"`
	Icon        *string `gorm:"type:text"`
	Color       *string `gorm:"type:text"`
	Description *string `gorm:"type:text"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (taskTypeRecord) TableName() string { return "task_types" }

type taskStatusRecord struct {
	ID        string  `gorm:"primarykey;type:uuid"`
	ProjectID string  `gorm:"type:uuid;not null;column:project_id"`
	Name      string  `gorm:"not null"`
	Color     *string `gorm:"type:text"`
	Position  int     `gorm:"not null;default:0"`
	Category  string  `gorm:"not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (taskStatusRecord) TableName() string { return "task_statuses" }

type taskRecord struct {
	ID            string  `gorm:"primarykey;type:uuid"`
	ProjectID     string  `gorm:"type:uuid;not null;column:project_id"`
	TaskTypeID    *string `gorm:"type:uuid;column:task_type_id"`
	StatusID      *string `gorm:"type:uuid;column:status_id"`
	SprintID      *string `gorm:"type:uuid;column:sprint_id"`
	ParentTaskID  *string `gorm:"type:uuid;column:parent_task_id"`
	Title         string  `gorm:"not null"`
	Description   *string `gorm:"type:text"`
	Importance    int     `gorm:"not null;default:0"`
	BoardPosition int     `gorm:"not null;default:0;column:board_position"`
	AssigneeID    *string `gorm:"type:uuid;column:assignee_id"`
	ReporterID    *string `gorm:"type:uuid;column:reporter_id"`
	CustomFields  []byte  `gorm:"type:jsonb;not null;column:custom_fields"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time `gorm:"index;column:deleted_at"`
}

func (taskRecord) TableName() string { return "tasks" }

// --- Repository struct -------------------------------------------------------

// TaskRepository is the GORM implementation of taskdom.Repository.
type TaskRepository struct {
	db *gorm.DB
}

// NewTaskRepository returns a new TaskRepository.
func NewTaskRepository(db *gorm.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

// --- Task Types -------------------------------------------------------------

// ListTaskTypes returns all task types for a project.
func (r *TaskRepository) ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error) {
	var records []taskTypeRecord
	if err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID.String()).
		Order("name ASC").
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("task type repo: list: %w", err)
	}
	out := make([]*taskdom.TaskType, 0, len(records))
	for i := range records {
		out = append(out, toTaskTypeEntity(&records[i]))
	}
	return out, nil
}

// FindTaskTypeByID returns the task type with the given ID.
func (r *TaskRepository) FindTaskTypeByID(ctx context.Context, id uuid.UUID) (*taskdom.TaskType, error) {
	var rec taskTypeRecord
	err := r.db.WithContext(ctx).Where("id = ?", id.String()).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, taskdom.ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("task type repo: find by id: %w", err)
	}
	return toTaskTypeEntity(&rec), nil
}

// CreateTaskType persists a new task type.
func (r *TaskRepository) CreateTaskType(ctx context.Context, t *taskdom.TaskType) error {
	rec := &taskTypeRecord{
		ID:          t.ID.String(),
		ProjectID:   t.ProjectID.String(),
		Name:        t.Name,
		Icon:        t.Icon,
		Color:       t.Color,
		Description: t.Description,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
		return fmt.Errorf("task type repo: create: %w", err)
	}
	return nil
}

// UpdateTaskType persists changes to an existing task type.
func (r *TaskRepository) UpdateTaskType(ctx context.Context, t *taskdom.TaskType) error {
	updates := map[string]any{
		"name":        t.Name,
		"icon":        t.Icon,
		"color":       t.Color,
		"description": t.Description,
		"updated_at":  t.UpdatedAt,
	}
	res := r.db.WithContext(ctx).Model(&taskTypeRecord{}).Where("id = ?", t.ID.String()).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("task type repo: update: %w", res.Error)
	}
	return nil
}

// DeleteTaskType removes a task type by ID.
func (r *TaskRepository) DeleteTaskType(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Delete(&taskTypeRecord{}, "id = ?", id.String())
	if res.Error != nil {
		return fmt.Errorf("task type repo: delete: %w", res.Error)
	}
	return nil
}

// --- Task Statuses ---------------------------------------------------------

// ListTaskStatuses returns all task statuses for a project ordered by position.
func (r *TaskRepository) ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskStatus, error) {
	var records []taskStatusRecord
	if err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID.String()).
		Order("position ASC").
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("task status repo: list: %w", err)
	}
	out := make([]*taskdom.TaskStatus, 0, len(records))
	for i := range records {
		out = append(out, toTaskStatusEntity(&records[i]))
	}
	return out, nil
}

// FindTaskStatusByID returns the task status with the given ID.
func (r *TaskRepository) FindTaskStatusByID(ctx context.Context, id uuid.UUID) (*taskdom.TaskStatus, error) {
	var rec taskStatusRecord
	err := r.db.WithContext(ctx).Where("id = ?", id.String()).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, taskdom.ErrStatusNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("task status repo: find by id: %w", err)
	}
	return toTaskStatusEntity(&rec), nil
}

// CreateTaskStatus persists a new task status.
func (r *TaskRepository) CreateTaskStatus(ctx context.Context, s *taskdom.TaskStatus) error {
	rec := &taskStatusRecord{
		ID:        s.ID.String(),
		ProjectID: s.ProjectID.String(),
		Name:      s.Name,
		Color:     s.Color,
		Position:  s.Position,
		Category:  string(s.Category),
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
		return fmt.Errorf("task status repo: create: %w", err)
	}
	return nil
}

// UpdateTaskStatus persists changes to an existing task status.
func (r *TaskRepository) UpdateTaskStatus(ctx context.Context, s *taskdom.TaskStatus) error {
	updates := map[string]any{
		"name":       s.Name,
		"color":      s.Color,
		"position":   s.Position,
		"category":   string(s.Category),
		"updated_at": s.UpdatedAt,
	}
	res := r.db.WithContext(ctx).Model(&taskStatusRecord{}).Where("id = ?", s.ID.String()).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("task status repo: update: %w", res.Error)
	}
	return nil
}

// DeleteTaskStatus removes a task status by ID.
func (r *TaskRepository) DeleteTaskStatus(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Delete(&taskStatusRecord{}, "id = ?", id.String())
	if res.Error != nil {
		return fmt.Errorf("task status repo: delete: %w", res.Error)
	}
	return nil
}

// --- Tasks ------------------------------------------------------------------

// ListTasks returns a page of tasks for a project with optional filters.
func (r *TaskRepository) ListTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, offset, limit int) ([]*taskdom.Task, int64, error) {
	q := r.db.WithContext(ctx).Model(&taskRecord{}).
		Where("project_id = ? AND deleted_at IS NULL", projectID.String())

	if filter.BacklogOnly {
		q = q.Where("sprint_id IS NULL")
	} else if filter.SprintID != nil {
		q = q.Where("sprint_id = ?", filter.SprintID.String())
	}
	if filter.StatusID != nil {
		q = q.Where("status_id = ?", filter.StatusID.String())
	}
	if filter.AssigneeID != nil {
		q = q.Where("assignee_id = ?", filter.AssigneeID.String())
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("task repo: list count: %w", err)
	}

	var records []taskRecord
	if err := q.Order("board_position ASC, created_at ASC").
		Offset(offset).Limit(limit).
		Find(&records).Error; err != nil {
		return nil, 0, fmt.Errorf("task repo: list: %w", err)
	}

	tasks := make([]*taskdom.Task, 0, len(records))
	for i := range records {
		t, err := toTaskEntity(&records[i])
		if err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, t)
	}
	return tasks, total, nil
}

// FindTaskByID returns the task with the given ID (non-deleted).
func (r *TaskRepository) FindTaskByID(ctx context.Context, id uuid.UUID) (*taskdom.Task, error) {
	var rec taskRecord
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id.String()).
		First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, taskdom.ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("task repo: find by id: %w", err)
	}
	return toTaskEntity(&rec)
}

// CreateTask persists a new task.
func (r *TaskRepository) CreateTask(ctx context.Context, t *taskdom.Task) error {
	cf, err := json.Marshal(t.CustomFields)
	if err != nil {
		return fmt.Errorf("task repo: marshal custom_fields: %w", err)
	}
	rec := &taskRecord{
		ID:            t.ID.String(),
		ProjectID:     t.ProjectID.String(),
		TaskTypeID:    uuidPtrToStrPtr(t.TaskTypeID),
		StatusID:      uuidPtrToStrPtr(t.StatusID),
		SprintID:      uuidPtrToStrPtr(t.SprintID),
		ParentTaskID:  uuidPtrToStrPtr(t.ParentTaskID),
		Title:         t.Title,
		Description:   t.Description,
		Importance:    t.Importance,
		BoardPosition: t.BoardPosition,
		AssigneeID:    uuidPtrToStrPtr(t.AssigneeID),
		ReporterID:    uuidPtrToStrPtr(t.ReporterID),
		CustomFields:  cf,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
		return fmt.Errorf("task repo: create: %w", err)
	}
	return nil
}

// UpdateTask persists changes to an existing task.
func (r *TaskRepository) UpdateTask(ctx context.Context, t *taskdom.Task) error {
	cf, err := json.Marshal(t.CustomFields)
	if err != nil {
		return fmt.Errorf("task repo: marshal custom_fields: %w", err)
	}
	updates := map[string]any{
		"task_type_id":   uuidPtrToStrPtr(t.TaskTypeID),
		"status_id":      uuidPtrToStrPtr(t.StatusID),
		"sprint_id":      uuidPtrToStrPtr(t.SprintID),
		"parent_task_id": uuidPtrToStrPtr(t.ParentTaskID),
		"title":          t.Title,
		"description":    t.Description,
		"importance":     t.Importance,
		"board_position": t.BoardPosition,
		"assignee_id":    uuidPtrToStrPtr(t.AssigneeID),
		"reporter_id":    uuidPtrToStrPtr(t.ReporterID),
		"custom_fields":  cf,
		"updated_at":     t.UpdatedAt,
	}
	res := r.db.WithContext(ctx).Model(&taskRecord{}).
		Where("id = ? AND deleted_at IS NULL", t.ID.String()).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("task repo: update: %w", res.Error)
	}
	return nil
}

// DeleteTask soft-deletes a task by setting deleted_at.
func (r *TaskRepository) DeleteTask(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&taskRecord{}).
		Where("id = ? AND deleted_at IS NULL", id.String()).
		Updates(map[string]any{"deleted_at": now, "updated_at": now})
	if res.Error != nil {
		return fmt.Errorf("task repo: delete: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return taskdom.ErrTaskNotFound
	}
	return nil
}

// --- Entity converters ------------------------------------------------------

func toTaskTypeEntity(r *taskTypeRecord) *taskdom.TaskType {
	id, _ := uuid.Parse(r.ID)
	pid, _ := uuid.Parse(r.ProjectID)
	return &taskdom.TaskType{
		ID:          id,
		ProjectID:   pid,
		Name:        r.Name,
		Icon:        r.Icon,
		Color:       r.Color,
		Description: r.Description,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func toTaskStatusEntity(r *taskStatusRecord) *taskdom.TaskStatus {
	id, _ := uuid.Parse(r.ID)
	pid, _ := uuid.Parse(r.ProjectID)
	return &taskdom.TaskStatus{
		ID:        id,
		ProjectID: pid,
		Name:      r.Name,
		Color:     r.Color,
		Position:  r.Position,
		Category:  taskdom.StatusCategory(r.Category),
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func toTaskEntity(r *taskRecord) (*taskdom.Task, error) {
	id, _ := uuid.Parse(r.ID)
	pid, _ := uuid.Parse(r.ProjectID)

	var cf map[string]any
	if len(r.CustomFields) > 0 {
		if err := json.Unmarshal(r.CustomFields, &cf); err != nil {
			return nil, fmt.Errorf("task repo: unmarshal custom_fields: %w", err)
		}
	}
	if cf == nil {
		cf = map[string]any{}
	}

	return &taskdom.Task{
		ID:            id,
		ProjectID:     pid,
		TaskTypeID:    strPtrToUUIDPtr(r.TaskTypeID),
		StatusID:      strPtrToUUIDPtr(r.StatusID),
		SprintID:      strPtrToUUIDPtr(r.SprintID),
		ParentTaskID:  strPtrToUUIDPtr(r.ParentTaskID),
		Title:         r.Title,
		Description:   r.Description,
		Importance:    r.Importance,
		BoardPosition: r.BoardPosition,
		AssigneeID:    strPtrToUUIDPtr(r.AssigneeID),
		ReporterID:    strPtrToUUIDPtr(r.ReporterID),
		CustomFields:  cf,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
		DeletedAt:     r.DeletedAt,
	}, nil
}

// --- helpers ----------------------------------------------------------------

func uuidPtrToStrPtr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

func strPtrToUUIDPtr(s *string) *uuid.UUID {
	if s == nil {
		return nil
	}
	u, err := uuid.Parse(*s)
	if err != nil {
		return nil
	}
	return &u
}
