package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// fakeChangesSvc records what the handler asked for and under which scope.
type fakeChangesSvc struct {
	set    *docdom.ChangeSet
	docs   []*docdom.Document
	err    error
	called bool

	gotProject uuid.UUID
	gotCursor  string
	gotIDs     []uuid.UUID
	gotScope   *iam.Node
	hasScope   bool
}

func (f *fakeChangesSvc) Changes(ctx context.Context, projectID uuid.UUID, cursor string) (*docdom.ChangeSet, error) {
	f.called, f.gotProject, f.gotCursor = true, projectID, cursor
	f.gotScope, f.hasScope = iam.ScopeFrom(ctx, "doc")
	return f.set, f.err
}

func (f *fakeChangesSvc) BatchGet(ctx context.Context, projectID uuid.UUID, ids []uuid.UUID) ([]*docdom.Document, error) {
	f.called, f.gotProject, f.gotIDs = true, projectID, ids
	f.gotScope, f.hasScope = iam.ScopeFrom(ctx, "doc")
	if len(ids) > docdom.MaxBatchGetIDs {
		return nil, docdom.ErrTooManyDocIDs
	}
	return f.docs, f.err
}

var _ docdom.ChangesService = (*fakeChangesSvc)(nil)

func newChangesRouter(svc *fakeChangesSvc, scoper handler.ListScoper) chi.Router {
	h := handler.NewDocumentHandler(&fakeDocSvc{}, &fakeDocActivitySvc{}).WithDocChanges(svc)
	if scoper != nil {
		h = h.WithDocListScoper(scoper)
	}
	r := chi.NewRouter()
	r.Use(claimsMiddleware(uuid.NewString()))
	r.Get("/projects/{projectId}/docs/changes", h.ListChanges)
	r.Post("/projects/{projectId}/docs/batch-get", h.BatchGetDocuments)
	return r
}

func serveChanges(r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		if s, ok := body.(string); ok {
			buf.WriteString(s)
		} else {
			_ = json.NewEncoder(&buf).Encode(body)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), method, path, &buf))
	return rec
}

// decodeData returns the "data" object of the response envelope, raw.
func decodeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var env struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	return env.Data
}

func TestListChanges_ResponseShape(t *testing.T) {
	projectID, folderID := uuid.New(), uuid.New()
	hash := strings.Repeat("ab", 32)
	updated := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	deletedAt := updated.Add(time.Hour)
	live := &docdom.Document{ID: uuid.New(), ProjectID: projectID, FolderID: &folderID, Title: "Plan", ContentHash: &hash, UpdatedAt: updated}
	gone := &docdom.Document{ID: uuid.New(), ProjectID: projectID, FolderID: &folderID, Title: "Secret title", ContentHash: &hash, UpdatedAt: updated, DeletedAt: &deletedAt}
	svc := &fakeChangesSvc{set: &docdom.ChangeSet{
		Cursor: "next", Docs: []*docdom.Document{live, gone},
		FoldersIncluded: true, Folders: []*docdom.DocFolder{{ID: folderID, ProjectID: projectID, Name: "Plans"}},
	}}
	scope := &iam.Node{Kind: iam.NodeCond, Key: "resource.id", Op: "In", Values: iam.ValueList{"d1"}}

	rec := serveChanges(newChangesRouter(svc, &fakeScoper{node: scope}), http.MethodGet, "/projects/"+projectID.String()+"/docs/changes?since=abc", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if svc.gotProject != projectID || svc.gotCursor != "abc" {
		t.Fatalf("service got project %s cursor %q", svc.gotProject, svc.gotCursor)
	}
	if !svc.hasScope || svc.gotScope != scope {
		t.Fatal("the caller's doc scope was not handed to the service")
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}

	data := decodeData(t, rec)
	var docs []map[string]any
	if err := json.Unmarshal(data["docs"], &docs); err != nil || len(docs) != 2 {
		t.Fatalf("docs = %s (%v)", data["docs"], err)
	}
	wantLive := map[string]any{
		"id": live.ID.String(), "folder_id": folderID.String(), "title": "Plan",
		"content_hash": hash, "updated_at": "2026-11-14T10:00:00Z", "deleted": false,
	}
	for k, v := range wantLive {
		if docs[0][k] != v {
			t.Errorf("live doc %s = %v, want %v", k, docs[0][k], v)
		}
	}
	if len(docs[0]) != len(wantLive) {
		t.Errorf("live doc has fields %v, want exactly %d", docs[0], len(wantLive))
	}
	// A tombstone says which document is gone and nothing else about it.
	wantGone := map[string]any{
		"id": gone.ID.String(), "folder_id": nil, "title": "",
		"content_hash": nil, "updated_at": "2026-11-14T11:00:00Z", "deleted": true,
	}
	for k, v := range wantGone {
		if docs[1][k] != v {
			t.Errorf("tombstone %s = %v, want %v", k, docs[1][k], v)
		}
	}
	var folders []map[string]any
	if err := json.Unmarshal(data["folders"], &folders); err != nil || len(folders) != 1 {
		t.Fatalf("folders = %s", data["folders"])
	}
	if folders[0]["id"] != folderID.String() || folders[0]["name"] != "Plans" || folders[0]["parent_id"] != nil || len(folders[0]) != 3 {
		t.Errorf("folder = %v, want id, parent_id null and name only", folders[0])
	}
	if string(data["cursor"]) != `"next"` || string(data["reset"]) != "false" {
		t.Errorf("cursor %s reset %s", data["cursor"], data["reset"])
	}
}

func TestListChanges_FoldersNullVersusEmpty(t *testing.T) {
	projectID := uuid.New()
	path := "/projects/" + projectID.String() + "/docs/changes"

	unchanged := &fakeChangesSvc{set: &docdom.ChangeSet{Cursor: "c"}}
	data := decodeData(t, serveChanges(newChangesRouter(unchanged, nil), http.MethodGet, path, nil))
	if string(data["folders"]) != "null" || string(data["docs"]) != "[]" {
		t.Fatalf("nothing changed: folders %s docs %s, want null and []", data["folders"], data["docs"])
	}
	if unchanged.gotCursor != "" {
		t.Fatalf("cursor = %q without a since parameter", unchanged.gotCursor)
	}

	// A reset in a project without folders: the tree is known to be empty.
	empty := &fakeChangesSvc{set: &docdom.ChangeSet{Cursor: "c", Reset: true, FoldersIncluded: true}}
	data = decodeData(t, serveChanges(newChangesRouter(empty, nil), http.MethodGet, path, nil))
	if string(data["folders"]) != "[]" || string(data["reset"]) != "true" {
		t.Fatalf("reset without folders: folders %s reset %s, want [] and true", data["folders"], data["reset"])
	}
}

func TestListChanges_ScopeErrorFailsClosed(t *testing.T) {
	svc := &fakeChangesSvc{set: &docdom.ChangeSet{}}
	r := newChangesRouter(svc, &fakeScoper{err: errors.New("db down")})
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/docs/changes", nil},
		{http.MethodPost, "/docs/batch-get", map[string]any{"ids": []string{uuid.NewString()}}},
	} {
		rec := serveChanges(r, req.method, "/projects/"+uuid.NewString()+req.path, req.body)
		if rec.Code != http.StatusInternalServerError || svc.called {
			t.Fatalf("%s %s: status %d, service called %v; want 500 and no call", req.method, req.path, rec.Code, svc.called)
		}
	}
}

