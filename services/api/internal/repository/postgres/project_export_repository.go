package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	exportdom "github.com/Paca-AI/api/internal/domain/export"
)

type projectExportRecord struct {
	ID           string     `db:"id"`
	ProjectID    string     `db:"project_id"`
	RequestedBy  *string    `db:"requested_by"`
	Kind         string     `db:"kind"`
	Status       string     `db:"status"`
	FileKey      *string    `db:"file_key"`
	FileName     *string    `db:"file_name"`
	FileSize     *int64     `db:"file_size"`
	RowCount     *int       `db:"row_count"`
	ErrorMessage *string    `db:"error_message"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
	CompletedAt  *time.Time `db:"completed_at"`
	ExpiresAt    *time.Time `db:"expires_at"`
}

const projectExportColumns = `id, project_id, requested_by, kind, status, file_key, file_name,
	file_size, row_count, error_message, created_at, updated_at, completed_at, expires_at`

func (r *projectExportRecord) toEntity() (*exportdom.ProjectExport, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("project export repo: parse id: %w", err)
	}
	projectID, err := uuid.Parse(r.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("project export repo: parse project id: %w", err)
	}
	e := &exportdom.ProjectExport{
		ID:           id,
		ProjectID:    projectID,
		Kind:         exportdom.Kind(r.Kind),
		Status:       exportdom.Status(r.Status),
		FileKey:      r.FileKey,
		FileName:     r.FileName,
		FileSize:     r.FileSize,
		RowCount:     r.RowCount,
		ErrorMessage: r.ErrorMessage,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		CompletedAt:  r.CompletedAt,
		ExpiresAt:    r.ExpiresAt,
	}
	if r.RequestedBy != nil {
		uid, err := uuid.Parse(*r.RequestedBy)
		if err != nil {
			return nil, fmt.Errorf("project export repo: parse requested_by: %w", err)
		}
		e.RequestedBy = &uid
	}
	return e, nil
}

// ProjectExportRepository is the PostgreSQL implementation of exportdom.Repository.
type ProjectExportRepository struct {
	db *sqlx.DB
}

// NewProjectExportRepository returns a ProjectExportRepository backed by db.
func NewProjectExportRepository(db *sqlx.DB) *ProjectExportRepository {
	return &ProjectExportRepository{db: db}
}

var _ exportdom.Repository = (*ProjectExportRepository)(nil)

// Create inserts a new export row.
func (r *ProjectExportRepository) Create(ctx context.Context, e *exportdom.ProjectExport) error {
	var requestedBy *string
	if e.RequestedBy != nil {
		s := e.RequestedBy.String()
		requestedBy = &s
	}
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO project_exports (id, project_id, requested_by, kind, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.ID.String(), e.ProjectID.String(), requestedBy, string(e.Kind), string(e.Status), e.CreatedAt, e.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("project export repo: create: %w", err)
	}
	return nil
}

// FindByID returns the export or exportdom.ErrNotFound.
func (r *ProjectExportRepository) FindByID(ctx context.Context, id uuid.UUID) (*exportdom.ProjectExport, error) {
	var rec projectExportRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+projectExportColumns+` FROM project_exports WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, exportdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("project export repo: find: %w", err)
	}
	return rec.toEntity()
}

// ListByProject returns the project's newest exports first.
func (r *ProjectExportRepository) ListByProject(ctx context.Context, projectID uuid.UUID, limit int) ([]*exportdom.ProjectExport, error) {
	var recs []projectExportRecord
	err := r.db.SelectContext(ctx, &recs,
		`SELECT `+projectExportColumns+` FROM project_exports
		 WHERE project_id = $1 ORDER BY created_at DESC LIMIT $2`,
		projectID.String(), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("project export repo: list: %w", err)
	}
	return toExportEntities(recs)
}

// HasActive reports a pending/processing export updated at or after notBefore.
func (r *ProjectExportRepository) HasActive(ctx context.Context, projectID uuid.UUID, notBefore time.Time) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists,
		`SELECT EXISTS (
		   SELECT 1 FROM project_exports
		   WHERE project_id = $1 AND status IN ('pending', 'processing') AND updated_at >= $2
		 )`,
		projectID.String(), notBefore,
	)
	if err != nil {
		return false, fmt.Errorf("project export repo: has active: %w", err)
	}
	return exists, nil
}

// Claim moves a pending export to processing, reporting whether this call won it.
func (r *ProjectExportRepository) Claim(ctx context.Context, id uuid.UUID) (bool, error) {
	res, err := r.db.ExecContext(ctx,
		`UPDATE project_exports SET status = 'processing', updated_at = now()
		 WHERE id = $1 AND status = 'pending'`,
		id.String(),
	)
	if err != nil {
		return false, fmt.Errorf("project export repo: claim: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("project export repo: claim rows: %w", err)
	}
	return n > 0, nil
}

// MarkCompleted records the generated file and finishes the export.
func (r *ProjectExportRepository) MarkCompleted(ctx context.Context, id uuid.UUID, fileKey, fileName string, fileSize int64, rowCount int, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE project_exports
		 SET status = 'completed', file_key = $2, file_name = $3, file_size = $4, row_count = $5,
		     error_message = NULL, completed_at = now(), updated_at = now(), expires_at = $6
		 WHERE id = $1`,
		id.String(), fileKey, fileName, fileSize, rowCount, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("project export repo: mark completed: %w", err)
	}
	return nil
}

// MarkFailed finishes the export with an error message.
func (r *ProjectExportRepository) MarkFailed(ctx context.Context, id uuid.UUID, message string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE project_exports
		 SET status = 'failed', error_message = $2, completed_at = now(), updated_at = now()
		 WHERE id = $1`,
		id.String(), message,
	)
	if err != nil {
		return fmt.Errorf("project export repo: mark failed: %w", err)
	}
	return nil
}

// ListExpired returns exports whose retention window has passed.
func (r *ProjectExportRepository) ListExpired(ctx context.Context, now time.Time, limit int) ([]*exportdom.ProjectExport, error) {
	var recs []projectExportRecord
	err := r.db.SelectContext(ctx, &recs,
		`SELECT `+projectExportColumns+` FROM project_exports
		 WHERE expires_at IS NOT NULL AND expires_at < $1
		 ORDER BY expires_at LIMIT $2`,
		now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("project export repo: list expired: %w", err)
	}
	return toExportEntities(recs)
}

// ListStale returns active exports untouched since before notAfter.
func (r *ProjectExportRepository) ListStale(ctx context.Context, notAfter time.Time, limit int) ([]*exportdom.ProjectExport, error) {
	var recs []projectExportRecord
	err := r.db.SelectContext(ctx, &recs,
		`SELECT `+projectExportColumns+` FROM project_exports
		 WHERE status IN ('pending', 'processing') AND updated_at < $1
		 ORDER BY updated_at LIMIT $2`,
		notAfter, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("project export repo: list stale: %w", err)
	}
	return toExportEntities(recs)
}

// Delete removes an export row.
func (r *ProjectExportRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM project_exports WHERE id = $1`, id.String()); err != nil {
		return fmt.Errorf("project export repo: delete: %w", err)
	}
	return nil
}

func toExportEntities(recs []projectExportRecord) ([]*exportdom.ProjectExport, error) {
	out := make([]*exportdom.ProjectExport, 0, len(recs))
	for i := range recs {
		e, err := recs[i].toEntity()
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
