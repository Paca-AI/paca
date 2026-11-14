package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// =============================================================================
// sqlx models
// =============================================================================

type docFolderRecord struct {
	ID        string    `db:"id"`
	ProjectID string    `db:"project_id"`
	ParentID  *string   `db:"parent_id"`
	Name      string    `db:"name"`
	Position  int       `db:"position"`
	CreatedBy *string   `db:"created_by"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type documentRecord struct {
	ID        string           `db:"id"`
	ProjectID string           `db:"project_id"`
	FolderID  *string          `db:"folder_id"`
	Title     string           `db:"title"`
	Content   *json.RawMessage `db:"content"`
	// ContentHash is NULL on rows written before migration 000087.
	ContentHash *string    `db:"content_hash"`
	Position    int        `db:"position"`
	CreatedBy   *string    `db:"created_by"`
	UpdatedBy   *string    `db:"updated_by"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
	DeletedAt   *time.Time `db:"deleted_at"`
	// Source is NULL on documents created in Paca (migration 000089).
	Source       *json.RawMessage `db:"source"`
	SourceLocked bool             `db:"source_locked"`
}

type docSnapshotRecord struct {
	ID             string           `db:"id"`
	DocumentID     string           `db:"document_id"`
	Title          string           `db:"title"`
	Content        *json.RawMessage `db:"content"`
	ContentHash    *string          `db:"content_hash"`
	SnapshotNumber int64            `db:"snapshot_number"`
	CreatedBy      *string          `db:"created_by"`
	CreatedAt      time.Time        `db:"created_at"`

	// Joined from project_members → users.
	CreatedByName *string `db:"created_by_name"`
}

// =============================================================================
// DocumentRepository
// =============================================================================

// DocumentRepository is the sqlx implementation of docdom.Repository.
type DocumentRepository struct {
	db *sqlx.DB
}

// NewDocumentRepository returns a new DocumentRepository backed by db.
func NewDocumentRepository(db *sqlx.DB) *DocumentRepository {
	return &DocumentRepository{db: db}
}

// =============================================================================
// Mapping helpers
// =============================================================================

