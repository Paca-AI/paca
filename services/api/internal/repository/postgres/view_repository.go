// Package postgres — sqlx implementation of sprintdom.ViewRepository.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
)

// --- sqlx models ------------------------------------------------------------

type sprintViewRecord struct {
	ID          string    `db:"id"`
	SprintID    *string   `db:"sprint_id"`
	ProjectID   string    `db:"project_id"`
	Name        string    `db:"name"`
	ViewType    string    `db:"view_type"`
	Config      []byte    `db:"config"`
	Position    float64   `db:"position"`
	ViewContext string    `db:"view_context"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

type viewTaskPositionRecord struct {
	ID       string  `db:"id"`
	ViewID   string  `db:"view_id"`
	TaskID   string  `db:"task_id"`
	Position float64 `db:"position"`
	GroupKey *string `db:"group_key"`
}

// --- Repository struct -------------------------------------------------------

// ViewRepository is the sqlx implementation of sprintdom.ViewRepository.
type ViewRepository struct {
	db *sqlx.DB
}

// NewViewRepository returns a new ViewRepository.
func NewViewRepository(db *sqlx.DB) *ViewRepository {
	return &ViewRepository{db: db}
}

const sprintViewSelectCols = `id, sprint_id, project_id, name, view_type, config, position, view_context, created_at, updated_at`

// --- View methods -----------------------------------------------------------

// ListViews returns all views for a sprint ordered by position.
func (r *ViewRepository) ListViews(ctx context.Context, sprintID uuid.UUID) ([]*sprintdom.SprintView, error) {
	var records []sprintViewRecord
	if err := r.db.SelectContext(ctx, &records, `SELECT `+sprintViewSelectCols+` FROM sprint_views WHERE sprint_id = $1 ORDER BY position ASC, created_at ASC`, sprintID.String()); err != nil {
		return nil, fmt.Errorf("view repo: list: %w", err)
	}
	out := make([]*sprintdom.SprintView, 0, len(records))
	for i := range records {
		v, err := toViewEntity(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// FindViewByID returns the view with the given ID.
func (r *ViewRepository) FindViewByID(ctx context.Context, id uuid.UUID) (*sprintdom.SprintView, error) {
	var rec sprintViewRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+sprintViewSelectCols+` FROM sprint_views WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, sprintdom.ErrViewNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("view repo: find by id: %w", err)
	}
	return toViewEntity(&rec)
}

