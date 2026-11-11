package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/domain/changeoutbox"
)

var _ changeoutbox.Repository = (*ChangeOutboxRepository)(nil)

// ChangeOutboxRepository reads the change_outbox table the capture triggers
// fill (migration 000083), for the change reconciler.
type ChangeOutboxRepository struct {
	db *sqlx.DB
}

// NewChangeOutboxRepository returns a ChangeOutboxRepository.
func NewChangeOutboxRepository(db *sqlx.DB) *ChangeOutboxRepository {
	return &ChangeOutboxRepository{db: db}
}

type changeOutboxRecord struct {
	ID         int64     `db:"id"`
	ProjectID  string    `db:"project_id"`
	EntityType string    `db:"entity_type"`
	EntityID   string    `db:"entity_id"`
	Op         string    `db:"op"`
	Version    *int64    `db:"version"`
	CreatedAt  time.Time `db:"created_at"`
}

// Claim locks up to limit rows older than olderThan (FOR UPDATE SKIP LOCKED, so
// replicas claim different rows), hands them to handle and, when it returns
// nil, deletes them in the same transaction. When handle fails nothing is
// deleted and the rows are claimed again on the next tick. It returns how many
// rows it handled.
func (r *ChangeOutboxRepository) Claim(ctx context.Context, olderThan time.Time, limit int, handle func([]changeoutbox.Row) error) (int, error) {
	handled := 0
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var recs []changeOutboxRecord
		if err := tx.SelectContext(ctx, &recs, `
			SELECT id, project_id, entity_type, entity_id, op, version, created_at
			FROM change_outbox
			WHERE created_at < $1
			ORDER BY id
			LIMIT $2
			FOR UPDATE SKIP LOCKED`, olderThan, limit); err != nil {
			return fmt.Errorf("change outbox: claim: %w", err)
		}
		if len(recs) == 0 {
			return nil
		}
		rows := make([]changeoutbox.Row, 0, len(recs))
		ids := make([]int64, 0, len(recs))
		for _, rec := range recs {
			pid, perr := uuid.Parse(rec.ProjectID)
			eid, eerr := uuid.Parse(rec.EntityID)
			ids = append(ids, rec.ID)
			if perr != nil || eerr != nil {
				continue // a malformed row can only be dropped
			}
			rows = append(rows, changeoutbox.Row{
				ID: rec.ID, ProjectID: pid, EntityType: rec.EntityType, EntityID: eid,
				Op: rec.Op, Version: rec.Version, CreatedAt: rec.CreatedAt,
			})
		}
		if err := handle(rows); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM change_outbox WHERE id = ANY($1)`, ids); err != nil {
			return fmt.Errorf("change outbox: delete: %w", err)
		}
		handled = len(recs)
		return nil
	})
	return handled, err
}

// Prune deletes rows older than olderThan, claimed or not.
func (r *ChangeOutboxRepository) Prune(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM change_outbox WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("change outbox: prune: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
