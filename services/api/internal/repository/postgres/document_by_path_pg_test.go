package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

func pathFolder() *docdom.DocFolder {
	now := time.Now()
	return &docdom.DocFolder{ID: uuid.New(), CreatedAt: now, UpdatedAt: now}
}

func pathDoc() *docdom.Document {
	now := time.Now()
	return &docdom.Document{ID: uuid.New(), Content: json.RawMessage(`[]`), CreatedAt: now, UpdatedAt: now}
}

func TestResolveDocPath(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)

	plans := &docdom.DocFolder{ID: uuid.New(), ProjectID: f.p, Name: "Plans"}
	sub := &docdom.DocFolder{ID: uuid.New(), ProjectID: f.p, ParentID: &plans.ID, Name: "2026"}
	for _, fo := range []*docdom.DocFolder{plans, sub} {
		if err := repo.CreateFolder(ctx, fo); err != nil {
			t.Fatal(err)
		}
	}
	doc := &docdom.Document{ID: uuid.New(), ProjectID: f.p, FolderID: &sub.ID, Title: "My Plan"}
	root := &docdom.Document{ID: uuid.New(), ProjectID: f.p, Title: "Readme"}
	for _, d := range []*docdom.Document{doc, root} {
		if err := repo.CreateDocument(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	res, err := repo.ResolveDocPath(ctx, f.p, []string{"plans", "2026"}, "my plan")
	if err != nil || res.Depth != 2 || *res.FolderID != sub.ID || res.Doc == nil || res.Doc.ID != doc.ID {
		t.Fatalf("full path: %+v err %v", res, err)
	}
	res, err = repo.ResolveDocPath(ctx, f.p, nil, "README")
	if err != nil || res.Depth != 0 || res.FolderID != nil || res.Doc == nil || res.Doc.ID != root.ID {
		t.Fatalf("root doc: %+v err %v", res, err)
	}
	res, err = repo.ResolveDocPath(ctx, f.p, []string{"Plans", "2027", "x"}, "t")
	if err != nil || res.Depth != 1 || *res.FolderID != plans.ID || res.Doc != nil {
		t.Fatalf("missing folder: %+v err %v", res, err)
	}
	res, err = repo.ResolveDocPath(ctx, f.p, []string{"Plans"}, "2026")
	if err != nil || !res.IsFolder || res.Doc != nil {
		t.Fatalf("names a folder: %+v err %v", res, err)
	}
	res, err = repo.ResolveDocPath(ctx, f.p, []string{"Plans", "2026"}, "other")
	if err != nil || res.Depth != 2 || res.Doc != nil || res.IsFolder {
		t.Fatalf("no doc yet: %+v err %v", res, err)
	}
	// Another project's folders never match.
	other := newScopeFx(t)
	res, err = repo.ResolveDocPath(ctx, other.p, []string{"Plans"}, "x")
	if err != nil || res.Depth != 0 || res.FolderID != nil {
		t.Fatalf("other project: %+v err %v", res, err)
	}
}

func TestWriteDocumentAtPath_CreatesFoldersThenUpdates(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)

	w := docdom.DocPathWrite{
		ProjectID: f.p, Folders: []string{"A", "B"}, Title: "Doc",
		Expect:    docdom.DocPathResolution{},
		NewFolder: pathFolder, NewDoc: pathDoc,
		Decide: func(*docdom.Document) error { t.Fatal("decide on create"); return nil },
	}
	out, err := repo.WriteDocumentAtPath(ctx, w)
	if err != nil || !out.Created || len(out.Folders) != 2 {
		t.Fatalf("create: %+v err %v", out, err)
	}
	if out.Folders[1].ParentID == nil || *out.Folders[1].ParentID != out.Folders[0].ID || *out.Doc.FolderID != out.Folders[1].ID {
		t.Fatalf("folders not chained: %+v", out)
	}

	// The same write again no longer matches what it expected.
	if _, err := repo.WriteDocumentAtPath(ctx, w); !errors.Is(err, docdom.ErrDocPathChanged) {
		t.Fatalf("stale expectation: err = %v", err)
	}

	res, _ := repo.ResolveDocPath(ctx, f.p, w.Folders, w.Title)
	w.Expect = *res
	w.Decide = func(d *docdom.Document) error { d.Title = "Doc"; d.Position = 7; return nil }
	out, err = repo.WriteDocumentAtPath(ctx, w)
	if err != nil || out.Created || out.Doc.Position != 7 || len(out.Folders) != 0 {
		t.Fatalf("update: %+v err %v", out, err)
	}
}

