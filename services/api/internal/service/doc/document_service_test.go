// Package docsvc_test contains unit tests for the doc service layer.
// Tests use in-memory fake repositories and do not require any infrastructure.
package docsvc_test

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	docsvc "github.com/Paca-AI/api/internal/service/doc"
)

// ---------------------------------------------------------------------------
// Fake repository
// ---------------------------------------------------------------------------

type fakeDocRepo struct {
	mu         sync.RWMutex
	folders    map[uuid.UUID]*docdom.DocFolder
	docs       map[uuid.UUID]*docdom.Document
	snapshots  map[uuid.UUID]*docdom.DocSnapshot
	activities map[uuid.UUID]*docdom.Activity
}

func newFakeDocRepo() *fakeDocRepo {
	return &fakeDocRepo{
		folders:    make(map[uuid.UUID]*docdom.DocFolder),
		docs:       make(map[uuid.UUID]*docdom.Document),
		snapshots:  make(map[uuid.UUID]*docdom.DocSnapshot),
		activities: make(map[uuid.UUID]*docdom.Activity),
	}
}

// -- DocFolderRepository --

func (r *fakeDocRepo) ListFolders(_ context.Context, projectID uuid.UUID) ([]*docdom.DocFolder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*docdom.DocFolder
	for _, f := range r.folders {
		if f.ProjectID == projectID {
			cp := *f
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeDocRepo) FindFolderByID(_ context.Context, id uuid.UUID) (*docdom.DocFolder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.folders[id]
	if !ok {
		return nil, docdom.ErrFolderNotFound
	}
	cp := *f
	return &cp, nil
}

func (r *fakeDocRepo) CreateFolder(_ context.Context, f *docdom.DocFolder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *f
	r.folders[f.ID] = &cp
	return nil
}

func (r *fakeDocRepo) UpdateFolder(_ context.Context, f *docdom.DocFolder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.folders[f.ID]; !ok {
		return docdom.ErrFolderNotFound
	}
	cp := *f
	r.folders[f.ID] = &cp
	return nil
}

func (r *fakeDocRepo) DeleteFolder(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.folders, id)
	return nil
}

// -- DocumentRepository --

func (r *fakeDocRepo) ListDocuments(_ context.Context, projectID uuid.UUID, folderID *uuid.UUID, search *string, cursor *string, limit *int) ([]*docdom.Document, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*docdom.Document
	for _, d := range r.docs {
		if d.ProjectID != projectID || d.DeletedAt != nil {
			continue
		}
		if folderID != nil {
			if d.FolderID == nil || *d.FolderID != *folderID {
				continue
			}
		}
		if search != nil && *search != "" {
			if !strings.Contains(strings.ToLower(d.Title), strings.ToLower(*search)) {
				continue
			}
		}
		cp := *d
		out = append(out, &cp)
	}
	// Mirrors the real repository's pagination-mode ordering (title ASC, id
	// ASC) — the map above has no inherent order, so this is required for
	// the cursor test below to be meaningful, not just cosmetic.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Title != out[j].Title {
			return out[i].Title < out[j].Title
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	if limit == nil {
		return out, false, nil
	}
	if cursor != nil {
		if cur, ok := docdom.DecodeDocumentCursor(*cursor); ok {
			i := 0
			for i < len(out) && !(out[i].Title > cur.Title || (out[i].Title == cur.Title && out[i].ID.String() > cur.ID)) {
				i++
			}
			out = out[i:]
		}
	}
	hasMore := len(out) > *limit
	if hasMore {
		out = out[:*limit]
	}
	return out, hasMore, nil
}

func (r *fakeDocRepo) FindDocumentByID(_ context.Context, id uuid.UUID) (*docdom.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.docs[id]
	if !ok || d.DeletedAt != nil {
		return nil, docdom.ErrDocNotFound
	}
	cp := *d
	return &cp, nil
}

func (r *fakeDocRepo) CreateDocument(_ context.Context, d *docdom.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *d
	r.docs[d.ID] = &cp
	return nil
}

func (r *fakeDocRepo) UpdateDocument(_ context.Context, d *docdom.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.docs[d.ID]; !ok {
		return docdom.ErrDocNotFound
	}
	cp := *d
	r.docs[d.ID] = &cp
	return nil
}

func (r *fakeDocRepo) DeleteDocument(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.docs[id]
	if !ok || d.DeletedAt != nil {
		return docdom.ErrDocNotFound
	}
	now := time.Now()
	d.DeletedAt = &now
	return nil
}

// -- DocSnapshotRepository --

func (r *fakeDocRepo) ListSnapshots(_ context.Context, documentID uuid.UUID) ([]*docdom.DocSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*docdom.DocSnapshot
	for _, s := range r.snapshots {
		if s.DocumentID == documentID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeDocRepo) FindSnapshotByID(_ context.Context, id uuid.UUID) (*docdom.DocSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.snapshots[id]
	if !ok {
		return nil, docdom.ErrSnapshotNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *fakeDocRepo) FindLatestSnapshot(_ context.Context, documentID uuid.UUID) (*docdom.DocSnapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest *docdom.DocSnapshot
	for _, s := range r.snapshots {
		if s.DocumentID != documentID {
			continue
		}
		if latest == nil || s.SnapshotNumber > latest.SnapshotNumber {
			cp := *s
			latest = &cp
		}
	}
	return latest, nil
}

func (r *fakeDocRepo) CreateSnapshot(_ context.Context, s *docdom.DocSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Auto-assign snapshot number per document.
	maxNum := int64(0)
	for _, existing := range r.snapshots {
		if existing.DocumentID == s.DocumentID && existing.SnapshotNumber > maxNum {
			maxNum = existing.SnapshotNumber
		}
	}
	s.SnapshotNumber = maxNum + 1
	cp := *s
	r.snapshots[s.ID] = &cp
	return nil
}

func (r *fakeDocRepo) DeleteRecentSnapshotsExcept(_ context.Context, documentID uuid.UUID, excludeID uuid.UUID, since time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, s := range r.snapshots {
		if s.DocumentID == documentID && s.ID != excludeID && !s.CreatedAt.Before(since) {
			delete(r.snapshots, id)
		}
	}
	return nil
}

// -- ActivityRepository --

func (r *fakeDocRepo) ListActivities(_ context.Context, documentID uuid.UUID) ([]*docdom.Activity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*docdom.Activity
	for _, a := range r.activities {
		if a.DocumentID == documentID && a.DeletedAt == nil {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeDocRepo) FindActivityByID(_ context.Context, id uuid.UUID) (*docdom.Activity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.activities[id]
	if !ok {
		return nil, docdom.ErrActivityNotFound
	}
	cp := *a
	return &cp, nil
}

func (r *fakeDocRepo) CreateActivity(_ context.Context, a *docdom.Activity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *a
	r.activities[a.ID] = &cp
	return nil
}

func (r *fakeDocRepo) UpdateActivity(_ context.Context, a *docdom.Activity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.activities[a.ID]; !ok {
		return docdom.ErrActivityNotFound
	}
	cp := *a
	r.activities[a.ID] = &cp
	return nil
}

func (r *fakeDocRepo) DeleteActivity(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.activities[id]
	if !ok {
		return docdom.ErrActivityNotFound
	}
	now := time.Now()
	a.DeletedAt = &now
	return nil
}

// ---------------------------------------------------------------------------
// Folder tests
// ---------------------------------------------------------------------------

func TestCreateFolder_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	actor := uuid.New()

	f, err := svc.CreateFolder(ctx, docdom.CreateFolderInput{
		ProjectID: projectID,
		Name:      "Design Docs",
		CreatedBy: &actor,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Name != "Design Docs" {
		t.Errorf("expected Name=Design Docs, got %q", f.Name)
	}
	if f.ProjectID != projectID {
		t.Errorf("expected ProjectID=%v, got %v", projectID, f.ProjectID)
	}
	if f.ID == uuid.Nil {
		t.Error("expected non-nil ID")
	}
}

func TestCreateFolder_EmptyName(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)

	_, err := svc.CreateFolder(ctx, docdom.CreateFolderInput{
		ProjectID: uuid.New(),
		Name:      "   ",
	})
	if err != docdom.ErrFolderNameInvalid {
		t.Errorf("expected ErrFolderNameInvalid, got %v", err)
	}
}

func TestCreateFolder_WithParent_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()

	parent, _ := svc.CreateFolder(ctx, docdom.CreateFolderInput{
		ProjectID: projectID,
		Name:      "Parent",
	})

	child, err := svc.CreateFolder(ctx, docdom.CreateFolderInput{
		ProjectID: projectID,
		Name:      "Child",
		ParentID:  &parent.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Errorf("expected ParentID=%v, got %v", parent.ID, child.ParentID)
	}
}

func TestCreateFolder_ParentNotInProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)

	// Create parent in project A.
	parentProjectID := uuid.New()
	parent, _ := svc.CreateFolder(ctx, docdom.CreateFolderInput{
		ProjectID: parentProjectID,
		Name:      "Parent",
	})

	// Create child in project B with parent from project A — should fail.
	childProjectID := uuid.New()
	_, err := svc.CreateFolder(ctx, docdom.CreateFolderInput{
		ProjectID: childProjectID,
		Name:      "Child",
		ParentID:  &parent.ID,
	})
	if err != docdom.ErrFolderNotInProject {
		t.Errorf("expected ErrFolderNotInProject, got %v", err)
	}
}

func TestUpdateFolder_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()

	f, _ := svc.CreateFolder(ctx, docdom.CreateFolderInput{ProjectID: projectID, Name: "Old Name"})

	pos := 5
	updated, err := svc.UpdateFolder(ctx, f.ID, docdom.UpdateFolderInput{
		ProjectID: projectID,
		Name:      "New Name",
		Position:  &pos,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "New Name" {
		t.Errorf("expected Name=New Name, got %q", updated.Name)
	}
	if updated.Position != 5 {
		t.Errorf("expected Position=5, got %d", updated.Position)
	}
}

func TestUpdateFolder_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)

	_, err := svc.UpdateFolder(ctx, uuid.New(), docdom.UpdateFolderInput{Name: "X"})
	if err != docdom.ErrFolderNotFound {
		t.Errorf("expected ErrFolderNotFound, got %v", err)
	}
}

func TestUpdateFolder_WrongProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectA := uuid.New()
	projectB := uuid.New()

	f, _ := svc.CreateFolder(ctx, docdom.CreateFolderInput{ProjectID: projectA, Name: "Folder"})

	// Attempt to update with the wrong projectID — should return 404 (not leak existence).
	_, err := svc.UpdateFolder(ctx, f.ID, docdom.UpdateFolderInput{
		ProjectID: projectB,
		Name:      "Hacked",
	})
	if err != docdom.ErrFolderNotFound {
		t.Errorf("expected ErrFolderNotFound, got %v", err)
	}
}

func TestUpdateFolder_NewParentNotInProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)

	projectA := uuid.New()
	projectB := uuid.New()

	// Create folder in project A.
	folder, _ := svc.CreateFolder(ctx, docdom.CreateFolderInput{ProjectID: projectA, Name: "Folder A"})

	// Create a parent folder in project B.
	parentB, _ := svc.CreateFolder(ctx, docdom.CreateFolderInput{ProjectID: projectB, Name: "Parent B"})

	// Attempt to move folder (project A) under parentB (project B) — should fail.
	parentPtr := &parentB.ID
	_, err := svc.UpdateFolder(ctx, folder.ID, docdom.UpdateFolderInput{
		ProjectID: projectA,
		ParentID:  &parentPtr,
	})
	if err != docdom.ErrFolderNotInProject {
		t.Errorf("expected ErrFolderNotInProject, got %v", err)
	}
}

