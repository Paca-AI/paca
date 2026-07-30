package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// taskLinkRecord mirrors a row in the task_links table.
type taskLinkRecord struct {
	ID           string    `db:"id"`
	SourceTaskID string    `db:"source_task_id"`
	TargetTaskID string    `db:"target_task_id"`
	LinkType     string    `db:"link_type"`
	CreatedBy    *string   `db:"created_by"`
	CreatedAt    time.Time `db:"created_at"`
}

// taskLinkWithTaskRow is used for the join query that fetches linked task info.
type taskLinkWithTaskRow struct {
	// link columns
	ID           string    `db:"id"`
	SourceTaskID string    `db:"source_task_id"`
	TargetTaskID string    `db:"target_task_id"`
	LinkType     string    `db:"link_type"`
	CreatedBy    *string   `db:"created_by"`
	CreatedAt    time.Time `db:"created_at"`
	// linked task columns (the "other" side of the link)
	LinkedTaskID       string  `db:"linked_task_id"`
	LinkedTaskNumber   int64   `db:"linked_task_number"`
	LinkedTaskTitle    string  `db:"linked_task_title"`
	LinkedTaskStatusID *string `db:"linked_task_status_id"`
	LinkedTaskTypeID   *string `db:"linked_task_type_id"`
	// perspective: "source" means the queried task is the source, "target" means it's the target
	Perspective string `db:"perspective"`
}

// TaskLinkRepository is the sqlx implementation of taskdom.TaskLinkRepository.
type TaskLinkRepository struct {
	db *sqlx.DB
}

// NewTaskLinkRepository returns a new TaskLinkRepository.
func NewTaskLinkRepository(db *sqlx.DB) *TaskLinkRepository {
	return &TaskLinkRepository{db: db}
}

// ListTaskLinks returns all links where taskID is either source or target.
func (r *TaskLinkRepository) ListTaskLinks(ctx context.Context, taskID uuid.UUID) ([]*taskdom.TaskLink, error) {
	const q = `
		SELECT
		    tl.id,
		    tl.source_task_id,
		    tl.target_task_id,
		    tl.link_type,
		    tl.created_by,
		    tl.created_at,
		    linked.id           AS linked_task_id,
		    linked.task_number  AS linked_task_number,
		    linked.title        AS linked_task_title,
		    linked.status_id    AS linked_task_status_id,
		    linked.task_type_id AS linked_task_type_id,
		    CASE WHEN tl.source_task_id = $1 THEN 'source' ELSE 'target' END AS perspective
		FROM task_links tl
		JOIN tasks linked ON linked.id = CASE
		    WHEN tl.source_task_id = $1 THEN tl.target_task_id
		    ELSE tl.source_task_id
		END
		WHERE (tl.source_task_id = $1 OR tl.target_task_id = $1)
		  AND linked.deleted_at IS NULL
		ORDER BY tl.created_at ASC`

	var rows []taskLinkWithTaskRow
	if err := r.db.SelectContext(ctx, &rows, q, taskID.String()); err != nil {
		return nil, err
	}

	links := make([]*taskdom.TaskLink, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		link, err := row.toDomain()
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, nil
}

// FindTaskLinkByID returns a single link by primary key (no task join).
func (r *TaskLinkRepository) FindTaskLinkByID(ctx context.Context, id uuid.UUID) (*taskdom.TaskLink, error) {
	const q = `
		SELECT id, source_task_id, target_task_id, link_type, created_by, created_at
		FROM task_links WHERE id = $1`

	var rec taskLinkRecord
	if err := r.db.GetContext(ctx, &rec, q, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, taskdom.ErrTaskLinkNotFound
		}
		return nil, err
	}
	return rec.toDomain()
}

// CreateTaskLinkIfNotExists creates l unless an equivalent link already
// exists (checked in both directions for relates_to). Both task rows are
// locked (in a stable, id-ascending order to avoid deadlocks) for the
// duration of the transaction, so two concurrent requests linking the same
// pair of tasks - including the two row-orderings possible for the
// symmetric relates_to type, which the unique index alone cannot catch -
// serialize instead of both passing the existence check.
func (r *TaskLinkRepository) CreateTaskLinkIfNotExists(ctx context.Context, l *taskdom.TaskLink) (bool, error) {
	created := false
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var locked []string
		if err := tx.SelectContext(ctx, &locked,
			`SELECT id FROM tasks WHERE id IN ($1, $2) ORDER BY id FOR UPDATE`,
			l.SourceTaskID.String(), l.TargetTaskID.String(),
		); err != nil {
			return fmt.Errorf("task link repo: lock tasks: %w", err)
		}

		exists, err := linkExistsTx(ctx, tx, l.SourceTaskID, l.TargetTaskID, l.LinkType)
		if err != nil {
			return fmt.Errorf("task link repo: check exists: %w", err)
		}
		if exists {
			return nil
		}

		var createdBy *string
		if l.CreatedBy != nil {
			s := l.CreatedBy.String()
			createdBy = &s
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO task_links (id, source_task_id, target_task_id, link_type, created_by, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			l.ID.String(), l.SourceTaskID.String(), l.TargetTaskID.String(),
			string(l.LinkType), createdBy, l.CreatedAt,
		)
		if err != nil {
			if isUniqueViolation(err) {
				// Lost a race to a concurrent insert outside this lock's scope
				// (e.g. a differently-ordered id pair); treat as already-exists.
				return nil
			}
			return fmt.Errorf("task link repo: create: %w", err)
		}
		created = true
		return nil
	})
	return created, err
}

