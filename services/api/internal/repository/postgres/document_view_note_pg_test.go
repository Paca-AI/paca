package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// Notes (migration 000094) are documents whose view_id names a note view.

// noteView inserts a note view of project; sprint may be nil (backlog).
func noteView(t *testing.T, f *scopeFx, project uuid.UUID, sprint *uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ctx := "backlog"
	if sprint != nil {
		ctx = "sprint"
	}
	pgExec(t, f.db, `INSERT INTO sprint_views (id, sprint_id, project_id, name, view_type, view_context) VALUES ($1,$2,$3,'Notes','note',$4)`,
		id, sprint, project, ctx)
	return id
}

func newNote(t *testing.T, repo *DocumentRepository, project, view uuid.UUID, title string) *docdom.Document {
	t.Helper()
	now := time.Now().Truncate(time.Microsecond)
	d := &docdom.Document{ID: uuid.New(), ProjectID: project, ViewID: &view, Title: title, CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateDocument(context.Background(), d); err != nil {
		t.Fatalf("create note %q: %v", title, err)
	}
	return d
}

func docIDSet(docs []*docdom.Document) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, d := range docs {
		out[d.ID] = true
	}
	return out
}

func TestNotes_ListedByTheirViewOnlyAndNeverInTheTree(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)
	view, other := noteView(t, f, f.p, nil), noteView(t, f, f.p, nil)
	note := newNote(t, repo, f.p, view, "Plan")
	elsewhere := newNote(t, repo, f.p, other, "Plan")
	tree := uuid.New()
	pgExec(t, f.db, `INSERT INTO documents (id, project_id, title) VALUES ($1,$2,'Plan')`, tree, f.p)

	got, _, err := repo.ListDocuments(ctx, f.p, docdom.DocumentFilter{ViewID: &view}, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != note.ID || got[0].ViewID == nil || *got[0].ViewID != view {
		t.Fatalf("notes of the view = %v, want only %s with its view", docIDSet(got), note.ID)
	}

	for name, filter := range map[string]docdom.DocumentFilter{
		"anywhere in the tree": {},
		"tree root":            {RootOnly: true},
		"by title":             {Title: "plan"},
	} {
		got, _, err := repo.ListDocuments(ctx, f.p, filter, nil, 50)
		if err != nil {
			t.Fatal(err)
		}
		if ids := docIDSet(got); len(ids) != 1 || !ids[tree] {
			t.Errorf("%s: listed %v, want only the tree document %s", name, ids, tree)
		}
	}

	// A path names a tree document: the root "Plan" is the tree one, never a
	// note with the same title.
	res, err := repo.ResolveDocPath(ctx, f.p, nil, "Plan")
	if err != nil {
		t.Fatal(err)
	}
	if res.Doc == nil || res.Doc.ID != tree {
		t.Fatalf("path Plan resolved to %+v, want the tree document", res.Doc)
	}
	// Search covers the tree too: every title matches, only the tree
	// document is a hit.
	hits, err := repo.SearchDocuments(ctx, f.p, "plan", 10)
	if err != nil {
		t.Fatal(err)
	}
	if ids := docIDSet(hits); len(ids) != 1 || !ids[tree] {
		t.Fatalf("search found %v, want only the tree document %s", ids, tree)
	}

	pgExec(t, f.db, `UPDATE documents SET deleted_at = NOW() WHERE id = $1`, tree)
	if res, err = repo.ResolveDocPath(ctx, f.p, nil, "Plan"); err != nil || res.Doc != nil {
		t.Fatalf("path Plan with only notes left = %+v, %v; want no document", res, err)
	}

	// Read by id, a note is an ordinary document.
	byID, err := repo.FindDocumentByID(ctx, elsewhere.ID)
	if err != nil || byID.ViewID == nil || *byID.ViewID != other {
		t.Fatalf("find note by id = %+v, %v", byID, err)
	}
}

func TestNotes_AreNotInTheChangeFeed(t *testing.T) {
	f := newChangesFx(t)
	ctx := context.Background()
	tree := f.doc("Tree", nil)
	before := f.fingerprint(ctx, nil)
	from := f.head()

	view := noteView(t, f.scopeFx, f.p, nil)
	note := newNote(t, f.repo, f.p, view, "Note")

	if got := f.fingerprint(ctx, nil); got != before {
		t.Fatal("adding a note changed the visible fingerprint of the docs tree")
	}
	f.read(ctx, func(r docdom.ChangeReader) {
		changed, err := r.ListDocChanges(ctx, f.p, from)
		if err != nil {
			t.Fatal(err)
		}
		if len(changed) != 0 {
			t.Fatalf("feed since the note = %v, want nothing", docIDSet(changed))
		}
		all, err := r.ListVisibleDocs(ctx, f.p)
		if err != nil {
			t.Fatal(err)
		}
		if ids := docIDSet(all); len(ids) != 1 || !ids[tree] || ids[note.ID] {
			t.Fatalf("visible docs = %v, want only the tree document", ids)
		}
	})
}

