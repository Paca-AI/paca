package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

func TestDocumentSource_StoredReadAndListed(t *testing.T) {
	f := newScopeFx(t)
	ctx := context.Background()
	repo := NewDocumentRepository(f.db)

	now := time.Now()
	src := &docdom.DocSource{Repository: "paca", Path: "docs/plan.md", Commit: "abc123"}
	d := &docdom.Document{
		ID: uuid.New(), ProjectID: f.p, Title: "managed", Content: json.RawMessage(`[]`),
		Source: src, SourceLocked: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.CreateDocument(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindDocumentByID(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !docdom.SameSource(got.Source, src) || !got.SourceLocked {
		t.Fatalf("read back source %+v locked %v", got.Source, got.SourceLocked)
	}
	docs, _, err := repo.ListDocuments(ctx, f.p, docdom.DocumentFilter{}, nil, 10)
	if err != nil || len(docs) != 1 || !docdom.SameSource(docs[0].Source, src) || !docs[0].SourceLocked {
		t.Fatalf("listed %+v err %v", docs, err)
	}

	// An update writes the source and the lock; NULL source reads as nil.
	next := &docdom.DocSource{Repository: "paca", Path: "docs/plan.md"}
	if _, err := repo.UpdateDocumentAtomic(ctx, d.ID, func(cur *docdom.Document) error {
		cur.Source, cur.SourceLocked = next, false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.FindDocumentByID(ctx, d.ID)
	if !docdom.SameSource(got.Source, next) || got.SourceLocked {
		t.Fatalf("after update source %+v locked %v", got.Source, got.SourceLocked)
	}
	var commitKey bool
	if err := f.db.GetContext(ctx, &commitKey, `SELECT source ? 'commit' FROM documents WHERE id = $1`, d.ID.String()); err != nil {
		t.Fatal(err)
	}
	if commitKey {
		t.Fatal("an empty commit must be left out of the stored source")
	}

	plain := &docdom.Document{ID: uuid.New(), ProjectID: f.p, Title: "plain", CreatedAt: now, UpdatedAt: now}
	if err := repo.CreateDocument(ctx, plain); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.FindDocumentByID(ctx, plain.ID); got.Source != nil || got.SourceLocked {
		t.Fatalf("plain doc source %+v locked %v", got.Source, got.SourceLocked)
	}
}
