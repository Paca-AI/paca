package docsvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
)

const hashBlocksA = `[{"id":"a1","type":"paragraph","content":[{"type":"text","text":"hello","styles":{}}]}]`
const hashBlocksANewIDs = `[{"type":"paragraph","id":"zz","content":[{"styles":{},"type":"text","text":"hello"}]}]`
const hashBlocksB = `[{"id":"b1","type":"paragraph","content":[{"type":"text","text":"changed","styles":{}}]}]`

func raw(s string) *json.RawMessage { r := json.RawMessage(s); return &r }

// seedDoc stores a doc straight into the fake (bypassing CreateDocument), with
// an UpdatedAt in the past so a write that bumps it is visible.
func seedDoc(repo *fakeDocRepo, projectID uuid.UUID, content string, storeHash bool) *docdom.Document {
	by := uuid.New()
	d := &docdom.Document{
		ID: uuid.New(), ProjectID: projectID, Title: "Doc", Content: json.RawMessage(content),
		UpdatedBy: &by, CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour).Truncate(time.Second),
	}
	if storeHash {
		h := docdom.ContentHash(d.Content)
		d.ContentHash = &h
	}
	_ = repo.CreateDocument(context.Background(), d)
	return d
}

func snapCount(t *testing.T, repo *fakeDocRepo, docID uuid.UUID) int {
	t.Helper()
	snaps, _, err := repo.ListSnapshots(context.Background(), docID, nil, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	return len(snaps)
}

func TestUpdateDocument_SameContentNewIDsIsNoOp(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedDoc(repo, projectID, hashBlocksA, true)

	title, pos := d.Title, d.Position
	var sameFolder *uuid.UUID
	got, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{
		Content: raw(hashBlocksANewIDs), Title: &title, Position: &pos, FolderID: &sameFolder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if change != (docdom.DocChange{}) || change.Any() {
		t.Fatalf("change = %+v, want none", change)
	}
	if !got.UpdatedAt.Equal(d.UpdatedAt) {
		t.Fatalf("UpdatedAt moved: %v -> %v", d.UpdatedAt, got.UpdatedAt)
	}
	stored, _ := repo.FindDocumentByID(ctx, d.ID)
	if !stored.UpdatedAt.Equal(d.UpdatedAt) || string(stored.Content) != hashBlocksA {
		t.Fatalf("stored row was rewritten: %+v", stored)
	}
	if n := snapCount(t, repo, d.ID); n != 0 {
		t.Fatalf("%d snapshots, want 0", n)
	}
}

func TestUpdateDocument_TitleOnlyStillSnapshots(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedDoc(repo, projectID, hashBlocksA, true)

	title := "Renamed"
	got, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Title: &title, Content: raw(hashBlocksANewIDs)})
	if err != nil {
		t.Fatal(err)
	}
	if change != (docdom.DocChange{Title: true}) {
		t.Fatalf("change = %+v, want title only", change)
	}
	if got.Title != "Renamed" || !got.UpdatedAt.After(d.UpdatedAt) {
		t.Fatalf("title write not applied: %+v", got)
	}
	if n := snapCount(t, repo, d.ID); n != 1 {
		t.Fatalf("%d snapshots, want 1", n)
	}
}

func TestUpdateDocument_IfMatchMismatch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedDoc(repo, projectID, hashBlocksA, true)

	stale := docdom.ContentHash(json.RawMessage(hashBlocksB))
	title := "New"
	_, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{
		Content: raw(hashBlocksB), Title: &title, IfMatch: &stale,
	})
	if !errors.Is(err, docdom.ErrVersionConflict) {
		t.Fatalf("err = %v, want ErrVersionConflict", err)
	}
	var vc *docdom.VersionConflictError
	if !errors.As(err, &vc) {
		t.Fatalf("err %T is not a *VersionConflictError", err)
	}
	if vc.CurrentHash != *d.ContentHash || !vc.UpdatedAt.Equal(d.UpdatedAt) || vc.UpdatedBy == nil || *vc.UpdatedBy != *d.UpdatedBy {
		t.Fatalf("conflict = %+v, want current hash %s, %v, %v", vc, *d.ContentHash, d.UpdatedAt, *d.UpdatedBy)
	}
	if change.Any() {
		t.Fatalf("change = %+v on conflict", change)
	}
	stored, _ := repo.FindDocumentByID(ctx, d.ID)
	if stored.Title != "Doc" || string(stored.Content) != hashBlocksA || !stored.UpdatedAt.Equal(d.UpdatedAt) {
		t.Fatalf("row touched: %+v", stored)
	}
	if n := snapCount(t, repo, d.ID); n != 0 {
		t.Fatalf("%d snapshots, want 0", n)
	}
}

func TestUpdateDocument_SnapshotRecordsOldHash(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedDoc(repo, projectID, hashBlocksA, true)

	got, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksB)})
	if err != nil {
		t.Fatal(err)
	}
	if !change.Content || change.Title {
		t.Fatalf("change = %+v, want content only", change)
	}
	if got.ContentHash == nil || *got.ContentHash != docdom.ContentHash(json.RawMessage(hashBlocksB)) {
		t.Fatalf("new hash = %v", got.ContentHash)
	}
	snaps, _, _ := repo.ListSnapshots(ctx, d.ID, nil, nil, 100)
	if len(snaps) != 1 || snaps[0].ContentHash == nil || *snaps[0].ContentHash != *d.ContentHash {
		t.Fatalf("snapshot must record the old hash %s, got %+v", *d.ContentHash, snaps)
	}
	// And it can be found by that hash.
	byHash, _, err := svc.ListSnapshots(ctx, projectID, d.ID, d.ContentHash, nil, 10)
	if err != nil || len(byHash) != 1 {
		t.Fatalf("snapshots by old hash = %d, err %v", len(byHash), err)
	}
}

