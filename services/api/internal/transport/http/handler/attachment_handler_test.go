package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// ---------------------------------------------------------------------------
// Minimal fakes
// ---------------------------------------------------------------------------

type fakeAttachmentSvc struct {
	contentData []byte
	contentType string
	contentErr  error
}

func (f *fakeAttachmentSvc) InitiateUpload(_ context.Context, _ uuid.UUID, _ attachmentdom.InitiateUploadInput) (*attachmentdom.UploadSession, error) {
	return &attachmentdom.UploadSession{FileID: uuid.New()}, nil
}
func (f *fakeAttachmentSvc) CompleteUpload(_ context.Context, _ uuid.UUID, _ attachmentdom.CompleteUploadInput) (*attachmentdom.TaskAttachment, error) {
	return &attachmentdom.TaskAttachment{}, nil
}
func (f *fakeAttachmentSvc) GetDownloadURL(_ context.Context, _, _, _ uuid.UUID, _ time.Duration, _ bool) (string, error) {
	return "https://example.com/download", nil
}
func (f *fakeAttachmentSvc) ListTaskAttachments(_ context.Context, _, _ uuid.UUID) ([]*attachmentdom.TaskAttachment, error) {
	return nil, nil
}
func (f *fakeAttachmentSvc) DeleteTaskAttachment(_ context.Context, _, _, _ uuid.UUID) error {
	return nil
}
func (f *fakeAttachmentSvc) GetAttachmentContent(_ context.Context, _, _, _ uuid.UUID) ([]byte, string, error) {
	if f.contentErr != nil {
		return nil, "", f.contentErr
	}
	return f.contentData, f.contentType, nil
}

var _ attachmentdom.Service = (*fakeAttachmentSvc)(nil)

// ---------------------------------------------------------------------------
// Router helper
// ---------------------------------------------------------------------------

// injectAuthClaims injects JWT claims for the attachment handler tests
// (InitiateUpload/CompleteUpload require claims.Subject for the uploader UUID).
func injectAuthClaimsMiddleware(sub string) func(http.Handler) http.Handler {
	claims := &domainauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: sub},
		Kind:             "access",
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), httpmw.ClaimsContextKey(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newAttachmentRouterWithAuth() chi.Router {
	h := handler.NewAttachmentHandler(&fakeAttachmentSvc{})
	r := chi.NewRouter()
	r.Use(injectAuthClaimsMiddleware(uuid.New().String()))
	r.Route("/projects/{projectId}/tasks/{taskId}/attachments", func(r chi.Router) {
		r.Post("/initiate-upload", h.InitiateUpload)
		r.Post("/complete-upload", h.CompleteUpload)
	})
	return r
}

func newAttachmentContentRouter(svc *fakeAttachmentSvc) chi.Router {
	h := handler.NewAttachmentHandler(svc)
	r := chi.NewRouter()
	r.Use(injectAuthClaimsMiddleware(uuid.New().String()))
	r.Route("/projects/{projectId}/tasks/{taskId}/attachments", func(r chi.Router) {
		r.Get("/{attachmentId}/content", h.GetAttachmentContent)
	})
	return r
}

func doAttachRequest(t *testing.T, r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, buf)
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

func TestInitiateUpload_MissingFileName_Returns400(t *testing.T) {
	r := newAttachmentRouterWithAuth()
	projectID := uuid.New()
	taskID := uuid.New()
	path := "/projects/" + projectID.String() + "/tasks/" + taskID.String() + "/attachments/initiate-upload"

	w := doAttachRequest(t, r, http.MethodPost, path,
		map[string]any{"content_type": "image/png", "file_size": 1024})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing file_name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInitiateUpload_MissingContentType_Returns400(t *testing.T) {
	r := newAttachmentRouterWithAuth()
	projectID := uuid.New()
	taskID := uuid.New()
	path := "/projects/" + projectID.String() + "/tasks/" + taskID.String() + "/attachments/initiate-upload"

	w := doAttachRequest(t, r, http.MethodPost, path,
		map[string]any{"file_name": "photo.png", "content_type": "", "file_size": 1024})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing content_type, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInitiateUpload_ZeroFileSize_Returns400(t *testing.T) {
	r := newAttachmentRouterWithAuth()
	projectID := uuid.New()
	taskID := uuid.New()
	path := "/projects/" + projectID.String() + "/tasks/" + taskID.String() + "/attachments/initiate-upload"

	w := doAttachRequest(t, r, http.MethodPost, path,
		map[string]any{"file_name": "photo.png", "content_type": "image/png", "file_size": 0})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for zero file_size, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCompleteUpload_MissingFileID_Returns400(t *testing.T) {
	r := newAttachmentRouterWithAuth()
	projectID := uuid.New()
	taskID := uuid.New()
	path := "/projects/" + projectID.String() + "/tasks/" + taskID.String() + "/attachments/complete-upload"

	// file_id absent → decodes to uuid.Nil → returns 400
	w := doAttachRequest(t, r, http.MethodPost, path, map[string]any{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing file_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetAttachmentContent_ReturnsRawBytesWithContentType(t *testing.T) {
	svc := &fakeAttachmentSvc{contentData: []byte("hello world"), contentType: "text/plain"}
	r := newAttachmentContentRouter(svc)
	projectID := uuid.New()
	taskID := uuid.New()
	attachmentID := uuid.New()
	path := "/projects/" + projectID.String() + "/tasks/" + taskID.String() +
		"/attachments/" + attachmentID.String() + "/content"

	w := doAttachRequest(t, r, http.MethodGet, path, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("expected Content-Type text/plain, got %q", got)
	}
	if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options nosniff, got %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); got != "attachment" {
		t.Fatalf("expected Content-Disposition attachment, got %q", got)
	}
	if got := w.Body.String(); got != "hello world" {
		t.Fatalf("expected body %q, got %q", "hello world", got)
	}
}

func TestGetAttachmentContent_TooLarge_Returns413(t *testing.T) {
	svc := &fakeAttachmentSvc{contentErr: attachmentdom.ErrAttachmentContentTooLarge}
	r := newAttachmentContentRouter(svc)
	projectID := uuid.New()
	taskID := uuid.New()
	attachmentID := uuid.New()
	path := "/projects/" + projectID.String() + "/tasks/" + taskID.String() +
		"/attachments/" + attachmentID.String() + "/content"

	w := doAttachRequest(t, r, http.MethodGet, path, nil)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
}
