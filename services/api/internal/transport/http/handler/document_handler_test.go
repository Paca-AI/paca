package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// ---------------------------------------------------------------------------
// Minimal fakes
// ---------------------------------------------------------------------------

type fakeDocSvc struct {
	// recorded by the tree-listing methods
	treeFolder  *uuid.UUID
	treeCursor  *string
	treeLimit   int
	treeDocs    []*docdom.Document
	treeFolders []*docdom.DocFolder
	treeMore    bool
	treeCalls   int
	docFilter   docdom.DocumentFilter
	snaps       []*docdom.DocSnapshot
	snapsMore   bool
	snapCursor  *string
	snapHash    *string
	// single-document reads and writes
	doc          *docdom.Document
	updateDoc    *docdom.Document
	updateChange docdom.DocChange
	updateErr    error
	lastUpdate   *docdom.UpdateDocumentInput
	snapLimit    int
}

func (f *fakeDocSvc) ListFolders(_ context.Context, _ uuid.UUID, parent *uuid.UUID, _ docdom.FolderFilter, cursor *string, limit int) ([]*docdom.DocFolder, bool, error) {
	f.treeCalls++
	f.treeFolder, f.treeCursor, f.treeLimit = parent, cursor, limit
	return f.treeFolders, f.treeMore, nil
}

func (f *fakeDocSvc) FolderPath(_ context.Context, _ uuid.UUID, id uuid.UUID) ([]*docdom.DocFolder, error) {
	return []*docdom.DocFolder{{ID: id, Name: "leaf"}}, nil
}

func (f *fakeDocSvc) CreateFolder(_ context.Context, _ docdom.CreateFolderInput) (*docdom.DocFolder, error) {
	return &docdom.DocFolder{ID: uuid.New()}, nil
}
func (f *fakeDocSvc) UpdateFolder(_ context.Context, _ uuid.UUID, _ docdom.UpdateFolderInput) (*docdom.DocFolder, error) {
	return nil, docdom.ErrFolderNotFound
}
func (f *fakeDocSvc) DeleteFolder(_ context.Context, _ uuid.UUID, _ uuid.UUID) error { return nil }

func (f *fakeDocSvc) ListDocuments(_ context.Context, _ uuid.UUID, filter docdom.DocumentFilter, cursor *string, limit int) ([]*docdom.Document, bool, error) {
	f.treeCalls++
	f.docFilter, f.treeFolder, f.treeCursor, f.treeLimit = filter, filter.FolderID, cursor, limit
	return f.treeDocs, f.treeMore, nil
}
func (f *fakeDocSvc) SearchDocuments(_ context.Context, _ uuid.UUID, _ string, _ int) ([]docdom.SearchHit, error) {
	return nil, nil
}
func (f *fakeDocSvc) GetDocument(_ context.Context, _ uuid.UUID) (*docdom.Document, error) {
	if f.doc != nil {
		cp := *f.doc
		return &cp, nil
	}
	return nil, docdom.ErrDocNotFound
}
func (f *fakeDocSvc) CreateDocument(_ context.Context, _ docdom.CreateDocumentInput) (*docdom.Document, error) {
	return &docdom.Document{ID: uuid.New()}, nil
}
func (f *fakeDocSvc) UpdateDocument(_ context.Context, _, _ uuid.UUID, in docdom.UpdateDocumentInput) (*docdom.Document, docdom.DocChange, error) {
	f.lastUpdate = &in
	if f.updateErr != nil || f.updateDoc != nil {
		return f.updateDoc, f.updateChange, f.updateErr
	}
	return nil, docdom.DocChange{}, docdom.ErrDocNotFound
}
func (f *fakeDocSvc) DeleteDocument(_ context.Context, _, _ uuid.UUID) error { return nil }

func (f *fakeDocSvc) ListSnapshots(_ context.Context, _, _ uuid.UUID, hash *string, cursor *string, limit int) ([]*docdom.DocSnapshot, bool, error) {
	f.snapHash, f.snapCursor, f.snapLimit = hash, cursor, limit
	return f.snaps, f.snapsMore, nil
}
func (f *fakeDocSvc) GetSnapshot(_ context.Context, _, _ uuid.UUID) (*docdom.DocSnapshot, error) {
	return nil, docdom.ErrSnapshotNotFound
}

type fakeDocActivitySvc struct {
	pageCursor *string
	pageLimit  int
	recorded   []docdom.RecordActivityInput
}

