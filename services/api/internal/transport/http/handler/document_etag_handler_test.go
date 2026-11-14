package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

type httpRecorder = httptest.ResponseRecorder

func newReq(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func serve(r http.Handler, req *http.Request) *httpRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func serveAs(r http.Handler, req *http.Request, actor uuid.UUID) *httpRecorder {
	return serve(r, req.WithContext(httpmw.WithActorID(req.Context(), actor)))
}

// createReturningSvc is a fakeDocSvc whose CreateDocument returns a given doc.
type createReturningSvc struct {
	*fakeDocSvc
	created *docdom.Document
}

func (s *createReturningSvc) CreateDocument(context.Context, docdom.CreateDocumentInput) (*docdom.Document, error) {
	return s.created, nil
}

const etagBlocks = `[{"id":"a","type":"paragraph","content":[{"type":"text","text":"hi","styles":{}}]}]`

func etagDoc(projectID uuid.UUID, storeHash bool) *docdom.Document {
	d := &docdom.Document{ID: uuid.New(), ProjectID: projectID, Title: "Doc", Content: json.RawMessage(etagBlocks), UpdatedAt: time.Now().Truncate(time.Second)}
	if storeHash {
		h := docdom.ContentHash(d.Content)
		d.ContentHash = &h
	}
	return d
}

func newDocRouterWithNotify(svc *fakeDocSvc, act *fakeDocActivitySvc, notify *mockNotificationSvc) chi.Router {
	h := handler.NewDocumentHandler(svc, act).WithDocNotificationService(notify)
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Get("/docs/{docId}", h.GetDocument)
		r.Patch("/docs/{docId}", h.UpdateDocument)
		r.Post("/docs", h.CreateDocument)
		r.Get("/docs/{docId}/snapshots", h.ListSnapshots)
	})
	return r
}

func getWithHeader(t *testing.T, r chi.Router, path, name, value string) *httpRecorder {
	t.Helper()
	req := newReq(t, http.MethodGet, path, nil)
	if name != "" {
		req.Header.Set(name, value)
	}
	return serve(r, req)
}

// etagHasHash reports whether etag is "<hash>-<16 hex>": the content hash, then
// the tag over the rest of the response.
func etagHasHash(etag, hash string) bool {
	return regexp.MustCompile(`^"` + hash + `-[0-9a-f]{16}"$`).MatchString(etag)
}

// A rename keeps the content hash but is a different response: a client that
// revalidates must get the new title, not a 304.
func TestGetDocument_RenameIsNot304(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	svc := &fakeDocSvc{doc: d}
	r := newDocRouterWithNotify(svc, &fakeDocActivitySvc{}, &mockNotificationSvc{})
	path := "/projects/" + projectID.String() + "/docs/" + d.ID.String()
	before := getWithHeader(t, r, path, "", "").Header().Get("ETag")

	d.Title = "Renamed"
	w := getWithHeader(t, r, path, "If-None-Match", before)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"title":"Renamed"`) {
		t.Fatalf("after a rename: got %d %s, want 200 with the new title", w.Code, w.Body.String())
	}
	after := w.Header().Get("ETag")
	if after == before || !etagHasHash(after, *d.ContentHash) {
		t.Fatalf("ETag after a rename = %q (before %q): want a new ETag with the same content hash", after, before)
	}
}

func TestGetDocument_SetsETagAndContentHash(t *testing.T) {
	projectID := uuid.New()
	for _, stored := range []bool{true, false} {
		d := etagDoc(projectID, stored)
		svc := &fakeDocSvc{doc: d}
		r := newDocRouterWithNotify(svc, &fakeDocActivitySvc{}, &mockNotificationSvc{})
		w := getWithHeader(t, r, "/projects/"+projectID.String()+"/docs/"+d.ID.String(), "", "")
		if w.Code != http.StatusOK {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
		want := docdom.ContentHash(json.RawMessage(etagBlocks))
		if got := w.Header().Get("ETag"); !etagHasHash(got, want) {
			t.Fatalf("stored=%v ETag = %q, want \"%s-<meta tag>\"", stored, got, want)
		}
		if got := w.Header().Get("Cache-Control"); got != "private, no-cache" {
			t.Fatalf("stored=%v Cache-Control = %q, want %q", stored, got, "private, no-cache")
		}
		var resp struct {
			Data struct {
				ContentHash string `json:"content_hash"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Data.ContentHash != want {
			t.Fatalf("stored=%v content_hash = %q, want %q", stored, resp.Data.ContentHash, want)
		}
	}
}

