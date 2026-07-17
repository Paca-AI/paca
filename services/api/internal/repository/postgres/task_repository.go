// Package postgres — sqlx implementation of taskdom.Repository.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// --- sqlx models ------------------------------------------------------------

type taskTypeRecord struct {
	ID          string    `db:"id"`
	ProjectID   string    `db:"project_id"`
	Name        string    `db:"name"`
	Icon        *string   `db:"icon"`
	Color       *string   `db:"color"`
	Description *string   `db:"description"`
	IsDefault   bool      `db:"is_default"`
	IsSystem    bool      `db:"is_system"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

type taskStatusRecord struct {
	ID        string    `db:"id"`
	ProjectID string    `db:"project_id"`
	Name      string    `db:"name"`
	Color     *string   `db:"color"`
	Position  int       `db:"position"`
	Category  string    `db:"category"`
	IsDefault bool      `db:"is_default"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type taskRecord struct {
	ID           string           `db:"id"`
	ProjectID    string           `db:"project_id"`
	TaskNumber   int64            `db:"task_number"`
	TaskTypeID   *string          `db:"task_type_id"`
	StatusID     *string          `db:"status_id"`
	SprintID     *string          `db:"sprint_id"`
	ParentTaskID *string          `db:"parent_task_id"`
	Title        string           `db:"title"`
	Description  *json.RawMessage `db:"description"`
	Importance   int              `db:"importance"`
	StoryPoints  *int             `db:"story_points"`
	ReporterID   *string          `db:"reporter_id"`
	CustomFields []byte           `db:"custom_fields"`
	StartDate    *time.Time       `db:"start_date"`
	DueDate      *time.Time       `db:"due_date"`
	Tags         []byte           `db:"tags"`
	CreatedAt    time.Time        `db:"created_at"`
	UpdatedAt    time.Time        `db:"updated_at"`
	DeletedAt    *time.Time       `db:"deleted_at"`
}

// taskCounterRecord mirrors the task_counters table used for atomic
// per-project task number generation.
type taskCounterRecord struct {
	ProjectID string `db:"project_id"`
	LastValue int64  `db:"last_value"`
}

// taskWithPositionRow is a flat struct for scanning the view_position LEFT JOIN result.
type taskWithPositionRow struct {
	ID           string           `db:"id"`
	ProjectID    string           `db:"project_id"`
	TaskNumber   int64            `db:"task_number"`
	TaskTypeID   *string          `db:"task_type_id"`
	StatusID     *string          `db:"status_id"`
	SprintID     *string          `db:"sprint_id"`
	ParentTaskID *string          `db:"parent_task_id"`
	Title        string           `db:"title"`
	Description  *json.RawMessage `db:"description"`
	Importance   int              `db:"importance"`
	StoryPoints  *int             `db:"story_points"`
	ReporterID   *string          `db:"reporter_id"`
	CustomFields []byte           `db:"custom_fields"`
	StartDate    *time.Time       `db:"start_date"`
	DueDate      *time.Time       `db:"due_date"`
	Tags         []byte           `db:"tags"`
	CreatedAt    time.Time        `db:"created_at"`
	UpdatedAt    time.Time        `db:"updated_at"`
	DeletedAt    *time.Time       `db:"deleted_at"`
	VTPPosition  *float64         `db:"vtp_position"`
}

func (r *taskWithPositionRow) asTaskRecord() taskRecord {
	return taskRecord{
		ID:           r.ID,
		ProjectID:    r.ProjectID,
		TaskNumber:   r.TaskNumber,
		TaskTypeID:   r.TaskTypeID,
		StatusID:     r.StatusID,
		SprintID:     r.SprintID,
		ParentTaskID: r.ParentTaskID,
		Title:        r.Title,
		Description:  r.Description,
		Importance:   r.Importance,
		StoryPoints:  r.StoryPoints,
		ReporterID:   r.ReporterID,
		CustomFields: r.CustomFields,
		StartDate:    r.StartDate,
		DueDate:      r.DueDate,
		Tags:         r.Tags,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		DeletedAt:    r.DeletedAt,
	}
}

// --- Repository struct -------------------------------------------------------

// TaskRepository is the sqlx implementation of taskdom.Repository.
// It embeds TaskLinkRepository so that a single pointer satisfies the full
// taskdom.Repository interface (which now includes TaskLinkRepository).
type TaskRepository struct {
	db *sqlx.DB
	*TaskLinkRepository
}

// NewTaskRepository returns a new TaskRepository.
func NewTaskRepository(db *sqlx.DB) *TaskRepository {
	return &TaskRepository{db: db, TaskLinkRepository: NewTaskLinkRepository(db)}
}

// --- queryBuilder is a small helper for building parameterized SQL queries ---

type queryBuilder struct {
	whereClauses []string
	args         []interface{}
	idx          int
}

func newQueryBuilder() *queryBuilder {
	return &queryBuilder{idx: 1}
}

func (b *queryBuilder) addInClause(col string, vals []string) {
	if len(vals) == 0 {
		return
	}
	placeholders := make([]string, len(vals))
	for i, v := range vals {
		placeholders[i] = fmt.Sprintf("$%d", b.idx)
		b.args = append(b.args, v)
		b.idx++
	}
	b.whereClauses = append(b.whereClauses, col+" IN ("+strings.Join(placeholders, ",")+")")
}

func (b *queryBuilder) placeholder() string {
	p := fmt.Sprintf("$%d", b.idx)
	b.idx++
	return p
}

// --- Task Types -------------------------------------------------------------

const taskTypeCols = `id, project_id, name, icon, color, description, is_default, is_system, created_at, updated_at`

// ListTaskTypes returns all task types for a project.
func (r *TaskRepository) ListTaskTypes(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error) {
	var records []taskTypeRecord
	if err := r.db.SelectContext(ctx, &records, `SELECT `+taskTypeCols+` FROM task_types WHERE project_id = $1 ORDER BY name ASC`, projectID.String()); err != nil {
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
	err := r.db.GetContext(ctx, &rec, `SELECT `+taskTypeCols+` FROM task_types WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, taskdom.ErrTypeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("task type repo: find by id: %w", err)
	}
	return toTaskTypeEntity(&rec), nil
}

// CreateTaskType persists a new task type.
func (r *TaskRepository) CreateTaskType(ctx context.Context, t *taskdom.TaskType) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO task_types (id, project_id, name, icon, color, description, is_default, is_system, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		t.ID.String(), t.ProjectID.String(), t.Name, t.Icon, t.Color,
		t.Description, t.IsDefault, t.IsSystem, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("task type repo: create: %w", err)
	}
	return nil
}

// UpdateTaskType persists changes to an existing task type.
func (r *TaskRepository) UpdateTaskType(ctx context.Context, t *taskdom.TaskType) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE task_types SET name=$1, icon=$2, color=$3, description=$4, updated_at=$5 WHERE id=$6`,
		t.Name, t.Icon, t.Color, t.Description, t.UpdatedAt, t.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("task type repo: update: %w", err)
	}
	return nil
}

// DeleteTaskType removes a task type by ID.
func (r *TaskRepository) DeleteTaskType(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM task_types WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("task type repo: delete: %w", err)
	}
	return nil
}

// SetDefaultTaskType atomically marks typeID as the project's default task type,
// clearing is_default on all other types in the same project.
func (r *TaskRepository) SetDefaultTaskType(ctx context.Context, projectID, typeID uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		// Lock all types for this project to serialize concurrent calls.
		var ids []string
		if err := tx.SelectContext(ctx, &ids, `SELECT id FROM task_types WHERE project_id = $1 FOR UPDATE`, projectID.String()); err != nil {
			return fmt.Errorf("task type repo: set default (lock): %w", err)
		}

		found := false
		for _, id := range ids {
			if id == typeID.String() {
				found = true
				break
			}
		}
		if !found {
			return taskdom.ErrTypeNotFound
		}

		if _, err := tx.ExecContext(ctx, `UPDATE task_types SET is_default = false, updated_at = NOW() WHERE project_id = $1 AND is_default = true`, projectID.String()); err != nil {
			return fmt.Errorf("task type repo: set default (clear): %w", err)
		}

		if _, err := tx.ExecContext(ctx, `UPDATE task_types SET is_default = true, updated_at = NOW() WHERE id = $1 AND project_id = $2`, typeID.String(), projectID.String()); err != nil {
			return fmt.Errorf("task type repo: set default (set): %w", err)
		}

		return nil
	})
}