func folderFromRecord(r docFolderRecord) *docdom.DocFolder {
	f := &docdom.DocFolder{
		ID:        uuid.MustParse(r.ID),
		ProjectID: uuid.MustParse(r.ProjectID),
		Name:      r.Name,
		Position:  r.Position,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
	if r.ParentID != nil {
		id := uuid.MustParse(*r.ParentID)
		f.ParentID = &id
	}
	if r.CreatedBy != nil {
		id := uuid.MustParse(*r.CreatedBy)
		f.CreatedBy = &id
	}
	return f
}

func folderToRecord(f *docdom.DocFolder) docFolderRecord {
	r := docFolderRecord{
		ID:        f.ID.String(),
		ProjectID: f.ProjectID.String(),
		Name:      f.Name,
		Position:  f.Position,
		CreatedAt: f.CreatedAt,
		UpdatedAt: f.UpdatedAt,
	}
	if f.ParentID != nil {
		s := f.ParentID.String()
		r.ParentID = &s
	}
	if f.CreatedBy != nil {
		s := f.CreatedBy.String()
		r.CreatedBy = &s
	}
	return r
}

func documentFromRecord(r documentRecord) *docdom.Document {
	d := &docdom.Document{
		ID:        uuid.MustParse(r.ID),
		ProjectID: uuid.MustParse(r.ProjectID),
		Title:     r.Title,
		Position:  r.Position,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
		DeletedAt: r.DeletedAt,
	}
	d.ContentHash = r.ContentHash
	d.Source = sourceFromJSON(r.Source)
	d.SourceLocked = r.SourceLocked
	if r.Content != nil {
		d.Content = *r.Content
	}
	if r.FolderID != nil {
		id := uuid.MustParse(*r.FolderID)
		d.FolderID = &id
	}
	if r.CreatedBy != nil {
		id := uuid.MustParse(*r.CreatedBy)
		d.CreatedBy = &id
	}
	if r.UpdatedBy != nil {
		id := uuid.MustParse(*r.UpdatedBy)
		d.UpdatedBy = &id
	}
	return d
}

func documentToRecord(d *docdom.Document) documentRecord {
	r := documentRecord{
		ID:        d.ID.String(),
		ProjectID: d.ProjectID.String(),
		Title:     d.Title,
		Position:  d.Position,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
		DeletedAt: d.DeletedAt,
	}
	r.ContentHash = d.ContentHash
	r.Source = sourceToJSON(d.Source)
	r.SourceLocked = d.SourceLocked
	if d.Content != nil {
		r.Content = &d.Content
	}
	if d.FolderID != nil {
		s := d.FolderID.String()
		r.FolderID = &s
	}
	if d.CreatedBy != nil {
		s := d.CreatedBy.String()
		r.CreatedBy = &s
	}
	if d.UpdatedBy != nil {
		s := d.UpdatedBy.String()
		r.UpdatedBy = &s
	}
	return r
}

// sourceFromJSON decodes a documents.source value; NULL (or anything that is
// not a source object) is nil.
func sourceFromJSON(raw *json.RawMessage) *docdom.DocSource {
	if raw == nil {
		return nil
	}
	var src docdom.DocSource
	if err := json.Unmarshal(*raw, &src); err != nil || src.Repository == "" {
		return nil
	}
	return &src
}

// sourceToJSON encodes a source for documents.source (nil is NULL).
func sourceToJSON(src *docdom.DocSource) *json.RawMessage {
	if src == nil {
		return nil
	}
	b, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	raw := json.RawMessage(b)
	return &raw
}

func snapshotFromRecord(r docSnapshotRecord) *docdom.DocSnapshot {
	s := &docdom.DocSnapshot{
		ID:             uuid.MustParse(r.ID),
		DocumentID:     uuid.MustParse(r.DocumentID),
		Title:          r.Title,
		SnapshotNumber: r.SnapshotNumber,
		ContentHash:    r.ContentHash,
		CreatedAt:      r.CreatedAt,
	}
	if r.Content != nil {
		s.Content = *r.Content
	}
	if r.CreatedBy != nil {
		id := uuid.MustParse(*r.CreatedBy)
		s.CreatedBy = &id
	}
	if r.CreatedByName != nil {
		s.CreatedByName = *r.CreatedByName
	}
	return s
}

// =============================================================================
// Folder CRUD
// =============================================================================

const docFolderCols = `id, project_id, parent_id, name, position, created_by, created_at, updated_at`

// ListAllFolders returns all folders for a project.
func (r *DocumentRepository) ListAllFolders(_ context.Context, projectID uuid.UUID) ([]*docdom.DocFolder, error) {
	var records []docFolderRecord
	if err := r.db.Select(&records, `SELECT `+docFolderCols+` FROM doc_folders WHERE project_id = $1 ORDER BY position ASC, name ASC`, projectID.String()); err != nil {
		return nil, err
	}
	out := make([]*docdom.DocFolder, 0, len(records))
	for _, rec := range records {
		out = append(out, folderFromRecord(rec))
	}
	return out, nil
}

// ListFolders returns one cursor-paginated level of the folder tree,
// ordered position, name, id.
func (r *DocumentRepository) ListFolders(ctx context.Context, projectID uuid.UUID, parentID *uuid.UUID, filter docdom.FolderFilter, cursorAfter *string, limit int) ([]*docdom.DocFolder, bool, error) {
	if limit <= 0 {
		limit = 50
	}
	args := []any{projectID.String()}
	where := `project_id = $1`
	if parentID == nil {
		where += ` AND parent_id IS NULL`
	} else {
		args = append(args, parentID.String())
		where += fmt.Sprintf(` AND parent_id = $%d`, len(args))
	}
	if name := strings.TrimSpace(filter.Name); name != "" {
		args = append(args, name)
		where += fmt.Sprintf(` AND LOWER(name) = LOWER($%d)`, len(args))
	}
	if cursorAfter != nil {
		cur, err := docdom.DecodeTreeCursor(*cursorAfter)
		if err != nil {
			return nil, false, err
		}
		args = append(args, cur.Position, cur.Name, cur.ID)
		where += fmt.Sprintf(` AND (position, name, id) > ($%d, $%d, $%d)`, len(args)-2, len(args)-1, len(args))
	}
	args = append(args, limit+1)
	var records []docFolderRecord
	if err := r.db.SelectContext(ctx, &records, `SELECT `+docFolderCols+` FROM doc_folders WHERE `+where+
		fmt.Sprintf(` ORDER BY position ASC, name ASC, id ASC LIMIT $%d`, len(args)), args...); err != nil {
		return nil, false, err
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	out := make([]*docdom.DocFolder, 0, len(records))
	for _, rec := range records {
		out = append(out, folderFromRecord(rec))
	}
	return out, hasMore, nil
}

// FolderPath returns the folders from the root down to folderID, inclusive.
func (r *DocumentRepository) FolderPath(ctx context.Context, projectID, folderID uuid.UUID) ([]*docdom.DocFolder, error) {
	var records []docFolderRecord
	if err := r.db.SelectContext(ctx, &records, `
		WITH RECURSIVE chain AS (
			SELECT `+docFolderCols+`, 1 AS depth FROM doc_folders WHERE id = $1 AND project_id = $2
			UNION ALL
			SELECT `+qualifiedDocFolderCols("f")+`, c.depth + 1
			FROM doc_folders f JOIN chain c ON f.id = c.parent_id
			WHERE f.project_id = $2
		)
		SELECT `+docFolderCols+` FROM chain ORDER BY depth DESC`, folderID.String(), projectID.String()); err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, docdom.ErrFolderNotFound
	}
	out := make([]*docdom.DocFolder, 0, len(records))
	for _, rec := range records {
		out = append(out, folderFromRecord(rec))
	}
	return out, nil
}

// qualifiedDocFolderCols prefixes docFolderCols with a table alias.
func qualifiedDocFolderCols(alias string) string {
	cols := strings.Split(docFolderCols, ",")
	for i, c := range cols {
		cols[i] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}

// FindFolderByID returns a single folder.
func (r *DocumentRepository) FindFolderByID(_ context.Context, id uuid.UUID) (*docdom.DocFolder, error) {
	var rec docFolderRecord
	if err := r.db.Get(&rec, `SELECT `+docFolderCols+` FROM doc_folders WHERE id = $1`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, docdom.ErrFolderNotFound
		}
		return nil, err
	}
	return folderFromRecord(rec), nil
}

// lockDocSeq takes the project's change counter row (doc_sync_state, migration
// 000088) for the rest of the transaction, creating it if the project has
// none yet. The triggers that number document and folder writes take the same
// row, and a transaction that reaches it while holding a document or folder
// row deadlocks with one that holds the counter and wants that row (a folder
// delete updates every document in the folder). So every transaction that
// writes documents or folders calls this first: counter, then rows.
func lockDocSeq(ctx context.Context, tx *sqlx.Tx, projectID string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO doc_sync_state AS s (project_id) VALUES ($1)
		ON CONFLICT (project_id) DO UPDATE SET seq = s.seq`, projectID)
	return err
}

// CreateFolder persists a new folder.
func (r *DocumentRepository) CreateFolder(ctx context.Context, f *docdom.DocFolder) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if err := lockDocSeq(ctx, tx, f.ProjectID.String()); err != nil {
			return err
		}
		return insertFolder(ctx, tx, f)
	})
}

func insertFolder(ctx context.Context, ex sqlx.ExecerContext, f *docdom.DocFolder) error {
	rec := folderToRecord(f)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO doc_folders (id, project_id, parent_id, name, position, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		rec.ID, rec.ProjectID, rec.ParentID, rec.Name, rec.Position, rec.CreatedBy, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// UpdateFolderAtomic implements docdom.DocFolderRepository — see its doc
// comment. A per-project advisory lock serializes folder updates so two
// concurrent moves can't each pass a cycle check against the other's old
// parent; the row lock then guards the read-modify-write itself.
func (r *DocumentRepository) UpdateFolderAtomic(ctx context.Context, id uuid.UUID, decide func(current *docdom.DocFolder, find func(id uuid.UUID) (*docdom.DocFolder, error)) error) (*docdom.DocFolder, error) {
	var result *docdom.DocFolder
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var projectID string
		if err := tx.GetContext(ctx, &projectID, `SELECT project_id FROM doc_folders WHERE id = $1`, id.String()); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return docdom.ErrFolderNotFound
			}
			return err
		}
		if err := lockDocSeq(ctx, tx, projectID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('doc_folders:' || $1::text, 0))`, projectID); err != nil {
			return err
		}
		var rec docFolderRecord
		if err := tx.GetContext(ctx, &rec, `SELECT `+docFolderCols+` FROM doc_folders WHERE id = $1 FOR UPDATE`, id.String()); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return docdom.ErrFolderNotFound
			}
			return err
		}
		current := folderFromRecord(rec)
		find := func(fid uuid.UUID) (*docdom.DocFolder, error) {
			var fr docFolderRecord
			if err := tx.GetContext(ctx, &fr, `SELECT `+docFolderCols+` FROM doc_folders WHERE id = $1`, fid.String()); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return nil, docdom.ErrFolderNotFound
				}
				return nil, err
			}
			return folderFromRecord(fr), nil
		}
		if err := decide(current, find); err != nil {
			return err
		}
		out := folderToRecord(current)
		if _, err := tx.ExecContext(ctx, `
			UPDATE doc_folders SET parent_id=$1, name=$2, position=$3, updated_at=$4
			WHERE id=$5`,
			out.ParentID, out.Name, out.Position, out.UpdatedAt, out.ID,
		); err != nil {
			return err
		}
		result = current
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// DeleteFolder permanently deletes a folder.
func (r *DocumentRepository) DeleteFolder(ctx context.Context, id uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var projectID string
		if err := tx.GetContext(ctx, &projectID, `SELECT project_id FROM doc_folders WHERE id = $1`, id.String()); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // already gone
			}
			return err
		}
		if err := lockDocSeq(ctx, tx, projectID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM doc_folders WHERE id = $1`, id.String())
		return err
	})
}

// =============================================================================
// Document CRUD
// =============================================================================

const documentCols = `id, project_id, folder_id, title, content, content_hash, position, created_by, updated_by, created_at, updated_at, deleted_at, source, source_locked`

// documentTreeCols is documentCols without the (potentially large) body.
const documentTreeCols = `id, project_id, folder_id, title, content_hash, position, created_by, updated_by, created_at, updated_at, deleted_at, source, source_locked`

// ListDocuments returns one cursor-paginated page of a project's documents in
// manual order (position, title, id), without their bodies. filter narrows by
// folder and title. The caller's IAM scope narrows the query itself, ahead of
// the cursor and LIMIT, so every page is a full page of allowed documents.
func (r *DocumentRepository) ListDocuments(ctx context.Context, projectID uuid.UUID, filter docdom.DocumentFilter, cursorAfter *string, limit int) ([]*docdom.Document, bool, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT ` + documentTreeCols + ` FROM documents WHERE project_id = $1 AND deleted_at IS NULL`
	args := []any{projectID.String()}
	switch {
	case filter.FolderID != nil:
		args = append(args, filter.FolderID.String())
		query += fmt.Sprintf(` AND folder_id = $%d`, len(args))
	case filter.RootOnly:
		query += ` AND folder_id IS NULL`
	}
	sink := &argList{args: args}
	clause, none, err := scopeSQL(ctx, "doc", docScopeColumns, sink)
	if err != nil {
		return nil, false, err
	}
	if none {
		return []*docdom.Document{}, false, nil
	}
	if clause != "" {
		query += " AND " + clause
	}
	args = sink.args
	if q := strings.TrimSpace(filter.Search); q != "" {
		args = append(args, "%"+escapeLikePattern(q)+"%")
		query += fmt.Sprintf(" AND title ILIKE $%d", len(args))
	}
	if t := strings.TrimSpace(filter.Title); t != "" {
		args = append(args, t)
		query += fmt.Sprintf(" AND LOWER(title) = LOWER($%d)", len(args))
	}
	if cursorAfter != nil {
		cur, err := docdom.DecodeTreeCursor(*cursorAfter)
		if err != nil {
			return nil, false, err
		}
		args = append(args, cur.Position, cur.Name, cur.ID)
		query += fmt.Sprintf(` AND (position, title, id) > ($%d, $%d, $%d)`, len(args)-2, len(args)-1, len(args))
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(` ORDER BY position ASC, title ASC, id ASC LIMIT $%d`, len(args))

	var records []documentRecord
	if err := r.db.SelectContext(ctx, &records, query, args...); err != nil {
		return nil, false, err
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	out := make([]*docdom.Document, 0, len(records))
	for _, rec := range records {
		out = append(out, documentFromRecord(rec))
	}
	return out, hasMore, nil
}

// ListAllDocuments returns every non-deleted document of a project with its
// body, in manual order — for whole-project jobs (export), not the API.
func (r *DocumentRepository) ListAllDocuments(ctx context.Context, projectID uuid.UUID) ([]*docdom.Document, error) {
	var records []documentRecord
	if err := r.db.SelectContext(ctx, &records,
		`SELECT `+documentCols+` FROM documents WHERE project_id = $1 AND deleted_at IS NULL ORDER BY position ASC, title ASC, id ASC`,
		projectID.String()); err != nil {
		return nil, err
	}
	out := make([]*docdom.Document, 0, len(records))
	for _, rec := range records {
		out = append(out, documentFromRecord(rec))
	}
	return out, nil
}

// SearchDocuments matches query (literally, case-insensitively) against the
// title or any inline "text" node inside the BlockNote JSON content. The
// jsonpath walk restricts body matching to visible text so structural keys
// like "type":"paragraph" never match.
func (r *DocumentRepository) SearchDocuments(ctx context.Context, projectID uuid.UUID, query string, limit int) ([]*docdom.Document, error) {
	pattern := "%" + escapeLikePattern(strings.TrimSpace(query)) + "%"
	sink := &argList{args: []any{projectID.String(), pattern}}
	clause, none, err := scopeSQL(ctx, "doc", docScopeColumns, sink)
	if err != nil {
		return nil, err
	}
	if none {
		return []*docdom.Document{}, nil
	}
	scopeAnd := ""
	if clause != "" {
		scopeAnd = " AND " + clause
	}
	limitP := sink.addArg(limit)
	var records []documentRecord
	err = r.db.SelectContext(ctx, &records, `SELECT `+documentCols+` FROM documents
		WHERE project_id = $1 AND deleted_at IS NULL
		  AND (title ILIKE $2 OR EXISTS (
		        SELECT 1 FROM jsonb_path_query(COALESCE(content, 'null'::jsonb), 'lax $.**.text') AS t
		        WHERE jsonb_typeof(t) = 'string' AND (t #>> '{}') ILIKE $2))`+scopeAnd+`
		ORDER BY updated_at DESC, id ASC
		LIMIT `+limitP, sink.args...)
	if err != nil {
		return nil, err
	}
	out := make([]*docdom.Document, 0, len(records))
	for _, rec := range records {
		out = append(out, documentFromRecord(rec))
	}
	return out, nil
}

// FindDocumentByID returns a single non-deleted document.
func (r *DocumentRepository) FindDocumentByID(_ context.Context, id uuid.UUID) (*docdom.Document, error) {
	var rec documentRecord
	if err := r.db.Get(&rec, `SELECT `+documentCols+` FROM documents WHERE id = $1 AND deleted_at IS NULL`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, docdom.ErrDocNotFound
		}
		return nil, err
	}
	return documentFromRecord(rec), nil
}

// FindDocumentTitleByID returns only the title of a non-deleted document — a
// cheaper read than FindDocumentByID (which pulls the full row, including the
// content) for callers that need just the title, e.g. building a notification
// event payload. Returns ErrDocNotFound when the document doesn't exist or is
// deleted.
func (r *DocumentRepository) FindDocumentTitleByID(_ context.Context, id uuid.UUID) (string, error) {
	var title string
	if err := r.db.Get(&title, `SELECT title FROM documents WHERE id = $1 AND deleted_at IS NULL`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", docdom.ErrDocNotFound
		}
		return "", err
	}
	return title, nil
}

