// Package postgres — sqlx implementation of sprintdom.SprintRepository.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
)

// --- sqlx model -------------------------------------------------------------

type sprintRecord struct {
	ID        string     `db:"id"`
	ProjectID string     `db:"project_id"`
	Name      string     `db:"name"`
	StartDate *time.Time `db:"start_date"`
	EndDate   *time.Time `db:"end_date"`
	Goal      *string    `db:"goal"`
	Status    string     `db:"status"`
	CreatedAt time.Time  `db:"created_at"`
	UpdatedAt time.Time  `db:"updated_at"`
}

// --- Repository struct -------------------------------------------------------

// SprintRepository is the sqlx implementation of sprintdom.SprintRepository.
type SprintRepository struct {
	db *sqlx.DB
}

// NewSprintRepository returns a new SprintRepository.
func NewSprintRepository(db *sqlx.DB) *SprintRepository {
	return &SprintRepository{db: db}
}

const sprintSelectCols = `id, project_id, name, start_date, end_date, goal, status, created_at, updated_at`

// --- Methods ----------------------------------------------------------------

// ListSprints returns all sprints for a project ordered by creation time.
func (r *SprintRepository) ListSprints(ctx context.Context, projectID uuid.UUID) ([]*sprintdom.Sprint, error) {
	var records []sprintRecord
	if err := r.db.SelectContext(ctx, &records, `SELECT `+sprintSelectCols+` FROM sprints WHERE project_id = $1 ORDER BY created_at ASC`, projectID.String()); err != nil {
		return nil, fmt.Errorf("sprint repo: list: %w", err)
	}
	out := make([]*sprintdom.Sprint, 0, len(records))
	for i := range records {
		out = append(out, toSprintEntity(&records[i]))
	}
	return out, nil
}

// FindSprintByID returns the sprint with the given ID.
func (r *SprintRepository) FindSprintByID(ctx context.Context, id uuid.UUID) (*sprintdom.Sprint, error) {
	var rec sprintRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+sprintSelectCols+` FROM sprints WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sprintdom.ErrSprintNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sprint repo: find by id: %w", err)
	}
	return toSprintEntity(&rec), nil
}

// CreateSprint persists a new sprint.
func (r *SprintRepository) CreateSprint(ctx context.Context, s *sprintdom.Sprint) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sprints (id, project_id, name, start_date, end_date, goal, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		s.ID.String(), s.ProjectID.String(), s.Name, s.StartDate, s.EndDate,
		s.Goal, string(s.Status), s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("sprint repo: create: %w", err)
	}
	return nil
}

// UpdateSprint persists changes to an existing sprint.
func (r *SprintRepository) UpdateSprint(ctx context.Context, s *sprintdom.Sprint) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE sprints SET name = $1, start_date = $2, end_date = $3, goal = $4, status = $5, updated_at = $6
		WHERE id = $7`,
		s.Name, s.StartDate, s.EndDate, s.Goal, string(s.Status), s.UpdatedAt, s.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("sprint repo: update: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sprintdom.ErrSprintNotFound
	}
	return nil
}

// DeleteSprint removes a sprint by ID.
func (r *SprintRepository) DeleteSprint(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM sprints WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("sprint repo: delete: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sprintdom.ErrSprintNotFound
	}
	return nil
}

// --- Entity converter -------------------------------------------------------

func toSprintEntity(r *sprintRecord) *sprintdom.Sprint {
	id, _ := uuid.Parse(r.ID)
	pid, _ := uuid.Parse(r.ProjectID)
	return &sprintdom.Sprint{
		ID:        id,
		ProjectID: pid,
		Name:      r.Name,
		StartDate: r.StartDate,
		EndDate:   r.EndDate,
		Goal:      r.Goal,
		Status:    sprintdom.SprintStatus(r.Status),
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}
