package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// The document change feed and batch read (docdom.ChangeRepository). The
// sequence columns and counters are described in migration 000088.

// documentFeedCols is documentTreeCols plus the body of the rows that have no
// stored hash yet: a live row written before migration 000087 and not reached
// by the hash backfill. The feed must report a hash for every live document,
// and Document.Hash on a row loaded without its body would report the hash of
// an empty document. Those rows' bodies are read, hashed and dropped again in
// feedDocFromRecord; once the backfill is done the CASE never yields a body.
const documentFeedCols = documentTreeCols + `,
	CASE WHEN content_hash IS NULL AND deleted_at IS NULL THEN content END AS content`

// feedDocFromRecord maps a row selected with documentFeedCols: no body, and a
// content hash on every live document.
func feedDocFromRecord(rec documentRecord) *docdom.Document {
	d := documentFromRecord(rec)
	if d.ContentHash == nil && d.DeletedAt == nil {
		h := docdom.ContentHash(d.Content)
		d.ContentHash = &h
	}
	d.Content = nil
	return d
}

// ReadDocChanges implements docdom.ChangeRepository. The snapshot is a
// read-only REPEATABLE READ transaction: every statement in it sees the
// database as of the first one. It takes no locks, so writers are not held up.
func (r *DocumentRepository) ReadDocChanges(ctx context.Context, fn func(docdom.ChangeReader) error) error {
	tx, err := r.db.BeginTxx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("doc changes: begin snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(&docChangeReader{q: tx}); err != nil {
		return err
	}
	return tx.Commit()
}

// docChangeReader is the docdom.ChangeReader bound to one snapshot.
type docChangeReader struct {
	q sqlx.QueryerContext
}

func (c *docChangeReader) DocSyncState(ctx context.Context, projectID uuid.UUID) (seq, foldersSeq int64, err error) {
	err = c.q.QueryRowxContext(ctx, `SELECT seq, folders_seq FROM doc_sync_state WHERE project_id = $1`, projectID.String()).Scan(&seq, &foldersSeq)
	if errors.Is(err, sql.ErrNoRows) {
		// No document or folder was ever written in this project.
		return 0, 0, nil
	}
	return seq, foldersSeq, err
}

// emptyFingerprint is the fingerprint of no documents: md5 of the empty string.
const emptyFingerprint = "d41d8cd98f00b204e9800998ecf8427e"

func (c *docChangeReader) VisibleFingerprint(ctx context.Context, projectID uuid.UUID, upToSeq *int64) (string, error) {
	sink := &argList{args: []any{projectID.String()}}
	where := `project_id = $1`
	if upToSeq == nil {
		where += ` AND deleted_at IS NULL`
	} else {
		// Alive at that sequence: created by then, and not deleted by then. A
		// deleted row keeps the seq of its deletion (migration 000088).
		p := sink.addArg(*upToSeq)
		where += ` AND created_seq <= ` + p + ` AND (deleted_at IS NULL OR seq > ` + p + `)`
	}
	clause, none, err := scopeSQL(ctx, "doc", docScopeColumns, sink)
	if err != nil {
		return "", err
	}
	if none {
		return emptyFingerprint, nil
	}
	if clause != "" {
		where += ` AND ` + clause
	}
	var fp string
	err = sqlx.GetContext(ctx, c.q, &fp,
		`SELECT md5(COALESCE(string_agg(id::text, ',' ORDER BY id), '')) FROM documents WHERE `+where, sink.args...)
	return fp, err
}

func (c *docChangeReader) ListDocChanges(ctx context.Context, projectID uuid.UUID, sinceSeq int64) ([]*docdom.Document, error) {
	// A tombstone is sent only for a row that existed at the cursor. One
	// created and deleted since then is nothing the client ever had, and
	// leaving it out also keeps a document that lived and died out of the
	// caller's sight from surfacing if its folder is deleted afterwards.
	return c.listFeedDocs(ctx, projectID,
		`seq > $2 AND (deleted_at IS NULL OR created_seq <= $2)`, sinceSeq)
}

func (c *docChangeReader) ListVisibleDocs(ctx context.Context, projectID uuid.UUID) ([]*docdom.Document, error) {
	return c.listFeedDocs(ctx, projectID, `deleted_at IS NULL`)
}

// listFeedDocs returns the project's documents matching cond (which may use
// $2.. for args) that the caller's scope allows, oldest change first.
func (c *docChangeReader) listFeedDocs(ctx context.Context, projectID uuid.UUID, cond string, args ...any) ([]*docdom.Document, error) {
	sink := &argList{args: append([]any{projectID.String()}, args...)}
	clause, none, err := scopeSQL(ctx, "doc", docScopeColumns, sink)
	if err != nil {
		return nil, err
	}
	if none {
		return []*docdom.Document{}, nil
	}
	query := `SELECT ` + documentFeedCols + ` FROM documents WHERE project_id = $1 AND ` + cond
	if clause != "" {
		query += ` AND ` + clause
	}
	var records []documentRecord
	if err := sqlx.SelectContext(ctx, c.q, &records, query+` ORDER BY seq ASC, id ASC`, sink.args...); err != nil {
		return nil, err
	}
	out := make([]*docdom.Document, 0, len(records))
	for _, rec := range records {
		out = append(out, feedDocFromRecord(rec))
	}
	return out, nil
}

func (c *docChangeReader) ListAllFolders(ctx context.Context, projectID uuid.UUID) ([]*docdom.DocFolder, error) {
	var records []docFolderRecord
	if err := sqlx.SelectContext(ctx, c.q, &records,
		`SELECT `+docFolderCols+` FROM doc_folders WHERE project_id = $1 ORDER BY position ASC, name ASC, id ASC`, projectID.String()); err != nil {
		return nil, err
	}
	out := make([]*docdom.DocFolder, 0, len(records))
	for _, rec := range records {
		out = append(out, folderFromRecord(rec))
	}
	return out, nil
}

// FindDocumentsByIDs implements docdom.ChangeRepository.
func (r *DocumentRepository) FindDocumentsByIDs(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID) ([]*docdom.Document, error) {
	if len(ids) == 0 {
		return []*docdom.Document{}, nil
	}
	idStrings := make([]string, len(ids))
	for i, id := range ids {
		idStrings[i] = id.String()
	}
	sink := &argList{args: []any{projectID.String(), idStrings}}
	clause, none, err := scopeSQL(ctx, "doc", docScopeColumns, sink)
	if err != nil {
		return nil, err
	}
	if none {
		return []*docdom.Document{}, nil
	}
	query := `SELECT ` + documentCols + ` FROM documents
		WHERE project_id = $1 AND id = ANY($2::uuid[]) AND deleted_at IS NULL`
	if clause != "" {
		query += ` AND ` + clause
	}
	var records []documentRecord
	if err := r.db.SelectContext(ctx, &records, query, sink.args...); err != nil {
		return nil, err
	}
	out := make([]*docdom.Document, 0, len(records))
	for _, rec := range records {
		out = append(out, documentFromRecord(rec))
	}
	return out, nil
}

var _ docdom.ChangeRepository = (*DocumentRepository)(nil)
