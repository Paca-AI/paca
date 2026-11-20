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

	filedom "github.com/Paca-AI/api/internal/domain/file"
)

// FileRepository is the sqlx implementation of filedom.Repository. Reads go
// through the file_items view (folders and uploaded files as one list); writes
// go to file_folders or files according to the node kind.
type FileRepository struct {
	db *sqlx.DB
}

// NewFileRepository returns a new FileRepository.
func NewFileRepository(db *sqlx.DB) *FileRepository { return &FileRepository{db: db} }

var _ filedom.Repository = (*FileRepository)(nil)

type fileNodeRow struct {
	ID              uuid.UUID  `db:"id"`
	Kind            string     `db:"kind"`
	ProjectID       *uuid.UUID `db:"project_id"`
	ParentID        *uuid.UUID `db:"parent_id"`
	Name            string     `db:"name"`
	SystemKey       string     `db:"system_key"`
	Area            string     `db:"area"`
	Source          string     `db:"source"`
	ContentType     string     `db:"content_type"`
	Size            int64      `db:"size"`
	StorageKey      string     `db:"storage_key"`
	Bucket          string     `db:"bucket"`
	CreatedBy       *uuid.UUID `db:"owner_id"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
	DeletedAt       *time.Time `db:"deleted_at"`
	DeletedBy       *uuid.UUID `db:"deleted_by"`
	OwnerName       string     `db:"owner_name"`
	ParentSystemKey string     `db:"parent_system_key"`
	Starred         bool       `db:"starred"`
	AccessedAt      *time.Time `db:"accessed_at"`
}

func (r *fileNodeRow) entity() *filedom.Node {
	return &filedom.Node{
		ID: r.ID, ProjectID: r.ProjectID, ParentID: r.ParentID,
		Kind: filedom.Kind(r.Kind), Name: r.Name, SystemKey: r.SystemKey, Area: r.Area, Source: r.Source,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		DeletedAt: r.DeletedAt, DeletedBy: r.DeletedBy, OwnerName: r.OwnerName,
		ContentType: r.ContentType, Size: r.Size, StorageKey: r.StorageKey, Bucket: r.Bucket,
		ParentSystemKey: r.ParentSystemKey, Starred: r.Starred, AccessedAt: r.AccessedAt,
	}
}

// fileDocScopeColumns is docScopeColumns over a file_items row n that is a
// document: the folder attributes come from the document itself (a document
// with no folder has none, although file_items parents it under Documentation).
var fileDocScopeColumns = scopeColumns{
	"resource.id":   {scalar: "n.id"},
	"doc.folder_id": {scalar: "(SELECT dx.folder_id FROM documents dx WHERE dx.id = n.id)"},
	"doc.ancestor_folder_ids": {multi: `ARRAY(WITH RECURSIVE anc(id, parent_id) AS (
			SELECT f.id, f.parent_id FROM doc_folders f WHERE f.id = (SELECT dx.folder_id FROM documents dx WHERE dx.id = n.id)
		  UNION
			SELECT f.id, f.parent_id FROM doc_folders f JOIN anc a ON f.id = a.parent_id
		) SELECT id::text FROM anc)`},
}

// docsAreaClause is the predicate over file_items alias n that decides whether
// a whole-scope list (search, starred, recent) may include Documentation, from
// the document scope the caller's handler attached to ctx:
//
//   - no scope attached, or one that allows no document: nothing in
//     Documentation (the safe default for any caller that did not scope);
//   - unrestricted: everything;
//   - a conditional scope: its folders and files (they follow the project-wide
//     docs:read the scope implies) and the documents the scope matches.
func docsAreaClause(ctx context.Context, sink argSink) (string, error) {
	node, ok := scopeFromContext(ctx, "doc")
	if !ok || node.DeniesAll() {
		return `n.area = ''`, nil
	}
	if node.Unrestricted() {
		return "", nil
	}
	clause, none, err := scopeSQL(ctx, "doc", fileDocScopeColumns, sink)
	if err != nil {
		return "", err
	}
	if none {
		return `n.area = ''`, nil
	}
	return `(n.area = '' OR n.kind <> 'doc' OR (` + clause + `))`, nil
}

// docRowsClause restricts the document rows of a listing to the caller's
// document scope (a Deny on one document, a role limited to some folders);
// folders and files are not affected, they follow the project-wide access. No
// scope attached, or an unrestricted one, adds nothing.
func docRowsClause(ctx context.Context, sink argSink) (string, error) {
	node, ok := scopeFromContext(ctx, "doc")
	if !ok || node.Unrestricted() {
		return "", nil
	}
	if node.DeniesAll() {
		return `n.kind <> 'doc'`, nil
	}
	clause, none, err := scopeSQL(ctx, "doc", fileDocScopeColumns, sink)
	if err != nil {
		return "", err
	}
	if none {
		return `n.kind <> 'doc'`, nil
	}
	return `(n.kind <> 'doc' OR (` + clause + `))`, nil
}

// nodeSelect lists the item columns with the owner, parent and star joined in.
// $1 is always the caller's id (uuid.Nil when there is none).
const nodeSelect = `
	SELECT n.id, n.kind, n.project_id, n.parent_id, n.name, n.system_key, n.area, n.source,
	       n.content_type, n.size, n.storage_key, n.bucket, n.owner_id,
	       n.created_at, n.updated_at, n.deleted_at, n.deleted_by,
	       COALESCE(u.name, '') AS owner_name,
	       COALESCE(pn.system_key, '') AS parent_system_key,
	       EXISTS (SELECT 1 FROM file_stars s WHERE s.item_id = n.id AND s.user_id = $1) AS starred,
	       %s AS accessed_at
	FROM file_items n
	LEFT JOIN accounts u     ON u.id = n.owner_id
	LEFT JOIN file_folders pn ON pn.id = n.parent_id`

func nodeSelectSQL(accessedAt string) string { return fmt.Sprintf(nodeSelect, accessedAt) }

func (r *FileRepository) selectNodes(ctx context.Context, query string, args ...any) ([]*filedom.Node, error) {
	var rows []fileNodeRow
	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("file repo: select nodes: %w", err)
	}
	out := make([]*filedom.Node, len(rows))
	for i := range rows {
		out[i] = rows[i].entity()
	}
	return out, nil
}

// scopeClause restricts a query on alias n to the scope.
func scopeClause(sink *argList, s filedom.Scope) string {
	if s.ProjectID == nil {
		return `n.project_id IS NULL`
	}
	return `n.project_id = ` + sink.addArg(*s.ProjectID)
}

// GetNode returns the folder or file, trashed or not.
func (r *FileRepository) GetNode(ctx context.Context, id, userID uuid.UUID) (*filedom.Node, error) {
	nodes, err := r.selectNodes(ctx, nodeSelectSQL("NULL::timestamptz")+` WHERE n.id = $2`, userID, id)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, filedom.ErrNodeNotFound
	}
	return nodes[0], nil
}

// typeClause maps a type filter to a predicate over the item alias n.
func typeClause(t string) (string, error) {
	switch t {
	case "":
		return "", nil
	case filedom.TypeDocument:
		return `(n.content_type IN ('application/msword','application/vnd.openxmlformats-officedocument.wordprocessingml.document','application/vnd.oasis.opendocument.text','application/rtf','text/plain','text/markdown')
			OR n.name ~* '\.(docx?|odt|rtf|txt|md)$')`, nil
	case filedom.TypeSpreadsheet:
		return `(n.content_type IN ('application/vnd.ms-excel','application/vnd.openxmlformats-officedocument.spreadsheetml.sheet','application/vnd.oasis.opendocument.spreadsheet','text/csv')
			OR n.name ~* '\.(xlsx?|ods|csv)$')`, nil
	case filedom.TypePresentation:
		return `(n.content_type IN ('application/vnd.ms-powerpoint','application/vnd.openxmlformats-officedocument.presentationml.presentation','application/vnd.oasis.opendocument.presentation')
			OR n.name ~* '\.(pptx?|odp)$')`, nil
	case filedom.TypePDF:
		return `(n.content_type = 'application/pdf' OR n.name ~* '\.pdf$')`, nil
	case filedom.TypeImage:
		return `n.content_type LIKE 'image/%'`, nil
	case filedom.TypeVideo:
		return `n.content_type LIKE 'video/%'`, nil
	case filedom.TypeAudio:
		return `n.content_type LIKE 'audio/%'`, nil
	case filedom.TypeArchive:
		return `(n.content_type IN ('application/zip','application/x-tar','application/gzip','application/x-7z-compressed','application/vnd.rar','application/x-rar-compressed')
			OR n.name ~* '\.(zip|tar|gz|tgz|7z|rar)$')`, nil
	}
	return "", filedom.ErrInvalidType
}

// List returns one page of live items, folders first.
func (r *FileRepository) List(ctx context.Context, p filedom.ListParams) ([]*filedom.Node, int, error) {
	sink := &argList{args: []any{p.UserID}}
	where := []string{scopeClause(sink, p.Scope), `n.deleted_at IS NULL`}
	switch {
	case p.Query != "":
		// Documentation is searched only as far as the caller's document scope
		// allows (see docsAreaClause).
		docs, err := docsAreaClause(ctx, sink)
		if err != nil {
			return nil, 0, err
		}
		if docs != "" {
			where = append(where, docs)
		}
		where = append(where, `n.name ILIKE `+sink.addArg("%"+escapeLike(p.Query)+"%")+` ESCAPE '\'`)
	case p.ParentID == nil:
		where = append(where, `n.parent_id IS NULL`)
	default:
		where = append(where, `n.parent_id = `+sink.addArg(*p.ParentID))
	}
	if p.Query == "" {
		// Browsing a folder lists the documents the caller's scope allows.
		docs, err := docRowsClause(ctx, sink)
		if err != nil {
			return nil, 0, err
		}
		if docs != "" {
			where = append(where, docs)
		}
	}
	if p.Type != "" {
		tc, err := typeClause(p.Type)
		if err != nil {
			return nil, 0, err
		}
		where = append(where, `n.kind = 'file'`, tc)
	}
	whereSQL := ` WHERE ` + strings.Join(where, ` AND `)

	var total int
	// $1 (the caller) only matters to the page query; naming it here keeps
	// Postgres from rejecting an argument the statement never mentions.
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM file_items n`+whereSQL+` AND $1::uuid IS NOT NULL`, sink.args...); err != nil {
		return nil, 0, fmt.Errorf("file repo: count nodes: %w", err)
	}

	dir := "ASC"
	if p.Desc {
		dir = "DESC"
	}
	var key string
	switch p.SortBy {
	case filedom.SortModified:
		key = `n.updated_at`
	case filedom.SortSize:
		key = `n.size`
	default:
		key = `lower(n.name)`
	}
	q := nodeSelectSQL("NULL::timestamptz") + whereSQL +
		` ORDER BY (n.kind = 'folder') DESC, ` + key + ` ` + dir + `, lower(n.name), n.id` +
		` LIMIT ` + sink.addArg(clampLimit(p.Limit)) + ` OFFSET ` + sink.addArg(p.Offset)
	nodes, err := r.selectNodes(ctx, q, sink.args...)
	return nodes, total, err
}

// ListStarred returns the caller's starred, live items in the scope.
func (r *FileRepository) ListStarred(ctx context.Context, scope filedom.Scope, userID uuid.UUID, limit, offset int) ([]*filedom.Node, int, error) {
	sink := &argList{args: []any{userID}}
	docs, err := docsAreaClause(ctx, sink)
	if err != nil {
		return nil, 0, err
	}
	if docs != "" {
		docs = " AND " + docs
	}
	where := ` WHERE ` + scopeClause(sink, scope) + ` AND n.deleted_at IS NULL` + docs + `
		AND EXISTS (SELECT 1 FROM file_stars s WHERE s.item_id = n.id AND s.user_id = $1)`
	var total int
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM file_items n`+where, sink.args...); err != nil {
		return nil, 0, fmt.Errorf("file repo: count starred: %w", err)
	}
	q := nodeSelectSQL("NULL::timestamptz") + where +
		` ORDER BY (n.kind = 'folder') DESC, lower(n.name), n.id LIMIT ` + sink.addArg(clampLimit(limit)) + ` OFFSET ` + sink.addArg(offset)
	nodes, err := r.selectNodes(ctx, q, sink.args...)
	return nodes, total, err
}