// --- Task Statuses ---------------------------------------------------------

const taskStatusCols = `id, project_id, name, color, position, category, is_default, created_at, updated_at`

// ListTaskStatuses returns all task statuses for a project ordered by position.
func (r *TaskRepository) ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskStatus, error) {
	var records []taskStatusRecord
	if err := r.db.SelectContext(ctx, &records, `SELECT `+taskStatusCols+` FROM task_statuses WHERE project_id = $1 ORDER BY position ASC`, projectID.String()); err != nil {
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
	err := r.db.GetContext(ctx, &rec, `SELECT `+taskStatusCols+` FROM task_statuses WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, taskdom.ErrStatusNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("task status repo: find by id: %w", err)
	}
	return toTaskStatusEntity(&rec), nil
}

// CreateTaskStatus persists a new task status.
func (r *TaskRepository) CreateTaskStatus(ctx context.Context, s *taskdom.TaskStatus) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO task_statuses (id, project_id, name, color, position, category, is_default, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		s.ID.String(), s.ProjectID.String(), s.Name, s.Color, s.Position,
		string(s.Category), s.IsDefault, s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("task status repo: create: %w", err)
	}
	return nil
}

// UpdateTaskStatus persists changes to an existing task status.
func (r *TaskRepository) UpdateTaskStatus(ctx context.Context, s *taskdom.TaskStatus) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE task_statuses SET name=$1, color=$2, category=$3, position=$4, updated_at=$5 WHERE id=$6`,
		s.Name, s.Color, string(s.Category), s.Position, s.UpdatedAt, s.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("task status repo: update: %w", err)
	}
	return nil
}

// DeleteTaskStatus removes a task status by ID.
func (r *TaskRepository) DeleteTaskStatus(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM task_statuses WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("task status repo: delete: %w", err)
	}
	return nil
}

// SetDefaultTaskStatus marks statusID as the project's default task status,
// clearing is_default on all other statuses in the same project.
func (r *TaskRepository) SetDefaultTaskStatus(ctx context.Context, projectID, statusID uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		// Lock all statuses for this project to serialize concurrent calls.
		var ids []string
		if err := tx.SelectContext(ctx, &ids, `SELECT id FROM task_statuses WHERE project_id = $1 FOR UPDATE`, projectID.String()); err != nil {
			return fmt.Errorf("task status repo: set default (lock): %w", err)
		}

		found := false
		for _, id := range ids {
			if id == statusID.String() {
				found = true
				break
			}
		}
		if !found {
			return taskdom.ErrStatusNotFound
		}

		if _, err := tx.ExecContext(ctx, `UPDATE task_statuses SET is_default = false, updated_at = NOW() WHERE project_id = $1 AND is_default = true`, projectID.String()); err != nil {
			return fmt.Errorf("task status repo: set default (clear): %w", err)
		}

		if _, err := tx.ExecContext(ctx, `UPDATE task_statuses SET is_default = true, updated_at = NOW() WHERE id = $1 AND project_id = $2`, statusID.String(), projectID.String()); err != nil {
			return fmt.Errorf("task status repo: set default (set): %w", err)
		}

		return nil
	})
}

// ReorderTaskStatuses atomically assigns position = index in statusIDs to
// each status. It verifies statusIDs is an exact permutation of the
// project's existing statuses before writing anything.
func (r *TaskRepository) ReorderTaskStatuses(ctx context.Context, projectID uuid.UUID, statusIDs []uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		// Lock all statuses for this project to serialize concurrent reorders.
		var ids []string
		if err := tx.SelectContext(ctx, &ids, `SELECT id FROM task_statuses WHERE project_id = $1 FOR UPDATE`, projectID.String()); err != nil {
			return fmt.Errorf("task status repo: reorder (lock): %w", err)
		}

		if len(ids) != len(statusIDs) {
			return taskdom.ErrStatusReorderInvalid
		}
		existing := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			existing[id] = struct{}{}
		}
		for _, id := range statusIDs {
			if _, ok := existing[id.String()]; !ok {
				return taskdom.ErrStatusReorderInvalid
			}
		}

		for i, id := range statusIDs {
			if _, err := tx.ExecContext(ctx, `UPDATE task_statuses SET position = $1, updated_at = NOW() WHERE id = $2 AND project_id = $3`, i, id.String(), projectID.String()); err != nil {
				return fmt.Errorf("task status repo: reorder: %w", err)
			}
		}
		return nil
	})
}

// FindDefaultTaskType returns the project's default task type, or nil if none is set.
func (r *TaskRepository) FindDefaultTaskType(ctx context.Context, projectID uuid.UUID) (*taskdom.TaskType, error) {
	var rec taskTypeRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+taskTypeCols+` FROM task_types WHERE project_id = $1 AND is_default = true`, projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("task type repo: find default: %w", err)
	}
	return toTaskTypeEntity(&rec), nil
}

// FindDefaultTaskStatus returns the project's default task status, or nil if none is set.
func (r *TaskRepository) FindDefaultTaskStatus(ctx context.Context, projectID uuid.UUID) (*taskdom.TaskStatus, error) {
	var rec taskStatusRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+taskStatusCols+` FROM task_statuses WHERE project_id = $1 AND is_default = true`, projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("task status repo: find default: %w", err)
	}
	return toTaskStatusEntity(&rec), nil
}

// --- Tasks ------------------------------------------------------------------

const taskCols = `id, project_id, task_number, task_type_id, status_id, sprint_id, parent_task_id,
	title, description, importance, story_points, reporter_id,
	custom_fields, start_date, due_date, tags, created_at, updated_at, deleted_at`

