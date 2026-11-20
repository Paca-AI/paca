package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

type fakeAreas struct {
	area map[uuid.UUID]string
	docs map[uuid.UUID]bool
}

func (f fakeAreas) FileNodeArea(_ context.Context, id uuid.UUID) (string, bool, error) {
	return f.area[id], f.docs[id], nil
}

// A file manager request needs the docs action only where it touches the
// Documentation subtree, and for both ends of a move.
func TestRequireFileDocsArea(t *testing.T) {
	project, user := uuid.New(), uuid.New()
	inDocs, elsewhere, otherDocs := uuid.New(), uuid.New(), uuid.New()
	doc := uuid.New()
	areas := fakeAreas{area: map[uuid.UUID]string{inDocs: FileAreaDocs, otherDocs: FileAreaDocs, doc: FileAreaDocs}, docs: map[uuid.UUID]bool{doc: true}}
	principal := iam.Principal{Type: "user", ID: user.String()}
	grant := func(actions ...string) map[iam.Principal][]iam.Grant {
		return map[iam.Principal][]iam.Grant{principal: {projectGrant(project.String(), iam.Statement{
			Effect: iam.EffectAllow, Actions: actions, Resources: []string{"project/" + project.String() + "/*"},
		})}}
	}
	path := func(id uuid.UUID) string { return "/projects/" + project.String() + "/files/nodes/" + id.String() }

	cases := []struct {
		name   string
		grants map[iam.Principal][]iam.Grant
		method string
		url    string
		body   string
		action iam.Action
		target []FileTarget
		want   int
	}{
		{"node outside docs needs no docs action", grant("files:read"), "GET", path(elsewhere), "", "docs:read", []FileTarget{FileURLNode("nodeId")}, 204},
		{"node in docs without docs:read", grant("files:read"), "GET", path(inDocs), "", "docs:read", []FileTarget{FileURLNode("nodeId")}, 403},
		{"node in docs with docs:read", grant("files:read", "docs:read"), "GET", path(inDocs), "", "docs:read", []FileTarget{FileURLNode("nodeId")}, 204},
		{"unknown node is left to the service", grant("files:read"), "GET", path(uuid.New()), "", "docs:read", []FileTarget{FileURLNode("nodeId")}, 204},
		{"move into docs needs docs:write for the destination", grant("files:write", "docs:read"), "PATCH", path(elsewhere), `{"move":true,"parent_id":"` + inDocs.String() + `"}`, "docs:write",
			[]FileTarget{FileURLNode("nodeId"), FileBodyField("parent_id")}, 403},
		{"move out of docs needs docs:write for the source", grant("files:write"), "PATCH", path(inDocs), `{"move":true,"parent_id":"` + elsewhere.String() + `"}`, "docs:write",
			[]FileTarget{FileURLNode("nodeId"), FileBodyField("parent_id")}, 403},
		{"move within docs with docs:write", grant("files:write", "docs:write"), "PATCH", path(inDocs), `{"move":true,"parent_id":"` + otherDocs.String() + `"}`, "docs:write",
			[]FileTarget{FileURLNode("nodeId"), FileBodyField("parent_id")}, 204},
		{"no parent means the root", grant("files:read"), "GET", "/projects/" + project.String() + "/files/nodes", "", "docs:read", []FileTarget{FileQueryParent}, 204},
		{"listing a docs folder", grant("files:read"), "GET", "/projects/" + project.String() + "/files/nodes?parent_id=" + inDocs.String(), "", "docs:read", []FileTarget{FileQueryParent}, 403},
		{"trailing data in the body is refused", grant("files:write", "docs:write"), "POST", "/projects/" + project.String() + "/files/folders", `{"parent_id":null} {"parent_id":"` + inDocs.String() + `"}`, "docs:write",
			[]FileTarget{FileBodyField("parent_id")}, 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newFakeIAM(&fakeIAMStore{grants: tc.grants})
			r := chi.NewRouter()
			h := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
			gate := RequireFileDocsArea(a, areas, tc.action, "projectId", tc.target...)
			r.With(asCaller(user, uuid.Nil), gate).MethodFunc(tc.method, "/projects/{projectId}/files/nodes/{nodeId}", h)
			r.With(asCaller(user, uuid.Nil), gate).MethodFunc(tc.method, "/projects/{projectId}/files/nodes", h)
			r.With(asCaller(user, uuid.Nil), gate).MethodFunc(tc.method, "/projects/{projectId}/files/folders", h)
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.url, strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestRequireFileDocsArea_NoLookupFailsClosed(t *testing.T) {
	project, user, node := uuid.New(), uuid.New(), uuid.New()
	a := newFakeIAM(&fakeIAMStore{})
	r := chi.NewRouter()
	r.With(asCaller(user, uuid.Nil), RequireFileDocsArea(a, nil, "docs:read", "projectId", FileURLNode("nodeId"))).
		Get("/projects/{projectId}/files/nodes/{nodeId}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/projects/"+project.String()+"/files/nodes/"+node.String(), nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rec.Code)
	}
}

// A document is authorized as that document: a Deny on it applies to the file
// routes that name it, as on the docs routes, while the rest of Documentation
// stays readable.
func TestRequireFileDocsArea_DocumentIsAuthorizedAsTheDocument(t *testing.T) {
	project, user := uuid.New(), uuid.New()
	secret, other, folder := uuid.New(), uuid.New(), uuid.New()
	areas := fakeAreas{
		area: map[uuid.UUID]string{secret: FileAreaDocs, other: FileAreaDocs, folder: FileAreaDocs},
		docs: map[uuid.UUID]bool{secret: true, other: true},
	}
	principal := iam.Principal{Type: "user", ID: user.String()}
	prefix := "project/" + project.String()
	grants := map[iam.Principal][]iam.Grant{principal: {projectGrant(project.String(),
		iam.Statement{Effect: iam.EffectAllow, Actions: []string{"docs:read"}, Resources: []string{prefix + "/*"}},
		iam.Statement{Effect: iam.EffectDeny, Actions: []string{"docs:read"}, Resources: []string{prefix + "/doc/" + secret.String()}},
	)}}
	for id, want := range map[uuid.UUID]int{secret: 403, other: 204, folder: 204} {
		a := newFakeIAM(&fakeIAMStore{grants: grants})
		r := chi.NewRouter()
		r.With(asCaller(user, uuid.Nil), RequireFileDocsArea(a, areas, "docs:read", "projectId", FileURLNode("nodeId"))).
			Get("/projects/{projectId}/files/nodes/{nodeId}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), "GET", "/projects/"+project.String()+"/files/nodes/"+id.String(), nil))
		if rec.Code != want {
			t.Errorf("node %s: got %d, want %d (%s)", id, rec.Code, want, rec.Body.String())
		}
	}
}