func TestGetDocument_IfNoneMatch304(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	r := newDocRouterWithNotify(&fakeDocSvc{doc: d}, &fakeDocActivitySvc{}, &mockNotificationSvc{})
	path := "/projects/" + projectID.String() + "/docs/" + d.ID.String()

	etag := getWithHeader(t, r, path, "", "").Header().Get("ETag")
	if !etagHasHash(etag, *d.ContentHash) {
		t.Fatalf("ETag = %q", etag)
	}
	for _, inm := range []string{etag, "W/" + etag, `*`, `"nope", ` + etag} {
		w := getWithHeader(t, r, path, "If-None-Match", inm)
		if w.Code != http.StatusNotModified {
			t.Fatalf("If-None-Match %q: got %d", inm, w.Code)
		}
		if w.Body.Len() != 0 {
			t.Fatalf("304 must have an empty body, got %q", w.Body.String())
		}
		if w.Header().Get("ETag") != etag {
			t.Fatalf("304 must repeat the ETag, got %q", w.Header().Get("ETag"))
		}
		if got := w.Header().Get("Cache-Control"); got != "private, no-cache" {
			t.Fatalf("304 Cache-Control = %q, want %q", got, "private, no-cache")
		}
	}
	// The content hash alone is not the response, so it is not a match.
	if w := getWithHeader(t, r, path, "If-None-Match", `"`+*d.ContentHash+`"`); w.Code != http.StatusOK {
		t.Fatalf("If-None-Match with the bare content hash: got %d, want 200", w.Code)
	}
	if w := getWithHeader(t, r, path, "If-None-Match", `"`+strings.Repeat("0", 64)+`"`); w.Code != http.StatusOK {
		t.Fatalf("stale If-None-Match: got %d", w.Code)
	}
	// A doc in another project is still 404, never 304.
	if w := getWithHeader(t, r, "/projects/"+uuid.NewString()+"/docs/"+d.ID.String(), "If-None-Match", "*"); w.Code != http.StatusNotFound {
		t.Fatalf("other project: got %d", w.Code)
	}
}

func TestUpdateDocument_BadIfMatch400(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	svc := &fakeDocSvc{doc: d, updateDoc: d}
	r := newDocRouterWithNotify(svc, &fakeDocActivitySvc{}, &mockNotificationSvc{})
	req := newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(), map[string]any{"title": "x"})
	req.Header.Set("If-Match", `"7"`)
	w := serve(r, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if svc.lastUpdate != nil {
		t.Fatal("a malformed If-Match must not reach the service")
	}
}

func TestUpdateDocument_StaleIfMatch412(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	by := uuid.New()
	at := time.Date(2026, 11, 14, 10, 30, 0, 0, time.UTC)
	svc := &fakeDocSvc{doc: d, updateErr: &docdom.VersionConflictError{CurrentHash: *d.ContentHash, UpdatedAt: at, UpdatedBy: &by}}
	act := &fakeDocActivitySvc{}
	r := newDocRouterWithNotify(svc, act, &mockNotificationSvc{})
	stale := strings.Repeat("a", 64)
	req := newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(), map[string]any{"title": "x"})
	req.Header.Set("If-Match", `"`+stale+`"`)
	w := serve(r, req)
	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if svc.lastUpdate == nil || svc.lastUpdate.IfMatch == nil || *svc.lastUpdate.IfMatch != stale {
		t.Fatalf("If-Match not passed to the service: %+v", svc.lastUpdate)
	}
	var resp struct {
		ErrorCode string            `json:"error_code"`
		Details   map[string]string `json:"error_details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.ErrorCode != "VERSION_CONFLICT" {
		t.Fatalf("error_code = %q", resp.ErrorCode)
	}
	if resp.Details["content_hash"] != *d.ContentHash || resp.Details["updated_at"] != "2026-11-14T10:30:00Z" || resp.Details["updated_by"] != by.String() {
		t.Fatalf("details = %v", resp.Details)
	}
	if len(act.recorded) != 0 {
		t.Fatal("a refused write must record no activity")
	}
}

func TestListSnapshots_FilterByContentHash(t *testing.T) {
	h := strings.Repeat("b", 64)
	snap := &docdom.DocSnapshot{ID: uuid.New(), SnapshotNumber: 2, ContentHash: &h}
	svc := &fakeDocSvc{snaps: []*docdom.DocSnapshot{snap}}
	r := newDocRouterWithNotify(svc, &fakeDocActivitySvc{}, &mockNotificationSvc{})
	path := "/projects/" + uuid.NewString() + "/docs/" + uuid.NewString() + "/snapshots"

	w := getWithHeader(t, r, path+"?content_hash="+strings.ToUpper(h), "", "")
	if w.Code != http.StatusOK || svc.snapHash == nil || *svc.snapHash != h {
		t.Fatalf("got %d hash=%v: %s", w.Code, svc.snapHash, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"content_hash":"`+h+`"`) {
		t.Fatalf("snapshot must carry its content_hash: %s", w.Body.String())
	}
	svc.snaps = nil
	w = getWithHeader(t, r, path+"?content_hash="+strings.Repeat("c", 64), "", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("unknown hash: got %d: %s", w.Code, w.Body.String())
	}
	svc.snapHash = nil
	if w := getWithHeader(t, r, path, "", ""); w.Code != http.StatusOK || svc.snapHash != nil {
		t.Fatalf("no filter: got %d hash=%v", w.Code, svc.snapHash)
	}
	if w := getWithHeader(t, r, path+"?content_hash=xyz", "", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed hash: got %d", w.Code)
	}
}