// ListRecent returns the items the caller opened last, newest first.
func (r *FileRepository) ListRecent(ctx context.Context, scope filedom.Scope, userID uuid.UUID, limit int) ([]*filedom.Node, error) {
	sink := &argList{args: []any{userID}}
	docs, err := docsAreaClause(ctx, sink)
	if err != nil {
		return nil, err
	}
	if docs != "" {
		docs = " AND " + docs
	}
	q := nodeSelectSQL(`rc.accessed_at`) + ` JOIN file_recents rc ON rc.item_id = n.id AND rc.user_id = $1
		WHERE ` + scopeClause(sink, scope) + ` AND n.deleted_at IS NULL` + docs + `
		ORDER BY rc.accessed_at DESC LIMIT ` + sink.addArg(clampLimit(limit))
	return r.selectNodes(ctx, q, sink.args...)
}

// ListTrash returns the roots of the trashed subtrees of the scope.
func (r *FileRepository) ListTrash(ctx context.Context, scope filedom.Scope, userID uuid.UUID, limit, offset int) ([]*filedom.Node, int, error) {
	sink := &argList{args: []any{userID}}
	docs, err := docsAreaClause(ctx, sink)
	if err != nil {
		return nil, 0, err
	}
	if docs != "" {
		docs = " AND " + docs
	}
	where := ` WHERE ` + scopeClause(sink, scope) + ` AND n.deleted_at IS NOT NULL` + docs + `
		AND (n.parent_id IS NULL OR pn.deleted_at IS DISTINCT FROM n.deleted_at)`
	var total int
	if err := r.db.GetContext(ctx, &total,
		`SELECT COUNT(*) FROM file_items n LEFT JOIN file_folders pn ON pn.id = n.parent_id`+where+` AND $1::uuid IS NOT NULL`, sink.args...); err != nil {
		return nil, 0, fmt.Errorf("file repo: count trash: %w", err)
	}
	q := nodeSelectSQL("NULL::timestamptz") + where +
		` ORDER BY n.deleted_at DESC, n.id LIMIT ` + sink.addArg(clampLimit(limit)) + ` OFFSET ` + sink.addArg(offset)
	nodes, err := r.selectNodes(ctx, q, sink.args...)
	return nodes, total, err
}

