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
	Position  int              `db:"position"`
	CreatedBy *string          `db:"created_by"`
	UpdatedBy *string          `db:"updated_by"`
	CreatedAt time.Time        `db:"created_at"`
	UpdatedAt time.Time        `db:"updated_at"`
	DeletedAt *time.Time       `db:"deleted_at"`
}

type docSnapshotRecord struct {
	ID             string           `db:"id"`
	DocumentID     string           `db:"document_id"`
	Title          string           `db:"title"`
	Content        *json.RawMessage `db:"content"`
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

func snapshotFromRecord(r docSnapshotRecord) *docdom.DocSnapshot {
	s := &docdom.DocSnapshot{
		ID:             uuid.MustParse(r.ID),
		DocumentID:     uuid.MustParse(r.DocumentID),
		Title:          r.Title,
		SnapshotNumber: r.SnapshotNumber,
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

// ListFolders returns all folders for a project.
func (r *DocumentRepository) ListFolders(_ context.Context, projectID uuid.UUID) ([]*docdom.DocFolder, error) {
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

// CreateFolder persists a new folder.
func (r *DocumentRepository) CreateFolder(ctx context.Context, f *docdom.DocFolder) error {
	rec := folderToRecord(f)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO doc_folders (id, project_id, parent_id, name, position, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		rec.ID, rec.ProjectID, rec.ParentID, rec.Name, rec.Position, rec.CreatedBy, rec.CreatedAt, rec.UpdatedAt,
	)
	return err
}

// UpdateFolder persists mutable changes to a folder.
func (r *DocumentRepository) UpdateFolder(ctx context.Context, f *docdom.DocFolder) error {
	rec := folderToRecord(f)
	_, err := r.db.ExecContext(ctx, `
		UPDATE doc_folders SET project_id=$1, parent_id=$2, name=$3, position=$4, created_by=$5, updated_at=$6
		WHERE id=$7`,
		rec.ProjectID, rec.ParentID, rec.Name, rec.Position, rec.CreatedBy, rec.UpdatedAt, rec.ID,
	)
	return err
}

// DeleteFolder permanently deletes a folder.
func (r *DocumentRepository) DeleteFolder(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM doc_folders WHERE id = $1`, id.String())
	return err
}

// =============================================================================
// Document CRUD
// =============================================================================

const documentCols = `id, project_id, folder_id, title, content, position, created_by, updated_by, created_at, updated_at, deleted_at`

// ListDocuments returns non-deleted documents for a project, optionally
// filtered to a folder and/or a case-insensitive title search, and
// optionally keyset-paginated — see docdom.Repository.ListDocuments's doc
// comment for the limit/cursor/hasMore contract and why pagination mode
// sorts title ASC, id ASC instead of the tree's position ASC, title ASC.
func (r *DocumentRepository) ListDocuments(ctx context.Context, projectID uuid.UUID, folderID *uuid.UUID, search *string, cursor *string, limit *int) ([]*docdom.Document, bool, error) {
	query := `SELECT ` + documentCols + ` FROM documents WHERE project_id = $1 AND deleted_at IS NULL`
	args := []any{projectID.String()}
	if folderID != nil {
		args = append(args, folderID.String())
		query += fmt.Sprintf(" AND folder_id = $%d", len(args))
	}
	if search != nil {
		if q := strings.TrimSpace(*search); q != "" {
			args = append(args, "%"+escapeLikePattern(q)+"%")
			query += fmt.Sprintf(" AND title ILIKE $%d", len(args))
		}
	}

	paginating := limit != nil
	if paginating {
		if cursor != nil {
			if cur, ok := docdom.DecodeDocumentCursor(*cursor); ok {
				args = append(args, cur.Title, cur.ID)
				query += fmt.Sprintf(" AND (title, id) > ($%d, $%d)", len(args)-1, len(args))
			}
		}
		query += " ORDER BY title ASC, id ASC"
		args = append(args, *limit+1)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	} else {
		query += " ORDER BY position ASC, title ASC"
	}

	var records []documentRecord
	if err := r.db.SelectContext(ctx, &records, query, args...); err != nil {
		return nil, false, err
	}

	hasMore := false
	if paginating && len(records) > *limit {
		hasMore = true
		records = records[:*limit]
	}
	out := make([]*docdom.Document, 0, len(records))
	for _, rec := range records {
		out = append(out, documentFromRecord(rec))
	}
	return out, hasMore, nil
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

// CreateDocument persists a new document.
func (r *DocumentRepository) CreateDocument(ctx context.Context, d *docdom.Document) error {
	rec := documentToRecord(d)
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO documents (id, project_id, folder_id, title, content, position, created_by, updated_by, created_at, updated_at, deleted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		rec.ID, rec.ProjectID, rec.FolderID, rec.Title, rec.Content,
		rec.Position, rec.CreatedBy, rec.UpdatedBy, rec.CreatedAt, rec.UpdatedAt, rec.DeletedAt,
	)
	return err
}

// UpdateDocument persists mutable changes to a document.
func (r *DocumentRepository) UpdateDocument(ctx context.Context, d *docdom.Document) error {
	rec := documentToRecord(d)
	_, err := r.db.ExecContext(ctx, `
		UPDATE documents SET project_id=$1, folder_id=$2, title=$3, content=$4, position=$5,
		  created_by=$6, updated_by=$7, updated_at=$8, deleted_at=$9
		WHERE id=$10`,
		rec.ProjectID, rec.FolderID, rec.Title, rec.Content, rec.Position,
		rec.CreatedBy, rec.UpdatedBy, rec.UpdatedAt, rec.DeletedAt, rec.ID,
	)
	return err
}

// DeleteDocument soft-deletes a document.
func (r *DocumentRepository) DeleteDocument(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE documents SET deleted_at = $1 WHERE id = $2`, time.Now(), id.String())
	return err
}

// =============================================================================
// Snapshot CRUD
// =============================================================================

const snapshotJoinSQL = `
	SELECT ds.id, ds.document_id, ds.title, ds.content, ds.snapshot_number, ds.created_by, ds.created_at,
	       u.full_name AS created_by_name
	FROM doc_snapshots ds
	LEFT JOIN project_members pm ON pm.id = ds.created_by
	LEFT JOIN users u ON u.id = pm.user_id`

// ListSnapshots returns all snapshots for a document, newest first.
func (r *DocumentRepository) ListSnapshots(_ context.Context, documentID uuid.UUID) ([]*docdom.DocSnapshot, error) {
	var records []docSnapshotRecord
	if err := r.db.Select(&records, snapshotJoinSQL+` WHERE ds.document_id = $1 ORDER BY ds.snapshot_number DESC`, documentID.String()); err != nil {
		return nil, err
	}
	out := make([]*docdom.DocSnapshot, 0, len(records))
	for _, rec := range records {
		out = append(out, snapshotFromRecord(rec))
	}
	return out, nil
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
		INSERT INTO doc_snapshots (id, document_id, title, content, snapshot_number, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		s.ID.String(), s.DocumentID.String(), s.Title, s.Content, s.SnapshotNumber, createdBy, s.CreatedAt,
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
