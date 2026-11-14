package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// -- fakeDocSvc by-path methods (configured through byPathSvc) --

func (f *fakeDocSvc) ResolveDocPath(_ context.Context, _ uuid.UUID, path string) (*uuid.UUID, *docdom.Document, error) {
	if f.doc == nil {
		return nil, nil, docdom.ErrDocNotFound
	}
	cp := *f.doc
	return cp.FolderID, &cp, nil
}

func (f *fakeDocSvc) UpsertDocumentByPath(ctx context.Context, in docdom.UpsertByPathInput) (*docdom.Document, docdom.DocChange, bool, error) {
	f.lastUpsert = &in
	if in.Authorize != nil {
		if err := in.Authorize(ctx, f.upsertFolder, f.doc); err != nil {
			return nil, docdom.DocChange{}, false, err
		}
	}
	if f.updateErr != nil {
		return nil, docdom.DocChange{}, false, f.updateErr
	}
	return f.updateDoc, f.updateChange, f.doc == nil, nil
}

// fakeDocAuthz answers every check with allow and records the checks.
type fakeDocAuthz struct {
	allow bool
	calls []string
	attrs []map[string][]string
}

func (a *fakeDocAuthz) Authorize(_ context.Context, _ iam.Principal, action, resource string, _ ...iam.AuthzOption) (iam.Result, error) {
	a.calls = append(a.calls, "authorize "+action+" "+resource)
	return iam.Result{Allowed: a.allow}, nil
}

func (a *fakeDocAuthz) AuthorizeCreate(_ context.Context, _ iam.Principal, action, resource string, attrs map[string][]string) (iam.Result, error) {
	a.calls = append(a.calls, "create "+action+" "+resource)
	a.attrs = append(a.attrs, attrs)
	return iam.Result{Allowed: a.allow}, nil
}

func (a *fakeDocAuthz) AuthorizeChange(_ context.Context, _ iam.Principal, action, resource string, attrs map[string][]string) (iam.Result, error) {
	a.calls = append(a.calls, "change "+action+" "+resource)
	a.attrs = append(a.attrs, attrs)
	return iam.Result{Allowed: a.allow}, nil
}

func newByPathRouter(svc *fakeDocSvc, act *fakeDocActivitySvc, authz *fakeDocAuthz) chi.Router {
	h := handler.NewDocumentHandler(svc, act)
	if authz != nil {
		h = h.WithDocAuthorizer(authz)
	}
	r := chi.NewRouter()
	r.Get("/projects/{projectId}/docs/by-path", h.GetDocumentByPath)
	r.Put("/projects/{projectId}/docs/by-path", h.PutDocumentByPath)
	return r
}

func TestGetByPath_ETag304(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	authz := &fakeDocAuthz{allow: true}
	r := newByPathRouter(&fakeDocSvc{doc: d}, &fakeDocActivitySvc{}, authz)
	url := "/projects/" + projectID.String() + "/docs/by-path?path=Plans/Doc"

	w := serveAs(r, newReq(t, http.MethodGet, url, nil), uuid.New())
	if w.Code != http.StatusOK || !etagHasHash(w.Header().Get("ETag"), *d.ContentHash) || !strings.Contains(w.Body.String(), d.ID.String()) {
		t.Fatalf("get: %d %q %s", w.Code, w.Header().Get("ETag"), w.Body.String())
	}
	if len(authz.calls) != 1 || authz.calls[0] != "authorize docs:read project/"+projectID.String()+"/doc/"+d.ID.String() {
		t.Fatalf("checks: %v", authz.calls)
	}
	req := newReq(t, http.MethodGet, url, nil)
	req.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w2 := serveAs(r, req, uuid.New())
	if w2.Code != http.StatusNotModified || w2.Body.Len() != 0 {
		t.Fatalf("revalidate: %d %q", w2.Code, w2.Body.String())
	}
}

// A document the caller may not read is reported missing, exactly as an
// absent path is.
func TestGetByPath_UnreadableIs404(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	url := "/projects/" + projectID.String() + "/docs/by-path?path=Doc"
	denied := serveAs(newByPathRouter(&fakeDocSvc{doc: d}, &fakeDocActivitySvc{}, &fakeDocAuthz{}), newReq(t, http.MethodGet, url, nil), uuid.New())
	missing := serveAs(newByPathRouter(&fakeDocSvc{}, &fakeDocActivitySvc{}, &fakeDocAuthz{allow: true}), newReq(t, http.MethodGet, url, nil), uuid.New())
	if denied.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("denied %d, missing %d", denied.Code, missing.Code)
	}
	var a, b map[string]any
	_ = json.Unmarshal(denied.Body.Bytes(), &a)
	_ = json.Unmarshal(missing.Body.Bytes(), &b)
	if a["error_code"] != b["error_code"] || a["error"] != b["error"] {
		t.Fatalf("denied %v vs missing %v", a, b)
	}
	if strings.Contains(denied.Body.String(), d.ID.String()) || denied.Header().Get("ETag") != "" {
		t.Fatal("a denied read leaked the document")
	}
}