func TestNotes_CreateChecksTheViewAndKeepsNotesOutOfFolders(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)
	otherProject := f.fx.project()
	foreign := noteView(t, f, otherProject, nil)

	now := time.Now()
	for name, viewID := range map[string]uuid.UUID{"another project's view": foreign, "missing view": uuid.New()} {
		v := viewID
		err := repo.CreateDocument(ctx, &docdom.Document{ID: uuid.New(), ProjectID: f.p, ViewID: &v, Title: "n", CreatedAt: now, UpdatedAt: now})
		if !errors.Is(err, docdom.ErrNoteViewInvalid) {
			t.Errorf("%s: err = %v, want ErrNoteViewInvalid", name, err)
		}
	}

	// The schema refuses a note in a folder, whatever writes it.
	view := noteView(t, f, f.p, nil)
	note := newNote(t, repo, f.p, view, "n")
	folder := uuid.New()
	pgExec(t, f.db, `INSERT INTO doc_folders (id, project_id, name) VALUES ($1,$2,'F')`, folder, f.p)
	if _, err := f.db.ExecContext(ctx, `UPDATE documents SET folder_id = $1 WHERE id = $2`, folder, note.ID); err == nil {
		t.Fatal("a note was put in a folder")
	}
}

func TestNotes_DeletedWithTheirView(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)
	views := NewViewRepository(f.db)

	view, kept := noteView(t, f, f.p, nil), noteView(t, f, f.p, nil)
	a, b := newNote(t, repo, f.p, view, "a"), newNote(t, repo, f.p, view, "b")
	survivor := newNote(t, repo, f.p, kept, "kept")
	// A note deleted earlier keeps the time of its deletion.
	if err := repo.DeleteDocument(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	var bDeleted time.Time
	if err := f.db.GetContext(ctx, &bDeleted, `SELECT deleted_at FROM documents WHERE id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}

	if err := views.DeleteView(ctx, view); err != nil {
		t.Fatalf("delete view: %v", err)
	}
	if _, err := repo.FindDocumentByID(ctx, a.ID); !errors.Is(err, docdom.ErrDocNotFound) {
		t.Fatalf("a note of the deleted view is still readable: %v", err)
	}
	var row struct {
		DeletedAt *time.Time `db:"deleted_at"`
		ViewID    *string    `db:"view_id"`
	}
	if err := f.db.GetContext(ctx, &row, `SELECT deleted_at, view_id FROM documents WHERE id = $1`, a.ID); err != nil {
		t.Fatalf("the note row is gone instead of soft-deleted: %v", err)
	}
	if row.DeletedAt == nil || row.ViewID != nil {
		t.Fatalf("note after its view's deletion = %+v, want soft-deleted with no view", row)
	}
	var bNow time.Time
	if err := f.db.GetContext(ctx, &bNow, `SELECT deleted_at FROM documents WHERE id = $1`, b.ID); err != nil {
		t.Fatal(err)
	}
	if !bNow.Equal(bDeleted) {
		t.Fatalf("an already deleted note was deleted again: %v, was %v", bNow, bDeleted)
	}
	if _, err := repo.FindDocumentByID(ctx, survivor.ID); err != nil {
		t.Fatalf("a note of another view went with it: %v", err)
	}
	// It stays out of the tree once its view is gone.
	got, _, err := repo.ListDocuments(ctx, f.p, docdom.DocumentFilter{}, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("tree after the view's deletion = %v, want empty", docIDSet(got))
	}
}

func TestNotes_DeletedWithTheirSprint(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)
	sprint := uuid.New()
	pgExec(t, f.db, `INSERT INTO sprints (id, project_id, name) VALUES ($1,$2,'S1')`, sprint, f.p)
	note := newNote(t, repo, f.p, noteView(t, f, f.p, &sprint), "Sprint goals")

	if err := NewSprintRepository(f.db).DeleteSprint(ctx, sprint); err != nil {
		t.Fatalf("delete sprint: %v", err)
	}
	var deleted bool
	if err := f.db.GetContext(ctx, &deleted, `SELECT deleted_at IS NOT NULL FROM documents WHERE id = $1`, note.ID); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("a note of a deleted sprint's view is still live")
	}
}

func TestNotes_DeletingAViewWithoutNotesLeavesTheDocCounterAlone(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	view := uuid.New()
	pgExec(t, f.db, `INSERT INTO sprint_views (id, project_id, name, view_type, view_context) VALUES ($1,$2,'Table','table','backlog')`, view, f.p)
	if err := NewViewRepository(f.db).DeleteView(ctx, view); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.db.GetContext(ctx, &n, `SELECT COUNT(*) FROM doc_sync_state WHERE project_id = $1`, f.p); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("deleting a view with no notes took the doc change counter")
	}
}

func TestNotes_GoWithAHardDeletedProject(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	project := f.fx.project()

	// The cascade reaches the view, whose trigger must leave the documents of
	// a project that is going away alone: written in the same transaction,
	// an update of them re-checks their foreign key to the deleted project.
	tx, err := f.db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	view, note := uuid.New(), uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO sprint_views (id, project_id, name, view_type, view_context) VALUES ($1,$2,'Notes','note','backlog')`, []any{view, project}},
		{`INSERT INTO documents (id, project_id, view_id, title) VALUES ($1,$2,$3,'n')`, []any{note, project, view}},
		{`DELETE FROM projects WHERE id = $1`, []any{project}},
	} {
		if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
			t.Fatalf("%s: %v", q.sql, err)
		}
	}
	var n int
	if err := tx.GetContext(ctx, &n, `SELECT COUNT(*) FROM documents WHERE id = $1`, note); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the note outlived its project")
	}
}