func (f *fakeDocActivitySvc) RecordActivity(_ context.Context, in docdom.RecordActivityInput) error {
	f.recorded = append(f.recorded, in)
	return nil
}
func (f *fakeDocActivitySvc) ListActivities(_ context.Context, _, _ uuid.UUID, cursor *string, limit int) ([]*docdom.Activity, *string, error) {
	f.pageCursor, f.pageLimit = cursor, limit
	next := "next-page"
	return []*docdom.Activity{{ID: uuid.New()}}, &next, nil
}
func (f *fakeDocActivitySvc) AddComment(_ context.Context, in docdom.AddCommentInput) (*docdom.Activity, error) {
	return &docdom.Activity{ID: uuid.New(), DocumentID: in.DocumentID, Content: in.Content}, nil
}
func (f *fakeDocActivitySvc) UpdateComment(_ context.Context, id uuid.UUID, _ uuid.UUID, _ uuid.UUID, content json.RawMessage) (*docdom.Activity, error) {
	return &docdom.Activity{ID: id, Content: content}, nil
}
func (f *fakeDocActivitySvc) DeleteComment(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

// compile-time interface checks
var _ docdom.Service = (*fakeDocSvc)(nil)
var _ docdom.ActivityService = (*fakeDocActivitySvc)(nil)

// ---------------------------------------------------------------------------
// Router helper
// ---------------------------------------------------------------------------

func newDocumentRouter() chi.Router {
	return newDocumentRouterWith(&fakeDocSvc{})
}

func newDocumentRouterWith(svc *fakeDocSvc) chi.Router {
	return newDocumentRouterWithActivity(svc, &fakeDocActivitySvc{})
}

func newDocumentRouterWithActivity(svc *fakeDocSvc, act *fakeDocActivitySvc) chi.Router {
	h := handler.NewDocumentHandler(svc, act)
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Get("/docs/folders", h.ListFolders)
		r.Get("/docs/folders/{folderId}/path", h.GetFolderPath)
		r.Get("/docs", h.ListDocuments)
		r.Get("/docs/{docId}/activities", h.ListActivities)
		r.Get("/docs/{docId}/snapshots", h.ListSnapshots)
		r.Post("/docs/folders", h.CreateFolder)
		r.Post("/docs", h.CreateDocument)
		r.Route("/docs/{docId}", func(r chi.Router) {
			r.Patch("/", h.UpdateDocument)
			r.Post("/comments", h.AddComment)
			r.Patch("/comments/{commentId}", h.UpdateComment)
		})
	})
	return r
}