func TestUpdateDocument_NoOpRecordsNothing(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	svc := &fakeDocSvc{doc: d, updateDoc: d, updateChange: docdom.DocChange{}}
	act := &fakeDocActivitySvc{}
	notify := &mockNotificationSvc{}
	r := newDocRouterWithNotify(svc, act, notify)
	actor := uuid.New()
	mentioned := uuid.New()
	// The same content with a new block id, plus a mention that is new
	// relative to nothing the service changed: a no-op must not notify.
	content := `[{"id":"new","type":"paragraph","content":[{"type":"text","text":"hi","styles":{}},{"type":"teamMention","props":{"id":"` + mentioned.String() + `","name":"Pat"}}]}]`
	req := newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(), map[string]any{"content": json.RawMessage(content), "title": "Doc"})
	w := serveAs(r, req, actor)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if len(act.recorded) != 0 {
		t.Fatalf("no-op recorded %d activities", len(act.recorded))
	}
	if notify.docMentions != 0 {
		t.Fatalf("no-op sent %d mention notifications", notify.docMentions)
	}
	var resp struct {
		Data struct {
			UpdatedAt time.Time `json:"updated_at"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Data.UpdatedAt.Equal(d.UpdatedAt) {
		t.Fatalf("updated_at = %v, want %v", resp.Data.UpdatedAt, d.UpdatedAt)
	}
	if !etagHasHash(w.Header().Get("ETag"), *d.ContentHash) {
		t.Fatalf("ETag = %q", w.Header().Get("ETag"))
	}

	// A real content change records one activity naming only the content.
	svc.updateChange = docdom.DocChange{Content: true}
	w = serveAs(r, newReq(t, http.MethodPatch, "/projects/"+projectID.String()+"/docs/"+d.ID.String(), map[string]any{"content": json.RawMessage(content), "title": "Doc"}), actor)
	if w.Code != http.StatusOK || len(act.recorded) != 1 || notify.docMentions != 1 {
		t.Fatalf("change: got %d, %d activities, %d mentions", w.Code, len(act.recorded), notify.docMentions)
	}
	var body struct {
		Changes []struct {
			Field string `json:"field"`
		} `json:"changes"`
	}
	_ = json.Unmarshal(act.recorded[0].Content, &body)
	if len(body.Changes) != 1 || body.Changes[0].Field != "content" {
		t.Fatalf("changes = %s", act.recorded[0].Content)
	}
}

func TestCreateDocument_SetsETagAndContentHash(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	svc := &createReturningSvc{fakeDocSvc: &fakeDocSvc{}, created: d}
	h := handler.NewDocumentHandler(svc, &fakeDocActivitySvc{})
	r := chi.NewRouter()
	r.Post("/projects/{projectId}/docs", h.CreateDocument)
	w := serve(r, newReq(t, http.MethodPost, "/projects/"+projectID.String()+"/docs", map[string]any{"title": "Doc", "content": json.RawMessage(etagBlocks)}))
	if w.Code != http.StatusCreated {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if !etagHasHash(w.Header().Get("ETag"), *d.ContentHash) || !strings.Contains(w.Body.String(), `"content_hash":"`+*d.ContentHash+`"`) {
		t.Fatalf("ETag %q body %s", w.Header().Get("ETag"), w.Body.String())
	}
}