func clampLimit(n int) int {
	if n <= 0 || n > 500 {
		return 100
	}
	return n
}

// Path returns the ancestors of the item and the item itself, root first.
func (r *FileRepository) Path(ctx context.Context, id uuid.UUID) ([]*filedom.Node, error) {
	self, err := r.GetNode(ctx, id, uuid.Nil)
	if err != nil {
		return nil, err
	}
	if self.ParentID == nil {
		return []*filedom.Node{self}, nil
	}
	q := `WITH RECURSIVE up AS (
			SELECT id, parent_id, 0 AS depth FROM file_folders WHERE id = $2
			UNION ALL
			SELECT p.id, p.parent_id, up.depth + 1 FROM file_folders p JOIN up ON p.id = up.parent_id
		) ` + nodeSelectSQL("NULL::timestamptz") + ` JOIN up ON up.id = n.id ORDER BY up.depth DESC`
	ancestors, err := r.selectNodes(ctx, q, uuid.Nil, *self.ParentID)
	if err != nil {
		return nil, err
	}
	return append(ancestors, self), nil
}

// CreateFolder inserts a folder.
func (r *FileRepository) CreateFolder(ctx context.Context, n *filedom.Node) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO file_folders (id, project_id, parent_id, name, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		n.ID, n.ProjectID, n.ParentID, n.Name, n.CreatedBy, n.CreatedAt, n.UpdatedAt)
	if err != nil {
		return fmt.Errorf("file repo: create folder: %w", err)
	}
	return nil
}

