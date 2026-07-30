package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// --- sqlx model -------------------------------------------------------------

type taskActivityRecord struct {
	ID           string          `db:"id"`
	TaskID       string          `db:"task_id"`
	ActorID      *string         `db:"actor_id"`
	ActivityType string          `db:"activity_type"`
	Content      json.RawMessage `db:"content"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
	DeletedAt    *time.Time      `db:"deleted_at"`

	// Joined from the project_members + users tables.
	ActorFullName *string `db:"actor_full_name"`
	ActorUsername *string `db:"actor_username"`
}

// --- Repository struct -------------------------------------------------------

// TaskActivityRepository is the sqlx implementation of taskdom.ActivityRepository.
type TaskActivityRepository struct {
	db *sqlx.DB
}

// NewTaskActivityRepository returns a new TaskActivityRepository backed by db.
func NewTaskActivityRepository(db *sqlx.DB) *TaskActivityRepository {
	return &TaskActivityRepository{db: db}
}

// --- Mapping helpers --------------------------------------------------------

func activityFromRecord(r taskActivityRecord) *taskdom.Activity {
	a := &taskdom.Activity{
		ID:           uuid.MustParse(r.ID),
		TaskID:       uuid.MustParse(r.TaskID),
		ActivityType: taskdom.ActivityType(r.ActivityType),
		Content:      r.Content,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		DeletedAt:    r.DeletedAt,
	}
	if r.ActorID != nil {
		id := uuid.MustParse(*r.ActorID)
		a.ActorID = &id
	}
	if r.ActorFullName != nil {
		a.ActorName = *r.ActorFullName
	}
	if r.ActorUsername != nil {
		a.ActorUsername = *r.ActorUsername
	}
	return a
}

// --- CRUD -------------------------------------------------------------------

const taskActivityJoinSQL = `
	SELECT ta.id, ta.task_id, ta.actor_id, ta.activity_type, ta.content,
	       ta.created_at, ta.updated_at, ta.deleted_at,
	       COALESCE(u.full_name, ag.name) AS actor_full_name,
	       COALESCE(u.username, ag.handle) AS actor_username
	FROM task_activities ta
	LEFT JOIN project_members pm ON pm.id = ta.actor_id
	LEFT JOIN users u ON u.id = pm.user_id
	LEFT JOIN agents ag ON ag.id = pm.agent_id`

// ListActivities returns all non-deleted activities for a task, oldest first.
func (r *TaskActivityRepository) ListActivities(ctx context.Context, taskID uuid.UUID) ([]*taskdom.Activity, error) {
	var records []taskActivityRecord
	err := r.db.SelectContext(ctx, &records, taskActivityJoinSQL+`
		WHERE ta.task_id = $1 AND ta.deleted_at IS NULL
		ORDER BY ta.created_at ASC`, taskID.String())
	if err != nil {
		return nil, err
	}
	out := make([]*taskdom.Activity, 0, len(records))
	for _, rec := range records {
		out = append(out, activityFromRecord(rec))
	}
	return out, nil
}

// FindActivityByID returns a single activity (including soft-deleted).
func (r *TaskActivityRepository) FindActivityByID(ctx context.Context, id uuid.UUID) (*taskdom.Activity, error) {
	var rec taskActivityRecord
	err := r.db.GetContext(ctx, &rec, taskActivityJoinSQL+` WHERE ta.id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, taskdom.ErrActivityNotFound
	}
	if err != nil {
		return nil, err
	}
	return activityFromRecord(rec), nil
}

// CreateActivity persists a new activity record.
func (r *TaskActivityRepository) CreateActivity(ctx context.Context, a *taskdom.Activity) error {
	content := a.Content
	if content == nil {
		content = json.RawMessage("{}")
	}
	var actorID *string
	if a.ActorID != nil {
		s := a.ActorID.String()
		actorID = &s
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO task_activities (id, task_id, actor_id, activity_type, content, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		a.ID.String(), a.TaskID.String(), actorID, string(a.ActivityType), content, a.CreatedAt, a.UpdatedAt,
	)
	return err
}

// UpdateActivity updates the content and updated_at of an existing activity.
func (r *TaskActivityRepository) UpdateActivity(ctx context.Context, a *taskdom.Activity) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE task_activities SET content = $1, updated_at = $2 WHERE id = $3`,
		a.Content, a.UpdatedAt, a.ID.String(),
	)
	return err
}

// DeleteActivity soft-deletes an activity by setting deleted_at.
func (r *TaskActivityRepository) DeleteActivity(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE task_activities SET deleted_at = $1 WHERE id = $2`, time.Now(), id.String())
	return err
}
