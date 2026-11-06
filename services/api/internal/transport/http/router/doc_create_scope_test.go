package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// A role whose docs:write is limited to one folder creates documents there
// through the real route, and nowhere else. Folder operations still need
// docs:write on the project itself.
func TestDocCreateHonoursFolderScopedPolicy(t *testing.T) {
	token := issueAccessTokenForRouterTests(t)
	project, folder, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
	scoped := allow([]string{"docs:write"}, "project/"+project+"/doc/*")
	scoped.Conditions = iam.Conditions{"StringEquals": {"doc.folder_id": {folder}}}
	grants := []iam.Grant{projectRole(project,
		allow([]string{"projects:read", "docs:read"}, "project/"+project, "project/"+project+"/*"),
		scoped,
	)}
	routes := map[string]routeGates{}
	for _, rt := range walkGates(t, coverageDeps(newTestIAM(&iamUserStore{grants: grants}))) {
		routes[rt.key()] = rt
	}
	call := func(key, body string) int {
		rt, ok := routes[key]
		if !ok {
			t.Fatalf("route %q is not registered", key)
		}
		path := strings.ReplaceAll(rt.pattern, "{projectId}", project)
		req := httptest.NewRequestWithContext(t.Context(), rt.method, concretePath(path), strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		rt.handler.ServeHTTP(rec, req)
		return rec.Code
	}
	const createDoc = "POST /api/v1/projects/{projectId}/docs/"
	const createFolder = "POST /api/v1/projects/{projectId}/docs/folders/"
	cases := []struct {
		name, key, body string
		want            int
	}{
		{"create in its folder", createDoc, `{"title":"x","folder_id":"` + folder + `"}`, http.StatusNoContent},
		{"create in another folder", createDoc, `{"title":"x","folder_id":"` + other + `"}`, http.StatusForbidden},
		{"create at the root", createDoc, `{"title":"x"}`, http.StatusForbidden},
		{"create a folder", createFolder, `{"name":"x"}`, http.StatusForbidden},
	}
	for _, c := range cases {
		if got := call(c.key, c.body); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
