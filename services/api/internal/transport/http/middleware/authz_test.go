package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// RequireAction on platform and project resources, and the public-project
// gate, judged by the IAM engine over a fake store. Grants are
// shaped the way migration 000064 writes them: a named platform permission
// on the platform roots, "*" on "*", a project role on project/<P>/*
// attached in that project.

var testPlatformRoots = []string{"user", "user/*", "role", "role/*", "plugin", "plugin/*", "settings", "sso", "agent", "agent/*", "project"}

// anyUserStore hands every user principal the same grants and every agent
// the agent grants (the tests below use random subjects).
type anyUserStore struct {
	user, agent []iam.Grant
}

func (s *anyUserStore) ListGrants(_ context.Context, p iam.Principal) ([]iam.Grant, error) {
	if p.Type == "agent" {
		return s.agent, nil
	}
	return s.user, nil
}

func iamWith(user []iam.Grant) *iam.Authorizer {
	return iamOver(&anyUserStore{user: user})
}

func iamOver(store iam.Store) *iam.Authorizer {
	return iam.NewAuthorizer(store, iam.NewRegistry(), iam.NewAttributeSchema())
}

func namedPlatform(actions ...string) iam.Grant {
	return platformGrant(actions, testPlatformRoots)
}

func starHolder() iam.Grant { return platformGrant([]string{"*"}, []string{"*"}) }

func withClaims(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), claimsContextKey{}, &domainauth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: uuid.NewString()},
				Role:             role,
				Kind:             "access",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// withAgentAuth simulates an agent-API-key-authenticated request: the shared
// static agent API key resolves to a fixed bot subject (seeded SUPER_ADMIN)
// plus an X-Agent-ID header identifying the acting agent — the same shape
// authn.go produces for a real agent API key request.
func withAgentAuth(botSubject, agentID uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), claimsContextKey{}, &domainauth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: botSubject.String()},
				Role:             "SUPER_ADMIN",
				Kind:             "access",
			})
			ctx = WithAgentID(ctx, agentID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func okRoute(r chi.Router, pattern string, mws ...func(http.Handler) http.Handler) {
	r.With(mws...).Get(pattern, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func get(t *testing.T, h http.Handler, path string) int {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	return w.Code
}

func TestRequireAction_UnauthenticatedPlatform(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/admin", RequireAction(iamWith(nil), iam.ActionUsersDelete, StaticResource(iam.PlatformResource(iam.ActionUsersDelete))))
	if code := get(t, r, "/admin"); code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", code)
	}
}

func TestRequireAction_Forbidden(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/admin", withClaims("USER"), RequireAction(iamWith(nil), iam.ActionUsersDelete, StaticResource(iam.PlatformResource(iam.ActionUsersDelete))))
	if code := get(t, r, "/admin"); code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", code)
	}
}

// The role NAME in the token confers nothing; only stored grants do.
func TestRequireAction_RoleNameGrantsNothing(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/admin", withClaims("ADMIN"), RequireAction(iamWith(nil), iam.ActionUsersRead, StaticResource(iam.PlatformResource(iam.ActionUsersRead))))
	if code := get(t, r, "/admin"); code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", code)
	}
}