// applyTaskFilter adds WHERE predicates for all TaskFilter fields.
// b is the shared queryBuilder; the base "project_id = $1 AND deleted_at IS NULL" clause is already set.
func applyTaskFilter(b *queryBuilder, filter taskdom.TaskFilter) {
	switch {
	case filter.ParentTaskID != nil:
		p := b.placeholder()
		b.whereClauses = append(b.whereClauses, "parent_task_id = "+p)
		b.args = append(b.args, filter.ParentTaskID.String())
	case len(filter.SprintIDs) > 0:
		b.addInClause("sprint_id", uuidSliceToStrSlice(filter.SprintIDs))
	case filter.BacklogOnly:
		b.whereClauses = append(b.whereClauses, "sprint_id IS NULL")
	case filter.SprintID != nil:
		p := b.placeholder()
		b.whereClauses = append(b.whereClauses, "sprint_id = "+p)
		b.args = append(b.args, filter.SprintID.String())
	}

	if len(filter.StatusIDs) > 0 {
		b.addInClause("status_id", uuidSliceToStrSlice(filter.StatusIDs))
	} else if filter.StatusID != nil {
		p := b.placeholder()
		b.whereClauses = append(b.whereClauses, "status_id = "+p)
		b.args = append(b.args, filter.StatusID.String())
	}

	switch {
	case filter.AssigneeNull && len(filter.AssigneeIDs) > 0:
		// Build: NOT EXISTS(any assignee) OR EXISTS(assignee IN (...))
		placeholders := make([]string, len(filter.AssigneeIDs))
		for i, id := range filter.AssigneeIDs {
			placeholders[i] = fmt.Sprintf("$%d", b.idx)
			b.args = append(b.args, id.String())
			b.idx++
		}
		b.whereClauses = append(b.whereClauses, "(NOT EXISTS (SELECT 1 FROM task_assignees ta WHERE ta.task_id = tasks.id) "+
			"OR EXISTS (SELECT 1 FROM task_assignees ta WHERE ta.task_id = tasks.id AND ta.member_id IN ("+strings.Join(placeholders, ",")+")))")
	case filter.AssigneeNull:
		b.whereClauses = append(b.whereClauses, "NOT EXISTS (SELECT 1 FROM task_assignees ta WHERE ta.task_id = tasks.id)")
	case len(filter.AssigneeIDs) > 0:
		placeholders := make([]string, len(filter.AssigneeIDs))
		for i, id := range filter.AssigneeIDs {
			placeholders[i] = fmt.Sprintf("$%d", b.idx)
			b.args = append(b.args, id.String())
			b.idx++
		}
		b.whereClauses = append(b.whereClauses, "EXISTS (SELECT 1 FROM task_assignees ta WHERE ta.task_id = tasks.id AND ta.member_id IN ("+strings.Join(placeholders, ",")+"))")
	case filter.AssigneeID != nil:
		p := b.placeholder()
		b.whereClauses = append(b.whereClauses, "EXISTS (SELECT 1 FROM task_assignees ta WHERE ta.task_id = tasks.id AND ta.member_id = "+p+")")
		b.args = append(b.args, filter.AssigneeID.String())
	}

	switch {
	case filter.TaskTypeNull:
		b.whereClauses = append(b.whereClauses, "task_type_id IS NULL")
	case len(filter.TaskTypeIDs) > 0:
		b.addInClause("task_type_id", uuidSliceToStrSlice(filter.TaskTypeIDs))
	}

	if filter.Search != nil {
		if q := strings.TrimSpace(*filter.Search); q != "" {
			pattern := "%" + escapeLikePattern(q) + "%"
			p1 := b.placeholder()
			b.args = append(b.args, pattern)
			p2 := b.placeholder()
			b.args = append(b.args, pattern)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf(
				"(title ILIKE %s OR ('#' || task_number::text) ILIKE %s)", p1, p2))
		}
	}

	if filter.StartDateAfter != nil {
		p := b.placeholder()
		b.whereClauses = append(b.whereClauses, "start_date >= "+p)
		b.args = append(b.args, *filter.StartDateAfter)
	}
	if filter.StartDateBefore != nil {
		p := b.placeholder()
		b.whereClauses = append(b.whereClauses, "start_date <= "+p)
		b.args = append(b.args, *filter.StartDateBefore)
	}
	if filter.DueDateAfter != nil {
		p := b.placeholder()
		b.whereClauses = append(b.whereClauses, "due_date >= "+p)
		b.args = append(b.args, *filter.DueDateAfter)
	}
	if filter.DueDateBefore != nil {
		p := b.placeholder()
		b.whereClauses = append(b.whereClauses, "due_date <= "+p)
		b.args = append(b.args, *filter.DueDateBefore)
	}
	if filter.StoryPointsMin != nil {
		p := b.placeholder()
		b.whereClauses = append(b.whereClauses, "story_points >= "+p)
		b.args = append(b.args, *filter.StoryPointsMin)
	}
	if filter.StoryPointsMax != nil {
		p := b.placeholder()
		b.whereClauses = append(b.whereClauses, "story_points <= "+p)
		b.args = append(b.args, *filter.StoryPointsMax)
	}
	if len(filter.ImportanceRanges) > 0 {
		parts := make([]string, len(filter.ImportanceRanges))
		for i, rg := range filter.ImportanceRanges {
			minP := b.placeholder()
			b.args = append(b.args, rg.Min)
			maxP := b.placeholder()
			b.args = append(b.args, rg.Max)
			parts[i] = "(importance BETWEEN " + minP + " AND " + maxP + ")"
		}
		b.whereClauses = append(b.whereClauses, "("+strings.Join(parts, " OR ")+")")
	}
	if len(filter.Tags) > 0 {
		parts := make([]string, len(filter.Tags))
		for i, tag := range filter.Tags {
			p := b.placeholder()
			b.args = append(b.args, tag)
			parts[i] = "tags @> to_jsonb(" + p + "::text)"
		}
		b.whereClauses = append(b.whereClauses, "("+strings.Join(parts, " OR ")+")")
	}

	applyCustomFieldFilters(b, filter.CustomFieldFilters)
}

// applyCustomFieldFilters adds WHERE predicates for TaskFilter.CustomFieldFilters,
// reading and casting the custom_fields JSONB column per field type. Field
// keys are bound as parameters (never string-concatenated) and iterated in
// sorted order so the generated SQL text is deterministic across calls with
// the same filter set, which helps the driver's prepared-statement cache.
func applyCustomFieldFilters(b *queryBuilder, filters map[string]taskdom.CustomFieldFilterQuery) {
	if len(filters) == 0 {
		return
	}
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		q := filters[key]
		switch q.FieldType {
		case "select", "boolean":
			if len(q.Values) == 0 {
				continue
			}
			keyP := b.placeholder()
			b.args = append(b.args, key)
			placeholders := make([]string, len(q.Values))
			for i, v := range q.Values {
				p := b.placeholder()
				b.args = append(b.args, v)
				placeholders[i] = p
			}
			b.whereClauses = append(b.whereClauses,
				"(custom_fields->>"+keyP+") IN ("+strings.Join(placeholders, ",")+")")
		case "multi_select":
			if len(q.Values) == 0 {
				continue
			}
			keyP := b.placeholder()
			b.args = append(b.args, key)
			parts := make([]string, len(q.Values))
			for i, v := range q.Values {
				p := b.placeholder()
				b.args = append(b.args, v)
				parts[i] = "custom_fields->" + keyP + " @> to_jsonb(" + p + "::text)"
			}
			b.whereClauses = append(b.whereClauses, "("+strings.Join(parts, " OR ")+")")
		case "number":
			if q.Min == nil && q.Max == nil {
				continue
			}
			keyP := b.placeholder()
			b.args = append(b.args, key)
			expr := customFieldNumericExpr(keyP)
			if q.Min != nil {
				p := b.placeholder()
				b.args = append(b.args, *q.Min)
				b.whereClauses = append(b.whereClauses, expr+" >= "+p)
			}
			if q.Max != nil {
				p := b.placeholder()
				b.args = append(b.args, *q.Max)
				b.whereClauses = append(b.whereClauses, expr+" <= "+p)
			}
		case "date":
			if q.After == nil && q.Before == nil {
				continue
			}
			keyP := b.placeholder()
			b.args = append(b.args, key)
			expr := customFieldDateExpr(keyP)
			if q.After != nil {
				p := b.placeholder()
				b.args = append(b.args, *q.After)
				b.whereClauses = append(b.whereClauses, expr+" >= "+p)
			}
			if q.Before != nil {
				p := b.placeholder()
				b.args = append(b.args, *q.Before)
				b.whereClauses = append(b.whereClauses, expr+" <= "+p)
			}
		case "text", "url":
			if q.Contains == nil || strings.TrimSpace(*q.Contains) == "" {
				continue
			}
			keyP := b.placeholder()
			b.args = append(b.args, key)
			pattern := "%" + escapeLikePattern(*q.Contains) + "%"
			p := b.placeholder()
			b.args = append(b.args, pattern)
			b.whereClauses = append(b.whereClauses,
				"(custom_fields->>"+keyP+") ILIKE "+p)
		}
	}
}

