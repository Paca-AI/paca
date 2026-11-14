package docsvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
)

func TestUpsertDocumentByPath_CreateUpdateNoOp(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	p := uuid.New()

	var authorized []string
	authz := func(_ context.Context, folderID *uuid.UUID, existing *docdom.Document) error {
		if existing == nil {
			authorized = append(authorized, "create")
		} else {
			authorized = append(authorized, "change")
		}
		return nil
	}
	src := &docdom.DocSource{Repository: " paca ", Path: "docs/plan.md", Commit: "abc"}
	d, change, created, err := svc.UpsertDocumentByPath(ctx, docdom.UpsertByPathInput{
		ProjectID: p, Path: " Plans / 2026 //My plan ", Content: json.RawMessage(hashBlocksA), Source: src, Authorize: authz,
	})
	if err != nil || !created || !change.Content || d.Title != "My plan" || d.Source.Repository != "paca" {
		t.Fatalf("create: %+v %+v %v %v", d, change, created, err)
	}
	if fid, got, err := svc.ResolveDocPath(ctx, p, "plans/2026/my plan"); err != nil || got.ID != d.ID || fid == nil {
		t.Fatalf("resolve: %v %v %v", fid, got, err)
	}

	// Same content (new ids) and a new commit: only the source changes.
	next := &docdom.DocSource{Repository: "paca", Path: "docs/plan.md", Commit: "def"}
	d2, change, created, err := svc.UpsertDocumentByPath(ctx, docdom.UpsertByPathInput{
		ProjectID: p, Path: "Plans/2026/My plan", Content: json.RawMessage(hashBlocksANewIDs), Source: next, Authorize: authz,
	})
	if err != nil || created || change != (docdom.DocChange{Source: true}) || !d2.UpdatedAt.Equal(d.UpdatedAt) {
		t.Fatalf("same content: %+v %v %v", change, created, err)
	}
	if n := snapCount(t, repo, d.ID); n != 0 {
		t.Fatalf("%d snapshots after a source-only push", n)
	}

	// New content updates the same document and keeps the old version.
	_, change, created, err = svc.UpsertDocumentByPath(ctx, docdom.UpsertByPathInput{
		ProjectID: p, Path: "Plans/2026/My plan", Content: json.RawMessage(hashBlocksB), Source: next, Authorize: authz,
	})
	if err != nil || created || !change.Content {
		t.Fatalf("update: %+v %v %v", change, created, err)
	}
	if n := snapCount(t, repo, d.ID); n != 1 {
		t.Fatalf("%d snapshots, want 1", n)
	}
	if len(authorized) != 3 || authorized[0] != "create" || authorized[2] != "change" {
		t.Fatalf("authorized = %v", authorized)
	}
}

func TestUpsertDocumentByPath_Errors(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	p := uuid.New()
	folder := &docdom.DocFolder{ID: uuid.New(), ProjectID: p, Name: "Plans"}
	_ = repo.CreateFolder(ctx, folder)

	for _, path := range []string{"", "   ", "///", " / / "} {
		if _, _, _, err := svc.UpsertDocumentByPath(ctx, docdom.UpsertByPathInput{ProjectID: p, Path: path, Content: json.RawMessage(`[]`)}); !errors.Is(err, docdom.ErrDocPathInvalid) {
			t.Errorf("path %q: err = %v", path, err)
		}
	}
	if _, _, _, err := svc.UpsertDocumentByPath(ctx, docdom.UpsertByPathInput{ProjectID: p, Path: "plans/", Content: json.RawMessage(`[]`)}); !errors.Is(err, docdom.ErrDocPathIsFolder) {
		t.Errorf("folder path: err = %v", err)
	}
	if _, _, err := svc.ResolveDocPath(ctx, p, "Plans"); !errors.Is(err, docdom.ErrDocPathIsFolder) {
		t.Errorf("resolve folder: err = %v", err)
	}
	if _, _, err := svc.ResolveDocPath(ctx, p, "Nope/x"); !errors.Is(err, docdom.ErrDocNotFound) {
		t.Errorf("resolve missing: err = %v", err)
	}
	bad := &docdom.DocSource{Repository: "", Path: "x"}
	if _, _, _, err := svc.UpsertDocumentByPath(ctx, docdom.UpsertByPathInput{ProjectID: p, Path: "a", Content: json.RawMessage(`[]`), Source: bad}); !errors.Is(err, docdom.ErrDocSourceInvalid) {
		t.Errorf("bad source: err = %v", err)
	}
	lock := true
	if _, _, _, err := svc.UpsertDocumentByPath(ctx, docdom.UpsertByPathInput{ProjectID: p, Path: "a", Content: json.RawMessage(`[]`), Lock: &lock}); !errors.Is(err, docdom.ErrDocLockNeedsSource) {
		t.Errorf("lock without source: err = %v", err)
	}
	// If-Match on a path with no document fails the precondition.
	star := "*"
	if _, _, _, err := svc.UpsertDocumentByPath(ctx, docdom.UpsertByPathInput{ProjectID: p, Path: "Plans/new", Content: json.RawMessage(`[]`), IfMatch: &star}); !errors.Is(err, docdom.ErrVersionConflict) {
		t.Errorf("If-Match on missing doc: err = %v", err)
	}
	// A refusal from Authorize writes nothing.
	denied := errors.New("denied")
	if _, _, _, err := svc.UpsertDocumentByPath(ctx, docdom.UpsertByPathInput{ProjectID: p, Path: "New/doc", Content: json.RawMessage(`[]`),
		Authorize: func(context.Context, *uuid.UUID, *docdom.Document) error { return denied }}); !errors.Is(err, denied) {
		t.Errorf("denied: err = %v", err)
	}
	if res, _ := repo.ResolveDocPath(ctx, p, []string{"New"}, "doc"); res.Depth != 0 {
		t.Errorf("a denied write created a folder")
	}
}

// Authorize sees the deepest existing folder, so a push that creates folders
// is checked against where they will be created.
func TestUpsertDocumentByPath_AuthorizesDeepestExistingFolder(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	p := uuid.New()
	folder := &docdom.DocFolder{ID: uuid.New(), ProjectID: p, Name: "Plans"}
	_ = repo.CreateFolder(ctx, folder)

	var seen *uuid.UUID
	_, _, created, err := svc.UpsertDocumentByPath(ctx, docdom.UpsertByPathInput{ProjectID: p, Path: "Plans/A/B/doc", Content: json.RawMessage(`[]`),
		Authorize: func(_ context.Context, f *uuid.UUID, _ *docdom.Document) error { seen = f; return nil }})
	if err != nil || !created || seen == nil || *seen != folder.ID {
		t.Fatalf("seen %v created %v err %v", seen, created, err)
	}
}