// updateNode runs a folder statement or a file statement according to kind;
// both take $1 = value, $2 = id.
func (r *FileRepository) updateNode(ctx context.Context, n *filedom.Node, folderSQL, fileSQL string, value any) error {
	q := fileSQL
	if n.IsFolder() {
		q = folderSQL
	}
	res, err := r.db.ExecContext(ctx, q, value, n.ID)
	if err != nil {
		return fmt.Errorf("file repo: update %s: %w", n.Kind, err)
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return filedom.ErrNodeNotFound
	}
	return nil
}

// Rename changes the name of a folder or file.
func (r *FileRepository) Rename(ctx context.Context, n *filedom.Node, name string) error {
	return r.updateNode(ctx, n,
		`UPDATE file_folders SET name = $1, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL`,
		`UPDATE files SET file_name = $1, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL`, name)
}

// Move reparents a folder or file (nil: the scope root).
func (r *FileRepository) Move(ctx context.Context, n *filedom.Node, parentID *uuid.UUID) error {
	return r.updateNode(ctx, n,
		`UPDATE file_folders SET parent_id = $1, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL`,
		`UPDATE files SET folder_id = $1, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL`, parentID)
}

// IsSelfOrDescendant reports whether candidate is the folder or lies below it.
func (r *FileRepository) IsSelfOrDescendant(ctx context.Context, folder, candidate uuid.UUID) (bool, error) {
	var ok bool
	err := r.db.GetContext(ctx, &ok, `
		WITH RECURSIVE up AS (
			SELECT id, parent_id FROM file_folders WHERE id = $1
			UNION ALL
			SELECT p.id, p.parent_id FROM file_folders p JOIN up ON p.id = up.parent_id
		) SELECT EXISTS (SELECT 1 FROM up WHERE id = $2)`, candidate, folder)
	if err != nil {
		return false, fmt.Errorf("file repo: descendant check: %w", err)
	}
	return ok, nil
}