// customFieldNumericExpr returns a SQL expression that reads the
// custom_fields JSONB text value under keyP (a bound $N placeholder holding
// the field key) and safely casts it to numeric, yielding NULL instead of
// erroring the whole query when the stored value isn't a valid number.
// custom_fields has no write-time validation against the field's declared
// type (any JSON value is accepted on task create/update), and a field
// definition can be deleted and recreated with the same field_key but a
// different type — either way, a task can end up with a stray/invalid value
// under a "numeric" field's key. A bare ::numeric cast on such a row aborts
// the entire query with a Postgres error instead of just excluding that row,
// so the cast is guarded by a regex check first.
func customFieldNumericExpr(keyP string) string {
	return "(CASE WHEN custom_fields->>" + keyP + " ~ '^-?[0-9]+(\\.[0-9]+)?$' THEN (custom_fields->>" + keyP + ")::numeric END)"
}

// customFieldDateExpr is the date-typed counterpart of customFieldNumericExpr.
func customFieldDateExpr(keyP string) string {
	return "(CASE WHEN custom_fields->>" + keyP + " ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}' THEN (custom_fields->>" + keyP + ")::date END)"
}

// escapeLikePattern escapes the LIKE/ILIKE wildcard characters (% and _) and
// the backslash escape character itself, so free-text search input is matched
// literally instead of as a glob pattern.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// applyTaskSort returns the FROM/JOIN extension and ORDER BY clause.
// When view_position is used, it also returns a modified SELECT cols string.
func applyTaskSort(sort taskdom.TaskSort, b *queryBuilder) (fromClause, orderByClause, selectCols string) {
	selectCols = "tasks." + strings.ReplaceAll(taskCols, "\n\t", "\n\ttasks.")
	fromClause = "FROM tasks"

	switch sort.By {
	case "view_position":
		if sort.ViewID != nil {
			p := b.placeholder()
			b.args = append(b.args, sort.ViewID.String())
			fromClause = "FROM tasks LEFT JOIN view_task_positions vtp ON vtp.task_id = tasks.id AND vtp.view_id = " + p
			selectCols = "tasks.*, vtp.position AS vtp_position"
		}
		orderByClause = "vtp.position ASC NULLS LAST, tasks.created_at ASC, tasks.id ASC"
	case "importance":
		orderByClause = "importance DESC, created_at ASC, id ASC"
	case "title":
		orderByClause = "title ASC, created_at ASC, id ASC"
	case "story_points":
		orderByClause = "story_points DESC NULLS LAST, created_at ASC, id ASC"
	case "start_date":
		orderByClause = "start_date ASC NULLS LAST, created_at ASC, id ASC"
	case "due_date":
		orderByClause = "due_date ASC NULLS LAST, created_at ASC, id ASC"
	default:
		if sort.By != "" && sort.CFType != "" {
			orderByClause = buildCFOrderBy(sort, b)
		} else {
			orderByClause = "created_at ASC, id ASC"
		}
	}
	return
}

func buildCFOrderBy(sort taskdom.TaskSort, b *queryBuilder) string {
	switch sort.CFType {
	case "number":
		p := b.placeholder()
		b.args = append(b.args, sort.By)
		return customFieldNumericExpr(p) + " ASC NULLS LAST, created_at ASC, id ASC"
	case "date":
		p := b.placeholder()
		b.args = append(b.args, sort.By)
		return customFieldDateExpr(p) + " ASC NULLS LAST, created_at ASC, id ASC"
	case "select":
		if len(sort.CFOpts) == 0 {
			return "created_at ASC, id ASC"
		}
		caseSQL, caseArgs := buildCFSelectCaseSQL(sort.By, sort.CFOpts, b)
		_ = caseArgs
		return caseSQL + " ASC, created_at ASC, id ASC"
	}
	return "created_at ASC, id ASC"
}

// buildCFSelectCaseSQL builds a parameterized CASE expression for select CF ordering.
func buildCFSelectCaseSQL(key string, opts []string, b *queryBuilder) (string, []interface{}) {
	keyP := b.placeholder()
	b.args = append(b.args, key)
	sql := "CASE custom_fields->>" + keyP
	for i, opt := range opts {
		p := b.placeholder()
		b.args = append(b.args, opt)
		sql += fmt.Sprintf(" WHEN %s THEN %d", p, i)
	}
	sql += " ELSE 9999 END"
	return sql, nil
}

