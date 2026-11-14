package docsvc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
)

// seedLocked stores a managed copy of repository "paca", locked.
func seedLocked(t *testing.T, repo *fakeDocRepo, projectID uuid.UUID) *docdom.Document {
	t.Helper()
	d := seedDoc(repo, projectID, hashBlocksA, true)
	stored, _ := repo.FindDocumentByID(context.Background(), d.ID)
	stored.Source = &docdom.DocSource{Repository: "paca", Path: "docs/plan.md", Commit: "abc"}
	stored.SourceLocked = true
	_ = repo.CreateDocument(context.Background(), stored)
	return stored
}

func TestUpdateDocument_LockedRejectsPlainWrite(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedLocked(t, repo, projectID)

	if _, _, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksB)}); !errors.Is(err, docdom.ErrDocLocked) {
		t.Fatalf("content write: err = %v, want ErrDocLocked", err)
	}
	title := "Renamed"
	if _, _, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Title: &title}); !errors.Is(err, docdom.ErrDocLocked) {
		t.Fatalf("title write: err = %v, want ErrDocLocked", err)
	}
	stored, _ := repo.FindDocumentByID(ctx, d.ID)
	if string(stored.Content) != hashBlocksA || stored.Title != d.Title {
		t.Fatalf("locked doc was changed: %+v", stored)
	}
	// Saving the content it already has (the web editor's autosave) is a
	// no-op, not a refusal.
	if _, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksANewIDs)}); err != nil || change.Any() {
		t.Fatalf("same-content write: change %+v err %v", change, err)
	}
}

func TestUpdateDocument_LockedAllowsSameRepositorySource(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedLocked(t, repo, projectID)

	src := &docdom.DocSource{Repository: "paca", Path: "docs/plan.md", Commit: "def"}
	got, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksB), Source: src})
	if err != nil || !change.Content || !change.Source {
		t.Fatalf("change %+v err %v", change, err)
	}
	if !got.SourceLocked || !docdom.SameSource(got.Source, src) {
		t.Fatalf("got %+v", got)
	}
}

func TestUpdateDocument_LockedRejectsOtherRepository(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedLocked(t, repo, projectID)

	src := &docdom.DocSource{Repository: "other", Path: "docs/plan.md"}
	if _, _, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksB), Source: src}); !errors.Is(err, docdom.ErrDocLocked) {
		t.Fatalf("err = %v, want ErrDocLocked", err)
	}
	stored, _ := repo.FindDocumentByID(ctx, d.ID)
	if stored.Source.Repository != "paca" || string(stored.Content) != hashBlocksA {
		t.Fatalf("stored = %+v", stored)
	}
}

func TestUpdateDocument_LockedAllowsMove(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedLocked(t, repo, projectID)
	folder := &docdom.DocFolder{ID: uuid.New(), ProjectID: projectID, Name: "F"}
	_ = repo.CreateFolder(ctx, folder)

	to := &folder.ID
	pos := 3
	got, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{FolderID: &to, Position: &pos})
	if err != nil || !change.Folder || !change.Position {
		t.Fatalf("change %+v err %v", change, err)
	}
	if got.FolderID == nil || *got.FolderID != folder.ID || !got.SourceLocked {
		t.Fatalf("got %+v", got)
	}
}

func TestUpdateDocument_UnlockThenWrite(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedLocked(t, repo, projectID)

	unlock := false
	got, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Lock: &unlock})
	if err != nil || change != (docdom.DocChange{Lock: true}) {
		t.Fatalf("unlock: change %+v err %v", change, err)
	}
	if got.SourceLocked || !got.UpdatedAt.Equal(d.UpdatedAt) || got.Source == nil {
		t.Fatalf("unlock: got %+v", got)
	}
	if n := snapCount(t, repo, d.ID); n != 0 {
		t.Fatalf("unlock took %d snapshots", n)
	}
	if _, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksB)}); err != nil || !change.Content {
		t.Fatalf("write after unlock: change %+v err %v", change, err)
	}

	// Locking again needs a source: this doc has one.
	lock := true
	if _, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Lock: &lock}); err != nil || !change.Lock {
		t.Fatalf("relock: change %+v err %v", change, err)
	}
}

func TestUpdateDocument_LockNeedsSource(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedDoc(repo, projectID, hashBlocksA, true)

	lock := true
	if _, _, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Lock: &lock}); !errors.Is(err, docdom.ErrDocLockNeedsSource) {
		t.Fatalf("err = %v, want ErrDocLockNeedsSource", err)
	}
	// With a source in the same write it can be locked.
	src := &docdom.DocSource{Repository: "paca", Path: "x.md"}
	got, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Lock: &lock, Source: src})
	if err != nil || !change.Lock || !change.Source || !got.SourceLocked {
		t.Fatalf("change %+v err %v got %+v", change, err, got)
	}
}
