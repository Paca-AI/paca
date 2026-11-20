package postgres

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	filedom "github.com/Paca-AI/api/internal/domain/file"
)

func addUploadedFile(t *testing.T, db *sqlx.DB, key, name, contentType string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	pgExec(t, db, `INSERT INTO files (id, storage_key, bucket, file_name, content_type, file_size, upload_status)
		VALUES ($1, $2, 'b', $3, $4, 10, 'uploaded')`, id, key, name, contentType)
	return id
}

func namesOf(nodes []*filedom.Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Name
	}
	return out
}

// Every project starts with its default folders, and a file's storage key
// decides where it lands without the upload services knowing about folders.
func TestFileRepository_DefaultFoldersAndClassification(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewFileRepository(db)
	project := fx.project()
	scope := filedom.Scope{ProjectID: &project}
	user := fx.user(false)

	root, total, err := repo.List(t.Context(), filedom.ListParams{Scope: scope, UserID: user})
	if err != nil {
		t.Fatal(err)
	}
	if got := namesOf(root); total != 4 || got[0] != "Annotation screenshots" || got[1] != "Document files" || got[2] != "Documentation" || got[3] != "Task attachments" {
		t.Fatalf("project root = %v (total %d), want the four default folders", got, total)
	}
	for _, n := range root {
		if !n.Locked() || n.SystemKey == "" {
			t.Fatalf("default folder %q must be locked with a system key", n.Name)
		}
	}

	task, doc := uuid.New(), uuid.New()
	pgExec(t, db, `UPDATE projects SET task_id_prefix = 'PACA' WHERE id = $1`, project)
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1, $2, 12, 't')`, task, project)
	pgExec(t, db, `INSERT INTO documents (id, project_id, title) VALUES ($1, $2, 'd')`, doc, project)
	taskFile := addUploadedFile(t, db, "tasks/"+task.String()+"/x/a.pdf", "a.pdf", "application/pdf")
	docFile := addUploadedFile(t, db, "docs/"+doc.String()+"/x/image.png", "image.png", "image/png")
	addUploadedFile(t, db, "avatars/users/u/f/upload", "avatar.png", "image/png") // never shown
	pending := uuid.New()
	pgExec(t, db, `INSERT INTO files (id, storage_key, bucket, file_name, upload_status) VALUES ($1, $2, 'b', 'p.pdf', 'pending')`,
		pending, "tasks/"+task.String()+"/y/p.pdf")

	var taskRoot, docsRoot uuid.UUID
	for _, n := range root {
		switch n.SystemKey {
		case filedom.SystemKeyTaskRoot:
			taskRoot = n.ID
		case filedom.SystemKeyDocs:
			docsRoot = n.ID
		}
	}
	taskFolders, _, err := repo.List(t.Context(), filedom.ListParams{Scope: scope, UserID: user, ParentID: &taskRoot})
	if err != nil || len(taskFolders) != 1 || taskFolders[0].Name != "PACA-12" || taskFolders[0].SystemKey != filedom.SystemKeyTask {
		t.Fatalf("task folders = %v (err %v), want one PACA-12 folder", namesOf(taskFolders), err)
	}
	inTask, _, _ := repo.List(t.Context(), filedom.ListParams{Scope: scope, UserID: user, ParentID: &taskFolders[0].ID})
	if len(inTask) != 1 || inTask[0].ID != taskFile || inTask[0].Source != filedom.SourceTaskAttachment || !inTask[0].Locked() {
		t.Fatalf("task folder = %+v, want the uploaded attachment only (pending files stay hidden)", inTask)
	}
	inDocs, _, _ := repo.List(t.Context(), filedom.ListParams{Scope: scope, UserID: user, ParentID: &docsRoot})
	if len(inDocs) != 1 || inDocs[0].ID != docFile || inDocs[0].Source != filedom.SourceDoc {
		t.Fatalf("docs folder = %+v, want the doc image", inDocs)
	}

	// Removing the attachment from its task sends the file to the trash.
	pgExec(t, db, `INSERT INTO task_attachments (task_id, file_id) VALUES ($1, $2)`, task, taskFile)
	pgExec(t, db, `DELETE FROM task_attachments WHERE file_id = $1`, taskFile)
	inTask, _, _ = repo.List(t.Context(), filedom.ListParams{Scope: scope, UserID: user, ParentID: &taskFolders[0].ID})
	if len(inTask) != 0 {
		t.Fatalf("detached attachment still listed: %+v", inTask)
	}
	trash, _, _ := repo.ListTrash(t.Context(), scope, user, 10, 0)
	if len(trash) != 1 || trash[0].ID != taskFile {
		t.Fatalf("trash = %+v, want the detached attachment", trash)
	}
}

func TestFileRepository_FoldersMoveTrashRestorePurge(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewFileRepository(db)
	project := fx.project()
	scope := filedom.Scope{ProjectID: &project}
	user := fx.user(false)
	now := time.Now().UTC()

	folder := func(parent *uuid.UUID, name string) *filedom.Node {
		n := &filedom.Node{ID: uuid.New(), ProjectID: &project, ParentID: parent, Kind: filedom.KindFolder, Name: name, CreatedBy: &user, CreatedAt: now, UpdatedAt: now}
		if err := repo.CreateFolder(t.Context(), n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	upload := func(parent *uuid.UUID, name string) *filedom.Node {
		id := uuid.New()
		pgExec(t, db, `INSERT INTO files (id, storage_key, bucket, file_name, content_type, file_size, upload_status, project_id, folder_id, uploaded_by)
			VALUES ($1, $2, 'b', $3, 'text/plain', 5, 'uploaded', $4, $5, $6)`, id, "files/"+project.String()+"/"+id.String()+"/"+name, name, project, parent, user)
		n, err := repo.GetNode(t.Context(), id, user)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	a := folder(nil, "A")
	b := folder(&a.ID, "B")
	f1 := upload(&b.ID, "one.txt")
	f2 := upload(nil, "two.txt")
	dup := upload(nil, "two.txt") // names need not be unique
	if f2.ID == dup.ID {
		t.Fatal("duplicate names must create distinct files")
	}

	if in, _ := repo.IsSelfOrDescendant(t.Context(), a.ID, b.ID); !in {
		t.Fatal("B lies below A")
	}
	if in, _ := repo.IsSelfOrDescendant(t.Context(), b.ID, a.ID); in {
		t.Fatal("A does not lie below B")
	}

	path, err := repo.Path(t.Context(), f1.ID)
	if err != nil || len(path) != 3 || path[0].ID != a.ID || path[1].ID != b.ID || path[2].ID != f1.ID {
		t.Fatalf("path = %v (err %v), want A > B > one.txt", namesOf(path), err)
	}

	// Rename and move both work on files and folders.
	if err := repo.Rename(t.Context(), f2, "renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Move(t.Context(), f2, &a.ID); err != nil {
		t.Fatal(err)
	}
	inA, total, _ := repo.List(t.Context(), filedom.ListParams{Scope: scope, UserID: user, ParentID: &a.ID})
	if total != 2 || inA[0].Name != "B" || inA[1].Name != "renamed.txt" {
		t.Fatalf("A = %v, want the folder B first, then renamed.txt", namesOf(inA))
	}

	// Stars and search.
	if err := repo.SetStarred(t.Context(), user, f1.ID, true); err != nil {
		t.Fatal(err)
	}
	starred, _, _ := repo.ListStarred(t.Context(), scope, user, 10, 0)
	if len(starred) != 1 || starred[0].ID != f1.ID || !starred[0].Starred {
		t.Fatalf("starred = %+v", starred)
	}
	found, _, _ := repo.List(t.Context(), filedom.ListParams{Scope: scope, UserID: user, Query: "ONE"})
	if len(found) != 1 || found[0].ID != f1.ID {
		t.Fatalf("search = %v, want one.txt", namesOf(found))
	}
	if _, _, err := repo.List(t.Context(), filedom.ListParams{Scope: scope, UserID: user, Type: "nope"}); err == nil {
		t.Fatal("an unknown type filter must be rejected")
	}

	// Trashing a folder takes its subtree; only the top shows in the trash.
	if err := repo.Trash(t.Context(), a, user, now); err != nil {
		t.Fatal(err)
	}
	if rootItems, _, _ := repo.List(t.Context(), filedom.ListParams{Scope: scope, UserID: user, Type: filedom.TypeDocument}); len(rootItems) != 1 {
		t.Fatalf("only the untouched duplicate remains listed, got %v", namesOf(rootItems))
	}
	trash, total, _ := repo.ListTrash(t.Context(), scope, user, 10, 0)
	if total != 1 || trash[0].ID != a.ID {
		t.Fatalf("trash = %v, want only A", namesOf(trash))
	}

	trashedA, _ := repo.GetNode(t.Context(), a.ID, user)
	if err := repo.Restore(t.Context(), trashedA); err != nil {
		t.Fatal(err)
	}
	back, _ := repo.GetNode(t.Context(), f1.ID, user)
	if back.DeletedAt != nil {
		t.Fatal("restoring A must bring back what was trashed with it")
	}

	// Purge removes the rows, reports the objects, and drops the stars.
	objs, err := repo.PurgeSubtree(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 {
		t.Fatalf("purged objects = %v, want one.txt and renamed.txt", objs)
	}
	if _, err := repo.GetNode(t.Context(), f1.ID, user); err != filedom.ErrNodeNotFound {
		t.Fatalf("purged file still found: %v", err)
	}
	var stars int
	if err := db.GetContext(t.Context(), &stars, `SELECT COUNT(*) FROM file_stars`); err != nil || stars != 0 {
		t.Fatalf("stars after purge = %d (err %v), want 0", stars, err)
	}
}

// A file's relations are the task it is attached to and the documents that
// mention it or own it.
func TestFileRepository_Relations(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	repo := NewFileRepository(db)
	project := fx.project()
	ctx := t.Context()

	task, mention, owner := uuid.New(), uuid.New(), uuid.New()
	pgExec(t, db, `UPDATE projects SET task_id_prefix = 'PACA' WHERE id = $1`, project)
	pgExec(t, db, `INSERT INTO tasks (id, project_id, task_number, title) VALUES ($1, $2, 12, 'Evaluate')`, task, project)
	attached := addUploadedFile(t, db, "tasks/"+task.String()+"/x/a.pdf", "a.pdf", "application/pdf")
	pgExec(t, db, `INSERT INTO task_attachments (task_id, file_id) VALUES ($1, $2)`, task, attached)
	pgExec(t, db, `INSERT INTO documents (id, project_id, title, content) VALUES ($1, $2, 'Method', $3::jsonb)`,
		mention, project, `[{"type":"file","props":{"url":"files://`+project.String()+`/`+attached.String()+`"}}]`)
	pgExec(t, db, `INSERT INTO documents (id, project_id, title) VALUES ($1, $2, 'Unrelated')`, owner, project)

	rel, err := repo.Relations(ctx, project, attached)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Task == nil || rel.Task.Key != "PACA-12" || rel.Task.Title != "Evaluate" {
		t.Fatalf("task = %+v", rel.Task)
	}
	if len(rel.Documents) != 1 || rel.Documents[0].ID != mention {
		t.Fatalf("documents = %+v, want only the one that mentions the file", rel.Documents)
	}

	docFile := addUploadedFile(t, db, "docs/"+owner.String()+"/y/image.png", "image.png", "image/png")
	rel, err = repo.Relations(ctx, project, docFile)
	if err != nil || rel.Task != nil || len(rel.Documents) != 1 || rel.Documents[0].ID != owner {
		t.Fatalf("document file relations = %+v, %v; want its owning document", rel, err)
	}
}
