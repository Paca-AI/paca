package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

type fakeDocSvc struct{}

func (f *fakeDocSvc) ListFolders(_ context.Context, _ uuid.UUID) ([]*docdom.DocFolder, error) {
	return nil, nil
}
func (f *fakeDocSvc) CreateFolder(_ context.Context, _ docdom.CreateFolderInput) (*docdom.DocFolder, error) {
	return &docdom.DocFolder{ID: uuid.New()}, nil
}
func (f *fakeDocSvc) UpdateFolder(_ context.Context, _ uuid.UUID, _ docdom.UpdateFolderInput) (*docdom.DocFolder, error) {
	return nil, docdom.ErrFolderNotFound
}
func (f *fakeDocSvc) DeleteFolder(_ context.Context, _ uuid.UUID, _ uuid.UUID) error { return nil }

func (f *fakeDocSvc) ListDocuments(_ context.Context, _ uuid.UUID, _ *uuid.UUID) ([]*docdom.Document, error) {
	return nil, nil
}
func (f *fakeDocSvc) GetDocument(_ context.Context, _ uuid.UUID) (*docdom.Document, error) {
	return nil, docdom.ErrDocNotFound
}
func (f *fakeDocSvc) CreateDocument(_ context.Context, _ docdom.CreateDocumentInput) (*docdom.Document, error) {
	return &docdom.Document{ID: uuid.New()}, nil
}
func (f *fakeDocSvc) UpdateDocument(_ context.Context, _ uuid.UUID, _ docdom.UpdateDocumentInput) (*docdom.Document, error) {
	return nil, docdom.ErrDocNotFound
}
func (f *fakeDocSvc) DeleteDocument(_ context.Context, _ uuid.UUID) error { return nil }

func (f *fakeDocSvc) ListSnapshots(_ context.Context, _ uuid.UUID) ([]*docdom.DocSnapshot, error) {
	return nil, nil
}
func (f *fakeDocSvc) GetSnapshot(_ context.Context, _ uuid.UUID) (*docdom.DocSnapshot, error) {
	return nil, docdom.ErrSnapshotNotFound
}

type fakeDocActivitySvc struct{}

func (f *fakeDocActivitySvc) RecordActivity(_ context.Context, _ docdom.RecordActivityInput) error {
	return nil
}
func (f *fakeDocActivitySvc) ListActivities(_ context.Context, _ uuid.UUID) ([]*docdom.Activity, error) {
	return nil, nil
}
func (f *fakeDocActivitySvc) AddComment(_ context.Context, in docdom.AddCommentInput) (*docdom.Activity, error) {
	return &docdom.Activity{ID: uuid.New(), DocumentID: in.DocumentID, Content: in.Content}, nil
}
func (f *fakeDocActivitySvc) UpdateComment(_ context.Context, id uuid.UUID, _ uuid.UUID, _ uuid.UUID, _ *uuid.UUID, content json.RawMessage) (*docdom.Activity, error) {
	return &docdom.Activity{ID: id, Content: content}, nil
}
func (f *fakeDocActivitySvc) DeleteComment(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ uuid.UUID, _ *uuid.UUID) error {
	return nil
}

// compile-time interface checks
var _ docdom.Service = (*fakeDocSvc)(nil)
var _ docdom.ActivityService = (*fakeDocActivitySvc)(nil)

// ---------------------------------------------------------------------------
// Router helper
// ---------------------------------------------------------------------------

func newDocumentRouter() chi.Router {
	h := handler.NewDocumentHandler(&fakeDocSvc{}, &fakeDocActivitySvc{})
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
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