// linkExistsTx reports whether a link of linkType between source and target
// already exists. For relates_to it checks both directions.
func linkExistsTx(ctx context.Context, tx *sqlx.Tx, sourceID, targetID uuid.UUID, linkType taskdom.LinkType) (bool, error) {
	var q string
	var args []interface{}

	if linkType == taskdom.LinkTypeRelatesTo {
		q = `SELECT EXISTS (
			SELECT 1 FROM task_links
			WHERE link_type = $1
			  AND ((source_task_id = $2 AND target_task_id = $3)
			    OR (source_task_id = $3 AND target_task_id = $2))
		)`
		args = []interface{}{string(linkType), sourceID.String(), targetID.String()}
	} else {
		q = `SELECT EXISTS (
			SELECT 1 FROM task_links
			WHERE source_task_id = $1 AND target_task_id = $2 AND link_type = $3
		)`
		args = []interface{}{sourceID.String(), targetID.String(), string(linkType)}
	}

	var exists bool
	if err := tx.GetContext(ctx, &exists, q, args...); err != nil {
		return false, err
	}
	return exists, nil
}

// DeleteTaskLink removes a task_links row by primary key.
func (r *TaskLinkRepository) DeleteTaskLink(ctx context.Context, id uuid.UUID) error {
	const q = `DELETE FROM task_links WHERE id = $1`
	res, err := r.db.ExecContext(ctx, q, id.String())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return taskdom.ErrTaskLinkNotFound
	}
	return nil
}

// --- row-to-domain helpers --------------------------------------------------

func (rec *taskLinkRecord) toDomain() (*taskdom.TaskLink, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, err
	}
	sourceID, err := uuid.Parse(rec.SourceTaskID)
	if err != nil {
		return nil, err
	}
	targetID, err := uuid.Parse(rec.TargetTaskID)
	if err != nil {
		return nil, err
	}
	var createdBy *uuid.UUID
	if rec.CreatedBy != nil {
		p, err := uuid.Parse(*rec.CreatedBy)
		if err != nil {
			return nil, err
		}
		createdBy = &p
	}
	return &taskdom.TaskLink{
		ID:           id,
		SourceTaskID: sourceID,
		TargetTaskID: targetID,
		LinkType:     taskdom.LinkType(rec.LinkType),
		CreatedBy:    createdBy,
		CreatedAt:    rec.CreatedAt,
	}, nil
}

func (row *taskLinkWithTaskRow) toDomain() (*taskdom.TaskLink, error) {
	rec := taskLinkRecord{
		ID:           row.ID,
		SourceTaskID: row.SourceTaskID,
		TargetTaskID: row.TargetTaskID,
		LinkType:     row.LinkType,
		CreatedBy:    row.CreatedBy,
		CreatedAt:    row.CreatedAt,
	}
	link, err := rec.toDomain()
	if err != nil {
		return nil, err
	}

	linkedID, err := uuid.Parse(row.LinkedTaskID)
	if err != nil {
		return nil, err
	}
	var linkedStatusID *uuid.UUID
	if row.LinkedTaskStatusID != nil {
		p, err := uuid.Parse(*row.LinkedTaskStatusID)
		if err != nil {
			return nil, err
		}
		linkedStatusID = &p
	}
	var linkedTypeID *uuid.UUID
	if row.LinkedTaskTypeID != nil {
		p, err := uuid.Parse(*row.LinkedTaskTypeID)
		if err != nil {
			return nil, err
		}
		linkedTypeID = &p
	}
	link.LinkedTask = &taskdom.LinkedTaskSummary{
		ID:         linkedID,
		TaskNumber: row.LinkedTaskNumber,
		Title:      row.LinkedTaskTitle,
		StatusID:   linkedStatusID,
		TaskTypeID: linkedTypeID,
	}

	// Compute the display label from the queried task's perspective.
	if row.Perspective == "source" {
		link.DisplayLinkType = row.LinkType
	} else {
		switch taskdom.LinkType(row.LinkType) {
		case taskdom.LinkTypeBlocks:
			link.DisplayLinkType = "is_blocked_by"
		case taskdom.LinkTypeRelatesTo:
			link.DisplayLinkType = row.LinkType
		case taskdom.LinkTypeDuplicates:
			link.DisplayLinkType = "is_duplicated_by"
		}
	}

	return link, nil
}
