package router

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/config"
	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// openRouteGroups is the complete list of routes a caller holding NO
// permissions may reach: public endpoints, credential exchange, self-service,
// and a few routes deliberately open to every authenticated user. Every other
// route must be guarded — TestEveryRouteIsGuarded fails for one that isn't.
//
// Adding a route here is a security decision. Prefer giving the route a gate
// (see guards.go); if it really is open, say why in the group's reason.
var openRouteGroups = []struct {
	reason string
	routes []string
}{
	{
		reason: "public infrastructure and metadata: no authentication",
		routes: []string{
			"GET /api/healthz",
			"GET /api/v1/version",
			"GET /api/v1/releases",
			"GET /api/v1/branding",
			"GET /api/v1/environments/config",
			"GET /api/v1/plugins",
			"GET /api/v1/skills",
		},
	},
	{
		reason: "credential exchange: the request itself carries the proof " +
			"(password, refresh token, set-password token, webhook token) or " +
			"any live session may end itself",
		routes: []string{
			"POST /api/v1/auth/login",
			"POST /api/v1/auth/refresh",
			"POST /api/v1/auth/annotation-refresh",
			"POST /api/v1/auth/password/set",
			"POST /api/v1/auth/logout",
			"POST /api/v1/webhooks/automations/{nodeId}",
		},
	},
	{
		reason: "plugin proxy: each plugin route declares its own middleware " +
			"policy in its manifest, enforced inside the handler with " +
			"middleware.EnforcePermissions",
		routes: []string{
			"GET /api/v1/plugins/{pluginId}/*",
			"POST /api/v1/plugins/{pluginId}/*",
			"PUT /api/v1/plugins/{pluginId}/*",
			"PATCH /api/v1/plugins/{pluginId}/*",
			"DELETE /api/v1/plugins/{pluginId}/*",
		},
	},
	{
		reason: "self-service: the caller's own account, credentials, notifications and tasks",
		routes: []string{
			"GET /api/v1/users/me",
			"PATCH /api/v1/users/me",
			"PATCH /api/v1/users/me/password",
			"GET /api/v1/users/me/global-permissions",
			"GET /api/v1/users/me/tasks",
			"POST /api/v1/users/me/avatar/initiate-upload",
			"POST /api/v1/users/me/avatar/complete-upload",
			"DELETE /api/v1/users/me/avatar",
			"GET /api/v1/users/me/api-keys",
			"POST /api/v1/users/me/api-keys",
			"DELETE /api/v1/users/me/api-keys/{keyId}",
			"GET /api/v1/users/me/notifications",
			"PATCH /api/v1/users/me/notifications/{notificationId}/read",
			"POST /api/v1/users/me/notifications/read-all",
		},
	},
	{
		reason: "open to any authenticated user, with the result scoped to what " +
			"the caller may already see inside the handler",
		routes: []string{
			"GET /api/v1/projects",
			"GET /api/v1/projects/workspace-stats",
			"GET /api/v1/port-forwards/resolve",
			"GET /api/v1/projects/{projectId}/members/me/permissions",
		},
	},
	{
		reason: "global agents and chat: any authenticated human may chat with a " +
			"global agent, like any project member may with a project agent; " +
			"the /agents/me/* routes are the agent API key's own self-service",
		routes: []string{
			"GET /api/v1/agents/",
			"GET /api/v1/agents/llm-models",
			"GET /api/v1/agents/skill-templates",
			"GET /api/v1/agents/me/global-permissions",
			"GET /api/v1/agents/me/projects",
			"GET /api/v1/agents/me/conversations/{conversationId}",
			"GET /api/v1/agents/me/conversations/{conversationId}/events",
			"POST /api/v1/agents/resolve-auto",
			"GET /api/v1/agents/{agentId}/chat-sessions",
			"POST /api/v1/agents/{agentId}/chat-sessions",
			"POST /api/v1/agents/chat-sessions/{sessionId}/messages",
			"GET /api/v1/agents/conversations",
			"GET /api/v1/agents/conversations/{conversationId}",
			"GET /api/v1/agents/conversations/{conversationId}/events",
			"POST /api/v1/agents/conversations/{conversationId}/stop",
			"POST /api/v1/agents/conversations/{conversationId}/pause",
			"POST /api/v1/agents/conversations/{conversationId}/heartbeat",
			"POST /api/v1/agents/conversations/{conversationId}/messages",
			"PATCH /api/v1/agents/conversations/{conversationId}",
			"DELETE /api/v1/agents/conversations/{conversationId}",
		},
	},
}

// privateProjects reports every project as non-public, so anonymous callers
// can never be admitted through the public-project path.
type privateProjects struct{}

func (privateProjects) IsProjectPublic(context.Context, uuid.UUID) (bool, error) { return false, nil }

// routeUnderTest is one registered route, rebuilt to run its real middleware
// chain against a stub endpoint.
type routeUnderTest struct {
	method, pattern string
	handler         http.Handler
}

func (rt routeUnderTest) key() string { return rt.method + " " + rt.pattern }

