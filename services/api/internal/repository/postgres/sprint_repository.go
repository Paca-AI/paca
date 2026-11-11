// Package postgres — sqlx implementation of sprintdom.SprintRepository.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
	Version   int64      `db:"version"`
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

const sprintSelectCols = `id, project_id, name, start_date, end_date, goal, status, created_at, updated_at, version`

// --- Methods ----------------------------------------------------------------

// defaultSprintPageSize is the page size sprint lists use when none is given.
const defaultSprintPageSize = 30

// sprintFilterSQL appends the status, search and id conditions of f, adding
// their arguments to sink.
func sprintFilterSQL(f sprintdom.SprintFilter, sink *argList) string {
	var q string
	if len(f.Statuses) > 0 {
		statuses := make([]string, len(f.Statuses))
		for i, st := range f.Statuses {
			statuses[i] = string(st)
		}
		q += ` AND status = ANY(` + sink.addArg(statuses) + `::text[])`
	}
	for _, word := range strings.Fields(f.Search) {
		q += ` AND LOWER(name) LIKE LOWER(` + sink.addArg("%"+escapeLike(word)+"%") + `) ESCAPE '\'`
	}
	if len(f.IDs) > 0 {
		ids := make([]string, len(f.IDs))
		for i, id := range f.IDs {
			ids[i] = id.String()
		}
		q += ` AND id = ANY(` + sink.addArg(ids) + `::uuid[])`
	}
	return q
}

// ListSprints returns one page of a project's sprints, by (LOWER(name), id)
// ascending or, for SprintSortRecent, (updated_at, id) descending. total is
// the number of sprints matching the filter, before paging.
func (r *SprintRepository) ListSprints(ctx context.Context, projectID uuid.UUID, filter sprintdom.SprintFilter, cursorAfter *string, limit int) ([]*sprintdom.Sprint, bool, int, error) {
	if limit <= 0 {
		limit = defaultSprintPageSize
	}
	sink := &argList{args: []any{projectID.String()}}
	clause, none, err := scopeSQL(ctx, "sprint", sprintScopeColumns, sink)
	if err != nil {
		return nil, false, 0, fmt.Errorf("sprint repo: list: %w", err)
	}
	if none {
		return []*sprintdom.Sprint{}, false, 0, nil
	}
	where := ` WHERE project_id = $1`
	if clause != "" {
		where += " AND " + clause
	}
	where += sprintFilterSQL(filter, sink)

	var total int
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM sprints`+where, sink.args...); err != nil {
		return nil, false, 0, fmt.Errorf("sprint repo: count: %w", err)
	}

	recent := filter.Sort == sprintdom.SprintSortRecent
	if cursorAfter != nil {
		name, updatedAt, id, err := sprintdom.DecodeCursor(*cursorAfter)
		if err != nil {
			return nil, false, 0, err
		}
		if recent {
			where += ` AND (updated_at, id) < (` + sink.addArg(updatedAt) + `, ` + sink.addArg(id.String()) + `::uuid)`
		} else {
			where += ` AND (LOWER(name), id) > (LOWER(` + sink.addArg(name) + `::text), ` + sink.addArg(id.String()) + `::uuid)`
		}
	}
	order := ` ORDER BY LOWER(name) ASC, id ASC`
	if recent {
		order = ` ORDER BY updated_at DESC, id DESC`
	}
	var records []sprintRecord
	q := `SELECT ` + sprintSelectCols + ` FROM sprints` + where + order + fmt.Sprintf(` LIMIT %d`, limit+1)
	if err := r.db.SelectContext(ctx, &records, q, sink.args...); err != nil {
		return nil, false, 0, fmt.Errorf("sprint repo: list: %w", err)
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	out := make([]*sprintdom.Sprint, 0, len(records))
	for i := range records {
		out = append(out, toSprintEntity(&records[i]))
	}
	return out, hasMore, total, nil
}

// ListAllSprints returns every sprint of a project the caller may read,
// oldest first. Not exposed over the API.
func (r *SprintRepository) ListAllSprints(ctx context.Context, projectID uuid.UUID) ([]*sprintdom.Sprint, error) {
	sink := &argList{args: []any{projectID.String()}}
	clause, none, err := scopeSQL(ctx, "sprint", sprintScopeColumns, sink)
	if err != nil {
		return nil, fmt.Errorf("sprint repo: list: %w", err)
	}
	if none {
		return []*sprintdom.Sprint{}, nil
	}
	scopeAnd := ""
	if clause != "" {
		scopeAnd = " AND " + clause
	}
	var records []sprintRecord
	if err := r.db.SelectContext(ctx, &records, `SELECT `+sprintSelectCols+` FROM sprints WHERE project_id = $1`+scopeAnd+` ORDER BY created_at ASC`, sink.args...); err != nil {
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
	s.Version = 1
	return nil
}

// UpdateSprintAtomic implements sprintdom.SprintRepository — see its doc comment.
func (r *SprintRepository) UpdateSprintAtomic(ctx context.Context, id uuid.UUID, decide func(current *sprintdom.Sprint) error) (*sprintdom.Sprint, error) {
	var out *sprintdom.Sprint
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var rec sprintRecord
		err := tx.GetContext(ctx, &rec, `SELECT `+sprintSelectCols+` FROM sprints WHERE id = $1 FOR UPDATE`, id.String())
		if errors.Is(err, sql.ErrNoRows) {
			return sprintdom.ErrSprintNotFound
		}
		if err != nil {
			return fmt.Errorf("sprint repo: find by id for update: %w", err)
		}
		s := toSprintEntity(&rec)
		if err := decide(s); err != nil {
			return err
		}
		if err := tx.QueryRowxContext(ctx, `
			UPDATE sprints SET name = $1, start_date = $2, end_date = $3, goal = $4, status = $5, updated_at = $6
			WHERE id = $7
			RETURNING version`,
			s.Name, s.StartDate, s.EndDate, s.Goal, string(s.Status), s.UpdatedAt, s.ID.String(),
		).Scan(&s.Version); err != nil {
			return fmt.Errorf("sprint repo: update: %w", err)
		}
		out = s
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
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
		Version:   r.Version,
	}
}
