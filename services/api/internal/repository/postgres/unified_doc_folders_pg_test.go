package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	filedom "github.com/Paca-AI/api/internal/domain/file"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// Documentation folders live in file_folders under the project's locked
// Documentation folder, while the docs repository keeps seeing the shape it
// always had (migration 000096).
func TestUnifiedDocFolders_DocsViewAndFileTree(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewDocumentRepository(db)
	project := fx.project()
	ctx := t.Context()

	var root uuid.UUID
	if err := db.GetContext(ctx, &root, `SELECT id FROM file_folders WHERE project_id = $1 AND system_key = 'documentation'`, project); err != nil {
		t.Fatalf("documentation folder: %v", err)
	}

	// Created through the docs repository: a top-level folder has no parent for
	// docs, and sits under the Documentation folder in file_folders.
	top := &docdom.DocFolder{ID: uuid.New(), ProjectID: project, Name: "Governance"}
	child := &docdom.DocFolder{ID: uuid.New(), ProjectID: project, ParentID: &top.ID, Name: "Risks", Position: 2}
	for _, f := range []*docdom.DocFolder{top, child} {
		if err := repo.CreateFolder(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	var parent uuid.UUID
	var area string
	if err := db.QueryRowxContext(ctx, `SELECT parent_id, area FROM file_folders WHERE id = $1`, top.ID).Scan(&parent, &area); err != nil || parent != root || area != "docs" {
		t.Fatalf("top folder parent=%v area=%q err=%v, want the Documentation folder and area docs", parent, area, err)
	}
	if err := db.QueryRowxContext(ctx, `SELECT area FROM file_folders WHERE id = $1`, child.ID).Scan(&area); err != nil || area != "docs" {
		t.Fatalf("child area = %q, %v", area, err)
	}

	got, _, err := repo.ListFolders(ctx, project, nil, docdom.FolderFilter{}, nil, 50)
	if err != nil || len(got) != 1 || got[0].ID != top.ID || got[0].ParentID != nil {
		t.Fatalf("top-level folders = %+v, %v", got, err)
	}
	path, err := repo.FolderPath(ctx, project, child.ID)
	if err != nil || len(path) != 2 || path[0].ID != top.ID || path[1].ID != child.ID {
		t.Fatalf("path = %+v, %v", path, err)
	}

	// Move to the top level and rename through the docs repository.
	if _, err := repo.UpdateFolderAtomic(ctx, child.ID, func(cur *docdom.DocFolder, _ func(uuid.UUID) (*docdom.DocFolder, error)) error {
		cur.ParentID, cur.Name = nil, "Risk register"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowxContext(ctx, `SELECT parent_id FROM file_folders WHERE id = $1`, child.ID).Scan(&parent); err != nil || parent != root {
		t.Fatalf("moved folder parent = %v, %v", parent, err)
	}

	// The Files tree sees the documentation folders; the file manager must not
	// manage them yet.
	files := NewFileRepository(db)
	n, err := files.GetNode(ctx, top.ID, fx.user(false))
	if err != nil || n.Area != "docs" || !n.Locked() {
		t.Fatalf("file node = %+v, %v; want a locked docs-area folder", n, err)
	}

	// A change-feed bump for folder writes in the subtree, not for the root.
	var folderSeq int64
	if err := db.GetContext(ctx, &folderSeq, `SELECT folders_seq FROM doc_sync_state WHERE project_id = $1`, project); err != nil || folderSeq == 0 {
		t.Fatalf("folders_seq = %d, %v", folderSeq, err)
	}

	// Deleting a folder trashes it with its documents and files, one shared
	// timestamp, instead of deleting rows.
	doc, file := uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO documents (id, project_id, folder_id, title) VALUES ($1, $2, $3, 'D')`, doc, project, top.ID)
	pgExec(t, db, `INSERT INTO files (id, storage_key, bucket, file_name, upload_status, project_id, folder_id)
		VALUES ($1, $2, 'b', 'g.docx', 'uploaded', $3, $4)`, file, "files/"+project.String()+"/x/g.docx", project, top.ID)
	if err := repo.DeleteFolder(ctx, top.ID); err != nil {
		t.Fatal(err)
	}
	var folderStamp, docStamp, docTrashed, fileStamp *time.Time
	if err := db.QueryRowxContext(ctx, `SELECT deleted_at FROM file_folders WHERE id = $1`, top.ID).Scan(&folderStamp); err != nil || folderStamp == nil {
		t.Fatalf("folder deleted_at = %v, %v; want it trashed, not deleted", folderStamp, err)
	}
	if err := db.QueryRowxContext(ctx, `SELECT deleted_at, trashed_at FROM documents WHERE id = $1`, doc).Scan(&docStamp, &docTrashed); err != nil || docStamp == nil || docTrashed == nil || !docStamp.Equal(*folderStamp) {
		t.Fatalf("document deleted_at=%v trashed_at=%v err=%v; want the folder's stamp", docStamp, docTrashed, err)
	}
	if err := db.QueryRowxContext(ctx, `SELECT deleted_at FROM files WHERE id = $1`, file).Scan(&fileStamp); err != nil || fileStamp == nil || !fileStamp.Equal(*folderStamp) {
		t.Fatalf("file deleted_at = %v, %v; want the folder's stamp", fileStamp, err)
	}
	if _, err := repo.FindFolderByID(ctx, top.ID); !errors.Is(err, docdom.ErrFolderNotFound) {
		t.Fatalf("trashed folder = %v, want not found for docs", err)
	}
	// The sibling moved out of it survives.
	if _, err := repo.FindFolderByID(ctx, child.ID); err != nil {
		t.Fatalf("surviving folder: %v", err)
	}
}

// A folder moved between the Documentation tree and the rest of Files takes its
// whole subtree along.
func TestUnifiedDocFolders_AreaFollowsParent(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	project := fx.project()
	a, b := uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO file_folders (id, project_id, name) VALUES ($1, $2, 'Plain')`, a, project)
	pgExec(t, db, `INSERT INTO file_folders (id, project_id, parent_id, name) VALUES ($1, $2, $3, 'Inner')`, b, project, a)
	areaOf := func(id uuid.UUID) string {
		var s *string
		if err := db.GetContext(t.Context(), &s, `SELECT area FROM file_folders WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		if s == nil {
			return ""
		}
		return *s
	}
	if areaOf(a) != "" || areaOf(b) != "" {
		t.Fatal("plain folders must have no area")
	}
	pgExec(t, db, `UPDATE file_folders SET parent_id = (SELECT id FROM file_folders WHERE project_id = $1 AND system_key = 'documentation') WHERE id = $2`, project, a)
	if areaOf(a) != "docs" || areaOf(b) != "docs" {
		t.Fatalf("after moving into Documentation: %q, %q", areaOf(a), areaOf(b))
	}
	pgExec(t, db, `UPDATE file_folders SET parent_id = NULL WHERE id = $1`, a)
	if areaOf(a) != "" || areaOf(b) != "" {
		t.Fatalf("after moving out: %q, %q", areaOf(a), areaOf(b))
	}
}

// Documents are items of the file tree: under their folder, or directly under
// Documentation when they have none. Docs-area nodes stay out of whole-scope
// lists, and the area lookup finds folders, documents and files.
func TestUnifiedDocFolders_DocumentsAreFileItems(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	docs := NewDocumentRepository(db)
	repo := NewFileRepository(db)
	project := fx.project()
	user := fx.user(false)
	scope := filedom.Scope{ProjectID: &project}
	ctx := t.Context()

	var root uuid.UUID
	if err := db.GetContext(ctx, &root, `SELECT id FROM file_folders WHERE project_id = $1 AND system_key = 'documentation'`, project); err != nil {
		t.Fatal(err)
	}
	folder := &docdom.DocFolder{ID: uuid.New(), ProjectID: project, Name: "Governance"}
	if err := docs.CreateFolder(ctx, folder); err != nil {
		t.Fatal(err)
	}
	inFolder, loose, trashedDoc, note, view := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO documents (id, project_id, folder_id, title) VALUES ($1, $2, $3, 'Charter')`, inFolder, project, folder.ID)
	pgExec(t, db, `INSERT INTO documents (id, project_id, title) VALUES ($1, $2, 'Loose')`, loose, project)
	pgExec(t, db, `INSERT INTO documents (id, project_id, title, deleted_at) VALUES ($1, $2, 'Gone', now())`, trashedDoc, project)
	pgExec(t, db, `INSERT INTO sprint_views (id, project_id, name, view_type) VALUES ($1, $2, 'Notes', 'note')`, view, project)
	pgExec(t, db, `INSERT INTO documents (id, project_id, view_id, title) VALUES ($1, $2, $3, 'Minutes')`, note, project, view)
	upload := uuid.New()
	pgExec(t, db, `INSERT INTO files (id, storage_key, bucket, file_name, upload_status, project_id, folder_id)
		VALUES ($1, $2, 'b', 'charter.docx', 'uploaded', $3, $4)`, upload, "files/"+project.String()+"/x/charter.docx", project, folder.ID)

	kinds := func(parent uuid.UUID) map[string]filedom.Kind {
		nodes, _, err := repo.List(ctx, filedom.ListParams{Scope: scope, UserID: user, ParentID: &parent})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]filedom.Kind{}
		for _, n := range nodes {
			out[n.Name] = n.Kind
		}
		return out
	}
	if got := kinds(root); len(got) != 2 || got["Governance"] != filedom.KindFolder || got["Loose.md"] != filedom.KindDoc {
		t.Fatalf("Documentation = %v, want the Governance folder and the loose document (no trashed doc, no note)", got)
	}
	if got := kinds(folder.ID); len(got) != 2 || got["Charter.md"] != filedom.KindDoc || got["charter.docx"] != filedom.KindFile {
		t.Fatalf("Governance = %v, want its document and its file", got)
	}

	n, err := repo.GetNode(ctx, inFolder, user)
	if err != nil || !n.Locked() || n.Area != "docs" || n.ParentID == nil || *n.ParentID != folder.ID {
		t.Fatalf("document node = %+v, %v; want a locked docs-area node under its folder", n, err)
	}
	f, err := repo.GetNode(ctx, upload, user)
	if err != nil || f.Locked() || f.Area != "docs" {
		t.Fatalf("file node = %+v, %v; a file in Documentation is still renamable and movable", f, err)
	}

	// Search, Starred and Recent never reach into Documentation.
	found, total, err := repo.List(ctx, filedom.ListParams{Scope: scope, UserID: user, Query: "charter"})
	if err != nil || total != 0 || len(found) != 0 {
		t.Fatalf("search = %v (%d), %v; want nothing from Documentation", namesOf(found), total, err)
	}
	if err := repo.SetStarred(ctx, user, inFolder, true); err != nil {
		t.Fatal(err)
	}
	if starred, _, err := repo.ListStarred(ctx, scope, user, 100, 0); err != nil || len(starred) != 0 {
		t.Fatalf("starred = %v, %v", namesOf(starred), err)
	}

	for id, want := range map[uuid.UUID]string{folder.ID: "docs", inFolder: "docs", upload: "docs", root: "docs", uuid.New(): ""} {
		if got, _, err := repo.FileNodeArea(ctx, id); err != nil || got != want {
			t.Errorf("area of %s = %q, %v; want %q", id, got, err, want)
		}
	}
	if _, isDoc, _ := repo.FileNodeArea(ctx, inFolder); !isDoc {
		t.Errorf("a document must be reported as one")
	}
	if _, isDoc, _ := repo.FileNodeArea(ctx, upload); isDoc {
		t.Errorf("a file must not be reported as a document")
	}
	if area, _, _ := repo.FileNodeArea(ctx, note); area != "" {
		t.Errorf("a note (not in the docs tree) has area %q, want none", area)
	}
}

// Whole-scope lists include Documentation only as far as the caller's document
// scope allows: a role limited to one folder's documents finds those, not the
// others, and a caller with no scope attached finds nothing there.
func TestUnifiedDocFolders_ListsFollowTheDocumentScope(t *testing.T) {
	f := newScopeFx(t)
	ctx := t.Context()
	docs := NewDocumentRepository(f.db)
	repo := NewFileRepository(f.db)
	scope := filedom.Scope{ProjectID: &f.p}

	allowed := &docdom.DocFolder{ID: uuid.New(), ProjectID: f.p, Name: "Open folder"}
	hidden := &docdom.DocFolder{ID: uuid.New(), ProjectID: f.p, Name: "Secret folder"}
	for _, fo := range []*docdom.DocFolder{allowed, hidden} {
		if err := docs.CreateFolder(ctx, fo); err != nil {
			t.Fatal(err)
		}
	}
	a, b, planFile := uuid.New(), uuid.New(), uuid.New()
	mention := `[{"type":"file","props":{"url":"files://` + f.p.String() + `/` + planFile.String() + `"}}]`
	pgExec(t, f.db, `INSERT INTO documents (id, project_id, folder_id, title, content) VALUES ($1, $3, $4, 'Plan visible', $6::jsonb), ($2, $3, $5, 'Plan hidden', $6::jsonb)`, a, b, f.p, allowed.ID, hidden.ID, mention)
	pgExec(t, f.db, `INSERT INTO files (id, storage_key, bucket, file_name, upload_status, project_id, folder_id)
		VALUES ($1, $2, 'b', 'plan.docx', 'uploaded', $3, $4)`, planFile, "files/"+f.p.String()+"/x/plan.docx", f.p, allowed.ID)
	for _, id := range []uuid.UUID{a, b} {
		if err := repo.SetStarred(ctx, f.u, id, true); err != nil {
			t.Fatal(err)
		}
		if err := repo.Touch(ctx, f.u, id); err != nil {
			t.Fatal(err)
		}
	}

	names := func(c context.Context) []string {
		t.Helper()
		found, _, err := repo.List(c, filedom.ListParams{Scope: scope, UserID: f.u, Query: "plan"})
		if err != nil {
			t.Fatal(err)
		}
		return sorted(namesOf(found))
	}
	starred := func(c context.Context) []string {
		t.Helper()
		got, _, err := repo.ListStarred(c, scope, f.u, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		return sorted(namesOf(got))
	}
	recent := func(c context.Context) []string {
		t.Helper()
		got, err := repo.ListRecent(c, scope, f.u, 100)
		if err != nil {
			t.Fatal(err)
		}
		return sorted(namesOf(got))
	}
	join := func(v []string) string { return strings.Join(v, "|") }

	// No scope attached: nothing in Documentation.
	if got := names(ctx); len(got) != 0 {
		t.Fatalf("unscoped search = %v, want nothing from Documentation", got)
	}

	// A role limited to one folder's documents.
	f.role(policyJSON("Allow", "docs:read", "project/*/doc/*", `{"StringEquals":{"doc.folder_id":["`+allowed.ID.String()+`"]}}`))
	scoped := f.ctx("docs:read", "doc")
	if got := join(names(scoped)); got != "Plan visible.md|plan.docx" {
		t.Fatalf("scoped search = %q, want the visible document and the file", got)
	}
	if got := join(starred(scoped)); got != "Plan visible.md" {
		t.Fatalf("scoped starred = %q", got)
	}
	if got := join(recent(scoped)); got != "Plan visible.md" {
		t.Fatalf("scoped recent = %q", got)
	}

	// Browsing a folder follows the scope too: the hidden document's folder lists
	// no document, and the other lists its own.
	browse := func(c context.Context, parent uuid.UUID) string {
		t.Helper()
		got, _, err := repo.List(c, filedom.ListParams{Scope: scope, UserID: f.u, ParentID: &parent})
		if err != nil {
			t.Fatal(err)
		}
		return join(sorted(namesOf(got)))
	}
	if got := browse(scoped, allowed.ID); got != "Plan visible.md|plan.docx" {
		t.Fatalf("scoped browse of the allowed folder = %q", got)
	}
	if got := browse(scoped, hidden.ID); got != "" {
		t.Fatalf("scoped browse of the hidden folder = %q, want no document", got)
	}
	// Without a scope (an internal caller) nothing is filtered.
	if got := browse(ctx, hidden.ID); got != "Plan hidden.md" {
		t.Fatalf("unscoped browse = %q", got)
	}
	// The documents that reference a file are limited the same way.
	rel, err := repo.Relations(scoped, f.p, planFile)
	if err != nil || len(rel.Documents) != 1 || rel.Documents[0].ID != a {
		t.Fatalf("scoped relations = %+v, %v; want only the visible document", rel, err)
	}
	if rel, err = repo.Relations(ctx, f.p, planFile); err != nil || len(rel.Documents) != 2 {
		t.Fatalf("unscoped relations = %+v, %v; want both documents", rel, err)
	}

	// An unrestricted scope sees both.
	open := iam.WithScope(ctx, "doc", &iam.Node{Kind: iam.NodeTrue})
	if got := join(names(open)); got != "Plan hidden.md|Plan visible.md|plan.docx" {
		t.Fatalf("unrestricted search = %q", got)
	}
	// A scope that allows no document sees nothing there.
	none := iam.WithScope(ctx, "doc", &iam.Node{Kind: iam.NodeFalse})
	if got := names(none); len(got) != 0 {
		t.Fatalf("deny-all search = %v", got)
	}
}

// A document and a documentation folder go to the trash, come back whole, and
// are erased (tombstone kept) when purged.
func TestUnifiedDocFolders_TrashRestorePurge(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	docs := NewDocumentRepository(db)
	repo := NewFileRepository(db)
	project := fx.project()
	user := fx.user(false)
	scope := filedom.Scope{ProjectID: &project}
	ctx := t.Context()
	unrestricted := iam.WithScope(ctx, "doc", &iam.Node{Kind: iam.NodeTrue})

	folder := &docdom.DocFolder{ID: uuid.New(), ProjectID: project, Name: "Governance"}
	if err := docs.CreateFolder(ctx, folder); err != nil {
		t.Fatal(err)
	}
	inFolder, loose := uuid.New(), uuid.New()
	pgExec(t, db, `INSERT INTO documents (id, project_id, folder_id, title, content) VALUES ($1, $3, $4, 'Charter', '[{"type":"paragraph"}]'::jsonb), ($2, $3, NULL, 'Loose', '[{"type":"paragraph"}]'::jsonb)`, inFolder, loose, project, folder.ID)
	pgExec(t, db, `INSERT INTO doc_snapshots (document_id, title, content) VALUES ($1, 'Charter', '[]'::jsonb)`, inFolder)

	node := func(id uuid.UUID) *filedom.Node {
		t.Helper()
		n, err := repo.GetNode(ctx, id, user)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	trashNames := func() string {
		t.Helper()
		nodes, _, err := repo.ListTrash(unrestricted, scope, user, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(sorted(namesOf(nodes)), "|")
	}
	at := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)

	// A single document.
	if err := repo.Trash(ctx, node(loose), user, at); err != nil {
		t.Fatal(err)
	}
	if got := trashNames(); got != "Loose.md" {
		t.Fatalf("trash = %q", got)
	}
	if _, err := docs.FindDocumentByID(ctx, loose); !errors.Is(err, docdom.ErrDocNotFound) {
		t.Fatalf("a trashed document must be gone for docs, got %v", err)
	}
	if err := repo.Restore(ctx, node(loose)); err != nil {
		t.Fatal(err)
	}
	if got := trashNames(); got != "" {
		t.Fatalf("trash after restore = %q", got)
	}
	if d, err := docs.FindDocumentByID(ctx, loose); err != nil || d.Title != "Loose" {
		t.Fatalf("restored document = %+v, %v", d, err)
	}

	// A folder with a document: only the folder is a trash root.
	if err := repo.Trash(ctx, node(folder.ID), user, at); err != nil {
		t.Fatal(err)
	}
	if got := trashNames(); got != "Governance" {
		t.Fatalf("trash = %q, want only the folder", got)
	}
	if err := repo.Restore(ctx, node(folder.ID)); err != nil {
		t.Fatal(err)
	}
	if d, err := docs.FindDocumentByID(ctx, inFolder); err != nil || d.FolderID == nil || *d.FolderID != folder.ID {
		t.Fatalf("document after folder restore = %+v, %v; want it back in its folder", d, err)
	}

	// Restoring a document whose folder is still trashed puts it at the top.
	if err := repo.Trash(ctx, node(inFolder), user, at); err != nil {
		t.Fatal(err)
	}
	if err := repo.Trash(ctx, node(folder.ID), user, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Restore(ctx, node(inFolder)); err != nil {
		t.Fatal(err)
	}
	if d, err := docs.FindDocumentByID(ctx, inFolder); err != nil || d.FolderID != nil {
		t.Fatalf("document restored out of a trashed folder = %+v, %v; want the top of Documentation", d, err)
	}
	if err := repo.Restore(ctx, node(folder.ID)); err != nil {
		t.Fatal(err)
	}

	// Purging erases the content and snapshots but keeps the tombstone.
	if err := repo.Trash(ctx, node(inFolder), user, at); err != nil {
		t.Fatal(err)
	}
	objs, err := repo.PurgeTrashedBefore(ctx, time.Now(), 10)
	if err != nil || len(objs) != 0 {
		t.Fatalf("purge = %v, %v", objs, err)
	}
	var content *string
	var deleted bool
	var snaps int
	if err := db.QueryRowxContext(ctx, `SELECT content::text, deleted_at IS NOT NULL FROM documents WHERE id = $1`, inFolder).Scan(&content, &deleted); err != nil || content != nil || !deleted {
		t.Fatalf("purged document content=%v deleted=%v err=%v; want erased tombstone", content, deleted, err)
	}
	if err := db.GetContext(ctx, &snaps, `SELECT count(*) FROM doc_snapshots WHERE document_id = $1`, inFolder); err != nil || snaps != 0 {
		t.Fatalf("snapshots after purge = %d, %v", snaps, err)
	}
	if got := trashNames(); got != "" {
		t.Fatalf("trash after purge = %q", got)
	}
	// A document deleted before trash existed is neither listed nor purged.
	legacy := uuid.New()
	pgExec(t, db, `INSERT INTO documents (id, project_id, title, content, deleted_at) VALUES ($1, $2, 'Old', '[]'::jsonb, now() - interval '90 days')`, legacy, project)
	if _, err := repo.PurgeTrashedBefore(ctx, time.Now(), 10); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowxContext(ctx, `SELECT content::text FROM documents WHERE id = $1`, legacy).Scan(&content); err != nil || content == nil {
		t.Fatalf("a legacy deleted document must keep its content, got %v, %v", content, err)
	}
}