func TestUpdateDocument_IfMatchMatchesAndStar(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedDoc(repo, projectID, hashBlocksA, true)

	current := *d.ContentHash
	got, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksB), IfMatch: &current})
	if err != nil || !change.Content {
		t.Fatalf("matching If-Match: change %+v err %v", change, err)
	}
	star := "*"
	title := "Starred"
	if _, change, err = svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Title: &title, IfMatch: &star}); err != nil || !change.Title {
		t.Fatalf("If-Match *: change %+v err %v", change, err)
	}
	// The previous hash is stale now.
	if _, _, err = svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksA), IfMatch: &current}); !errors.Is(err, docdom.ErrVersionConflict) {
		t.Fatalf("old hash after a write: err %v, want conflict", err)
	}
	// A matching If-Match on a no-op write is still a no-op, not an error.
	h := *got.ContentHash
	if _, change, err = svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksB), IfMatch: &h}); err != nil || change.Any() {
		t.Fatalf("matching no-op: change %+v err %v", change, err)
	}
	// "*" on a missing doc is not found.
	if _, _, err = svc.UpdateDocument(ctx, projectID, uuid.New(), docdom.UpdateDocumentInput{Title: &title, IfMatch: &star}); !errors.Is(err, docdom.ErrDocNotFound) {
		t.Fatalf("If-Match * on a missing doc: err %v", err)
	}
}

func TestUpdateDocument_IfMatchOnNullHashRow(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedDoc(repo, projectID, hashBlocksA, false) // written before the column existed
	if d.ContentHash != nil {
		t.Fatal("seed must have no stored hash")
	}

	computed := docdom.ContentHash(json.RawMessage(hashBlocksA))
	// A no-op write leaves the NULL row alone (no UPDATE just to store a hash).
	if _, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksANewIDs), IfMatch: &computed}); err != nil || change.Any() {
		t.Fatalf("no-op on NULL row: change %+v err %v", change, err)
	}
	if stored, _ := repo.FindDocumentByID(ctx, d.ID); stored.ContentHash != nil {
		t.Fatal("a no-op must not write the hash")
	}
	// A real write (title only) matching the computed hash passes and stores it.
	title := "Now hashed"
	if _, change, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Title: &title, IfMatch: &computed}); err != nil || !change.Title {
		t.Fatalf("write on NULL row: change %+v err %v", change, err)
	}
	stored, _ := repo.FindDocumentByID(ctx, d.ID)
	if stored.ContentHash == nil || *stored.ContentHash != computed {
		t.Fatalf("stored hash = %v, want %s", stored.ContentHash, computed)
	}
}

func TestUpdateDocument_IfMatchCheckedUnderLock(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	d := seedDoc(repo, projectID, hashBlocksA, true)

	stale := "0000"
	_, _, err := svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksB), IfMatch: &stale})
	if !errors.Is(err, docdom.ErrVersionConflict) {
		t.Fatalf("err = %v, want conflict", err)
	}
	if n := repo.decideCalls.Load(); n != 1 {
		t.Fatalf("decide ran %d times, want 1: the check must happen inside the locked step", n)
	}
	if err := lastDecideErr(repo); !errors.Is(err, docdom.ErrVersionConflict) {
		t.Fatalf("decide returned %v; the If-Match comparison must be made inside decide", err)
	}

	// Same for the no-op decision.
	repo.decideCalls.Store(0)
	_, _, err = svc.UpdateDocument(ctx, projectID, d.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksANewIDs)})
	if err != nil || repo.decideCalls.Load() != 1 || !errors.Is(lastDecideErr(repo), docdom.ErrNoChange) {
		t.Fatalf("no-op: err %v calls %d decide err %v", err, repo.decideCalls.Load(), lastDecideErr(repo))
	}
}

func lastDecideErr(repo *fakeDocRepo) error {
	repo.mu.RLock()
	defer repo.mu.RUnlock()
	return repo.lastDecideErr
}

// Timestamps a write returns are at the precision Postgres stores, so the
// response of a write and the next read of the row agree.
func TestDocumentWrites_TimestampsAtStoredPrecision(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()

	created, err := svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, Title: "Doc", Content: json.RawMessage(hashBlocksA)})
	if err != nil {
		t.Fatal(err)
	}
	updated, _, err := svc.UpdateDocument(ctx, projectID, created.ID, docdom.UpdateDocumentInput{Content: raw(hashBlocksB)})
	if err != nil {
		t.Fatal(err)
	}
	for name, ts := range map[string]time.Time{"created_at": created.CreatedAt, "create updated_at": created.UpdatedAt, "update updated_at": updated.UpdatedAt} {
		if !ts.Equal(ts.Truncate(time.Microsecond)) {
			t.Errorf("%s = %v has sub-microsecond digits Postgres would drop", name, ts)
		}
	}
}