// applyCursorWhere adds the keyset-pagination WHERE predicate.
func applyCursorWhere(b *queryBuilder, cur *taskdom.TaskCursor, sort taskdom.TaskSort) {
	ca := cur.CreatedAt.UTC()
	id := cur.ID

	switch cur.SortBy {
	case "view_position":
		if cur.SortNumVal != nil {
			pos := *cur.SortNumVal
			p1 := b.placeholder()
			b.args = append(b.args, pos)
			p2 := b.placeholder()
			b.args = append(b.args, pos)
			p3 := b.placeholder()
			b.args = append(b.args, ca)
			p4 := b.placeholder()
			b.args = append(b.args, id)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf(
				"(vtp.position > %s OR (vtp.position = %s AND (tasks.created_at, tasks.id) > (%s, %s)) OR vtp.position IS NULL)",
				p1, p2, p3, p4))
		} else {
			p1 := b.placeholder()
			b.args = append(b.args, ca)
			p2 := b.placeholder()
			b.args = append(b.args, id)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf("(vtp.position IS NULL AND (tasks.created_at, tasks.id) > (%s, %s))", p1, p2))
		}
	case "importance":
		imp := int64(*cur.SortNumVal)
		p1 := b.placeholder()
		b.args = append(b.args, imp)
		p2 := b.placeholder()
		b.args = append(b.args, imp)
		p3 := b.placeholder()
		b.args = append(b.args, ca)
		p4 := b.placeholder()
		b.args = append(b.args, id)
		b.whereClauses = append(b.whereClauses, fmt.Sprintf("(importance < %s OR (importance = %s AND (created_at, id) > (%s, %s)))", p1, p2, p3, p4))
	case "title":
		t := *cur.SortStrVal
		p1 := b.placeholder()
		b.args = append(b.args, t)
		p2 := b.placeholder()
		b.args = append(b.args, t)
		p3 := b.placeholder()
		b.args = append(b.args, ca)
		p4 := b.placeholder()
		b.args = append(b.args, id)
		b.whereClauses = append(b.whereClauses, fmt.Sprintf("(title > %s OR (title = %s AND (created_at, id) > (%s, %s)))", p1, p2, p3, p4))
	case "story_points":
		if cur.SortNumVal != nil {
			sp := int64(*cur.SortNumVal)
			p1 := b.placeholder()
			b.args = append(b.args, sp)
			p2 := b.placeholder()
			b.args = append(b.args, sp)
			p3 := b.placeholder()
			b.args = append(b.args, ca)
			p4 := b.placeholder()
			b.args = append(b.args, id)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf("(story_points < %s OR (story_points = %s AND (created_at, id) > (%s, %s)) OR story_points IS NULL)", p1, p2, p3, p4))
		} else {
			p1 := b.placeholder()
			b.args = append(b.args, ca)
			p2 := b.placeholder()
			b.args = append(b.args, id)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf("(story_points IS NULL AND (created_at, id) > (%s, %s))", p1, p2))
		}
	case "start_date":
		if cur.SortTimeVal != nil {
			d := *cur.SortTimeVal
			p1 := b.placeholder()
			b.args = append(b.args, d)
			p2 := b.placeholder()
			b.args = append(b.args, d)
			p3 := b.placeholder()
			b.args = append(b.args, ca)
			p4 := b.placeholder()
			b.args = append(b.args, id)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf("(start_date > %s::date OR (start_date = %s::date AND (created_at, id) > (%s, %s)) OR start_date IS NULL)", p1, p2, p3, p4))
		} else {
			p1 := b.placeholder()
			b.args = append(b.args, ca)
			p2 := b.placeholder()
			b.args = append(b.args, id)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf("(start_date IS NULL AND (created_at, id) > (%s, %s))", p1, p2))
		}
	case "due_date":
		if cur.SortTimeVal != nil {
			d := *cur.SortTimeVal
			p1 := b.placeholder()
			b.args = append(b.args, d)
			p2 := b.placeholder()
			b.args = append(b.args, d)
			p3 := b.placeholder()
			b.args = append(b.args, ca)
			p4 := b.placeholder()
			b.args = append(b.args, id)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf("(due_date > %s::date OR (due_date = %s::date AND (created_at, id) > (%s, %s)) OR due_date IS NULL)", p1, p2, p3, p4))
		} else {
			p1 := b.placeholder()
			b.args = append(b.args, ca)
			p2 := b.placeholder()
			b.args = append(b.args, id)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf("(due_date IS NULL AND (created_at, id) > (%s, %s))", p1, p2))
		}
	default:
		if cur.SortBy != "" && sort.CFType != "" {
			applyCFCursorWhere(b, cur, sort, ca, id)
			return
		}
		p1 := b.placeholder()
		b.args = append(b.args, ca)
		p2 := b.placeholder()
		b.args = append(b.args, id)
		b.whereClauses = append(b.whereClauses, fmt.Sprintf("((created_at, id) > (%s, %s))", p1, p2))
	}
}

// applyCFCursorWhere builds the keyset WHERE predicate for custom-field sorts.
func applyCFCursorWhere(b *queryBuilder, cur *taskdom.TaskCursor, sort taskdom.TaskSort, ca interface{}, id string) {
	switch sort.CFType {
	case "number":
		if cur.SortNumVal != nil {
			v := *cur.SortNumVal
			keyP := b.placeholder()
			b.args = append(b.args, sort.By)
			vP := b.placeholder()
			b.args = append(b.args, v)
			keyP2 := b.placeholder()
			b.args = append(b.args, sort.By)
			vP2 := b.placeholder()
			b.args = append(b.args, v)
			caP := b.placeholder()
			b.args = append(b.args, ca)
			idP := b.placeholder()
			b.args = append(b.args, id)
			keyP3 := b.placeholder()
			b.args = append(b.args, sort.By)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf(
				"(%s > %s OR (%s = %s AND (created_at, id) > (%s, %s)) OR custom_fields->>%s IS NULL)",
				customFieldNumericExpr(keyP), vP, customFieldNumericExpr(keyP2), vP2, caP, idP, keyP3))
		} else {
			keyP := b.placeholder()
			b.args = append(b.args, sort.By)
			caP := b.placeholder()
			b.args = append(b.args, ca)
			idP := b.placeholder()
			b.args = append(b.args, id)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf("(custom_fields->>%s IS NULL AND (created_at, id) > (%s, %s))", keyP, caP, idP))
		}
	case "date":
		if cur.SortTimeVal != nil {
			d := *cur.SortTimeVal
			keyP := b.placeholder()
			b.args = append(b.args, sort.By)
			dP := b.placeholder()
			b.args = append(b.args, d)
			keyP2 := b.placeholder()
			b.args = append(b.args, sort.By)
			dP2 := b.placeholder()
			b.args = append(b.args, d)
			caP := b.placeholder()
			b.args = append(b.args, ca)
			idP := b.placeholder()
			b.args = append(b.args, id)
			keyP3 := b.placeholder()
			b.args = append(b.args, sort.By)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf(
				"(%s > %s::date OR (%s = %s::date AND (created_at, id) > (%s, %s)) OR custom_fields->>%s IS NULL)",
				customFieldDateExpr(keyP), dP, customFieldDateExpr(keyP2), dP2, caP, idP, keyP3))
		} else {
			keyP := b.placeholder()
			b.args = append(b.args, sort.By)
			caP := b.placeholder()
			b.args = append(b.args, ca)
			idP := b.placeholder()
			b.args = append(b.args, id)
			b.whereClauses = append(b.whereClauses, fmt.Sprintf("(custom_fields->>%s IS NULL AND (created_at, id) > (%s, %s))", keyP, caP, idP))
		}
	case "select":
		curIdx := 9999
		if cur.SortStrVal != nil {
			for i, opt := range sort.CFOpts {
				if opt == *cur.SortStrVal {
					curIdx = i
					break
				}
			}
		}
		caseSQL1, _ := buildCFSelectCaseSQL(sort.By, sort.CFOpts, b)
		curIdxP := b.placeholder()
		b.args = append(b.args, curIdx)
		caseSQL2, _ := buildCFSelectCaseSQL(sort.By, sort.CFOpts, b)
		curIdxP2 := b.placeholder()
		b.args = append(b.args, curIdx)
		caP := b.placeholder()
		b.args = append(b.args, ca)
		idP := b.placeholder()
		b.args = append(b.args, id)
		b.whereClauses = append(b.whereClauses, fmt.Sprintf(
			"((%s) > %s OR ((%s) = %s AND (created_at, id) > (%s, %s)))",
			caseSQL1, curIdxP, caseSQL2, curIdxP2, caP, idP))
	default:
		p1 := b.placeholder()
		b.args = append(b.args, ca)
		p2 := b.placeholder()
		b.args = append(b.args, id)
		b.whereClauses = append(b.whereClauses, fmt.Sprintf("((created_at, id) > (%s, %s))", p1, p2))
	}
}