// Two first pushes of one path at once: one document, no duplicate folders.
func TestWriteDocumentAtPath_ConcurrentCreatesOneDoc(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each writer resolves first, as the service does, then retries
			// when the path changed under it.
			for range 5 {
				res, err := repo.ResolveDocPath(ctx, f.p, []string{"Plans", "Deep"}, "Same")
				if err != nil {
					errs[i] = err
					return
				}
				_, err = repo.WriteDocumentAtPath(ctx, docdom.DocPathWrite{
					ProjectID: f.p, Folders: []string{"Plans", "Deep"}, Title: "Same", Expect: *res,
					NewFolder: pathFolder, NewDoc: pathDoc,
					Decide: func(*docdom.Document) error { return docdom.ErrNoChange },
				})
				if !errors.Is(err, docdom.ErrDocPathChanged) {
					errs[i] = err
					return
				}
			}
			errs[i] = errors.New("never settled")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}
	var docs, folders int
	if err := f.db.GetContext(ctx, &docs, `SELECT count(*) FROM documents WHERE project_id = $1 AND deleted_at IS NULL`, f.p); err != nil {
		t.Fatal(err)
	}
	if err := f.db.GetContext(ctx, &folders, `SELECT count(*) FROM doc_folders WHERE project_id = $1`, f.p); err != nil {
		t.Fatal(err)
	}
	if docs != 1 || folders != 2 {
		t.Fatalf("%d documents and %d folders, want 1 and 2", docs, folders)
	}
}

// A document deleted by id (which takes no folder lock) after the write
// resolved it, but before the write locked its row, means the path changed:
// the service resolves again and creates the document. It is not a 404.
func TestWriteDocumentAtPath_DocDeletedBeforeRowLockIsPathChanged(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)

	d := pathDoc()
	d.ProjectID, d.Title = f.p, "Gone"
	if err := repo.CreateDocument(ctx, d); err != nil {
		t.Fatal(err)
	}
	expect, err := repo.ResolveDocPath(ctx, f.p, nil, "Gone")
	if err != nil || expect.Doc == nil {
		t.Fatalf("resolve: %+v %v", expect, err)
	}

	// Another transaction deletes the row and holds its lock until we
	// commit it, so the write resolves the document (the delete is not yet
	// visible) and then waits on the row in FOR UPDATE.
	del, err := f.db.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = del.Rollback() }()
	if _, err := del.ExecContext(ctx, `UPDATE documents SET deleted_at = now() WHERE id = $1`, d.ID.String()); err != nil {
		t.Fatal(err)
	}

	errc := make(chan error, 1)
	go func() {
		_, err := repo.WriteDocumentAtPath(ctx, docdom.DocPathWrite{
			ProjectID: f.p, Title: "Gone", Expect: *expect,
			NewFolder: pathFolder, NewDoc: pathDoc,
			Decide: func(*docdom.Document) error { return nil },
		})
		errc <- err
	}()

	// Wait until the write is blocked on the row lock.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := f.db.GetContext(ctx, &waiting, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%FOR UPDATE%'`); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("write never waited on the row lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := del.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errc:
		if !errors.Is(err, docdom.ErrDocPathChanged) {
			t.Fatalf("err = %v, want ErrDocPathChanged", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("write did not finish")
	}
}
