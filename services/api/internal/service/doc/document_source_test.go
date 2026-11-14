package docsvc_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
)

// A push of the same content from a new commit records the new source and
// nothing else: no snapshot, updated_at and updated_by unchanged.
func TestUpdateDocument_SourceOnlyKeepsUpdatedAt(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedDoc(repo, projectID, hashBlocksA, true)

	first := &docdom.DocSource{Repository: "paca", Path: "docs/plan.md", Commit: "abc"}
	got, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{
		Content: raw(hashBlocksANewIDs), Source: first,
	})
	if err != nil {
		t.Fatal(err)
	}
	if change != (docdom.DocChange{Source: true}) || change.Edited() {
		t.Fatalf("change = %+v, want source only", change)
	}
	if !got.UpdatedAt.Equal(d.UpdatedAt) || got.UpdatedBy == nil || *got.UpdatedBy != *d.UpdatedBy {
		t.Fatalf("updated_at/by moved: %v %v", got.UpdatedAt, got.UpdatedBy)
	}
	stored, _ := repo.FindDocumentByID(ctx, d.ID)
	if !docdom.SameSource(stored.Source, first) || string(stored.Content) != hashBlocksA {
		t.Fatalf("stored = %+v", stored)
	}
	if n := snapCount(t, repo, d.ID); n != 0 {
		t.Fatalf("%d snapshots, want 0", n)
	}

	// The same source again changes nothing.
	same := *first
	_, change, err = svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksA), Source: &same})
	if err != nil || change.Any() {
		t.Fatalf("repeat: change %+v err %v", change, err)
	}

	// A new commit with new content is a normal edit that keeps the new source.
	next := &docdom.DocSource{Repository: "paca", Path: "docs/plan.md", Commit: "def"}
	got, change, err = svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksB), Source: next})
	if err != nil || !change.Content || !change.Source {
		t.Fatalf("edit: change %+v err %v", change, err)
	}
	if got.UpdatedAt.Equal(d.UpdatedAt) || !docdom.SameSource(got.Source, next) {
		t.Fatalf("edit: %+v", got)
	}
}