// ListTasks returns a page of tasks with optional filter.
func (r *TaskRepository) ListTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, limit int, sort taskdom.TaskSort) ([]*taskdom.Task, bool, error) {
	b := newQueryBuilder()

	// Base fixed args
	pidP := b.placeholder()
	b.args = append(b.args, projectID.String())
	baseWhere := "tasks.project_id = " + pidP + " AND tasks.deleted_at IS NULL"

	// Apply sort first (it may add a JOIN arg for view_id)
	fromClause, orderByClause, selectCols := applyTaskSort(sort, b)

	// Apply filters
	applyTaskFilter(b, filter)

	// Apply cursor
	if filter.CursorAfter != nil {
		cur, err := taskdom.DecodeTaskCursor(*filter.CursorAfter)
		if err != nil {
			return nil, false, fmt.Errorf("task repo: invalid cursor: %w", err)
		}
		applyCursorWhere(b, cur, sort)
	}

	limitP := b.placeholder()
	b.args = append(b.args, limit+1)

	whereSQL := baseWhere
	if len(b.whereClauses) > 0 {
		whereSQL += " AND " + strings.Join(b.whereClauses, " AND ")
	}

	query := fmt.Sprintf(`SELECT %s %s WHERE %s ORDER BY %s LIMIT %s`, selectCols, fromClause, whereSQL, orderByClause, limitP)

	if sort.By == "view_position" && sort.ViewID != nil {
		var rows []taskWithPositionRow
		if err := r.db.SelectContext(ctx, &rows, query, b.args...); err != nil {
			return nil, false, fmt.Errorf("task repo: list (view_position): %w", err)
		}
		hasMore := len(rows) > limit
		if hasMore {
			rows = rows[:limit]
		}
		tasks := make([]*taskdom.Task, 0, len(rows))
		for i := range rows {
			rec := rows[i].asTaskRecord()
			t, err := toTaskEntity(&rec)
			if err != nil {
				return nil, false, err
			}
			t.ViewPosition = rows[i].VTPPosition
			tasks = append(tasks, t)
		}
		if err := r.attachAssigneeIDs(ctx, tasks); err != nil {
			return nil, false, err
		}
		return tasks, hasMore, nil
	}

	var records []taskRecord
	if err := r.db.SelectContext(ctx, &records, query, b.args...); err != nil {
		return nil, false, fmt.Errorf("task repo: list: %w", err)
	}

	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}

	tasks := make([]*taskdom.Task, 0, len(records))
	for i := range records {
		t, err := toTaskEntity(&records[i])
		if err != nil {
			return nil, false, err
		}
		tasks = append(tasks, t)
	}
	if err := r.attachAssigneeIDs(ctx, tasks); err != nil {
		return nil, false, err
	}
	return tasks, hasMore, nil
}

// CountTasks returns the total number of tasks matching filter for a project.
func (r *TaskRepository) CountTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter) (int64, error) {
	b := newQueryBuilder()

	pidP := b.placeholder()
	b.args = append(b.args, projectID.String())
	baseWhere := "project_id = " + pidP + " AND deleted_at IS NULL"

	applyTaskFilter(b, filter)

	whereSQL := baseWhere
	if len(b.whereClauses) > 0 {
		whereSQL += " AND " + strings.Join(b.whereClauses, " AND ")
	}

	var count int64
	if err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM tasks WHERE `+whereSQL, b.args...); err != nil {
		return 0, fmt.Errorf("task repo: count: %w", err)
	}
	return count, nil
}

// SumTaskField sums a numeric task field across all tasks matching filter.
func (r *TaskRepository) SumTaskField(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, fieldKey string) (float64, error) {
	b := newQueryBuilder()

	pidP := b.placeholder()
	b.args = append(b.args, projectID.String())
	baseWhere := "project_id = " + pidP + " AND deleted_at IS NULL"

	applyTaskFilter(b, filter)

	whereSQL := baseWhere
	if len(b.whereClauses) > 0 {
		whereSQL += " AND " + strings.Join(b.whereClauses, " AND ")
	}

	var sum float64
	var err error
	if fieldKey == "story_points" {
		err = r.db.GetContext(ctx, &sum, `SELECT COALESCE(SUM(story_points), 0) FROM tasks WHERE `+whereSQL, b.args...)
	} else {
		keyP := b.placeholder()
		b.args = append(b.args, fieldKey)
		err = r.db.GetContext(ctx, &sum, `SELECT COALESCE(SUM(`+customFieldNumericExpr(keyP)+`), 0) FROM tasks WHERE `+whereSQL, b.args...)
	}
	if err != nil {
		return 0, fmt.Errorf("task repo: sum field %q: %w", fieldKey, err)
	}
	return sum, nil
}

// FindTaskByID returns the task with the given ID (non-deleted).
func (r *TaskRepository) FindTaskByID(ctx context.Context, id uuid.UUID) (*taskdom.Task, error) {
	var rec taskRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+taskCols+` FROM tasks WHERE id = $1 AND deleted_at IS NULL`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, taskdom.ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("task repo: find by id: %w", err)
	}
	t, err := toTaskEntity(&rec)
	if err != nil {
		return nil, err
	}
	if err := r.attachAssigneeIDs(ctx, []*taskdom.Task{t}); err != nil {
		return nil, err
	}
	return t, nil
}

// FindTaskByNumber returns the task with the given project-scoped task number (non-deleted).
func (r *TaskRepository) FindTaskByNumber(ctx context.Context, projectID uuid.UUID, taskNumber int64) (*taskdom.Task, error) {
	var rec taskRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+taskCols+` FROM tasks WHERE project_id = $1 AND task_number = $2 AND deleted_at IS NULL`, projectID.String(), taskNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, taskdom.ErrTaskNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("task repo: find by number: %w", err)
	}
	t, err := toTaskEntity(&rec)
	if err != nil {
		return nil, err
	}
	if err := r.attachAssigneeIDs(ctx, []*taskdom.Task{t}); err != nil {
		return nil, err
	}
	return t, nil
}

// CreateTask persists a new task, assigning the next per-project task_number atomically.
func (r *TaskRepository) CreateTask(ctx context.Context, t *taskdom.Task) error {
	cf, err := json.Marshal(t.CustomFields)
	if err != nil {
		return fmt.Errorf("task repo: marshal custom_fields: %w", err)
	}
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("task repo: marshal tags: %w", err)
	}

	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		// Atomically increment the per-project counter and retrieve its new value.
		var counter taskCounterRecord
		if err := tx.GetContext(ctx, &counter, `
			INSERT INTO task_counters (project_id, last_value)
			VALUES ($1, 1)
			ON CONFLICT (project_id) DO UPDATE
			  SET last_value = task_counters.last_value + 1
			RETURNING project_id, last_value`,
			t.ProjectID.String(),
		); err != nil {
			return fmt.Errorf("task repo: increment counter: %w", err)
		}
		t.TaskNumber = counter.LastValue

		_, err := tx.ExecContext(ctx, `
			INSERT INTO tasks (id, project_id, task_number, task_type_id, status_id, sprint_id, parent_task_id,
			  title, description, importance, story_points, reporter_id,
			  custom_fields, start_date, due_date, tags, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
			t.ID.String(), t.ProjectID.String(), t.TaskNumber,
			uuidPtrToStrPtr(t.TaskTypeID), uuidPtrToStrPtr(t.StatusID),
			uuidPtrToStrPtr(t.SprintID), uuidPtrToStrPtr(t.ParentTaskID),
			t.Title, t.Description, t.Importance, t.StoryPoints,
			uuidPtrToStrPtr(t.ReporterID),
			cf, t.StartDate, t.DueDate, tagsJSON, t.CreatedAt, t.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("task repo: create: %w", err)
		}
		if err := insertTaskAssignees(ctx, tx, t.ID, t.AssigneeIDs); err != nil {
			return err
		}
		return nil
	})
}

