package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	accountdom "github.com/Paca-AI/api/internal/domain/account"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	searchdom "github.com/Paca-AI/api/internal/domain/search"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// fakeSearchSource records what the handler let it search and answers with
// fixed hits.
type fakeSearchSource struct {
	calls            int
	projectIDs       []uuid.UUID
	taskProjectIDs   []uuid.UUID
	taskScopes       map[string]*iam.Node
	taskScoped       bool
	memberProjectIDs []uuid.UUID
	allUsers         bool
	limit            int

	tasks  []searchdom.TaskHit
	people []searchdom.PersonHit
}

func (f *fakeSearchSource) SearchProjects(_ context.Context, ids []uuid.UUID, _ string, limit int) ([]searchdom.ProjectHit, error) {
	f.calls++
	f.projectIDs, f.limit = ids, limit
	hits := []searchdom.ProjectHit{}
	for _, id := range ids {
		hits = append(hits, searchdom.ProjectHit{ID: id, Name: "P"})
	}
	return hits, nil
}

func (f *fakeSearchSource) SearchTasks(ctx context.Context, ids []uuid.UUID, _ string, _ int) ([]searchdom.TaskHit, error) {
	f.taskProjectIDs = ids
	f.taskScopes, f.taskScoped = iam.ProjectScopesFrom(ctx, "task")
	return f.tasks, nil
}

func (f *fakeSearchSource) SearchPeople(_ context.Context, ids []uuid.UUID, allUsers bool, _ string, _ int) ([]searchdom.PersonHit, error) {
	f.memberProjectIDs, f.allUsers = ids, allUsers
	return f.people, nil
}

type searchBody struct {
	Query    string `json:"query"`
	Projects []struct {
		ID string `json:"id"`
	} `json:"projects"`
	Tasks []struct {
		ID      string `json:"id"`
		TaskKey string `json:"task_key"`
	} `json:"tasks"`
	People []struct {
		ID        string  `json:"id"`
		ProjectID *string `json:"project_id"`
	} `json:"people"`
}

// searchFixture is a regular user who is a member of three projects:
// in p[0] they may read the project, its members and its tasks; in p[1] only
// tasks; in p[2] nothing at all.
type searchFixture struct {
	user     uuid.UUID
	projects []uuid.UUID
	src      *fakeSearchSource
	router   http.Handler
}

func newSearchFixture(t *testing.T, platformActions ...string) *searchFixture {
	t.Helper()
	f := &searchFixture{
		user:     uuid.New(),
		projects: []uuid.UUID{uuid.New(), uuid.New(), uuid.New()},
		src:      &fakeSearchSource{},
	}
	inProject := func(pid uuid.UUID, actions ...string) iam.Grant {
		res := "project/" + pid.String()
		return iam.Grant{RoleID: uuid.NewString(), ProjectID: pid.String(), Policy: &iam.Policy{Statements: []iam.Statement{
			{Effect: iam.EffectAllow, Actions: actions, Resources: []string{res, res + "/*"}},
		}}}
	}
	grants := []iam.Grant{
		inProject(f.projects[0], "projects:read", "project.members:read", "tasks:read"),
		inProject(f.projects[1], "tasks:read"),
	}
	if len(platformActions) > 0 {
		grants = append(grants, iam.Grant{RoleID: "platform", Policy: &iam.Policy{Statements: []iam.Statement{
			{Effect: iam.EffectAllow, Actions: platformActions, Resources: []string{"user", "user/*"}},
		}}})
	}
	authorizer := iam.NewAuthorizer(grantsByPrincipal{iam.User(f.user.String()): grants}, iam.NewRegistry(), iam.NewAttributeSchema())

	svc := &mockProjectSvc{
		listAccessible: func(_ context.Context, uid uuid.UUID, _, _ int) ([]*projectdom.Project, int64, error) {
			if uid != f.user {
				t.Errorf("listed the projects of %s, want the caller %s", uid, f.user)
			}
			out := make([]*projectdom.Project, len(f.projects))
			for i, id := range f.projects {
				out[i] = &projectdom.Project{ID: id}
			}
			return out, int64(len(out)), nil
		},
	}
	h := handler.NewProjectHandler(svc, authorizer,
		handler.WithProjectTaskScoper(authorizer),
		handler.WithProjectSearch(f.src))

	principal := httpmw.Principal{AccountID: f.user, Kind: accountdom.KindUser, Method: httpmw.MethodSession}
	claims := &domainauth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: f.user.String()}, Kind: "access"}
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := context.WithValue(req.Context(), httpmw.ClaimsContextKey(), claims)
			next.ServeHTTP(w, req.WithContext(httpmw.WithPrincipal(ctx, principal)))
		})
	}).Get("/search", h.Search)
	f.router = r
	return f
}