func doDocRequest(t *testing.T, r chi.Router, method, path string, body any, actorID *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	ctx := context.Background()
	if actorID != nil {
		ctx = httpmw.WithActorID(ctx, *actorID)
	}
	req := httptest.NewRequestWithContext(ctx, method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCreateFolder_EmptyName_Returns400(t *testing.T) {
	r := newDocumentRouter()
	projectID := uuid.New()

	w := doDocRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/docs/folders",
		map[string]any{"name": ""},
		nil,
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty folder name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDocAddComment_EmptyContent_Returns400(t *testing.T) {
	r := newDocumentRouter()
	projectID := uuid.New()
	docID := uuid.New()
	actorID := uuid.New()

	// content field absent → json.RawMessage is nil → len == 0 → handler returns 400
	w := doDocRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/docs/"+docID.String()+"/comments",
		map[string]any{},
		&actorID,
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty content, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDocUpdateComment_EmptyContent_Returns400(t *testing.T) {
	r := newDocumentRouter()
	projectID := uuid.New()
	docID := uuid.New()
	commentID := uuid.New()
	actorID := uuid.New()

	// content field absent → json.RawMessage is nil → len == 0 → handler returns 400
	w := doDocRequest(t, r, http.MethodPatch,
		"/projects/"+projectID.String()+"/docs/"+docID.String()+"/comments/"+commentID.String(),
		map[string]any{},
		&actorID,
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty content, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateDocument_StringContent_Returns400 guards against GitHub issue
// #233: document content must be a BlockNote block array, not a plain string.
func TestCreateDocument_StringContent_Returns400(t *testing.T) {
	r := newDocumentRouter()
	projectID := uuid.New()

	w := doDocRequest(t, r, http.MethodPost,
		"/projects/"+projectID.String()+"/docs",
		map[string]any{"title": "Doc", "content": "just a plain string"},
		nil,
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for string content, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateDocument_StringContent_Returns400 mirrors the create case above
// for PATCH /projects/:projectId/docs/:docId.
func TestUpdateDocument_StringContent_Returns400(t *testing.T) {
	r := newDocumentRouter()
	projectID := uuid.New()
	docID := uuid.New()

	w := doDocRequest(t, r, http.MethodPatch,
		"/projects/"+projectID.String()+"/docs/"+docID.String(),
		map[string]any{"content": "just a plain string"},
		nil,
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for string content, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListDocuments_PassesFilterPagingAndReturnsNextCursor(t *testing.T) {
	last := &docdom.Document{ID: uuid.New(), Position: 3, Title: "x"}
	svc := &fakeDocSvc{treeDocs: []*docdom.Document{last}, treeMore: true}
	r := newDocumentRouterWith(svc)
	folder := uuid.New()

	w := doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs?folder_id="+folder.String()+"&search=%20plan%20&page_size=7&cursor=abc", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if svc.treeCalls != 1 || svc.treeLimit != 7 || svc.treeCursor == nil || *svc.treeCursor != "abc" ||
		svc.docFilter.FolderID == nil || *svc.docFilter.FolderID != folder || svc.docFilter.RootOnly || svc.docFilter.Search != "plan" {
		t.Fatalf("call = %+v filter=%+v", svc, svc.docFilter)
	}
	var resp struct {
		Data struct {
			Items      []map[string]any `json:"items"`
			NextCursor *string          `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Items) != 1 || resp.Data.NextCursor == nil || *resp.Data.NextCursor != docdom.EncodeDocumentTreeCursor(last) {
		t.Fatalf("response = %s", w.Body.String())
	}

	// "null" means documents outside any folder; absent means anywhere.
	doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs?folder_id=null", nil, nil)
	if !svc.docFilter.RootOnly || svc.docFilter.FolderID != nil || svc.treeLimit != 50 {
		t.Fatalf("root filter=%+v limit=%d", svc.docFilter, svc.treeLimit)
	}
	doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs", nil, nil)
	if svc.docFilter.RootOnly || svc.docFilter.FolderID != nil {
		t.Fatalf("unfiltered listing got filter=%+v", svc.docFilter)
	}
	for _, q := range []string{"folder_id=nope", "page_size=0", "page_size=201"} {
		w := doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs?"+q, nil, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", q, w.Code)
		}
	}
}

func TestListFolders_IsPaginatedAndDefaultsToTheTopLevel(t *testing.T) {
	f := &docdom.DocFolder{ID: uuid.New(), Name: "f"}
	svc := &fakeDocSvc{treeFolders: []*docdom.DocFolder{f}, treeMore: true}
	r := newDocumentRouterWith(svc)

	w := doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs/folders", nil, nil)
	if w.Code != http.StatusOK || svc.treeCalls != 1 || svc.treeFolder != nil || svc.treeLimit != 50 {
		t.Fatalf("got %d, calls=%d folder=%v limit=%d", w.Code, svc.treeCalls, svc.treeFolder, svc.treeLimit)
	}
	if !strings.Contains(w.Body.String(), docdom.EncodeFolderTreeCursor(f)) {
		t.Fatalf("missing next_cursor: %s", w.Body.String())
	}
	parent := uuid.New()
	w = doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs/folders?parent_id="+parent.String()+"&page_size=10", nil, nil)
	if w.Code != http.StatusOK || svc.treeFolder == nil || *svc.treeFolder != parent || svc.treeLimit != 10 {
		t.Fatalf("parent listing: %d folder=%v limit=%d", w.Code, svc.treeFolder, svc.treeLimit)
	}
}

func TestDocTree_FolderPath(t *testing.T) {
	r := newDocumentRouter()
	id := uuid.New()
	w := doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs/folders/"+id.String()+"/path", nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), id.String()) {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	w = doDocRequest(t, r, http.MethodGet, "/projects/"+uuid.NewString()+"/docs/folders/nope/path", nil, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: got %d", w.Code)
	}
}

func TestDocActivities_PagedMode(t *testing.T) {
	act := &fakeDocActivitySvc{}
	r := newDocumentRouterWithActivity(&fakeDocSvc{}, act)
	path := "/projects/" + uuid.NewString() + "/docs/" + uuid.NewString() + "/activities"

	w := doDocRequest(t, r, http.MethodGet, path+"?page_size=7&cursor=zzz", nil, nil)
	if w.Code != http.StatusOK || act.pageLimit != 7 || act.pageCursor == nil || *act.pageCursor != "zzz" {
		t.Fatalf("got %d limit=%d cursor=%v", w.Code, act.pageLimit, act.pageCursor)
	}
	if !strings.Contains(w.Body.String(), `"next_cursor":"next-page"`) {
		t.Fatalf("missing next_cursor: %s", w.Body.String())
	}
	if w := doDocRequest(t, r, http.MethodGet, path+"?page_size=500", nil, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("page_size=500: got %d", w.Code)
	}
}

func TestListSnapshots_IsPaginatedAndOmitsBodies(t *testing.T) {
	older := &docdom.DocSnapshot{ID: uuid.New(), SnapshotNumber: 4, Title: "v4"}
	svc := &fakeDocSvc{snaps: []*docdom.DocSnapshot{older}, snapsMore: true}
	r := newDocumentRouterWith(svc)
	path := "/projects/" + uuid.NewString() + "/docs/" + uuid.NewString() + "/snapshots"

	w := doDocRequest(t, r, http.MethodGet, path+"?page_size=3&cursor=abc", nil, nil)
	if w.Code != http.StatusOK || svc.snapLimit != 3 || svc.snapCursor == nil || *svc.snapCursor != "abc" {
		t.Fatalf("got %d limit=%d cursor=%v: %s", w.Code, svc.snapLimit, svc.snapCursor, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), docdom.EncodeSnapshotCursor(older)) {
		t.Fatalf("missing next_cursor: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"content"`) {
		t.Fatalf("a snapshot list must not carry bodies: %s", w.Body.String())
	}
	svc.snapLimit = 0
	if w := doDocRequest(t, r, http.MethodGet, path, nil, nil); w.Code != http.StatusOK || svc.snapLimit != 30 {
		t.Fatalf("default page size: %d limit=%d", w.Code, svc.snapLimit)
	}
	if w := doDocRequest(t, r, http.MethodGet, path+"?page_size=101", nil, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("page_size=101: got %d", w.Code)
	}
}