// UpdateTask persists changes to an existing task.
func (r *TaskRepository) UpdateTask(ctx context.Context, t *taskdom.Task) error {
	cf, err := json.Marshal(t.CustomFields)
	if err != nil {
		return fmt.Errorf("task repo: marshal custom_fields: %w", err)
	}
	tags := t.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("task repo: marshal tags: %w", err)
	}
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE tasks SET
			  task_type_id=$1, status_id=$2, sprint_id=$3, parent_task_id=$4,
			  title=$5, description=$6, importance=$7, story_points=$8,
			  reporter_id=$9, custom_fields=$10,
			  start_date=$11, due_date=$12, tags=$13, updated_at=$14
			WHERE id=$15`,
			uuidPtrToStrPtr(t.TaskTypeID), uuidPtrToStrPtr(t.StatusID),
			uuidPtrToStrPtr(t.SprintID), uuidPtrToStrPtr(t.ParentTaskID),
			t.Title, t.Description, t.Importance, t.StoryPoints,
			uuidPtrToStrPtr(t.ReporterID),
			cf, t.StartDate, t.DueDate, tagsJSON, t.UpdatedAt, t.ID.String(),
		)
		if err != nil {
			return fmt.Errorf("task repo: update: %w", err)
		}
		if err := syncTaskAssignees(ctx, tx, t.ID, t.AssigneeIDs); err != nil {
			return err
		}
		return nil
	})
}

// syncTaskAssignees reconciles task_assignees with wantIDs by removing only
// members no longer present and inserting only members newly present,
// leaving unchanged members' rows (and their assigned_at) untouched. A naive
// delete-all-then-reinsert would reset assigned_at for every assignee on
// every task update — including ones that don't touch assignees at all —
// which both loses assignment history and makes the assigned_at-ordered
// assignee list (used for e.g. "first assignee" swimlane grouping) shuffle
// nondeterministically after unrelated edits, since a bulk reinsert gives
// every row the same NOW() timestamp.
func syncTaskAssignees(ctx context.Context, tx *sqlx.Tx, taskID uuid.UUID, wantIDs []uuid.UUID) error {
	var currentIDStrs []string
	if err := tx.SelectContext(ctx, &currentIDStrs, `SELECT member_id FROM task_assignees WHERE task_id=$1`, taskID.String()); err != nil {
		return fmt.Errorf("task repo: update: read current assignees: %w", err)
	}
	current := make(map[string]struct{}, len(currentIDStrs))
	for _, id := range currentIDStrs {
		current[id] = struct{}{}
	}
	want := make(map[string]struct{}, len(wantIDs))
	for _, id := range wantIDs {
		want[id.String()] = struct{}{}
	}

	var toRemove []string
	for id := range current {
		if _, ok := want[id]; !ok {
			toRemove = append(toRemove, id)
		}
	}
	if len(toRemove) > 0 {
		placeholders := make([]string, len(toRemove))
		args := make([]interface{}, 0, len(toRemove)+1)
		args = append(args, taskID.String())
		for i, id := range toRemove {
			placeholders[i] = fmt.Sprintf("$%d", i+2)
			args = append(args, id)
		}
		query := `DELETE FROM task_assignees WHERE task_id=$1 AND member_id IN (` + strings.Join(placeholders, ",") + `)`
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("task repo: update: remove assignees: %w", err)
		}
	}

	var toAdd []uuid.UUID
	for _, id := range wantIDs {
		if _, ok := current[id.String()]; !ok {
			toAdd = append(toAdd, id)
		}
	}
	return insertTaskAssignees(ctx, tx, taskID, toAdd)
}

// insertTaskAssignees bulk-inserts task_assignees rows for taskID. No-op when
// memberIDs is empty. ON CONFLICT DO NOTHING guards against duplicate member
// IDs in the input (the PK is (task_id, member_id)).
func insertTaskAssignees(ctx context.Context, tx *sqlx.Tx, taskID uuid.UUID, memberIDs []uuid.UUID) error {
	if len(memberIDs) == 0 {
		return nil
	}
	valuePlaceholders := make([]string, len(memberIDs))
	args := make([]interface{}, 0, len(memberIDs)*2)
	for i, mid := range memberIDs {
		valuePlaceholders[i] = fmt.Sprintf("($%d,$%d)", i*2+1, i*2+2)
		args = append(args, taskID.String(), mid.String())
	}
	query := `INSERT INTO task_assignees (task_id, member_id) VALUES ` + strings.Join(valuePlaceholders, ",") + ` ON CONFLICT DO NOTHING`
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("task repo: insert assignees: %w", err)
	}
	return nil
}

// attachAssigneeIDs batch-loads task_assignees for tasks and sets each
// Task.AssigneeIDs in place, avoiding an N+1 query per task. tasks with no
// assignees get an empty (non-nil) slice.
func (r *TaskRepository) attachAssigneeIDs(ctx context.Context, tasks []*taskdom.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	byID := make(map[string]*taskdom.Task, len(tasks))
	placeholders := make([]string, len(tasks))
	args := make([]interface{}, len(tasks))
	for i, t := range tasks {
		t.AssigneeIDs = []uuid.UUID{}
		id := t.ID.String()
		byID[id] = t
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	type assigneeRow struct {
		TaskID   string `db:"task_id"`
		MemberID string `db:"member_id"`
	}
	var rows []assigneeRow
	query := `SELECT task_id, member_id FROM task_assignees WHERE task_id IN (` +
		strings.Join(placeholders, ",") + `) ORDER BY assigned_at ASC`
	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return fmt.Errorf("task repo: attach assignees: %w", err)
	}
	for _, row := range rows {
		t, ok := byID[row.TaskID]
		if !ok {
			continue
		}
		memberID, err := uuid.Parse(row.MemberID)
		if err != nil {
			continue
		}
		t.AssigneeIDs = append(t.AssigneeIDs, memberID)
	}
	return nil
}

// DeleteTask soft-deletes a task by setting deleted_at.
func (r *TaskRepository) DeleteTask(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `UPDATE tasks SET deleted_at=$1 WHERE id=$2 AND deleted_at IS NULL`, time.Now(), id.String())
	if err != nil {
		return fmt.Errorf("task repo: delete: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return taskdom.ErrTaskNotFound
	}
	return nil
}

// BulkMoveSprintTasks reassigns all non-done tasks that belong to sourceSprintID
// to targetSprintID (nil = backlog) in a single UPDATE statement.
func (r *TaskRepository) BulkMoveSprintTasks(ctx context.Context, projectID, sourceSprintID uuid.UUID, targetSprintID *uuid.UUID) error {
	var targetSprintStr *string
	if targetSprintID != nil {
		s := targetSprintID.String()
		targetSprintStr = &s
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE tasks SET sprint_id=$1, updated_at=$2
		WHERE project_id=$3 AND sprint_id=$4
		  AND (status_id IS NULL OR status_id NOT IN (
		    SELECT id FROM task_statuses WHERE project_id=$3 AND category=$5
		  ))`,
		targetSprintStr, time.Now(), projectID.String(), sourceSprintID.String(), string(taskdom.StatusCategoryDone),
	)
	if err != nil {
		return fmt.Errorf("task repo: bulk move sprint tasks: %w", err)
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
		IsDefault:   r.IsDefault,
		IsSystem:    r.IsSystem,
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
		IsDefault: r.IsDefault,
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

	var tags []string
	if len(r.Tags) > 0 {
		if err := json.Unmarshal(r.Tags, &tags); err != nil {
			return nil, fmt.Errorf("task repo: unmarshal tags: %w", err)
		}
	}
	if tags == nil {
		tags = []string{}
	}

	var desc json.RawMessage
	if r.Description != nil {
		desc = *r.Description
	}

	return &taskdom.Task{
		ID:           id,
		ProjectID:    pid,
		TaskNumber:   r.TaskNumber,
		TaskTypeID:   strPtrToUUIDPtr(r.TaskTypeID),
		StatusID:     strPtrToUUIDPtr(r.StatusID),
		SprintID:     strPtrToUUIDPtr(r.SprintID),
		ParentTaskID: strPtrToUUIDPtr(r.ParentTaskID),
		Title:        r.Title,
		Description:  desc,
		Importance:   r.Importance,
		StoryPoints:  r.StoryPoints,
		ReporterID:   strPtrToUUIDPtr(r.ReporterID),
		CustomFields: cf,
		StartDate:    r.StartDate,
		DueDate:      r.DueDate,
		Tags:         tags,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		DeletedAt:    r.DeletedAt,
	}, nil
}