func TestUpdateFolder_SelfParent(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()

	folder, _ := svc.CreateFolder(ctx, docdom.CreateFolderInput{ProjectID: projectID, Name: "Folder"})

	// Attempt to set the folder as its own parent — should fail.
	selfPtr := &folder.ID
	_, err := svc.UpdateFolder(ctx, folder.ID, docdom.UpdateFolderInput{
		ProjectID: projectID,
		ParentID:  &selfPtr,
	})
	if err != docdom.ErrFolderSelfParent {
		t.Errorf("expected ErrFolderSelfParent, got %v", err)
	}
}

func TestDeleteFolder_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()

	f, _ := svc.CreateFolder(ctx, docdom.CreateFolderInput{ProjectID: projectID, Name: "TBD"})
	if err := svc.DeleteFolder(ctx, f.ID, projectID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := repo.FindFolderByID(ctx, f.ID)
	if err != docdom.ErrFolderNotFound {
		t.Errorf("expected ErrFolderNotFound after delete, got %v", err)
	}
}

func TestDeleteFolder_WrongProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectA := uuid.New()
	projectB := uuid.New()

	f, _ := svc.CreateFolder(ctx, docdom.CreateFolderInput{ProjectID: projectA, Name: "TBD"})

	// Attempt to delete with wrong projectID — should return 404.
	err := svc.DeleteFolder(ctx, f.ID, projectB)
	if err != docdom.ErrFolderNotFound {
		t.Errorf("expected ErrFolderNotFound, got %v", err)
	}
}