func TestBatchGet_ScopedAndCapped(t *testing.T) {
	projectID := uuid.New()
	path := "/projects/" + projectID.String() + "/docs/batch-get"
	content := json.RawMessage(`[{"id":"a","type":"paragraph","content":[{"type":"text","text":"hi","styles":{}}]}]`)
	doc := &docdom.Document{ID: uuid.New(), ProjectID: projectID, Title: "Plan", Content: content}
	scope := &iam.Node{Kind: iam.NodeCond, Key: "resource.id", Op: "In", Values: iam.ValueList{doc.ID.String()}}

	t.Run("returns the documents with content and hash, under the caller's scope", func(t *testing.T) {
		svc := &fakeChangesSvc{docs: []*docdom.Document{doc}}
		hidden := uuid.New()
		rec := serveChanges(newChangesRouter(svc, &fakeScoper{node: scope}), http.MethodPost, path, map[string]any{"ids": []string{doc.ID.String(), hidden.String()}})
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		if !svc.hasScope || svc.gotScope != scope || svc.gotProject != projectID {
			t.Fatal("the caller's doc scope or the project was not handed to the service")
		}
		if len(svc.gotIDs) != 2 || svc.gotIDs[0] != doc.ID || svc.gotIDs[1] != hidden {
			t.Fatalf("service got ids %v", svc.gotIDs)
		}
		var items []map[string]any
		if err := json.Unmarshal(decodeData(t, rec)["items"], &items); err != nil || len(items) != 1 {
			t.Fatalf("items = %s", rec.Body)
		}
		if items[0]["id"] != doc.ID.String() || items[0]["content_hash"] != docdom.ContentHash(content) || items[0]["content"] == nil {
			t.Fatalf("item = %v, want the document with its content and content_hash", items[0])
		}
	})

	t.Run("101 ids is a 400", func(t *testing.T) {
		ids := make([]string, docdom.MaxBatchGetIDs+1)
		for i := range ids {
			ids[i] = uuid.NewString()
		}
		svc := &fakeChangesSvc{}
		rec := serveChanges(newChangesRouter(svc, nil), http.MethodPost, path, map[string]any{"ids": ids})
		if rec.Code != http.StatusBadRequest || svc.called {
			t.Fatalf("status %d, service called %v; want 400 and no call", rec.Code, svc.called)
		}
		rec = serveChanges(newChangesRouter(svc, nil), http.MethodPost, path, map[string]any{"ids": ids[:docdom.MaxBatchGetIDs]})
		if rec.Code != http.StatusOK {
			t.Fatalf("100 ids: status %d: %s", rec.Code, rec.Body)
		}
	})

	t.Run("bad input is a 400", func(t *testing.T) {
		for name, body := range map[string]any{
			"not a uuid":  map[string]any{"ids": []string{"nope"}},
			"not a list":  `{"ids": "` + uuid.NewString() + `"}`,
			"not json":    `{`,
			"wrong types": `{"ids": [1, 2]}`,
		} {
			svc := &fakeChangesSvc{}
			rec := serveChanges(newChangesRouter(svc, nil), http.MethodPost, path, body)
			if rec.Code != http.StatusBadRequest || svc.called {
				t.Errorf("%s: status %d, service called %v; want 400 and no call", name, rec.Code, svc.called)
			}
		}
	})

	t.Run("no ids is an empty result", func(t *testing.T) {
		svc := &fakeChangesSvc{}
		rec := serveChanges(newChangesRouter(svc, nil), http.MethodPost, path, map[string]any{"ids": []string{}})
		if rec.Code != http.StatusOK || string(decodeData(t, rec)["items"]) != "[]" {
			t.Fatalf("status %d body %s; want 200 and an empty list", rec.Code, rec.Body)
		}
	})
}
