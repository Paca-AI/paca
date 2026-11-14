package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// Documents addressed by path: see docdom.DocPathRepository.

// resolveFolderPathSQL walks the path's folder names ($2) from the project
// root, one level per step. Each step picks the child of the previous folder
// whose name matches case-insensitively, preferring the exact-case name and
// then manual order, so the walk is deterministic when siblings share a name.
// It stops at the first missing name, so the rows returned are the folders
// that exist, root first.
const resolveFolderPathSQL = `
	WITH RECURSIVE walk(depth, id) AS (
		SELECT 0, NULL::uuid
	  UNION ALL
		SELECT w.depth + 1, (
			SELECT f.id FROM doc_folders f
			WHERE f.project_id = $1
			  AND f.parent_id IS NOT DISTINCT FROM w.id
			  AND LOWER(f.name) = LOWER(($2::text[])[w.depth + 1])
			ORDER BY (f.name = ($2::text[])[w.depth + 1]) DESC, f.position, f.name, f.id
			LIMIT 1)
		FROM walk w
		WHERE w.depth < cardinality($2::text[]) AND (w.depth = 0 OR w.id IS NOT NULL)
	)
	SELECT depth, id::text AS id FROM walk WHERE id IS NOT NULL ORDER BY depth`

// findDocInFolderSQL finds the live document titled $3 in folder $2 (NULL =
// the root), with the same tie-breaking as the folder walk.
const findDocInFolderSQL = `SELECT ` + documentCols + ` FROM documents
	WHERE project_id = $1 AND folder_id IS NOT DISTINCT FROM $2::uuid AND deleted_at IS NULL
	  AND LOWER(title) = LOWER($3)
	ORDER BY (title = $3) DESC, position, title, id
	LIMIT 1`

// folderNamedSQL reports whether folder $2 (NULL = the root) has a child
// folder named $3 (case-insensitively).
const folderNamedSQL = `SELECT EXISTS (SELECT 1 FROM doc_folders
	WHERE project_id = $1 AND parent_id IS NOT DISTINCT FROM $2::uuid AND LOWER(name) = LOWER($3))`

// ResolveDocPath implements docdom.DocPathRepository.
func (r *DocumentRepository) ResolveDocPath(ctx context.Context, projectID uuid.UUID, folders []string, title string) (*docdom.DocPathResolution, error) {
	return resolveDocPath(ctx, r.db, projectID, folders, title)
}

func resolveDocPath(ctx context.Context, q sqlx.QueryerContext, projectID uuid.UUID, folders []string, title string) (*docdom.DocPathResolution, error) {
	res := &docdom.DocPathResolution{}
	if len(folders) > 0 {
		var steps []struct {
			Depth int    `db:"depth"`
			ID    string `db:"id"`
		}
		if err := sqlx.SelectContext(ctx, q, &steps, resolveFolderPathSQL, projectID.String(), folders); err != nil {
			return nil, err
		}
		if n := len(steps); n > 0 {
			id := uuid.MustParse(steps[n-1].ID)
			res.Depth, res.FolderID = steps[n-1].Depth, &id
		}
	}
	if res.Depth < len(folders) {
		return res, nil // a folder is missing: so is the document
	}
	var folderArg *string
	if res.FolderID != nil {
		s := res.FolderID.String()
		folderArg = &s
	}
	var rec documentRecord
	switch err := sqlx.GetContext(ctx, q, &rec, findDocInFolderSQL, projectID.String(), folderArg, title); {
	case err == nil:
		res.Doc = documentFromRecord(rec)
		return res, nil
	case !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	if err := sqlx.GetContext(ctx, q, &res.IsFolder, folderNamedSQL, projectID.String(), folderArg, title); err != nil {
		return nil, err
	}
	return res, nil
}

// sameResolution reports whether two resolutions name the same place and the
// same document (or the same absence of one).
func sameResolution(a, b docdom.DocPathResolution) bool {
	if a.Depth != b.Depth || a.IsFolder != b.IsFolder || (a.FolderID == nil) != (b.FolderID == nil) ||
		(a.FolderID != nil && *a.FolderID != *b.FolderID) || (a.Doc == nil) != (b.Doc == nil) {
		return false
	}
	return a.Doc == nil || a.Doc.ID == b.Doc.ID
}

// WriteDocumentAtPath implements docdom.DocPathRepository. It takes the same
// per-project advisory lock as UpdateFolderAtomic, so it is serialized against
// other by-path writes (no second insert of one path, no duplicate folder) and
// against folder moves (the path it resolved stays valid until commit). The
// document's own row lock still guards the update against a PATCH by id.
func (r *DocumentRepository) WriteDocumentAtPath(ctx context.Context, w docdom.DocPathWrite) (*docdom.DocPathWriteResult, error) {
	var result *docdom.DocPathWriteResult
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		// The change counter first, then the folder lock, then rows: the
		// order every document and folder write takes them in (lockDocSeq).
		if err := lockDocSeq(ctx, tx, w.ProjectID.String()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('doc_folders:' || $1::text, 0))`, w.ProjectID.String()); err != nil {
			return err
		}
		res, err := resolveDocPath(ctx, tx, w.ProjectID, w.Folders, w.Title)
		if err != nil {
			return err
		}
		if !sameResolution(*res, w.Expect) {
			return docdom.ErrDocPathChanged
		}
		if res.IsFolder {
			return docdom.ErrDocPathIsFolder
		}
		out := &docdom.DocPathWriteResult{}
		parent := res.FolderID
		for _, name := range w.Folders[res.Depth:] {
			f := w.NewFolder()
			f.ProjectID, f.ParentID, f.Name = w.ProjectID, parent, name
			if err := insertFolder(ctx, tx, f); err != nil {
				return err
			}
			out.Folders = append(out.Folders, f)
			id := f.ID
			parent = &id
		}
		if res.Doc != nil {
			// A document is deleted by id, without the folder lock, so it can
			// go between the resolve above and the row lock. Then the row is
			// missing before decide runs: the path changed, and the service
			// resolves again (and creates the document). ErrDocNotFound from
			// decide itself is passed on.
			decided := false
			decide := func(d *docdom.Document) error {
				decided = true
				return w.Decide(d)
			}
			if out.Doc, err = updateDocumentLocked(ctx, tx, res.Doc.ID, decide); err != nil {
				if !decided && errors.Is(err, docdom.ErrDocNotFound) {
					return docdom.ErrDocPathChanged
				}
				return err
			}
		} else {
			d := w.NewDoc()
			d.ProjectID, d.FolderID, d.Title = w.ProjectID, parent, w.Title
			if err := insertDocument(ctx, tx, d); err != nil {
				return err
			}
			out.Doc, out.Created = d, true
		}
		result = out
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