func TestDeleteFolder_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()

	err := svc.DeleteFolder(ctx, uuid.New(), projectID)
	if err != docdom.ErrFolderNotFound {
		t.Errorf("expected ErrFolderNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Document tests
// ---------------------------------------------------------------------------

func TestCreateDocument_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	actor := uuid.New()
	content := json.RawMessage(`{"type":"doc","content":[]}`)

	d, err := svc.CreateDocument(ctx, docdom.CreateDocumentInput{
		ProjectID: projectID,
		Title:     "Architecture Overview",
		Content:   content,
		CreatedBy: &actor,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Title != "Architecture Overview" {
		t.Errorf("expected Title=Architecture Overview, got %q", d.Title)
	}
	if d.ProjectID != projectID {
		t.Errorf("expected ProjectID=%v, got %v", projectID, d.ProjectID)
	}
	if d.ID == uuid.Nil {
		t.Error("expected non-nil ID")
	}
}

func TestCreateDocument_EmptyTitleDefaultsToUntitled(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()

	d, err := svc.CreateDocument(ctx, docdom.CreateDocumentInput{
		ProjectID: projectID,
		Title:     "   ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Title != "Untitled" {
		t.Errorf("expected Title=Untitled, got %q", d.Title)
	}
}

func TestCreateDocument_FolderNotInProject(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)

	// Create folder in project A.
	folderProjectID := uuid.New()
	folder, _ := svc.CreateFolder(ctx, docdom.CreateFolderInput{ProjectID: folderProjectID, Name: "F"})

	// Create document in project B referencing folder from project A — should fail.
	docProjectID := uuid.New()
	_, err := svc.CreateDocument(ctx, docdom.CreateDocumentInput{
		ProjectID: docProjectID,
		Title:     "Doc",
		FolderID:  &folder.ID,
	})
	if err != docdom.ErrFolderNotInProject {
		t.Errorf("expected ErrFolderNotInProject, got %v", err)
	}
}

func TestGetDocument_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()

	d, _ := svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, Title: "Hello"})

	got, err := svc.GetDocument(ctx, d.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != d.ID {
		t.Errorf("expected ID=%v, got %v", d.ID, got.ID)
	}
}

func TestGetDocument_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)

	_, err := svc.GetDocument(ctx, uuid.New())
	if err != docdom.ErrDocNotFound {
		t.Errorf("expected ErrDocNotFound, got %v", err)
	}
}