// CreateDocument persists a new document.
func (r *DocumentRepository) CreateDocument(ctx context.Context, d *docdom.Document) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if err := lockDocSeq(ctx, tx, d.ProjectID.String()); err != nil {
			return err
		}
		return insertDocument(ctx, tx, d)
	})
}

func insertDocument(ctx context.Context, ex sqlx.ExecerContext, d *docdom.Document) error {
	rec := documentToRecord(d)
	_, err := ex.ExecContext(ctx, `
		INSERT INTO documents (id, project_id, folder_id, title, content, content_hash, position, created_by, updated_by, created_at, updated_at, deleted_at, source, source_locked)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		rec.ID, rec.ProjectID, rec.FolderID, rec.Title, rec.Content, rec.ContentHash,
		rec.Position, rec.CreatedBy, rec.UpdatedBy, rec.CreatedAt, rec.UpdatedAt, rec.DeletedAt,
		rec.Source, rec.SourceLocked,
	)
	return err
}

// lockDocSeqOf is lockDocSeq for the project of document id. The project is
// read without a lock (a document never changes project). ErrDocNotFound when
// the document does not exist.
func lockDocSeqOf(ctx context.Context, tx *sqlx.Tx, id uuid.UUID) error {
	var projectID string
	if err := tx.GetContext(ctx, &projectID, `SELECT project_id FROM documents WHERE id = $1`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return docdom.ErrDocNotFound
		}
		return err
	}
	return lockDocSeq(ctx, tx, projectID)
}

// UpdateDocumentAtomic implements docdom.DocumentRepository — see its doc
// comment.
func (r *DocumentRepository) UpdateDocumentAtomic(ctx context.Context, id uuid.UUID, decide func(current *docdom.Document) error) (*docdom.Document, error) {
	var result *docdom.Document
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if err := lockDocSeqOf(ctx, tx, id); err != nil {
			return err
		}
		var err error
		result, err = updateDocumentLocked(ctx, tx, id, decide)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// updateDocumentLocked is UpdateDocumentAtomic's read-modify-write inside the
// caller's transaction tx.
func updateDocumentLocked(ctx context.Context, tx *sqlx.Tx, id uuid.UUID, decide func(current *docdom.Document) error) (*docdom.Document, error) {
	var rec documentRecord
	if err := tx.GetContext(ctx, &rec, `SELECT `+documentCols+` FROM documents WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, docdom.ErrDocNotFound
		}
		return nil, err
	}
	current := documentFromRecord(rec)
	if err := decide(current); err != nil {
		if errors.Is(err, docdom.ErrNoChange) {
			// Nothing to write: return the row as stored, not as decide left it.
			return documentFromRecord(rec), nil
		}
		return nil, err
	}
	out := documentToRecord(current)
	if _, err := tx.ExecContext(ctx, `
		UPDATE documents SET folder_id=$1, title=$2, content=$3, content_hash=$4, position=$5, updated_by=$6, updated_at=$7,
			source=$9, source_locked=$10
		WHERE id=$8 AND deleted_at IS NULL`,
		out.FolderID, out.Title, out.Content, out.ContentHash, out.Position, out.UpdatedBy, out.UpdatedAt, out.ID,
		out.Source, out.SourceLocked,
	); err != nil {
		return nil, err
	}
	return current, nil
}