// Trash marks the item, and a folder's live contents, trashed with one
// shared timestamp.
func (r *FileRepository) Trash(ctx context.Context, n *filedom.Node, by uuid.UUID, at time.Time) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		// Documents and documentation folders number their writes in the
		// project's change feed: take its counter before any row (lockDocSeq).
		if err := lockDocsOf(ctx, tx, n); err != nil {
			return err
		}
		if n.Kind == filedom.KindDoc {
			res, err := tx.ExecContext(ctx, `UPDATE documents SET deleted_at = $2, trashed_at = $2 WHERE id = $1 AND deleted_at IS NULL`, n.ID, at)
			if err != nil {
				return err
			}
			if c, _ := res.RowsAffected(); c == 0 {
				return filedom.ErrNodeNotFound
			}
			return nil
		}
		if !n.IsFolder() {
			res, err := tx.ExecContext(ctx, `UPDATE files SET deleted_at = $2, deleted_by = $3, updated_at = $2 WHERE id = $1 AND deleted_at IS NULL`, n.ID, at, by)
			if err != nil {
				return err
			}
			if c, _ := res.RowsAffected(); c == 0 {
				return filedom.ErrNodeNotFound
			}
			return nil
		}
		res, err := tx.ExecContext(ctx, `
			WITH RECURSIVE sub AS (
				SELECT id FROM file_folders WHERE id = $1 AND deleted_at IS NULL
				UNION ALL
				SELECT c.id FROM file_folders c JOIN sub s ON c.parent_id = s.id WHERE c.deleted_at IS NULL
			)
			UPDATE file_folders SET deleted_at = $2, deleted_by = $3, updated_at = $2
			WHERE id IN (SELECT id FROM sub)`, n.ID, at, by)
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			return filedom.ErrNodeNotFound
		}
		// The documents and files inside carry the same stamp as the folders
		// just marked.
		if _, err = tx.ExecContext(ctx, `
			UPDATE documents SET deleted_at = $1, trashed_at = $1
			WHERE deleted_at IS NULL AND folder_id IN (SELECT id FROM file_folders WHERE deleted_at = $1)`, at); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE files SET deleted_at = $1, deleted_by = $2, updated_at = $1
			WHERE deleted_at IS NULL AND folder_id IN (SELECT id FROM file_folders WHERE deleted_at = $1)`, at, by)
		return err
	})
}

// lockDocsOf takes the project's doc change counter when n is a document or a
// documentation folder, whose writes (and the documents below the folder) are
// numbered by triggers.
func lockDocsOf(ctx context.Context, tx *sqlx.Tx, n *filedom.Node) error {
	if n.ProjectID == nil || (n.Kind != filedom.KindDoc && n.Area != "docs") {
		return nil
	}
	return lockDocSeq(ctx, tx, n.ProjectID.String())
}

// Restore brings back the item, and what was trashed together with it.
func (r *FileRepository) Restore(ctx context.Context, n *filedom.Node) error {
	if n.DeletedAt == nil {
		return filedom.ErrNodeNotTrashed
	}
	stamp := *n.DeletedAt
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if err := lockDocsOf(ctx, tx, n); err != nil {
			return err
		}
		// An item whose parent folder is still in the trash goes back to the root
		// (for a document, the top of Documentation).
		if n.ParentID != nil {
			var parentTrashed bool
			if err := tx.GetContext(ctx, &parentTrashed, `SELECT COALESCE((SELECT deleted_at IS NOT NULL FROM file_folders WHERE id = $1), TRUE)`, *n.ParentID); err != nil {
				return err
			}
			if parentTrashed {
				q := `UPDATE files SET folder_id = NULL WHERE id = $1`
				switch {
				case n.IsFolder() && n.Area == "docs":
					// A documentation folder stays in Documentation: its top.
					q = `UPDATE file_folders SET parent_id = (SELECT r.id FROM file_folders r
						WHERE r.project_id = file_folders.project_id AND r.system_key = 'documentation') WHERE id = $1`
				case n.IsFolder():
					q = `UPDATE file_folders SET parent_id = NULL WHERE id = $1`
				case n.Kind == filedom.KindDoc:
					q = `UPDATE documents SET folder_id = NULL WHERE id = $1`
				}
				if _, err := tx.ExecContext(ctx, q, n.ID); err != nil {
					return err
				}
			}
		}
		if n.Kind == filedom.KindDoc {
			_, err := tx.ExecContext(ctx, `UPDATE documents SET deleted_at = NULL, trashed_at = NULL WHERE id = $1 AND trashed_at IS NOT NULL`, n.ID)
			return err
		}
		if !n.IsFolder() {
			_, err := tx.ExecContext(ctx, `UPDATE files SET deleted_at = NULL, deleted_by = NULL, updated_at = NOW() WHERE id = $1`, n.ID)
			return err
		}
		const sub = `WITH RECURSIVE sub AS (
			SELECT id FROM file_folders WHERE id = $1
			UNION ALL
			SELECT c.id FROM file_folders c JOIN sub s ON c.parent_id = s.id WHERE c.deleted_at = $2
		) `
		if _, err := tx.ExecContext(ctx, sub+`
			UPDATE documents SET deleted_at = NULL, trashed_at = NULL
			WHERE trashed_at = $2 AND folder_id IN (SELECT id FROM sub)`, n.ID, stamp); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, sub+`
			UPDATE files SET deleted_at = NULL, deleted_by = NULL, updated_at = NOW()
			WHERE deleted_at = $2 AND folder_id IN (SELECT id FROM sub)`, n.ID, stamp); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, sub+`
			UPDATE file_folders SET deleted_at = NULL, deleted_by = NULL, updated_at = NOW()
			WHERE id IN (SELECT id FROM sub)`, n.ID, stamp)
		return err
	})
}

// purgeTx removes one item (and a folder's contents) inside tx and returns the
// objects to delete.
func purgeTx(ctx context.Context, tx *sqlx.Tx, n *filedom.Node) ([]filedom.StorageObject, error) {
	type fileObj struct {
		ID     uuid.UUID `db:"id"`
		Bucket string    `db:"bucket"`
		Key    string    `db:"storage_key"`
	}
	var files []fileObj
	var folderIDs []uuid.UUID
	// Erasing a document and deleting a documentation folder are numbered in the
	// change feed: take its counter before any row.
	if err := lockDocsOf(ctx, tx, n); err != nil {
		return nil, err
	}
	// A purged document keeps only its tombstone row (the change feed needs it):
	// its content, snapshots and per-user marks go.
	var docIDs []uuid.UUID
	if n.Kind == filedom.KindDoc {
		docIDs = []uuid.UUID{n.ID}
	}
	if n.IsFolder() {
		if err := tx.SelectContext(ctx, &folderIDs, `
			WITH RECURSIVE sub AS (
				SELECT id FROM file_folders WHERE id = $1
				UNION ALL
				SELECT c.id FROM file_folders c JOIN sub s ON c.parent_id = s.id
			) SELECT id FROM sub`, n.ID); err != nil {
			return nil, err
		}
		if err := tx.SelectContext(ctx, &files,
			`SELECT id, bucket, storage_key FROM files WHERE folder_id = ANY($1::uuid[])`, uuidStrings(folderIDs)); err != nil {
			return nil, err
		}
		if err := tx.SelectContext(ctx, &docIDs,
			`SELECT id FROM documents WHERE trashed_at IS NOT NULL AND folder_id = ANY($1::uuid[])`, uuidStrings(folderIDs)); err != nil {
			return nil, err
		}
	} else if n.Kind != filedom.KindDoc {
		if err := tx.SelectContext(ctx, &files, `SELECT id, bucket, storage_key FROM files WHERE id = $1`, n.ID); err != nil {
			return nil, err
		}
	}

	itemIDs := append(uuidStrings(folderIDs), fileIDStrings(files, func(f fileObj) uuid.UUID { return f.ID })...)
	itemIDs = append(itemIDs, uuidStrings(docIDs)...)
	if len(docIDs) > 0 {
		ids := uuidStrings(docIDs)
		if _, err := tx.ExecContext(ctx, `DELETE FROM doc_snapshots WHERE document_id = ANY($1::uuid[])`, ids); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET content = NULL, content_text = NULL, trashed_at = NULL WHERE id = ANY($1::uuid[])`, ids); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM file_stars WHERE item_id = ANY($1::uuid[])`, itemIDs); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM file_recents WHERE item_id = ANY($1::uuid[])`, itemIDs); err != nil {
		return nil, err
	}
	if len(files) > 0 {
		// A file a task still lists as an attachment is unlinked in the same
		// transaction, so the task and the file never end up half-deleted.
		if _, err := tx.ExecContext(ctx, `DELETE FROM task_attachments WHERE file_id = ANY($1::uuid[])`, fileIDStrings(files, func(f fileObj) uuid.UUID { return f.ID })); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE id = ANY($1::uuid[])`, fileIDStrings(files, func(f fileObj) uuid.UUID { return f.ID })); err != nil {
			return nil, err
		}
	}
	if n.IsFolder() {
		if _, err := tx.ExecContext(ctx, `DELETE FROM file_folders WHERE id = $1`, n.ID); err != nil {
			return nil, err
		}
	}
	out := make([]filedom.StorageObject, 0, len(files))
	for _, f := range files {
		out = append(out, filedom.StorageObject{Bucket: f.Bucket, Key: f.Key})
	}
	return out, nil
}

func fileIDStrings[T any](items []T, id func(T) uuid.UUID) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = id(it).String()
	}
	return out
}

// PurgeSubtree permanently removes the item and a folder's contents.
func (r *FileRepository) PurgeSubtree(ctx context.Context, n *filedom.Node) ([]filedom.StorageObject, error) {
	var out []filedom.StorageObject
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var err error
		out, err = purgeTx(ctx, tx, n)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("file repo: purge: %w", err)
	}
	return out, nil
}

// PurgeTrashedBefore removes every trashed subtree older than t, up to limit
// roots at a time.
func (r *FileRepository) PurgeTrashedBefore(ctx context.Context, t time.Time, limit int) ([]filedom.StorageObject, error) {
	var roots []struct {
		ID        uuid.UUID  `db:"id"`
		Kind      string     `db:"kind"`
		ProjectID *uuid.UUID `db:"project_id"`
		Area      string     `db:"area"`
	}
	if err := r.db.SelectContext(ctx, &roots, `
		SELECT n.id, n.kind, n.project_id, n.area FROM file_items n LEFT JOIN file_folders pn ON pn.id = n.parent_id
		WHERE n.deleted_at IS NOT NULL AND n.deleted_at < $1
		  AND (n.parent_id IS NULL OR pn.deleted_at IS DISTINCT FROM n.deleted_at)
		ORDER BY n.deleted_at LIMIT $2`, t, limit); err != nil {
		return nil, fmt.Errorf("file repo: find expired trash: %w", err)
	}
	// One failing root must not block the newer ones behind it on every tick.
	var all []filedom.StorageObject
	var errs []error
	for _, root := range roots {
		objs, err := r.PurgeSubtree(ctx, &filedom.Node{ID: root.ID, Kind: filedom.Kind(root.Kind), ProjectID: root.ProjectID, Area: root.Area})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		all = append(all, objs...)
	}
	return all, errors.Join(errs...)
}

// SetStarred stars or unstars an item for the user.
func (r *FileRepository) SetStarred(ctx context.Context, userID, itemID uuid.UUID, starred bool) error {
	var err error
	if starred {
		_, err = r.db.ExecContext(ctx, `INSERT INTO file_stars (user_id, item_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, itemID)
	} else {
		_, err = r.db.ExecContext(ctx, `DELETE FROM file_stars WHERE user_id = $1 AND item_id = $2`, userID, itemID)
	}
	if err != nil {
		return fmt.Errorf("file repo: set starred: %w", err)
	}
	return nil
}