// ListProjectViews returns all views for a project filtered by viewCtx,
// ordered by position.  Use ViewContextBacklog or ViewContextTimeline.
func (r *ViewRepository) ListProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext) ([]*sprintdom.SprintView, error) {
	var records []sprintViewRecord
	if err := r.db.SelectContext(ctx, &records, `
		SELECT `+sprintViewSelectCols+` FROM sprint_views
		WHERE project_id = $1 AND view_context = $2
		ORDER BY position ASC, created_at ASC`, projectID.String(), string(viewCtx)); err != nil {
		return nil, fmt.Errorf("view repo: list project views (%s): %w", viewCtx, err)
	}
	out := make([]*sprintdom.SprintView, 0, len(records))
	for i := range records {
		v, err := toViewEntity(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// CreateView persists a new sprint view.
func (r *ViewRepository) CreateView(ctx context.Context, v *sprintdom.SprintView) error {
	configBytes, err := json.Marshal(v.Config)
	if err != nil {
		return fmt.Errorf("view repo: marshal config: %w", err)
	}
	var sprintIDStr *string
	if v.SprintID != nil {
		s := v.SprintID.String()
		sprintIDStr = &s
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO sprint_views (id, sprint_id, project_id, name, view_type, config, position, view_context, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		v.ID.String(), sprintIDStr, v.ProjectID.String(), v.Name,
		string(v.ViewType), configBytes, v.Position, string(v.ViewContext),
		v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("view repo: create: %w", err)
	}
	return nil
}

// UpdateView persists changes to an existing sprint view.
func (r *ViewRepository) UpdateView(ctx context.Context, v *sprintdom.SprintView) error {
	configBytes, err := json.Marshal(v.Config)
	if err != nil {
		return fmt.Errorf("view repo: marshal config: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE sprint_views SET name = $1, view_type = $2, config = $3, position = $4, updated_at = $5
		WHERE id = $6`,
		v.Name, string(v.ViewType), configBytes, v.Position, v.UpdatedAt, v.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("view repo: update: %w", err)
	}
	return nil
}

// DeleteView removes a sprint view by ID.
func (r *ViewRepository) DeleteView(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sprint_views WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("view repo: delete: %w", err)
	}
	return nil
}

// CountViews returns the number of views for a sprint.
func (r *ViewRepository) CountViews(ctx context.Context, sprintID uuid.UUID) (int, error) {
	var count int64
	if err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM sprint_views WHERE sprint_id = $1`, sprintID.String()); err != nil {
		return 0, fmt.Errorf("view repo: count: %w", err)
	}
	return int(count), nil
}

// CountProjectViews returns the number of views for a project with the given viewCtx.
func (r *ViewRepository) CountProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext) (int, error) {
	var count int64
	if err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM sprint_views WHERE project_id = $1 AND view_context = $2`, projectID.String(), string(viewCtx)); err != nil {
		return 0, fmt.Errorf("view repo: count project views (%s): %w", viewCtx, err)
	}
	return int(count), nil
}

// --- Task position methods --------------------------------------------------

// UpsertTaskPosition stores or updates the manual position of a task in a view.
func (r *ViewRepository) UpsertTaskPosition(ctx context.Context, pos *sprintdom.ViewTaskPosition) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO view_task_positions (id, view_id, task_id, position, group_key)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (view_id, task_id) DO UPDATE
		  SET position = EXCLUDED.position, group_key = EXCLUDED.group_key`,
		pos.ID.String(), pos.ViewID.String(), pos.TaskID.String(), pos.Position, pos.GroupKey,
	)
	if err != nil {
		return fmt.Errorf("view repo: upsert task position: %w", err)
	}
	return nil
}

// BulkUpsertTaskPositions stores or updates multiple task positions in a single
// transaction.  A single INSERT … ON CONFLICT statement is used when possible.
func (r *ViewRepository) BulkUpsertTaskPositions(ctx context.Context, positions []*sprintdom.ViewTaskPosition) error {
	if len(positions) == 0 {
		return nil
	}
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		for _, pos := range positions {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO view_task_positions (id, view_id, task_id, position, group_key)
				VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (view_id, task_id) DO UPDATE
				  SET position = EXCLUDED.position, group_key = EXCLUDED.group_key`,
				pos.ID.String(), pos.ViewID.String(), pos.TaskID.String(), pos.Position, pos.GroupKey,
			)
			if err != nil {
				return fmt.Errorf("view repo: bulk upsert task position: %w", err)
			}
		}
		return nil
	})
}

// ReorderViews bulk-updates the position column for multiple views inside a
// single transaction.  Each item maps a view ID to its new position value.
func (r *ViewRepository) ReorderViews(ctx context.Context, items []sprintdom.ViewReorderItem) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		for _, item := range items {
			_, err := tx.ExecContext(ctx, `UPDATE sprint_views SET position = $1 WHERE id = $2`, item.Position, item.ID.String())
			if err != nil {
				return fmt.Errorf("view repo: reorder view %s: %w", item.ID, err)
			}
		}
		return nil
	})
}

// ListTaskPositions returns all manual positions for a view ordered by position ASC.
func (r *ViewRepository) ListTaskPositions(ctx context.Context, viewID uuid.UUID) ([]*sprintdom.ViewTaskPosition, error) {
	var records []viewTaskPositionRecord
	if err := r.db.SelectContext(ctx, &records, `
		SELECT id, view_id, task_id, position, group_key FROM view_task_positions
		WHERE view_id = $1 ORDER BY position ASC`, viewID.String()); err != nil {
		return nil, fmt.Errorf("view repo: list task positions: %w", err)
	}
	out := make([]*sprintdom.ViewTaskPosition, 0, len(records))
	for i := range records {
		out = append(out, toTaskPositionEntity(&records[i]))
	}
	return out, nil
}

// --- Per-user view config methods -------------------------------------------

// userViewConfigRecord scans a row of the user_view_configs table.
type userViewConfigRecord struct {
	ViewID string `db:"view_id"`
	Config []byte `db:"config"`
}

// GetUserViewConfigs returns the given user's personal config overrides for the
// provided view IDs, keyed by view ID. Views without an override are omitted.
func (r *ViewRepository) GetUserViewConfigs(ctx context.Context, userID uuid.UUID, viewIDs []uuid.UUID) (map[uuid.UUID]sprintdom.ViewConfig, error) {
	out := make(map[uuid.UUID]sprintdom.ViewConfig, len(viewIDs))
	if len(viewIDs) == 0 {
		return out, nil
	}
	ids := make([]string, len(viewIDs))
	for i, id := range viewIDs {
		ids[i] = id.String()
	}
	query, args, err := sqlx.In(
		`SELECT view_id, config FROM user_view_configs WHERE user_id = ? AND view_id IN (?)`,
		userID.String(), ids,
	)
	if err != nil {
		return nil, fmt.Errorf("view repo: build get user view configs: %w", err)
	}
	query = r.db.Rebind(query)
	var records []userViewConfigRecord
	if err := r.db.SelectContext(ctx, &records, query, args...); err != nil {
		return nil, fmt.Errorf("view repo: get user view configs: %w", err)
	}
	for i := range records {
		vid, err := uuid.Parse(records[i].ViewID)
		if err != nil {
			return nil, fmt.Errorf("view repo: parse user view config id: %w", err)
		}
		var cfg sprintdom.ViewConfig
		if len(records[i].Config) > 0 {
			if err := json.Unmarshal(records[i].Config, &cfg); err != nil {
				return nil, fmt.Errorf("view repo: unmarshal user view config: %w", err)
			}
		}
		out[vid] = cfg
	}
	return out, nil
}

// UpsertUserViewConfig stores or replaces a user's personal config for a view.
func (r *ViewRepository) UpsertUserViewConfig(ctx context.Context, viewID, userID uuid.UUID, cfg sprintdom.ViewConfig) error {
	configBytes, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("view repo: marshal user view config: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO user_view_configs (view_id, user_id, config, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		ON CONFLICT (view_id, user_id) DO UPDATE
		  SET config = EXCLUDED.config, updated_at = NOW()`,
		viewID.String(), userID.String(), configBytes,
	)
	if err != nil {
		return fmt.Errorf("view repo: upsert user view config: %w", err)
	}
	return nil
}

// --- Entity converters ------------------------------------------------------

func toViewEntity(r *sprintViewRecord) (*sprintdom.SprintView, error) {
	id, _ := uuid.Parse(r.ID)
	pid, _ := uuid.Parse(r.ProjectID)
	var sid *uuid.UUID
	if r.SprintID != nil {
		parsed, _ := uuid.Parse(*r.SprintID)
		sid = &parsed
	}
	var cfg sprintdom.ViewConfig
	if len(r.Config) > 0 {
		if err := json.Unmarshal(r.Config, &cfg); err != nil {
			return nil, fmt.Errorf("view repo: unmarshal config: %w", err)
		}
	}
	return &sprintdom.SprintView{
		ID:          id,
		SprintID:    sid,
		ProjectID:   pid,
		Name:        r.Name,
		ViewType:    sprintdom.ViewType(r.ViewType),
		Config:      cfg,
		Position:    r.Position,
		ViewContext: sprintdom.ViewContext(r.ViewContext),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}, nil
}

func toTaskPositionEntity(r *viewTaskPositionRecord) *sprintdom.ViewTaskPosition {
	id, _ := uuid.Parse(r.ID)
	vid, _ := uuid.Parse(r.ViewID)
	tid, _ := uuid.Parse(r.TaskID)
	return &sprintdom.ViewTaskPosition{
		ID:       id,
		ViewID:   vid,
		TaskID:   tid,
		Position: r.Position,
		GroupKey: r.GroupKey,
	}
}