// DeleteDocument soft-deletes a document.
func (r *DocumentRepository) DeleteDocument(ctx context.Context, id uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if err := lockDocSeqOf(ctx, tx, id); err != nil {
			if errors.Is(err, docdom.ErrDocNotFound) {
				return nil // nothing to delete, as before
			}
			return err
		}
		// An already deleted document keeps the time of its deletion.
		_, err := tx.ExecContext(ctx, `UPDATE documents SET deleted_at = $1 WHERE id = $2 AND deleted_at IS NULL`, time.Now(), id.String())
		return err
	})
}

// BackfillContentHashes implements worker.DocHashStore: it stores
// hash(content) on up to limit documents whose content_hash is NULL (deleted
// ones included) and returns how many it filled.
//
// The batch runs in one transaction with paca.skip_doc_seq = 'on', which the
// documents_bump_seq trigger reads (migration 000088): storing a hash changes
// nothing a client can see, so the rows keep their seq and stay out of the
// change feed. The setting is transaction-local, so it never reaches another
// statement on the pooled connection. updated_at is not written either.
//
// The rows are locked while they are hashed, so a concurrent save cannot slip
// between the read and the write; a save that got there first has stored its
// own hash and the row no longer matches. Rows another transaction holds are
// skipped, not waited for: the backfill takes document rows without the
// change counter (see lockDocSeq), so waiting could deadlock with a folder
// delete. A skipped row is picked up by a later batch or the next start.
func (r *DocumentRepository) BackfillContentHashes(ctx context.Context, limit int, hash func(content json.RawMessage) string) (int, error) {
	filled := 0
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT set_config('paca.skip_doc_seq', 'on', true)`); err != nil {
			return err
		}
		var rows []struct {
			ID      string           `db:"id"`
			Content *json.RawMessage `db:"content"`
		}
		if err := tx.SelectContext(ctx, &rows,
			`SELECT id, content FROM documents WHERE content_hash IS NULL ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit); err != nil {
			return err
		}
		for _, row := range rows {
			var content json.RawMessage
			if row.Content != nil {
				content = *row.Content
			}
			if _, err := tx.ExecContext(ctx,
				`UPDATE documents SET content_hash = $1 WHERE id = $2 AND content_hash IS NULL`, hash(content), row.ID); err != nil {
				return err
			}
		}
		filled = len(rows)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return filled, nil
}