func TestRequireAction_AllowedByStore(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/admin", withClaims("USER"), RequireAction(iamWith([]iam.Grant{namedPlatform("users:delete")}), iam.ActionUsersDelete, StaticResource(iam.PlatformResource(iam.ActionUsersDelete))))
	if code := get(t, r, "/admin"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

// A global-scope, agent-authenticated request is judged by the agent's own
// grants — never the SUPER_ADMIN bot user behind the shared key, which would
// let every agent act with full privilege.
func TestRequireAction_AgentGlobalScope_UsesAgentsOwnRole(t *testing.T) {
	bot, agent := uuid.New(), uuid.New()
	store := &anyUserStore{user: []iam.Grant{starHolder()}} // the bot is SUPER_ADMIN; the agent has nothing
	r := chi.NewRouter()
	okRoute(r, "/admin", withAgentAuth(bot, agent), RequireAction(iamOver(store), iam.ActionUsersRead, StaticResource(iam.PlatformResource(iam.ActionUsersRead))))
	if code := get(t, r, "/admin"); code != http.StatusForbidden {
		t.Fatalf("expected 403 (agent judged by its own grants, not the bot's), got %d", code)
	}
}

func TestRequireAction_AgentGlobalScope_AllowedByOwnRole(t *testing.T) {
	bot, agent := uuid.New(), uuid.New()
	store := &anyUserStore{agent: []iam.Grant{namedPlatform("users:read")}}
	r := chi.NewRouter()
	okRoute(r, "/admin", withAgentAuth(bot, agent), RequireAction(iamOver(store), iam.ActionUsersRead, StaticResource(iam.PlatformResource(iam.ActionUsersRead))))
	if code := get(t, r, "/admin"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

// Project scope: a project role grants it; a named platform permission does
// not reach into the project (GHSA-hjcj); "*" on "*" does.
func TestRequireAction_ProjectScope(t *testing.T) {
	projectID := uuid.New()
	p := projectID.String()
	cases := []struct {
		name   string
		grants []iam.Grant
		want   int
	}{
		{"project role", []iam.Grant{projectGrant(p, iam.Statement{Effect: iam.EffectAllow, Actions: []string{"tasks:read"}, Resources: []string{"project/" + p + "/*"}})}, http.StatusOK},
		{"named platform permission", []iam.Grant{namedPlatform("tasks:read")}, http.StatusForbidden},
		{"star holder", []iam.Grant{starHolder()}, http.StatusOK},
		{"role in another project", []iam.Grant{projectGrant(uuid.NewString(), iam.Statement{Effect: iam.EffectAllow, Actions: []string{"tasks:read"}, Resources: []string{"project/*"}})}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := chi.NewRouter()
			okRoute(r, "/projects/{projectId}/tasks", withClaims("USER"),
				RequireAction(iamWith(tc.grants), iam.ActionTasksRead, ProjectResource("projectId")))
			if code := get(t, r, "/projects/"+p+"/tasks"); code != tc.want {
				t.Fatalf("got %d, want %d", code, tc.want)
			}
		})
	}
}

func TestRequireAction_ProjectScope_InvalidID_Returns400(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}/tasks", withClaims("USER"),
		RequireAction(iamWith([]iam.Grant{starHolder()}), iam.ActionTasksRead, ProjectResource("projectId")))
	if code := get(t, r, "/projects/nope/tasks"); code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", code)
	}
}

// Global actions without a platform root are checked on "*", so only a "*"
// holder passes them at global scope.
func TestRequireAction_GlobalNonPlatformActionNeedsStar(t *testing.T) {
	for _, tc := range []struct {
		name   string
		grants []iam.Grant
		want   int
	}{
		{"named platform", []iam.Grant{namedPlatform("tasks:read")}, http.StatusForbidden},
		{"star", []iam.Grant{starHolder()}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := chi.NewRouter()
			okRoute(r, "/x", withClaims("USER"), RequireAction(iamWith(tc.grants), iam.ActionTasksRead, StaticResource(iam.PlatformResource(iam.ActionTasksRead))))
			if code := get(t, r, "/x"); code != tc.want {
				t.Fatalf("got %d, want %d", code, tc.want)
			}
		})
	}
}

// --- RequirePublicProjectOrActions with the router's two groups -----------

type mockVisibilityChecker struct {
	public bool
	err    error
}

func (m *mockVisibilityChecker) IsProjectPublic(_ context.Context, _ uuid.UUID) (bool, error) {
	return m.public, m.err
}

