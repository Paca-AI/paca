package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// attrLoader serves the current attributes of existing resources and, as an
// expander, derives a document's ancestor folders from its folder.
type attrLoader struct {
	kind      string
	data      map[string]map[string][]string
	ancestors map[string][]string // folder -> folder + ancestors
}

func (l attrLoader) Kind() string { return l.kind }

func (l attrLoader) Load(_ context.Context, id, _ string, _ []string) (map[string][]string, error) {
	d, ok := l.data[id]
	if !ok {
		return nil, iam.ErrResourceNotInProject
	}
	return d, nil
}

func (l attrLoader) Expand(_ context.Context, _, _ string, attrs map[string][]string) (map[string][]string, error) {
	out := map[string][]string{}
	for k, v := range attrs {
		out[k] = v
	}
	if f := attrs["doc.folder_id"]; len(f) == 1 {
		out["doc.ancestor_folder_ids"] = l.ancestors[f[0]]
	} else {
		out["doc.ancestor_folder_ids"] = nil
	}
	return out, nil
}

type fakeMembers map[uuid.UUID]string

func (m fakeMembers) MemberPrincipalID(_ context.Context, id uuid.UUID) (string, error) {
	if p, ok := m[id]; ok {
		return p, nil
	}
	return "", http.ErrNoLocation
}

func serveBody(t *testing.T, method, pattern, path, body string, mw func(http.Handler) http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	var gotBody string
	r := chi.NewRouter()
	r.Use(asCaller(uuid.MustParse("00000000-0000-0000-0000-0000000000aa"), uuid.Nil))
	r.With(mw).MethodFunc(method, pattern, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body)))
	if rec.Code == http.StatusNoContent && gotBody != body {
		t.Errorf("handler saw body %q, want it restored as %q", gotBody, body)
	}
	return rec
}

