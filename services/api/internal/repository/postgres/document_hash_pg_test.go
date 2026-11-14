package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

func TestCreateDocument_StoresContentHash(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)

	content := json.RawMessage(`[{"id":"a","type":"paragraph","content":[{"type":"text","text":"hi","styles":{}}]}]`)
	hash := docdom.ContentHash(content)
	now := time.Now()
	d := &docdom.Document{
		ID: uuid.New(), ProjectID: f.p, Title: "hashed", Content: content,
		ContentHash: &hash, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.CreateDocument(ctx, d); err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindDocumentByID(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentHash == nil || *got.ContentHash != hash {
		t.Fatalf("stored hash = %v, want %s", got.ContentHash, hash)
	}
	var raw *string
	if err := f.db.GetContext(ctx, &raw, `SELECT content_hash FROM documents WHERE id = $1`, d.ID.String()); err != nil {
		t.Fatal(err)
	}
	if raw == nil || *raw != hash {
		t.Fatalf("column content_hash = %v, want %s", raw, hash)
	}
	// The tree listing carries the hash too (it has no body to compute it from).
	docs, _, err := repo.ListDocuments(ctx, f.p, docdom.DocumentFilter{}, nil, 10)
	if err != nil || len(docs) != 1 || docs[0].ContentHash == nil || *docs[0].ContentHash != hash {
		t.Fatalf("list hash = %+v, err %v", docs, err)
	}
}

func TestUpdateDocumentAtomic_ErrNoChangeSkipsWrite(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)

	id := uuid.New()
	pgExec(t, f.db, `INSERT INTO documents (id, project_id, title, content, updated_at) VALUES ($1,$2,'t','[{"type":"paragraph"}]'::jsonb,'2026-01-01T00:00:00Z')`, id, f.p)
	var before time.Time
	if err := f.db.GetContext(ctx, &before, `SELECT updated_at FROM documents WHERE id = $1`, id.String()); err != nil {
		t.Fatal(err)
	}

	got, err := repo.UpdateDocumentAtomic(ctx, id, func(d *docdom.Document) error {
		// decide mutates the row, then says nothing changed: the mutation must not be written.
		d.Title = "must not be stored"
		d.UpdatedAt = time.Now()
		return docdom.ErrNoChange
	})
	if err != nil {
		t.Fatalf("ErrNoChange must come back as a nil error, got %v", err)
	}
	if got == nil || got.ID != id {
		t.Fatalf("returned doc = %+v, want the current row", got)
	}
	if got.Title != "t" || !got.UpdatedAt.Equal(before) {
		t.Fatalf("returned doc must be the stored row, got title %q updated_at %v", got.Title, got.UpdatedAt)
	}
	var title string
	var after time.Time
	if err := f.db.QueryRowContext(ctx, `SELECT title, updated_at FROM documents WHERE id = $1`, id.String()).Scan(&title, &after); err != nil {
		t.Fatal(err)
	}
	if title != "t" || !after.Equal(before) {
		t.Fatalf("row was written: title %q updated_at %v (was %v)", title, after, before)
	}

	// Other errors still abort and are returned as is.
	boom := errors.New("boom")
	if _, err := repo.UpdateDocumentAtomic(ctx, id, func(*docdom.Document) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func TestSnapshots_StoreAndFilterByContentHash(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)
	docID := uuid.New()
	pgExec(t, f.db, `INSERT INTO documents (id, project_id, title) VALUES ($1,$2,'d')`, docID, f.p)

	h1, h2 := docdom.ContentHash(json.RawMessage(`[{"type":"a"}]`)), docdom.ContentHash(json.RawMessage(`[{"type":"b"}]`))
	for i, h := range []*string{&h1, &h2, nil} {
		s := &docdom.DocSnapshot{
			ID: uuid.New(), DocumentID: docID, Title: "v", Content: json.RawMessage(`[{"type":"a"}]`),
			ContentHash: h, CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
		if err := repo.CreateSnapshot(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	all, _, err := repo.ListSnapshots(ctx, docID, nil, nil, 10)
	if err != nil || len(all) != 3 {
		t.Fatalf("unfiltered: %d snapshots, err %v", len(all), err)
	}
	got, _, err := repo.ListSnapshots(ctx, docID, &h2, nil, 10)
	if err != nil || len(got) != 1 || got[0].ContentHash == nil || *got[0].ContentHash != h2 {
		t.Fatalf("filter by h2 = %+v, err %v", got, err)
	}
	none := "feedface"
	got, _, err = repo.ListSnapshots(ctx, docID, &none, nil, 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("filter by unknown hash = %+v, err %v", got, err)
	}
}