func decodeSearch(t *testing.T, body []byte) searchBody {
	t.Helper()
	var env struct {
		Data searchBody `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return env.Data
}

func TestSearch_ScopesEachKindToWhatTheCallerMayRead(t *testing.T) {
	f := newSearchFixture(t)
	p := f.projects

	w := do(t, f.router, http.MethodGet, "/search?q=%20alpha%20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeSearch(t, w.Body.Bytes())
	if got.Query != "alpha" {
		t.Errorf("query = %q, want the trimmed %q", got.Query, "alpha")
	}

	// Projects: only the one with projects:read.
	if !slices.Equal(f.src.projectIDs, []uuid.UUID{p[0]}) {
		t.Errorf("searched projects %v, want only %v", f.src.projectIDs, p[0])
	}
	// People: only the teams whose member list may be read, and not every user.
	if !slices.Equal(f.src.memberProjectIDs, []uuid.UUID{p[0]}) || f.src.allUsers {
		t.Errorf("people searched in %v (all users %v), want only %v", f.src.memberProjectIDs, f.src.allUsers, p[0])
	}
	// Tasks: every project of the caller, narrowed by the per-project scope.
	if !slices.Equal(f.src.taskProjectIDs, p) {
		t.Errorf("tasks searched in %v, want %v", f.src.taskProjectIDs, p)
	}
	if !f.src.taskScoped {
		t.Fatal("task search ran without the caller's task scope")
	}
	for i, wantOpen := range []bool{true, true, false} {
		node := f.src.taskScopes[p[i].String()]
		if open := node != nil && !node.DeniesAll(); open != wantOpen {
			t.Errorf("project %d task scope = %v, want readable %v", i, node, wantOpen)
		}
	}
	if f.src.limit != 5 {
		t.Errorf("limit = %d, want the default 5", f.src.limit)
	}
}

func TestSearch_UsersWriteFindsEveryUser(t *testing.T) {
	f := newSearchFixture(t, "users:write")
	if w := do(t, f.router, http.MethodGet, "/search?q=bo", nil); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !f.src.allUsers {
		t.Error("a user admin (users:write) should search every user")
	}
}

// users:read is in the built-in default role, so it must not lift the team
// limit, or every user would find every user.
func TestSearch_UsersReadAloneKeepsPeopleToTeams(t *testing.T) {
	f := newSearchFixture(t, "users:read")
	if w := do(t, f.router, http.MethodGet, "/search?q=bo", nil); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if f.src.allUsers || !slices.Equal(f.src.memberProjectIDs, []uuid.UUID{f.projects[0]}) {
		t.Errorf("users:read: all users %v, teams %v; want only team %v", f.src.allUsers, f.src.memberProjectIDs, f.projects[0])
	}
}

func TestSearch_FormatsTaskKeysAndPersonLinks(t *testing.T) {
	f := newSearchFixture(t)
	shared := f.projects[0]
	f.src.tasks = []searchdom.TaskHit{
		{ID: uuid.New(), ProjectID: shared, TaskIDPrefix: "prj", TaskNumber: 7, Title: "a"},
		{ID: uuid.New(), ProjectID: shared, TaskNumber: 3, Title: "b"},
	}
	f.src.people = []searchdom.PersonHit{
		{ID: uuid.New(), Name: "Ann", ProjectID: &shared},
		{ID: uuid.New(), Name: "Bob"},
	}

	got := decodeSearch(t, do(t, f.router, http.MethodGet, "/search?q=x&limit=20", nil).Body.Bytes())
	if len(got.Tasks) != 2 || got.Tasks[0].TaskKey != "PRJ-7" || got.Tasks[1].TaskKey != "#3" {
		t.Errorf("task keys = %+v, want PRJ-7 and #3", got.Tasks)
	}
	if len(got.People) != 2 || got.People[0].ProjectID == nil || *got.People[0].ProjectID != shared.String() || got.People[1].ProjectID != nil {
		t.Errorf("people = %+v, want Ann linked to %s and Bob unlinked", got.People, shared)
	}
	if f.src.limit != 20 {
		t.Errorf("limit = %d, want 20", f.src.limit)
	}
}

func TestSearch_EmptyQueryReturnsEmptyListsWithoutSearching(t *testing.T) {
	f := newSearchFixture(t)
	w := do(t, f.router, http.MethodGet, "/search?q=%20%20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if f.src.calls != 0 {
		t.Error("an empty query should not reach the repository")
	}
	if body := w.Body.String(); !strings.Contains(body, `"projects":[]`) || !strings.Contains(body, `"tasks":[]`) || !strings.Contains(body, `"people":[]`) {
		t.Errorf("want empty lists, got %s", body)
	}
}

func TestSearch_RejectsBadParameters(t *testing.T) {
	f := newSearchFixture(t)
	for _, path := range []string{
		"/search?q=x&limit=0",
		"/search?q=x&limit=21",
		"/search?q=x&limit=abc",
		"/search?q=" + strings.Repeat("a", 201),
	} {
		if w := do(t, f.router, http.MethodGet, path, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", path[:min(len(path), 40)], w.Code)
		}
	}
}

func TestSearch_NoSourceReturnsEmptyLists(t *testing.T) {
	h := handler.NewProjectHandler(&mockProjectSvc{}, adminAuthorizer())
	r := chi.NewRouter()
	r.Use(adminClaimsMiddleware())
	r.Get("/search", h.Search)

	w := do(t, r, http.MethodGet, "/search?q=x", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"tasks":[]`) {
		t.Fatalf("got %d %s, want 200 with empty lists", w.Code, w.Body.String())
	}
}