// =============================================================================
// Snapshot CRUD
// =============================================================================

const snapshotJoinSQL = `
	SELECT ds.id, ds.document_id, ds.title, ds.content, ds.content_hash, ds.snapshot_number, ds.created_by, ds.created_at,
	       acc.name AS created_by_name
	FROM doc_snapshots ds
	LEFT JOIN project_members pm ON pm.id = ds.created_by
	LEFT JOIN accounts acc ON acc.id = pm.account_id`

// snapshotListJoinSQL is snapshotJoinSQL without the body column, for lists.
const snapshotListJoinSQL = `
	SELECT ds.id, ds.document_id, ds.title, ds.content_hash, ds.snapshot_number, ds.created_by, ds.created_at,
	       acc.name AS created_by_name
	FROM doc_snapshots ds
	LEFT JOIN project_members pm ON pm.id = ds.created_by
	LEFT JOIN accounts acc ON acc.id = pm.account_id`

// ListSnapshots returns one page of a document's snapshots, newest first and
// without their bodies, keyset-paginated on snapshot_number.
func (r *DocumentRepository) ListSnapshots(ctx context.Context, documentID uuid.UUID, contentHash *string, cursorAfter *string, limit int) ([]*docdom.DocSnapshot, bool, error) {
	if limit <= 0 {
		limit = 30
	}
	where := `ds.document_id = $1`
	args := []any{documentID.String()}
	if contentHash != nil {
		args = append(args, *contentHash)
		where += fmt.Sprintf(` AND ds.content_hash = $%d`, len(args))
	}
	if cursorAfter != nil {
		n, err := docdom.DecodeSnapshotCursor(*cursorAfter)
		if err != nil {
			return nil, false, err
		}
		args = append(args, n)
		where += fmt.Sprintf(` AND ds.snapshot_number < $%d`, len(args))
	}
	args = append(args, limit+1)
	var records []docSnapshotRecord
	if err := r.db.SelectContext(ctx, &records, snapshotListJoinSQL+` WHERE `+where+
		fmt.Sprintf(` ORDER BY ds.snapshot_number DESC LIMIT $%d`, len(args)), args...); err != nil {
		return nil, false, err
	}
	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	out := make([]*docdom.DocSnapshot, 0, len(records))
	for _, rec := range records {
		out = append(out, snapshotFromRecord(rec))
	}
	return out, hasMore, nil
}