// Touch records that the user opened the item.
func (r *FileRepository) Touch(ctx context.Context, userID, itemID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO file_recents (user_id, item_id, accessed_at) VALUES ($1, $2, NOW())
		ON CONFLICT (user_id, item_id) DO UPDATE SET accessed_at = NOW()`, userID, itemID)
	if err != nil {
		return fmt.Errorf("file repo: touch recent: %w", err)
	}
	return nil
}

// FileNodeArea reports the area of a folder, file or document by id: "docs"
// inside the Documentation subtree, "" elsewhere or when the id is unknown, and
// whether the id is a document. It reads the tables rather than file_items so a
// pending upload (not yet in the view) is found too.
func (r *FileRepository) FileNodeArea(ctx context.Context, id uuid.UUID) (area string, doc bool, err error) {
	var row struct {
		Area string `db:"area"`
		Doc  bool   `db:"doc"`
	}
	err = r.db.GetContext(ctx, &row, `
		SELECT COALESCE(area, '') AS area, doc FROM (
			SELECT area, false AS doc FROM file_folders WHERE id = $1
			UNION ALL
			SELECT ff.area, false FROM files f JOIN file_folders ff ON ff.id = f.folder_id WHERE f.id = $1
			UNION ALL
			SELECT 'docs', true FROM documents WHERE id = $1 AND view_id IS NULL
		) x LIMIT 1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("file repo: node area: %w", err)
	}
	return row.Area, row.Doc, nil
}