func TestRequireRequestAttrs_Tasks(t *testing.T) {
	project, s5, s6 := uuid.NewString(), uuid.NewString(), uuid.NewString()
	tIn5, tIn6 := uuid.NewString(), uuid.NewString()
	grant := projectGrant(project, iam.Statement{
		Effect: iam.EffectAllow, Actions: []string{"tasks:write"}, Resources: []string{"project/*/task/*"},
		Conditions: iam.Conditions{"StringEquals": {"task.sprint_id": {s5}}},
	})
	user := iam.User("00000000-0000-0000-0000-0000000000aa")
	store := &fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: {grant}}}
	a := newFakeIAM(store)
	a.RegisterLoader(attrLoader{kind: "task", data: map[string]map[string][]string{
		tIn5: {"task.sprint_id": {s5}}, tIn6: {"task.sprint_id": {s6}},
	}})

	create := RequireRequestAttrs(a, iam.ActionTasksWrite, ProjectChildCollection("projectId", "task"), TaskAttrs(nil), true)
	change := RequireRequestAttrs(a, iam.ActionTasksWrite, ProjectChildResource("projectId", "task", "taskId"), TaskAttrs(nil), false)
	base := "/projects/" + project + "/tasks"

	cases := []struct {
		name, method, pattern, path, body string
		mw                                func(http.Handler) http.Handler
		want                              int
	}{
		{"create in the allowed sprint", "POST", "/projects/{projectId}/tasks", base, `{"title":"t","sprint_id":"` + s5 + `"}`, create, 204},
		{"create in another sprint", "POST", "/projects/{projectId}/tasks", base, `{"title":"t","sprint_id":"` + s6 + `"}`, create, 403},
		{"create with no sprint at all", "POST", "/projects/{projectId}/tasks", base, `{"title":"t"}`, create, 403},
		{"create with sprint null", "POST", "/projects/{projectId}/tasks", base, `{"title":"t","sprint_id":null}`, create, 403},
		{"edit title, no attribute", "PATCH", "/projects/{projectId}/tasks/{taskId}", base + "/" + tIn5, `{"title":"x"}`, change, 204},
		{"keep the sprint", "PATCH", "/projects/{projectId}/tasks/{taskId}", base + "/" + tIn5, `{"sprint_id":"` + s5 + `"}`, change, 204},
		{"move out of the allowed sprint", "PATCH", "/projects/{projectId}/tasks/{taskId}", base + "/" + tIn5, `{"sprint_id":"` + s6 + `"}`, change, 403},
		{"move to backlog (null)", "PATCH", "/projects/{projectId}/tasks/{taskId}", base + "/" + tIn5, `{"sprint_id":null}`, change, 403},
		{"pull a task in from another sprint", "PATCH", "/projects/{projectId}/tasks/{taskId}", base + "/" + tIn6, `{"sprint_id":"` + s5 + `"}`, change, 403},
		{"malformed uuid is a 400", "PATCH", "/projects/{projectId}/tasks/{taskId}", base + "/" + tIn5, `{"sprint_id":"nope"}`, change, 400},
		{"not an object is left to the handler", "PATCH", "/projects/{projectId}/tasks/{taskId}", base + "/" + tIn5, `[1]`, change, 204},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := serveBody(t, c.method, c.pattern, c.path, c.body, c.mw); rec.Code != c.want {
				t.Fatalf("got %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}

	t.Run("a store failure is a 500, never an allow", func(t *testing.T) {
		bad := newFakeIAM(&fakeIAMStore{err: context.DeadlineExceeded})
		mw := RequireRequestAttrs(bad, iam.ActionTasksWrite, ProjectChildCollection("projectId", "task"), TaskAttrs(nil), true)
		if rec := serveBody(t, "POST", "/projects/{projectId}/tasks", base, `{"sprint_id":"`+s5+`"}`, mw); rec.Code != http.StatusInternalServerError {
			t.Fatalf("got %d", rec.Code)
		}
	})
}

func TestRequireRequestAttrs_TaskAssignees(t *testing.T) {
	project, me, other := uuid.NewString(), "00000000-0000-0000-0000-0000000000aa", uuid.NewString()
	myMember, otherMember := uuid.New(), uuid.New()
	// May write only tasks assigned to themself.
	grant := projectGrant(project, iam.Statement{
		Effect: iam.EffectAllow, Actions: []string{"tasks:write"}, Resources: []string{"project/*/task/*"},
		Conditions: iam.Conditions{"StringEquals": {"task.assignee_id": {me}}},
	})
	a := newFakeIAM(&fakeIAMStore{grants: map[iam.Principal][]iam.Grant{iam.User(me): {grant}}})
	members := fakeMembers{myMember: me, otherMember: other}
	create := RequireRequestAttrs(a, iam.ActionTasksWrite, ProjectChildCollection("projectId", "task"), TaskAttrs(members), true)
	path := "/projects/" + project + "/tasks"

	if rec := serveBody(t, "POST", "/projects/{projectId}/tasks", path, `{"assignee_ids":["`+myMember.String()+`"]}`, create); rec.Code != 204 {
		t.Fatalf("assigned to self: %d %s", rec.Code, rec.Body)
	}
	if rec := serveBody(t, "POST", "/projects/{projectId}/tasks", path, `{"assignee_ids":["`+otherMember.String()+`"]}`, create); rec.Code != 403 {
		t.Fatalf("assigned to someone else: %d", rec.Code)
	}
	if rec := serveBody(t, "POST", "/projects/{projectId}/tasks", path, `{"assignee_ids":[]}`, create); rec.Code != 403 {
		t.Fatalf("unassigned: %d", rec.Code)
	}
}

func TestRequireRequestAttrs_DocFolders(t *testing.T) {
	project, abc, sub, out := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	inABC := uuid.NewString()
	grant := projectGrant(project, iam.Statement{
		Effect: iam.EffectAllow, Actions: []string{"docs:write"}, Resources: []string{"project/*/doc/*"},
		Conditions: iam.Conditions{"In": {"doc.ancestor_folder_ids": {abc}}},
	})
	user := iam.User("00000000-0000-0000-0000-0000000000aa")
	a := newFakeIAM(&fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: {grant}}})
	a.RegisterLoader(attrLoader{
		kind:      "doc",
		data:      map[string]map[string][]string{inABC: {"doc.folder_id": {abc}, "doc.ancestor_folder_ids": {abc}}},
		ancestors: map[string][]string{abc: {abc}, sub: {sub, abc}, out: {out}},
	})
	create := RequireRequestAttrs(a, iam.ActionDocsWrite, ProjectChildCollection("projectId", "doc"), DocAttrs, true)
	move := RequireRequestAttrs(a, iam.ActionDocsWrite, ProjectChildResource("projectId", "doc", "docId"), DocAttrs, false)
	base := "/projects/" + project + "/docs"

	for _, c := range []struct {
		name, method, pattern, path, body string
		mw                                func(http.Handler) http.Handler
		want                              int
	}{
		{"create in a subfolder", "POST", "/projects/{projectId}/docs", base, `{"folder_id":"` + sub + `"}`, create, 204},
		{"create elsewhere", "POST", "/projects/{projectId}/docs", base, `{"folder_id":"` + out + `"}`, create, 403},
		{"create at the root", "POST", "/projects/{projectId}/docs", base, `{}`, create, 403},
		{"move into a subfolder", "PATCH", "/projects/{projectId}/docs/{docId}", base + "/" + inABC, `{"folder_id":"` + sub + `"}`, move, 204},
		{"move out", "PATCH", "/projects/{projectId}/docs/{docId}", base + "/" + inABC, `{"folder_id":"` + out + `"}`, move, 403},
		{"move to the root", "PATCH", "/projects/{projectId}/docs/{docId}", base + "/" + inABC, `{"folder_id":null}`, move, 403},
		{"edit content only", "PATCH", "/projects/{projectId}/docs/{docId}", base + "/" + inABC, `{"title":"x"}`, move, 204},
	} {
		t.Run(c.name, func(t *testing.T) {
			if rec := serveBody(t, c.method, c.pattern, c.path, c.body, c.mw); rec.Code != c.want {
				t.Fatalf("got %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}
}

type fakeTaskNumbers map[int64]uuid.UUID

func (f fakeTaskNumbers) FindTaskByNumber(_ context.Context, _ uuid.UUID, n int64) (*taskdom.Task, error) {
	id, ok := f[n]
	if !ok {
		return nil, taskdom.ErrTaskNotFound
	}
	return &taskdom.Task{ID: id}, nil
}

func TestTaskByNumberResource(t *testing.T) {
	project, task := uuid.NewString(), uuid.New()
	lookup := fakeTaskNumbers{7: task}
	cases := []struct{ number, want string }{
		{"7", "project/" + project + "/task/" + task.String()},
		{"8", "project/" + project},   // unknown number: needs project access, handler 404s
		{"abc", "project/" + project}, // not a number: same
	}
	for _, c := range cases {
		r := chi.NewRouter()
		var got string
		r.Get("/projects/{projectId}/tasks/by-number/{taskNumber}", func(_ http.ResponseWriter, req *http.Request) {
			got, _ = TaskByNumberResource(lookup, "projectId", "taskNumber")(req)
		})
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/"+project+"/tasks/by-number/"+c.number, nil))
		if got != c.want {
			t.Errorf("number %q: resource %q, want %q", c.number, got, c.want)
		}
	}
}

func TestViewCreateAndReorderGates(t *testing.T) {
	project, s5, s6 := uuid.NewString(), uuid.NewString(), uuid.NewString()
	vIn5a, vIn5b, vIn6, vBacklog := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	grant := projectGrant(project, iam.Statement{
		Effect: iam.EffectAllow, Actions: []string{"views:write"}, Resources: []string{"project/*/view/*"},
		Conditions: iam.Conditions{"StringEquals": {"view.sprint_id": {s5}}},
	})
	user := iam.User("00000000-0000-0000-0000-0000000000aa")
	a := newFakeIAM(&fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: {grant}}})
	a.RegisterLoader(attrLoader{kind: "view", data: map[string]map[string][]string{
		vIn5a: {"view.sprint_id": {s5}}, vIn5b: {"view.sprint_id": {s5}}, vIn6: {"view.sprint_id": {s6}}, vBacklog: {},
	}})
	create := RequireRequestAttrsFromRequest(a, iam.ActionViewsWrite, ProjectChildCollection("projectId", "view"), ViewAttrs, true)
	reorder := RequireViewIDs(a, iam.ActionViewsWrite, "projectId")
	base := "/projects/" + project + "/views"
	pattern := "/projects/{projectId}/views"

	for _, c := range []struct {
		name, method, pattern, path, body string
		mw                                func(http.Handler) http.Handler
		want                              int
	}{
		{"create in the allowed sprint", "POST", pattern, base + "?context=sprint&sprint_id=" + s5, `{"name":"v"}`, create, 204},
		{"context defaults to sprint", "POST", pattern, base + "?sprint_id=" + s5, `{"name":"v"}`, create, 204},
		{"create in another sprint", "POST", pattern, base + "?context=sprint&sprint_id=" + s6, `{"name":"v"}`, create, 403},
		{"create a backlog view", "POST", pattern, base + "?context=backlog", `{"name":"v"}`, create, 403},
		{"create a timeline view", "POST", pattern, base + "?context=timeline", `{"name":"v"}`, create, 403},
		{"sprint context without a sprint (handler answers 400)", "POST", pattern, base + "?context=sprint", `{"name":"v"}`, create, 403},
		{"malformed sprint", "POST", pattern, base + "?sprint_id=nope", `{"name":"v"}`, create, 403},
		{"invalid context", "POST", pattern, base + "?context=bogus", `{"name":"v"}`, create, 403},
		{"a body sprint_id is ignored", "POST", pattern, base + "?sprint_id=" + s6, `{"name":"v","sprint_id":"` + s5 + `"}`, create, 403},
		{"reorder views of the allowed sprint", "PUT", pattern + "/positions", base + "/positions", `{"view_ids":["` + vIn5a + `","` + vIn5b + `"]}`, reorder, 204},
		{"reorder naming one view of another sprint", "PUT", pattern + "/positions", base + "/positions", `{"view_ids":["` + vIn5a + `","` + vIn6 + `"]}`, reorder, 403},
		{"reorder naming a backlog view", "PUT", pattern + "/positions", base + "/positions", `{"view_ids":["` + vBacklog + `"]}`, reorder, 403},
		{"reorder naming an unknown view", "PUT", pattern + "/positions", base + "/positions", `{"view_ids":["` + uuid.NewString() + `"]}`, reorder, 403},
		{"reorder body not the handler's shape", "PUT", pattern + "/positions", base + "/positions", `[1]`, reorder, 204},
	} {
		t.Run(c.name, func(t *testing.T) {
			if rec := serveBody(t, c.method, c.pattern, c.path, c.body, c.mw); rec.Code != c.want {
				t.Fatalf("got %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}
}