func projectOrPublic(checker ProjectVisibilityChecker, a *iam.Authorizer) func(http.Handler) http.Handler {
	return RequirePublicProjectOrActions(checker, a,
		[]ActionCheck{{Action: "projects:read", Resource: StaticResource("project")}},
		[]ActionCheck{{Action: "projects:read", Resource: ProjectResource("projectId")}},
	)
}

func TestPermissionGroups_Unauthenticated(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", projectOrPublic(&mockVisibilityChecker{}, iamWith(nil)))
	if code := get(t, r, "/projects/"+uuid.NewString()); code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", code)
	}
}

func TestPermissionGroups_Forbidden(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", withClaims("USER"), projectOrPublic(&mockVisibilityChecker{}, iamWith(nil)))
	if code := get(t, r, "/projects/"+uuid.NewString()); code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", code)
	}
}

func TestPermissionGroups_AllowedByFirstGroup_GlobalProjectsRead(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", withClaims("USER"), projectOrPublic(&mockVisibilityChecker{}, iamWith([]iam.Grant{namedPlatform("projects:read")})))
	if code := get(t, r, "/projects/"+uuid.NewString()); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

func TestPermissionGroups_AllowedBySecondGroup_ProjectScopedRead(t *testing.T) {
	p := uuid.NewString()
	grant := projectGrant(p, iam.Statement{Effect: iam.EffectAllow, Actions: []string{"projects:read"}, Resources: []string{"project/" + p + "/*"}})
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", withClaims("USER"), projectOrPublic(&mockVisibilityChecker{}, iamWith([]iam.Grant{grant})))
	if code := get(t, r, "/projects/"+p); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

func TestPermissionGroups_AllowedByWildcard_GlobalProjectsAll(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", withClaims("USER"), projectOrPublic(&mockVisibilityChecker{}, iamWith([]iam.Grant{namedPlatform("projects:*")})))
	if code := get(t, r, "/projects/"+uuid.NewString()); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

func TestPermissionGroups_InvalidProjectID_Returns400(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", withClaims("USER"), projectOrPublic(&mockVisibilityChecker{}, iamWith(nil)))
	if code := get(t, r, "/projects/not-a-uuid"); code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", code)
	}
}

func TestPermissionGroups_InvalidProjectID_GlobalGroupSucceeds(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", withClaims("USER"), projectOrPublic(&mockVisibilityChecker{}, iamWith([]iam.Grant{namedPlatform("projects:read")})))
	if code := get(t, r, "/projects/not-a-uuid"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

func TestRequirePublicProjectOrPermissions_AnonymousPublicProject_Allows(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", projectOrPublic(&mockVisibilityChecker{public: true}, iamWith(nil)))
	if code := get(t, r, "/projects/"+uuid.NewString()); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

func TestRequirePublicProjectOrPermissions_AnonymousPrivateProject_Returns401(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", projectOrPublic(&mockVisibilityChecker{public: false}, iamWith(nil)))
	if code := get(t, r, "/projects/"+uuid.NewString()); code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", code)
	}
}

func TestRequirePublicProjectOrPermissions_AnonymousInvalidProjectID_Returns400(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", projectOrPublic(&mockVisibilityChecker{public: true}, iamWith(nil)))
	if code := get(t, r, "/projects/not-a-uuid"); code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", code)
	}
}

func TestRequirePublicProjectOrPermissions_AuthenticatedWithPermission_Allows(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", withClaims("USER"), projectOrPublic(&mockVisibilityChecker{public: false}, iamWith([]iam.Grant{starHolder()})))
	if code := get(t, r, "/projects/"+uuid.NewString()); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

// Being logged in does not fall back to the public flag.
func TestRequirePublicProjectOrPermissions_AuthenticatedWithoutPermission_Returns403(t *testing.T) {
	r := chi.NewRouter()
	okRoute(r, "/projects/{projectId}", withClaims("USER"), projectOrPublic(&mockVisibilityChecker{public: true}, iamWith(nil)))
	if code := get(t, r, "/projects/"+uuid.NewString()); code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", code)
	}
}