// allRoutes builds the full router — every handler registered, each wired to
// no services — and returns each route rebuilt in a tiny router of its own:
// the route's real middleware stack (global, group and inline, in order)
// ending in a stub that always answers 204. chi resolves URL parameters
// exactly as it does in the real router, so scope resolvers behave the same.
// Running the real handlers instead is not an option: with no services behind
// them they panic, some in goroutines of their own.
func allRoutes(t *testing.T, authorizer *authz.Authorizer, visibility privateProjects) []routeUnderTest {
	t.Helper()

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := New(Deps{
		TokenManager:         jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour),
		Authorizer:           authorizer,
		ProjectVisibilitySvc: visibility,
		Health:               handler.NewHealthHandler(),
		Version:              handler.NewVersionHandler(config.ReleaseConfig{}, nil, log),
		Auth:                 handler.NewAuthHandler(nil, handler.CookieConfig{}),
		User:                 handler.NewUserHandler(nil),
		GlobalRole:           handler.NewGlobalRoleHandler(nil),
		Project:              handler.NewProjectHandler(nil, authorizer),
		Task:                 handler.NewTaskHandler(nil, nil, nil),
		Sprint:               handler.NewSprintHandler(nil, nil),
		View:                 handler.NewViewHandler(nil),
		Attachment:           handler.NewAttachmentHandler(nil),
		Document:             handler.NewDocumentHandler(nil, nil),
		DocFile:              handler.NewDocFileHandler(nil),
		Notification:         handler.NewNotificationHandler(nil),
		APIKey:               handler.NewAPIKeyHandler(nil),
		Plugin:               handler.NewPluginHandler(nil, nil, nil),
		Skills:               handler.NewSkillsHandler(nil, ""),
		Agent:                handler.NewAgentHandler(nil, "", "", ""),
		Environment:          handler.NewEnvironmentHandler(nil, ""),
		Annotation:           handler.NewAnnotationHandler(nil),
		Conversation:         handler.NewConversationHandler(nil),
		Automation:           handler.NewAutomationHandler(nil),
		Settings:             handler.NewSettingsHandler(nil),
		Log:                  log,
	})
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("router is %T, not chi.Routes", router)
	}

	var out []routeUnderTest
	err := chi.Walk(routes, func(method, pattern string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			return nil // HEAD/OPTIONS/... are registered wholesale for Handle() routes
		}
		mini := chi.NewRouter()
		mini.With(middlewares...).MethodFunc(method, pattern, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		out = append(out, routeUnderTest{method: method, pattern: pattern, handler: mini})
		return nil
	})
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}

var routeParamRe = regexp.MustCompile(`\{[^}]+\}`)

// concretePath fills a route pattern's URL parameters with valid UUIDs.
func concretePath(pattern string) string {
	pattern = strings.ReplaceAll(pattern, "*", "x")
	return routeParamRe.ReplaceAllString(pattern, "11111111-1111-1111-1111-111111111111")
}