// --- Custom Field Definitions -----------------------------------------------

type customFieldDefinitionRecord struct {
	ID          string    `db:"id"`
	ProjectID   string    `db:"project_id"`
	FieldKey    string    `db:"field_key"`
	DisplayName string    `db:"display_name"`
	FieldType   string    `db:"field_type"`
	Options     []byte    `db:"options"`
	IsRequired  bool      `db:"is_required"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

const customFieldCols = `id, project_id, field_key, display_name, field_type, options, is_required, created_at, updated_at`

// ListCustomFieldDefinitions returns all custom field definitions for a project ordered by display_name.
func (r *TaskRepository) ListCustomFieldDefinitions(ctx context.Context, projectID uuid.UUID) ([]*taskdom.CustomFieldDefinition, error) {
	var records []customFieldDefinitionRecord
	if err := r.db.SelectContext(ctx, &records, `SELECT `+customFieldCols+` FROM custom_field_definitions WHERE project_id = $1 ORDER BY display_name ASC`, projectID.String()); err != nil {
		return nil, fmt.Errorf("custom field repo: list: %w", err)
	}
	out := make([]*taskdom.CustomFieldDefinition, 0, len(records))
	for i := range records {
		f, err := toCustomFieldEntity(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// FindCustomFieldDefinitionByID returns the custom field definition with the given ID.
func (r *TaskRepository) FindCustomFieldDefinitionByID(ctx context.Context, id uuid.UUID) (*taskdom.CustomFieldDefinition, error) {
	var rec customFieldDefinitionRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+customFieldCols+` FROM custom_field_definitions WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, taskdom.ErrCustomFieldNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("custom field repo: find by id: %w", err)
	}
	return toCustomFieldEntity(&rec)
}

// CreateCustomFieldDefinition persists a new custom field definition.
func (r *TaskRepository) CreateCustomFieldDefinition(ctx context.Context, f *taskdom.CustomFieldDefinition) error {
	opts, err := marshalOptions(f.Options)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO custom_field_definitions (id, project_id, field_key, display_name, field_type, options, is_required, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		f.ID.String(), f.ProjectID.String(), f.FieldKey, f.DisplayName,
		string(f.FieldType), opts, f.IsRequired, f.CreatedAt, f.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return taskdom.ErrCustomFieldKeyTaken
		}
		return fmt.Errorf("custom field repo: create: %w", err)
	}
	return nil
}

// UpdateCustomFieldDefinition persists changes to an existing custom field definition.
func (r *TaskRepository) UpdateCustomFieldDefinition(ctx context.Context, f *taskdom.CustomFieldDefinition) error {
	opts, err := marshalOptions(f.Options)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE custom_field_definitions SET display_name=$1, field_type=$2, options=$3, is_required=$4, updated_at=$5
		WHERE id=$6`,
		f.DisplayName, string(f.FieldType), opts, f.IsRequired, f.UpdatedAt, f.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("custom field repo: update: %w", err)
	}
	return nil
}

// DeleteCustomFieldDefinition removes a custom field definition by ID and
// purges its value from every task in the project's custom_fields JSONB
// column. Without this, a deleted field's stray/stale values remain attached
// to tasks forever; if a new field is later created reusing the same
// field_key (e.g. because the display name is unchanged), it silently
// inherits data from the old, possibly incompatible, field type.
func (r *TaskRepository) DeleteCustomFieldDefinition(ctx context.Context, id uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var deleted struct {
			ProjectID string `db:"project_id"`
			FieldKey  string `db:"field_key"`
		}
		err := tx.GetContext(ctx, &deleted,
			`DELETE FROM custom_field_definitions WHERE id = $1 RETURNING project_id, field_key`,
			id.String(),
		)
		if errors.Is(err, sql.ErrNoRows) {
			return taskdom.ErrCustomFieldNotFound
		}
		if err != nil {
			return fmt.Errorf("custom field repo: delete: %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE tasks SET custom_fields = custom_fields - $1 WHERE project_id = $2`,
			deleted.FieldKey, deleted.ProjectID,
		); err != nil {
			return fmt.Errorf("custom field repo: delete: purge task values: %w", err)
		}
		return nil
	})
}

func toCustomFieldEntity(r *customFieldDefinitionRecord) (*taskdom.CustomFieldDefinition, error) {
	id, _ := uuid.Parse(r.ID)
	pid, _ := uuid.Parse(r.ProjectID)

	var opts []string
	if len(r.Options) > 0 {
		if err := json.Unmarshal(r.Options, &opts); err != nil {
			return nil, fmt.Errorf("custom field repo: unmarshal options: %w", err)
		}
	}
	if opts == nil {
		opts = []string{}
	}

	return &taskdom.CustomFieldDefinition{
		ID:          id,
		ProjectID:   pid,
		FieldKey:    r.FieldKey,
		DisplayName: r.DisplayName,
		FieldType:   taskdom.FieldType(r.FieldType),
		Options:     opts,
		IsRequired:  r.IsRequired,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}, nil
}

func marshalOptions(opts []string) ([]byte, error) {
	if opts == nil {
		opts = []string{}
	}
	b, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("custom field repo: marshal options: %w", err)
	}
	return b, nil
}

// --- helpers ----------------------------------------------------------------

func uuidSliceToStrSlice(ids []uuid.UUID) []string {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	return s
}

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