// Relations returns the task a project file is attached to and the documents
// of the docs tree whose content mentions its id (files://<project>/<id>) or
// that own it (a document file is stored under docs/<docId>/...).
func (r *FileRepository) Relations(ctx context.Context, projectID, fileID uuid.UUID) (*filedom.Relations, error) {
	out := &filedom.Relations{Documents: []filedom.DocRef{}}

	var tasks []struct {
		ID     uuid.UUID `db:"id"`
		Number int64     `db:"task_number"`
		Prefix string    `db:"task_id_prefix"`
		Title  string    `db:"title"`
	}
	if err := r.db.SelectContext(ctx, &tasks, `
		SELECT t.id, t.task_number, COALESCE(p.task_id_prefix, '') AS task_id_prefix, t.title
		  FROM task_attachments ta
		  JOIN tasks t ON t.id = ta.task_id AND t.deleted_at IS NULL
		  JOIN projects p ON p.id = t.project_id
		 WHERE t.project_id = $1 AND ta.file_id = $2
		 ORDER BY ta.created_at LIMIT 1`, projectID, fileID); err != nil {
		return nil, fmt.Errorf("file repo: relations tasks: %w", err)
	}
	if len(tasks) > 0 {
		t := tasks[0]
		prefix := t.Prefix
		if prefix == "" {
			prefix = "TASK"
		}
		out.Task = &filedom.TaskRef{ID: t.ID, Number: t.Number, Key: fmt.Sprintf("%s-%d", prefix, t.Number), Title: t.Title}
	}

	var docs []struct {
		ID    uuid.UUID `db:"id"`
		Title string    `db:"title"`
	}
	// Only the documents the caller's document scope allows (see docsScopeSQL).
	sink := &argList{args: []any{projectID, fileID}}
	scope, none, err := scopeSQL(ctx, "doc", docScopeColumns, sink)
	if err != nil {
		return nil, err
	}
	if none {
		return out, nil
	}
	if scope != "" {
		scope = " AND (" + scope + ")"
	}
	if err := r.db.SelectContext(ctx, &docs, `
		SELECT documents.id, documents.title
		  FROM documents
		 WHERE documents.project_id = $1 AND documents.deleted_at IS NULL AND documents.view_id IS NULL
		   AND ((documents.content IS NOT NULL AND documents.content::text LIKE '%' || $2::uuid::text || '%')
		     OR EXISTS (SELECT 1 FROM files fx WHERE fx.id = $2::uuid AND fx.source = 'doc'
		                AND fx.storage_key LIKE 'docs/' || documents.id::text || '/%'))`+scope+`
		 ORDER BY lower(documents.title), documents.id`, sink.args...); err != nil {
		return nil, fmt.Errorf("file repo: relations documents: %w", err)
	}
	for _, d := range docs {
		out.Documents = append(out.Documents, filedom.DocRef{ID: d.ID, Title: d.Title})
	}
	return out, nil
}