// errorCodeOf returns the error_code of a JSON error response.
func errorCodeOf(rec *httptest.ResponseRecorder) string {
	var env struct {
		ErrorCode string `json:"error_code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return env.ErrorCode
}

// TestEveryRouteIsGuarded is the safety net behind the router's model that
// every route declares its own gate. Each registered route is called twice:
// by an authenticated caller who holds no permissions at all (must be refused
// with 403 FORBIDDEN) and by an anonymous caller (must be refused with 401).
// The only routes exempt are the reviewed openRouteGroups. A new route
// registered without a gate therefore fails here, instead of shipping open.
func TestEveryRouteIsGuarded(t *testing.T) {
	authorizer := authz.NewAuthorizer(&staticPermissionStore{}) // holds nothing
	routes := allRoutes(t, authorizer, privateProjects{})
	token := issueAccessTokenForRouterTests(t)

	open := map[string]bool{}
	for _, group := range openRouteGroups {
		for _, route := range group.routes {
			open[route] = true
		}
	}

	seen := map[string]bool{}
	for _, rt := range routes {
		seen[rt.key()] = true
		if open[rt.key()] {
			continue
		}

		req := httptest.NewRequestWithContext(t.Context(), rt.method, concretePath(rt.pattern), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		rt.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || errorCodeOf(rec) != "FORBIDDEN" {
			t.Errorf("%s reached by an authenticated caller with no permissions: got %d %q, want 403 FORBIDDEN — "+
				"give the route a gate in router.go (or, if it is open by design, add it to openRouteGroups with a reason)",
				rt.key(), rec.Code, errorCodeOf(rec))
		}

		req = httptest.NewRequestWithContext(t.Context(), rt.method, concretePath(rt.pattern), nil)
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		rt.handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s reached by an anonymous caller: got %d, want 401", rt.key(), rec.Code)
		}
	}

	// The allowlist must not rot: every entry has to be a real route.
	for _, group := range openRouteGroups {
		for _, route := range group.routes {
			if !seen[route] {
				t.Errorf("openRouteGroups lists %q, which is not a registered route — remove or fix it", route)
			}
		}
	}

	if len(routes) < 200 {
		t.Fatalf("only %d routes were walked; the router walk is not seeing the whole API", len(routes))
	}
}

// TestRoleAssignmentIsSeparatePrivilege pins the boundary this router draws
// around roles: editing a user or an agent needs only its own permission, but
// changing who holds which global role needs global_roles.assign — and on the
// agent route agents.write as well, since it changes an agent. The profile
// routes must not double as a back door to a role, so they neither need nor
// accept global_roles.assign on its own.
func TestRoleAssignmentIsSeparatePrivilege(t *testing.T) {
	const (
		usersWrite    = authz.PermissionUsersWrite
		agentsWrite   = authz.PermissionAgentsWrite
		rolesAssign   = authz.PermissionGlobalRolesAssign
		userPath      = "/api/v1/admin/users/{userId}"
		agentPath     = "/api/v1/admin/agents/{agentId}"
		agentRolePath = agentPath + "/global-role"
	)
	tests := []struct {
		route   string
		allowed [][]authz.Permission // any one of these grant sets opens the route
	}{
		{"POST /api/v1/admin/users", [][]authz.Permission{{usersWrite}}},
		{"PATCH " + userPath, [][]authz.Permission{{usersWrite}}},
		{"POST /api/v1/admin/agents", [][]authz.Permission{{agentsWrite}}},
		{"PATCH " + agentPath, [][]authz.Permission{{agentsWrite}}},
		{"PUT " + userPath + "/global-roles", [][]authz.Permission{{rolesAssign}}},
		{"PUT " + agentRolePath, [][]authz.Permission{{agentsWrite, rolesAssign}}},
		{"DELETE " + agentRolePath, [][]authz.Permission{{agentsWrite, rolesAssign}}},
	}
	// Every grant set worth trying; a route is open to exactly the ones it lists.
	candidates := [][]authz.Permission{
		{usersWrite}, {agentsWrite}, {rolesAssign}, {usersWrite, agentsWrite}, {agentsWrite, rolesAssign},
	}

	token := issueAccessTokenForRouterTests(t)
	for _, grants := range candidates {
		authorizer := authz.NewAuthorizer(&scopedStore{global: grants})
		byKey := map[string]routeUnderTest{}
		for _, rt := range allRoutes(t, authorizer, privateProjects{}) {
			byKey[rt.key()] = rt
		}

		for _, tc := range tests {
			rt, ok := byKey[tc.route]
			if !ok {
				t.Fatalf("%q is not a registered route", tc.route)
			}
			wantOpen := false
			for _, set := range tc.allowed {
				if containsAll(grants, set) {
					wantOpen = true
				}
			}

			req := httptest.NewRequestWithContext(t.Context(), rt.method, concretePath(rt.pattern), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			rt.handler.ServeHTTP(rec, req)

			gotOpen := rec.Code == http.StatusNoContent
			if gotOpen != wantOpen {
				t.Errorf("%s with %v: open = %v, want %v (status %d)", tc.route, grants, gotOpen, wantOpen, rec.Code)
			}
		}
	}
}

// The default role is a property of the role *definition*: it decides what new
// users and agents start with, not who holds what today. So choosing it needs
// global_roles.write, like editing or deleting the role, and neither
// global_roles.assign (which hands roles to accounts) nor users.write is enough.
func TestSettingTheDefaultRoleIsRoleDefinitionWork(t *testing.T) {
	const (
		rolesRead   = authz.PermissionGlobalRolesRead
		rolesWrite  = authz.PermissionGlobalRolesWrite
		rolesAssign = authz.PermissionGlobalRolesAssign
		usersWrite  = authz.PermissionUsersWrite
	)
	routes := []string{
		"PUT /api/v1/admin/global-roles/{roleId}/set-default",
		"DELETE /api/v1/admin/global-roles/{roleId}",
	}
	candidates := []struct {
		grants   []authz.Permission
		wantOpen bool
	}{
		{[]authz.Permission{rolesWrite}, true},
		{[]authz.Permission{rolesRead}, false},
		{[]authz.Permission{rolesAssign}, false},
		{[]authz.Permission{usersWrite}, false},
		{[]authz.Permission{rolesRead, rolesAssign, usersWrite}, false},
	}

	token := issueAccessTokenForRouterTests(t)
	for _, tc := range candidates {
		authorizer := authz.NewAuthorizer(&scopedStore{global: tc.grants})
		byKey := map[string]routeUnderTest{}
		for _, rt := range allRoutes(t, authorizer, privateProjects{}) {
			byKey[rt.key()] = rt
		}

		for _, route := range routes {
			rt, ok := byKey[route]
			if !ok {
				t.Fatalf("%q is not a registered route", route)
			}
			req := httptest.NewRequestWithContext(t.Context(), rt.method, concretePath(rt.pattern), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			rt.handler.ServeHTTP(rec, req)

			if gotOpen := rec.Code == http.StatusNoContent; gotOpen != tc.wantOpen {
				t.Errorf("%s with %v: open = %v, want %v (status %d)", route, tc.grants, gotOpen, tc.wantOpen, rec.Code)
			}
		}
	}
}

// containsAll reports whether have holds every permission in want.
func containsAll(have, want []authz.Permission) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