func TestPutByPath_CreatesWith201AndChecksCreate(t *testing.T) {
	projectID := uuid.New()
	folder := uuid.New()
	created := etagDoc(projectID, true)
	created.FolderID = &folder
	svc := &fakeDocSvc{updateDoc: created, upsertFolder: &folder}
	act := &fakeDocActivitySvc{}
	authz := &fakeDocAuthz{allow: true}
	r := newByPathRouter(svc, act, authz)

	body := map[string]any{"path": "Plans/Doc", "content": json.RawMessage(etagBlocks),
		"source": map[string]string{"repository": "paca", "path": "docs/x.md", "commit": "abc"}, "lock": true}
	w := serveAs(r, newReq(t, http.MethodPut, "/projects/"+projectID.String()+"/docs/by-path", body), uuid.New())
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"changed":true`) || w.Header().Get("ETag") == "" {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if len(authz.calls) != 1 || authz.calls[0] != "create docs:write project/"+projectID.String()+"/doc/*" ||
		len(authz.attrs[0]["doc.folder_id"]) != 1 || authz.attrs[0]["doc.folder_id"][0] != folder.String() {
		t.Fatalf("checks %v attrs %v", authz.calls, authz.attrs)
	}
	in := svc.lastUpsert
	if in.Path != "Plans/Doc" || in.Source == nil || in.Source.Repository != "paca" || in.Lock == nil || !*in.Lock {
		t.Fatalf("input: %+v", in)
	}
	if len(act.recorded) != 1 || act.recorded[0].ActivityType != docdom.ActivityTypeDocCreated {
		t.Fatalf("activities: %+v", act.recorded)
	}
}

func TestPutByPath_UpdateAndUnchanged(t *testing.T) {
	projectID := uuid.New()
	d := etagDoc(projectID, true)
	svc := &fakeDocSvc{doc: d, updateDoc: d, updateChange: docdom.DocChange{Source: true}}
	act := &fakeDocActivitySvc{}
	authz := &fakeDocAuthz{allow: true}
	r := newByPathRouter(svc, act, authz)
	url := "/projects/" + projectID.String() + "/docs/by-path"
	body := map[string]any{"path": "Doc", "content": json.RawMessage(etagBlocks)}

	// A source-only change: 200, changed false, no activity.
	w := serveAs(r, newReq(t, http.MethodPut, url, body), uuid.New())
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"changed":false`) || len(act.recorded) != 0 {
		t.Fatalf("unchanged: %d %s %d activities", w.Code, w.Body.String(), len(act.recorded))
	}
	if authz.calls[0] != "change docs:write project/"+projectID.String()+"/doc/"+d.ID.String() {
		t.Fatalf("checks: %v", authz.calls)
	}

	// A content change: 200, changed true, one doc.updated naming content.
	svc.updateChange = docdom.DocChange{Content: true}
	w = serveAs(r, newReq(t, http.MethodPut, url, body), uuid.New())
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"changed":true`) || len(act.recorded) != 1 ||
		act.recorded[0].ActivityType != docdom.ActivityTypeDocUpdated {
		t.Fatalf("changed: %d %s %+v", w.Code, w.Body.String(), act.recorded)
	}
}

func TestPutByPath_DeniedAndBadRequests(t *testing.T) {
	projectID := uuid.New()
	url := "/projects/" + projectID.String() + "/docs/by-path"
	svc := &fakeDocSvc{updateDoc: etagDoc(projectID, true)}

	w := serveAs(newByPathRouter(svc, &fakeDocActivitySvc{}, &fakeDocAuthz{}), newReq(t, http.MethodPut, url, map[string]any{"path": "x", "content": json.RawMessage(`[]`)}), uuid.New())
	if w.Code != http.StatusForbidden {
		t.Fatalf("denied: %d %s", w.Code, w.Body.String())
	}
	allow := &fakeDocAuthz{allow: true}
	for name, body := range map[string]any{
		"no content":          map[string]any{"path": "x"},
		"string content":      map[string]any{"path": "x", "content": "hi"},
		"lock without source": map[string]any{"path": "x", "content": json.RawMessage(`[]`), "lock": true},
	} {
		w := serveAs(newByPathRouter(svc, &fakeDocActivitySvc{}, allow), newReq(t, http.MethodPut, url, body), uuid.New())
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	for _, err := range []error{docdom.ErrDocPathInvalid, docdom.ErrDocPathIsFolder, docdom.ErrDocSourceInvalid} {
		svc := &fakeDocSvc{updateErr: err}
		w := serveAs(newByPathRouter(svc, &fakeDocActivitySvc{}, allow), newReq(t, http.MethodPut, url, map[string]any{"path": "x", "content": json.RawMessage(`[]`)}), uuid.New())
		want := map[error]int{docdom.ErrDocPathInvalid: 400, docdom.ErrDocPathIsFolder: 409, docdom.ErrDocSourceInvalid: 400}[err]
		if w.Code != want {
			t.Errorf("%v: %d, want %d", err, w.Code, want)
		}
	}
	// No authorizer configured: refuse.
	w = serveAs(newByPathRouter(svc, &fakeDocActivitySvc{}, nil), newReq(t, http.MethodPut, url, map[string]any{"path": "x", "content": json.RawMessage(`[]`)}), uuid.New())
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("no authorizer: %d", w.Code)
	}
}