// FindSnapshotByID returns a single snapshot.
func (r *DocumentRepository) FindSnapshotByID(_ context.Context, id uuid.UUID) (*docdom.DocSnapshot, error) {
	var rec docSnapshotRecord
	if err := r.db.Get(&rec, snapshotJoinSQL+` WHERE ds.id = $1`, id.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, docdom.ErrSnapshotNotFound
		}
		return nil, err
	}
	return snapshotFromRecord(rec), nil
}

// FindLatestSnapshot returns the snapshot with the highest snapshot_number.
func (r *DocumentRepository) FindLatestSnapshot(_ context.Context, documentID uuid.UUID) (*docdom.DocSnapshot, error) {
	var rec docSnapshotRecord
	if err := r.db.Get(&rec, snapshotJoinSQL+` WHERE ds.document_id = $1 ORDER BY ds.snapshot_number DESC LIMIT 1`, documentID.String()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // no snapshots yet — not an error
		}
		return nil, err
	}
	return snapshotFromRecord(rec), nil
}

// CreateSnapshot persists a new snapshot.
func (r *DocumentRepository) CreateSnapshot(ctx context.Context, s *docdom.DocSnapshot) error {
	var createdBy *string
	if s.CreatedBy != nil {
		str := s.CreatedBy.String()
		createdBy = &str
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO doc_snapshots (id, document_id, title, content, content_hash, snapshot_number, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		s.ID.String(), s.DocumentID.String(), s.Title, s.Content, s.ContentHash, s.SnapshotNumber, createdBy, s.CreatedAt,
	)
	return err
}

// DeleteRecentSnapshotsExcept deletes all snapshots for a document created at
// or after `since` whose ID is not `excludeID`. This consolidates rapid saves
// so that at most one snapshot exists per time window.
func (r *DocumentRepository) DeleteRecentSnapshotsExcept(ctx context.Context, documentID uuid.UUID, excludeID uuid.UUID, since time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM doc_snapshots WHERE document_id = $1 AND id != $2 AND created_at >= $3`,
		documentID.String(), excludeID.String(), since,
	)
	return err
}