func TestUpdateDocument_TitleChange_CreatesSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	actor := uuid.New()

	d, _ := svc.CreateDocument(ctx, docdom.CreateDocumentInput{
		ProjectID: projectID,
		Title:     "Old Title",
		Content:   json.RawMessage(`{"type":"doc"}`),
	})

	newTitle := "New Title"
	updated, err := svc.UpdateDocument(ctx, d.ID, docdom.UpdateDocumentInput{
		Title:     &newTitle,
		UpdatedBy: &actor,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Title != "New Title" {
		t.Errorf("expected Title=New Title, got %q", updated.Title)
	}

	// A snapshot should have been created preserving the old title.
	snaps, err := repo.ListSnapshots(ctx, d.ID)
	if err != nil {
		t.Fatalf("ListSnapshots error: %v", err)
	}
	if len(snaps) != 1 {
		t.Errorf("expected 1 snapshot after title change, got %d", len(snaps))
	}
	if snaps[0].Title != "Old Title" {
		t.Errorf("snapshot should preserve old title, got %q", snaps[0].Title)
	}
}

func TestUpdateDocument_ContentChange_CreatesSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	actor := uuid.New()

	originalContent := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`)
	d, _ := svc.CreateDocument(ctx, docdom.CreateDocumentInput{
		ProjectID: projectID,
		Title:     "Doc",
		Content:   originalContent,
	})

	// First update — changes content.
	newContent := json.RawMessage(`{"type":"doc","content":[{"type":"heading"}]}`)
	_, err := svc.UpdateDocument(ctx, d.ID, docdom.UpdateDocumentInput{
		Content:   &newContent,
		UpdatedBy: &actor,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snaps, _ := repo.ListSnapshots(ctx, d.ID)
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snaps))
	}
	// Snapshot should store the OLD content.
	if string(snaps[0].Content) != string(originalContent) {
		t.Errorf("snapshot content mismatch: got %s, want %s", snaps[0].Content, originalContent)
	}

	// Second update with identical content — no new snapshot.
	_, err = svc.UpdateDocument(ctx, d.ID, docdom.UpdateDocumentInput{
		Content:   &newContent,
		UpdatedBy: &actor,
	})
	if err != nil {
		t.Fatalf("unexpected error on second update: %v", err)
	}
	snaps, _ = repo.ListSnapshots(ctx, d.ID)
	if len(snaps) != 1 {
		t.Errorf("expected still 1 snapshot (no duplicate on identical content), got %d", len(snaps))
	}
}

func TestUpdateDocument_EmptyTitle_Error(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()

	d, _ := svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, Title: "Doc"})

	empty := "  "
	_, err := svc.UpdateDocument(ctx, d.ID, docdom.UpdateDocumentInput{Title: &empty})
	if err != docdom.ErrDocTitleInvalid {
		t.Errorf("expected ErrDocTitleInvalid, got %v", err)
	}
}

func TestUpdateDocument_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)

	newTitle := "X"
	_, err := svc.UpdateDocument(ctx, uuid.New(), docdom.UpdateDocumentInput{Title: &newTitle})
	if err != docdom.ErrDocNotFound {
		t.Errorf("expected ErrDocNotFound, got %v", err)
	}
}

func TestDeleteDocument_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()

	d, _ := svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, Title: "To Delete"})

	if err := svc.DeleteDocument(ctx, d.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.GetDocument(ctx, d.ID)
	if err != docdom.ErrDocNotFound {
		t.Errorf("expected ErrDocNotFound after delete, got %v", err)
	}
}

func TestDeleteDocument_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)

	err := svc.DeleteDocument(ctx, uuid.New())
	if err != docdom.ErrDocNotFound {
		t.Errorf("expected ErrDocNotFound, got %v", err)
	}
}

func TestListDocuments_Search(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	content := json.RawMessage(`{"type":"doc","content":[]}`)

	if _, err := svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, Title: "Onboarding Flow", Content: content}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, Title: "Architecture Overview", Content: content}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	search := "onboard"
	docs, _, err := svc.ListDocuments(ctx, projectID, nil, &search, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 matching document, got %d", len(docs))
	}
	if docs[0].Title != "Onboarding Flow" {
		t.Errorf("expected Title=Onboarding Flow, got %q", docs[0].Title)
	}

	all, _, err := svc.ListDocuments(ctx, projectID, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 documents with no search filter, got %d", len(all))
	}
}

func TestListDocuments_CursorPagination(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	content := json.RawMessage(`{"type":"doc","content":[]}`)

	titles := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"}
	for _, title := range titles {
		if _, err := svc.CreateDocument(ctx, docdom.CreateDocumentInput{ProjectID: projectID, Title: title, Content: content}); err != nil {
			t.Fatalf("unexpected error creating %q: %v", title, err)
		}
	}

	pageSize := 2
	var seen []string
	var cursor *string
	for {
		page, hasMore, err := svc.ListDocuments(ctx, projectID, nil, nil, cursor, &pageSize)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(page) > pageSize {
			t.Fatalf("page returned %d documents, want at most %d", len(page), pageSize)
		}
		for _, d := range page {
			seen = append(seen, d.Title)
		}
		if !hasMore {
			break
		}
		s := docdom.EncodeDocumentCursor(page[len(page)-1])
		cursor = &s
	}

	if len(seen) != len(titles) {
		t.Fatalf("expected to page through all %d documents exactly once, got %d: %v", len(titles), len(seen), seen)
	}
	want := append([]string(nil), titles...)
	sort.Strings(want)
	for i, title := range want {
		if seen[i] != title {
			t.Errorf("page %d: expected %q, got %q (full sequence: %v)", i, title, seen[i], seen)
		}
	}
}

// ---------------------------------------------------------------------------
// Snapshot tests
// ---------------------------------------------------------------------------

func TestListSnapshots_DocNotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)

	_, err := svc.ListSnapshots(ctx, uuid.New())
	if err != docdom.ErrDocNotFound {
		t.Errorf("expected ErrDocNotFound, got %v", err)
	}
}

func TestListSnapshots_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	actor := uuid.New()

	d, _ := svc.CreateDocument(ctx, docdom.CreateDocumentInput{
		ProjectID: projectID,
		Title:     "Doc",
		Content:   json.RawMessage(`{"v":1}`),
	})

	// Update to trigger snapshot.
	v2 := json.RawMessage(`{"v":2}`)
	_, _ = svc.UpdateDocument(ctx, d.ID, docdom.UpdateDocumentInput{Content: &v2, UpdatedBy: &actor})

	snaps, err := svc.ListSnapshots(ctx, d.ID)
	if err != nil {
		t.Fatalf("ListSnapshots error: %v", err)
	}
	if len(snaps) != 1 {
		t.Errorf("expected 1 snapshot, got %d", len(snaps))
	}
}

func TestGetSnapshot_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)

	_, err := svc.GetSnapshot(ctx, uuid.New())
	if err != docdom.ErrSnapshotNotFound {
		t.Errorf("expected ErrSnapshotNotFound, got %v", err)
	}
}

func TestGetSnapshot_OK(t *testing.T) {
	ctx := context.Background()
	repo := newFakeDocRepo()
	svc := docsvc.New(repo, nil)
	projectID := uuid.New()
	actor := uuid.New()

	d, _ := svc.CreateDocument(ctx, docdom.CreateDocumentInput{
		ProjectID: projectID,
		Title:     "Doc",
		Content:   json.RawMessage(`{"v":1}`),
	})

	v2 := json.RawMessage(`{"v":2}`)
	_, _ = svc.UpdateDocument(ctx, d.ID, docdom.UpdateDocumentInput{Content: &v2, UpdatedBy: &actor})

	snaps, _ := svc.ListSnapshots(ctx, d.ID)
	if len(snaps) == 0 {
		t.Fatal("no snapshots created")
	}

	snap, err := svc.GetSnapshot(ctx, snaps[0].ID)
	if err != nil {
		t.Fatalf("GetSnapshot error: %v", err)
	}
	if snap.ID != snaps[0].ID {
		t.Errorf("snapshot ID mismatch")
	}
}
