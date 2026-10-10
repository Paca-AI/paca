package router

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/config"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// Route coverage (spec 6.2): every registered route that is not reviewed as
// open must declare an authorization gate.
//
// How gates are detected: every guards gate is wrapped by markGate, which
// makes the handler it builds a gateHandler (no runtime effect). chi.Walk
// hands us each route's full middleware list (group r.Use and inline r.With);
// applying each middleware to a stub and type-asserting the result finds the
// gates without running any request, and gateHandler.resource tells which
// resource the gate authorizes against.
//
// The open (ungated) allow-list is openRouteGroups in authorization_test.go —
// one reviewed list, each group with its reason, shared with the behavioural
// TestEveryRouteIsGuarded so the two cannot drift: public infrastructure
// (health, version/releases, branding, environments/config, plugin and skill
// listings), credential exchange (/auth/* login/refresh/password-set/logout,
// automation webhooks), the plugin proxy (gated per-manifest inside the
// handler), self-service /users/me/*, handler-scoped listings (/projects,
// workspace-stats, port-forward resolve, members/me/permissions) and the
// global-agent chat surface.

// routeGates is one walked route and the gates declared on it.
type routeGates struct {
	method, pattern string
	gates           []gateHandler
	handler         http.Handler // the route's real middleware chain over a 204 stub
}

func (r routeGates) key() string { return r.method + " " + r.pattern }

func coverageDeps(iamAuth *iam.Authorizer) Deps {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return Deps{
		TokenManager:         jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour),
		IAM:                  iamAuth,
		AgentEnvironments:    noEnvironment{},
		SessionEnvironments:  noEnvironment{},
		ProjectVisibilitySvc: privateProjects{},
		Health:               handler.NewHealthHandler(),
		Version:              handler.NewVersionHandler(config.ReleaseConfig{}, nil, log),
		Auth:                 handler.NewAuthHandler(nil, handler.CookieConfig{}),
		User:                 handler.NewUserHandler(nil),
		Role:                 handler.NewRoleHandler(nil),
		RoleAttachments:      noRoleAttachments{},
		Project:              handler.NewProjectHandler(nil, iamAuth),
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
		ProjectExport:        handler.NewProjectExportHandler(nil),
		Annotation:           handler.NewAnnotationHandler(nil),
		Conversation:         handler.NewConversationHandler(nil),
		Automation:           handler.NewAutomationHandler(nil),
		Settings:             handler.NewSettingsHandler(nil),
		SSO:                  handler.NewSSOHandler(nil, nil, ""),
		Log:                  log,
	}
}

// walkGates builds the full router from deps and returns every route with its
// declared gates.
func walkGates(t *testing.T, deps Deps) []routeGates {
	t.Helper()
	routes, ok := New(deps).(chi.Routes)
	if !ok {
		t.Fatal("router is not chi.Routes")
	}
	stub := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	var out []routeGates
	err := chi.Walk(routes, func(method, pattern string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			return nil // HEAD/OPTIONS/... are registered wholesale for Handle() routes
		}
		rg := routeGates{method: method, pattern: pattern, gates: gatesOf(mws)}
		mini := chi.NewRouter()
		mini.With(mws...).MethodFunc(method, pattern, stub)
		rg.handler = mini
		out = append(out, rg)
		return nil
	})
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	if len(out) < 200 {
		t.Fatalf("only %d routes walked; the walk is not seeing the whole API", len(out))
	}
	return out
}

// ssoOpenRoutes extends openRouteGroups with the public SSO sign-in routes.
// They are registered only when Deps.SSO is set, which allRoutes (and so
// TestEveryRouteIsGuarded) does not do; coverageDeps does, so the admin SSO
// provider routes are covered too.
var ssoOpenRoutes = []string{
	// The login page lists the enabled providers before anyone is signed in.
	"GET /api/v1/auth/sso/providers",
	// Top-level browser navigations to and back from the identity provider:
	// the provider's signed response (state + code) is the credential.
	"GET /api/v1/auth/sso/{slug}/login",
	"GET /api/v1/auth/sso/{slug}/callback",
}

// gatesOf returns the guards gates among a route's middlewares.
func gatesOf(mws []func(http.Handler) http.Handler) []gateHandler {
	stub := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	var gates []gateHandler
	for _, mw := range mws {
		if g, ok := mw(stub).(gateHandler); ok {
			gates = append(gates, g)
		}
	}
	return gates
}

// The detector itself: a route with only authentication/other middleware has
// no gate; group-level (r.Use) and inline (r.With) gates are both found.
func TestRouteCoverage_DetectsGates(t *testing.T) {
	g := newGuards(Deps{IAM: newTestIAM(&iamUserStore{})})
	r := chi.NewRouter()
	r.With(httpmw.RequireJSONContentType()).Get("/ungated", func(http.ResponseWriter, *http.Request) {})
	r.With(g.Project(iam.ActionTasksRead)).Get("/inline/{projectId}", func(http.ResponseWriter, *http.Request) {})
	r.Group(func(r chi.Router) {
		r.Use(g.Global(iam.ActionPluginsWrite))
		r.Get("/group", func(http.ResponseWriter, *http.Request) {})
	})
	got := map[string]int{}
	_ = chi.Walk(r, func(method, pattern string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		got[pattern] = len(gatesOf(mws))
		return nil
	})
	want := map[string]int{"/ungated": 0, "/inline/{projectId}": 1, "/group": 1}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %d gates detected, want %d", k, got[k], v)
		}
	}
}

func openRoutes() map[string]bool {
	open := map[string]bool{}
	for _, group := range openRouteGroups {
		for _, r := range group.routes {
			open[r] = true
		}
	}
	for _, r := range ssoOpenRoutes {
		open[r] = true
	}
	return open
}

func TestRouteCoverage_EveryNonOpenRouteDeclaresAGate(t *testing.T) {
	open := openRoutes()
	for _, rt := range walkGates(t, coverageDeps(newTestIAM(&iamUserStore{}))) {
		switch {
		case open[rt.key()] && len(rt.gates) > 0:
			t.Errorf("%s is listed as open in openRouteGroups but declares a gate — remove it from the list", rt.key())
		case !open[rt.key()] && len(rt.gates) == 0:
			t.Errorf("%s declares no authorization gate — give it a require.* gate in router.go "+
				"(or, if it is open by design, add it to openRouteGroups with a reason)", rt.key())
		}
	}
}

// iamResourceRoutes is the exact set of routes whose IAM gate authorizes
// against something other than project/{projectId} or a platform root:
// every route on one environment, and using one project agent (chat
// sessions). Changing it is a deliberate policy change.
var iamResourceRoutes = mergeRoutes(roleRoutes, map[string]string{
	"GET /api/v1/projects/{projectId}/agents/{agentId}/chat-sessions":                                                                         resAgent,
	"POST /api/v1/projects/{projectId}/agents/{agentId}/chat-sessions":                                                                        resAgent,
	"POST /api/v1/projects/{projectId}/agents/{agentId}/chat-sessions/{sessionId}/messages":                                                   resAgent,
	"GET /api/v1/projects/{projectId}/environments/{environmentId}":                                                                           resEnvironment,
	"PATCH /api/v1/projects/{projectId}/environments/{environmentId}":                                                                         resEnvironment,
	"DELETE /api/v1/projects/{projectId}/environments/{environmentId}":                                                                        resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/start":                                                                    resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/stop":                                                                     resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/restart":                                                                  resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/heartbeat":                                                                resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/verify-cli-login":                                                         resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/stats-ticket":                                                             resEnvironment,
	"GET /api/v1/projects/{projectId}/environments/{environmentId}/folders":                                                                   resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/folders":                                                                  resEnvironment,
	"DELETE /api/v1/projects/{projectId}/environments/{environmentId}/folders/{folderId}":                                                     resEnvironment,
	"GET /api/v1/projects/{projectId}/environments/{environmentId}/browse":                                                                    resEnvironment,
	"GET /api/v1/projects/{projectId}/environments/{environmentId}/ssh-keys":                                                                  resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/ssh-keys":                                                                 resEnvironment,
	"DELETE /api/v1/projects/{projectId}/environments/{environmentId}/ssh-keys/{keyId}":                                                       resEnvironment,
	"GET /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards":                                                             resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards":                                                            resEnvironment,
	"GET /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}":                                             resEnvironment,
	"DELETE /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}":                                          resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/terminal-ticket":                                                          resEnvironment,
	"GET /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/":                                resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/":                               resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/upload-url":                     resEnvironment,
	"GET /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/{annotationId}":                  resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/{annotationId}/complete-upload": resEnvironment,
	"GET /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/{annotationId}/screenshot-url":   resEnvironment,
	"PATCH /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/{annotationId}/resolve":        resEnvironment,
	"PATCH /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/{annotationId}/reopen":         resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/{annotationId}/comments":        resEnvironment,
	"POST /api/v1/projects/{projectId}/environments/{environmentId}/port-forwards/{portForwardId}/annotations/{annotationId}/create-task":     resEnvironment,
})

// entityRouteResource returns the resource a route on one task, document,
// sprint or view must be authorized on: every route whose path names the entity
// (including nested ones such as a task's comments). A view's task-positions
// act on tasks, not on the view, and stay project-gated.
func entityRouteResource(pattern string) (string, bool) {
	switch {
	case strings.Contains(pattern, "/tasks/{taskId}"), strings.Contains(pattern, "/task-positions/{taskId}"), strings.Contains(pattern, "/tasks/by-number/"):
		return resTask, true
	case strings.Contains(pattern, "/projects/{projectId}/agents/{agentId}"):
		return resAgent, true
	case strings.Contains(pattern, "/projects/{projectId}/automations/{automationId}"):
		return resWorkflow, true
	case strings.Contains(pattern, "/projects/{projectId}/conversations/{conversationId}"):
		return resConversation, true
	case strings.Contains(pattern, "/projects/{projectId}/annotations/{annotationId}"):
		// (The routes nested under a port forward are authorized on the
		// annotation as well as the environment: see annotationRoutesAreEntityGated.)
		return resAnnotation, true
	case strings.Contains(pattern, "/docs/{docId}"):
		return resDoc, true
	case strings.Contains(pattern, "/sprints/{sprintId}"):
		return resSprint, true
	case strings.Contains(pattern, "/views/{viewId}") && !strings.Contains(pattern, "/task-positions"):
		return resView, true
	}
	return "", false
}

func mergeRoutes(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// roleRoutes pins the resource every IAM roles/attachments route authorizes
// against (the first gate that is neither a project nor a platform-root gate).
// Routes missing here are gated on project/{projectId} or a platform root; the
// collection routes of the platform roles are gated on the platform root
// "role/*" (a Global gate).
var roleRoutes = map[string]string{
	"GET /api/v1/admin/roles/{roleId}":                   resRole,
	"PUT /api/v1/admin/roles/{roleId}":                   resRole,
	"DELETE /api/v1/admin/roles/{roleId}":                resRole,
	"PUT /api/v1/admin/roles/{roleId}/default":           resRole,
	"GET /api/v1/admin/users/{userId}/roles":             resUser,
	"GET /api/v1/admin/agents/{agentId}/roles":           resGlobalAgent,
	"PUT /api/v1/admin/agents/{agentId}/roles":           resGlobalAgent,
	"GET /api/v1/projects/{projectId}/roles/":            resProjectRoleCollection,
	"POST /api/v1/projects/{projectId}/roles/":           resProjectRoleCollection,
	"GET /api/v1/projects/{projectId}/roles/{roleId}":    resProjectRole,
	"PUT /api/v1/projects/{projectId}/roles/{roleId}":    resProjectRole,
	"DELETE /api/v1/projects/{projectId}/roles/{roleId}": resProjectRole,
}

// assignGates pins exactly which routes carry the role-assignment gate
// (roles:assign on the resource of each role added or removed) and in which
// scope. Every route that changes who holds a role must be listed.
var assignGates = map[string]string{
	"PUT /api/v1/admin/users/{userId}/roles":                    resAssignRoles,
	"PUT /api/v1/admin/agents/{agentId}/roles":                  resAssignRoles,
	"PUT /api/v1/projects/{projectId}/members/{memberId}/roles": resAssignProjectRoles,
	"POST /api/v1/projects/{projectId}/members/":                resAssignProjectRoles,
	"POST /api/v1/projects/{projectId}/agents/":                 resAssignProjectRoles,
}

// assignBody is a body naming one role, so the assignment gate has something
// to authorize in the behavioural sweep.
const assignBody = `{"role_ids":["11111111-1111-1111-1111-111111111111"]}`

// noRoleAttachments is a lookup of attachments for targets that hold no roles.
type noRoleAttachments struct{}

func (noRoleAttachments) UserRoleIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (noRoleAttachments) AgentRoleIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

func (noRoleAttachments) MemberRoleIDs(context.Context, uuid.UUID, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

// isAttrsGate reports whether resource names a request-attribute gate, which
// sits after the route's action gate and authorizes the body's attributes.
func isAttrsGate(resource string) bool {
	return resource == resTaskAttrs || resource == resDocAttrs || resource == resTaskPositionItems ||
		resource == resViewAttrs || resource == resViewReorderItems
}

func isAssignGate(resource string) bool {
	return resource == resAssignRoles || resource == resAssignProjectRoles
}

// noEnvironment is a lookup for agents and sessions that have no environment.
type noEnvironment struct{}

func (noEnvironment) DefaultEnvironmentID(context.Context, uuid.UUID, uuid.UUID) (*uuid.UUID, error) {
	return nil, nil
}

func (noEnvironment) SessionEnvironmentID(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*uuid.UUID, error) {
	return nil, nil
}

// The environment a chat runs in is authorized on exactly these two routes
// (in addition to their agent gate): starting a chat, and sending into one.
var chatEnvironmentRoutes = map[string]string{
	"POST /api/v1/projects/{projectId}/agents/{agentId}/chat-sessions":                      resChatEnvironment,
	"POST /api/v1/projects/{projectId}/agents/{agentId}/chat-sessions/{sessionId}/messages": resSessionEnvironment,
}

func TestRouteCoverage_ChatEnvironmentGates(t *testing.T) {
	seen := map[string]bool{}
	for _, rt := range walkGates(t, coverageDeps(newTestIAM(&iamUserStore{}))) {
		var got []string
		for _, g := range rt.gates {
			if g.resource == resChatEnvironment || g.resource == resSessionEnvironment {
				got = append(got, g.resource)
			}
		}
		want, ok := chatEnvironmentRoutes[rt.key()]
		switch {
		case ok && (len(got) != 1 || got[0] != want):
			t.Errorf("%s: environment gates %v, want [%s]", rt.key(), got, want)
		case !ok && len(got) > 0:
			t.Errorf("%s: unexpected chat environment gate %v", rt.key(), got)
		}
		if ok {
			seen[rt.key()] = true
		}
	}
	for k := range chatEnvironmentRoutes {
		if !seen[k] {
			t.Errorf("%q is not a registered route", k)
		}
	}
}

func TestRouteCoverage_IAMResources(t *testing.T) {
	deps := coverageDeps(newTestIAM(&iamUserStore{}))
	seen := map[string]bool{}
	for _, rt := range walkGates(t, deps) {
		want, special := iamResourceRoutes[rt.key()]
		if !special {
			want, special = entityRouteResource(rt.pattern)
		}
		seen[rt.key()] = true
		var child []string
		for _, g := range rt.gates {
			if g.resource != resProject && g.resource != resPlatform && !isAssignGate(g.resource) && !isAttrsGate(g.resource) {
				child = append(child, g.resource)
			}
		}
		switch {
		case special && (len(child) == 0 || child[0] != want):
			t.Errorf("%s: IAM gates %v, want one on %s", rt.key(), child, want)
		case !special && len(child) > 0:
			t.Errorf("%s: unexpected resource-level IAM gate %v — add it to iamResourceRoutes if intended", rt.key(), child)
		}
		// Every route on one annotation, however it is reached, is authorized
		// on the annotation itself.
		if strings.Contains(rt.pattern, "/annotations/{annotationId}") {
			hasAnnotationGate := false
			for _, c := range child {
				hasAnnotationGate = hasAnnotationGate || c == resAnnotation
			}
			if !hasAnnotationGate {
				t.Errorf("%s acts on one annotation but is not authorized on %s (gates %v)", rt.key(), resAnnotation, child)
			}
		}
		// Every route on one environment must be authorized on it.
		if strings.Contains(rt.pattern, "/environments/{environmentId}") && !special {
			t.Errorf("%s acts on one environment but is not authorized on %s", rt.key(), resEnvironment)
		}
	}
	for k := range iamResourceRoutes {
		if !seen[k] {
			t.Errorf("iamResourceRoutes lists %q, which is not a registered route", k)
		}
	}
}

// Behavioural sweep: every gated route refuses an authenticated caller with
// no grants (403) and an anonymous one (401), and a "*" holder passes every
// gate — so every gate's resource resolves on a well-formed URL.
func TestRouteCoverage_Behaviour(t *testing.T) {
	token := issueAccessTokenForRouterTests(t)
	open := openRoutes()
	call := func(rt routeGates, auth bool) *httptest.ResponseRecorder {
		var body io.Reader
		if _, ok := assignGates[rt.key()]; ok {
			body = strings.NewReader(assignBody)
		}
		req := httptest.NewRequestWithContext(t.Context(), rt.method, concretePath(rt.pattern), body)
		req.Header.Set("Content-Type", "application/json")
		if auth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		rt.handler.ServeHTTP(rec, req)
		return rec
	}

	nobody := walkGates(t, coverageDeps(newTestIAM(&iamUserStore{})))
	for _, rt := range nobody {
		if open[rt.key()] {
			continue
		}
		if rec := call(rt, true); rec.Code != http.StatusForbidden || errorCodeOf(rec) != "FORBIDDEN" {
			t.Errorf("%s: authenticated caller with no IAM grants got %d %q, want 403 FORBIDDEN", rt.key(), rec.Code, errorCodeOf(rec))
		}
		if rec := call(rt, false); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: anonymous caller got %d, want 401", rt.key(), rec.Code)
		}
	}

	star := newTestIAM(&iamUserStore{grants: []iam.Grant{platformRole(allow([]string{"*"}, "*"))}})
	for _, rt := range walkGates(t, coverageDeps(star)) {
		if open[rt.key()] {
			continue
		}
		if rec := call(rt, true); rec.Code != http.StatusNoContent {
			t.Errorf("%s: \"*\" holder got %d %q, want 204", rt.key(), rec.Code, errorCodeOf(rec))
		}
	}
}

func TestRouteCoverage_AssignGates(t *testing.T) {
	seen := map[string]bool{}
	for _, rt := range walkGates(t, coverageDeps(newTestIAM(&iamUserStore{}))) {
		var got []string
		for _, g := range rt.gates {
			if isAssignGate(g.resource) {
				got = append(got, g.resource)
			}
		}
		want, ok := assignGates[rt.key()]
		if ok {
			seen[rt.key()] = true
		}
		if ok != (len(got) > 0) || (ok && (len(got) != 1 || got[0] != want)) {
			t.Errorf("%s: assignment gates %v, want [%s]", rt.key(), got, want)
		}
	}
	for k := range assignGates {
		if !seen[k] {
			t.Errorf("assignGates lists %q, which is not a registered route", k)
		}
	}
}

// Routes that manage members or agents keep their own action gate next to the
// assignment gate: a caller holding only roles:assign cannot add a member or
// a project agent. Changing an existing member's roles is the exception: that
// route needs only roles:assign (per role).
func TestRouteCoverage_AssignRoutesKeepTheirActionGate(t *testing.T) {
	for _, rt := range walkGates(t, coverageDeps(newTestIAM(&iamUserStore{}))) {
		scope, ok := assignGates[rt.key()]
		if !ok {
			continue
		}
		other := 0
		for _, g := range rt.gates {
			if !isAssignGate(g.resource) {
				other++
			}
		}
		if rt.key() == "PUT /api/v1/projects/{projectId}/members/{memberId}/roles" {
			if other != 0 {
				t.Errorf("%s must be gated by roles:assign alone, found %d other gates", rt.key(), other)
			}
			continue
		}
		if scope == resAssignProjectRoles && other == 0 {
			t.Errorf("%s has only the assignment gate; project routes also need their members/agents action gate", rt.key())
		}
		if rt.key() == "PUT /api/v1/admin/agents/{agentId}/roles" && other == 0 {
			t.Errorf("%s lost its agents:write gate on the agent", rt.key())
		}
	}
}

// The role editor helpers are open to any authenticated caller: unauthenticated
// callers get 401, and a caller with no grants is let in (except a simulation
// that names a principal, which exposes that principal's grants).
func TestRouteCoverage_RoleHelpersAreAuthenticatedOnly(t *testing.T) {
	token := issueAccessTokenForRouterTests(t)
	routes := map[string]routeGates{}
	for _, rt := range walkGates(t, coverageDeps(newTestIAM(&iamUserStore{}))) {
		routes[rt.key()] = rt
	}
	call := func(key, body string, auth bool) *httptest.ResponseRecorder {
		rt, ok := routes[key]
		if !ok {
			t.Fatalf("route %q is not registered", key)
		}
		req := httptest.NewRequestWithContext(t.Context(), rt.method, concretePath(rt.pattern), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if auth {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		rt.handler.ServeHTTP(rec, req)
		return rec
	}
	for _, key := range []string{
		"GET /api/v1/roles/actions", "GET /api/v1/roles/attribute-schema",
		"POST /api/v1/roles/validate", "POST /api/v1/roles/simulate",
	} {
		if rec := call(key, "{}", false); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s anonymous: %d, want 401", key, rec.Code)
		}
		if rec := call(key, "{}", true); rec.Code != http.StatusNoContent {
			t.Errorf("%s authenticated without grants: %d, want 204", key, rec.Code)
		}
	}
	named := `{"principal":{"type":"user","id":"` + uuid.NewString() + `"}}`
	if rec := call("POST /api/v1/roles/simulate", named, true); rec.Code != http.StatusForbidden {
		t.Errorf("simulate naming a principal without roles:read: %d, want 403", rec.Code)
	}
}

// The project mirror of the role editor helpers: each is gated on
// roles:read on project/{projectId} (pinned individually), and the simulation
// additionally goes through the principal guard (not a marked gate, so it is
// exercised behaviourally below).
func TestRouteCoverage_ProjectRoleHelperRoutes(t *testing.T) {
	want := []string{
		"GET /api/v1/projects/{projectId}/roles/actions",
		"GET /api/v1/projects/{projectId}/roles/attribute-schema",
		"POST /api/v1/projects/{projectId}/roles/validate",
		"POST /api/v1/projects/{projectId}/roles/simulate",
	}
	routes := map[string]routeGates{}
	for _, rt := range walkGates(t, coverageDeps(newTestIAM(&iamUserStore{}))) {
		routes[rt.key()] = rt
	}
	for _, key := range want {
		rt, ok := routes[key]
		if !ok {
			t.Errorf("%s is not registered", key)
			continue
		}
		if len(rt.gates) != 1 || rt.gates[0].resource != resProject {
			t.Errorf("%s: gates %+v, want exactly one on %s", key, rt.gates, resProject)
		}
	}
}

// R1: a project-scoped roles:read holder may simulate a bare policy but not
// with a named principal (that would expose any account's grants); a holder of
// roles:read on role/* may.
func TestRouteCoverage_ProjectSimulatePrincipalNeedsPlatformRolesRead(t *testing.T) {
	token := issueAccessTokenForRouterTests(t)
	pid := uuid.NewString()
	call := func(grants []iam.Grant, body string) int {
		deps := coverageDeps(newTestIAM(&iamUserStore{grants: grants}))
		for _, rt := range walkGates(t, deps) {
			if rt.key() != "POST /api/v1/projects/{projectId}/roles/simulate" {
				continue
			}
			req := httptest.NewRequestWithContext(t.Context(), rt.method, "/api/v1/projects/"+pid+"/roles/simulate", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			rt.handler.ServeHTTP(rec, req)
			return rec.Code
		}
		t.Fatal("route not registered")
		return 0
	}
	projectReader := []iam.Grant{projectRole(pid, allow([]string{"roles:read"}, "project/"+pid+"/*"))}
	platformReader := []iam.Grant{platformRole(allow([]string{"roles:read"}, "role/*", "project/"+pid))}
	bare := `{"action":"a:b","resource":"*"}`
	named := `{"principal":{"type":"user","id":"` + uuid.NewString() + `"},"action":"a:b","resource":"*"}`
	if got := call(projectReader, bare); got != http.StatusNoContent {
		t.Errorf("project reader, no principal: %d, want pass", got)
	}
	if got := call(projectReader, named); got != http.StatusForbidden {
		t.Errorf("project reader naming a principal: %d, want 403", got)
	}
	if got := call(platformReader, named); got != http.StatusNoContent {
		t.Errorf("platform roles:read holder naming a principal: %d, want pass", got)
	}
}

// A role can name one task, document, sprint or view: the by-id routes are
// authorized on the entity, so a Deny on it blocks every route on it (reads,
// writes and nested resources) and leaves its siblings alone.
// idParam is the URL parameter naming a kind's id (automations are "workflow"
// resources addressed by automationId).
func idParam(kind string) string {
	if kind == "workflow" {
		return "automationId"
	}
	return kind + "Id"
}

func TestEntityRoutesHonourPerEntityPolicy(t *testing.T) {
	token := issueAccessTokenForRouterTests(t)
	project, blocked, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
	cases := []struct {
		kind, collection, actionRead, actionWrite string
	}{
		{"task", "tasks", "tasks:read", "tasks:write"},
		{"doc", "docs", "docs:read", "docs:write"},
		{"sprint", "sprints", "sprints:read", "sprints:write"},
		{"view", "views", "views:read", "views:write"},
		{"agent", "agents", "agents:read", "agents:write"},
		{"workflow", "automations", "workflows:read", "workflows:write"},
		{"conversation", "conversations", "conversations:read", "conversations:write"},
		{"annotation", "annotations", "annotations:read", "annotations:write"},
	}
	for _, c := range cases {
		grants := []iam.Grant{projectRole(project,
			allow([]string{c.actionRead, c.actionWrite}, "project/"+project),
			allow([]string{c.actionRead, c.actionWrite, "conversations:read", "conversations:write"}, "project/"+project+"/"+c.kind+"/*"),
			deny([]string{c.actionRead, c.actionWrite, "conversations:read", "conversations:write"}, "project/"+project+"/"+c.kind+"/"+blocked),
		)}
		routes := walkGates(t, coverageDeps(newTestIAM(&iamUserStore{grants: grants})))
		checked := 0
		for _, rt := range routes {
			res, ok := entityRouteResource(rt.pattern)
			// (by-number is covered in the middleware tests: its number
			// resolves to an id through a lookup.)
			if !ok || !strings.Contains(res, "/"+c.kind+"/") || strings.Contains(rt.pattern, "by-number") {
				continue
			}
			for id, want := range map[string]int{blocked: http.StatusForbidden, other: http.StatusNoContent} {
				path := routeParamRe.ReplaceAllStringFunc(strings.ReplaceAll(rt.pattern, "*", "x"), func(p string) string {
					switch p {
					case "{projectId}":
						return project
					case "{" + idParam(c.kind) + "}":
						return id
					}
					return "11111111-1111-1111-1111-111111111111"
				})
				req := httptest.NewRequestWithContext(t.Context(), rt.method, path, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				rt.handler.ServeHTTP(rec, req)
				if rec.Code != want {
					t.Errorf("%s %s: got %d, want %d", rt.method, path, rec.Code, want)
				}
			}
			checked++
		}
		if checked == 0 {
			t.Errorf("no %s routes were exercised", c.kind)
		}
	}
}

// The routes nested under a port forward act on one annotation: besides the
// environment, a Deny on the annotation (or a role that does not name it)
// blocks every one of them, and its siblings stay reachable.
func TestAnnotationRoutesNestedUnderPortForwardHonourPerAnnotationPolicy(t *testing.T) {
	token := issueAccessTokenForRouterTests(t)
	project, env, blocked, other := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	acts := []string{"annotations:read", "annotations:write", "annotations:resolve", "tasks:write"}
	grants := []iam.Grant{projectRole(project,
		allow(acts, "project/"+project, "project/"+project+"/environment/"+env, "project/"+project+"/annotation/*"),
		deny(acts, "project/"+project+"/annotation/"+blocked),
	)}
	checked := 0
	for _, rt := range walkGates(t, coverageDeps(newTestIAM(&iamUserStore{grants: grants}))) {
		if !strings.Contains(rt.pattern, "/port-forwards/{portForwardId}/annotations/{annotationId}") {
			continue
		}
		for id, want := range map[string]int{blocked: http.StatusForbidden, other: http.StatusNoContent} {
			path := routeParamRe.ReplaceAllStringFunc(rt.pattern, func(p string) string {
				switch p {
				case "{projectId}":
					return project
				case "{environmentId}":
					return env
				case "{annotationId}":
					return id
				}
				return "11111111-1111-1111-1111-111111111111"
			})
			req := httptest.NewRequestWithContext(t.Context(), rt.method, path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			rt.handler.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Errorf("%s %s: got %d, want %d", rt.method, path, rec.Code, want)
			}
		}
		checked++
	}
	if checked != 7 {
		t.Errorf("exercised %d nested annotation routes, want 7", checked)
	}
}

// A bulk task-position update names its tasks in the body: each one is
// authorized (tasks:write on the task), so one Deny refuses the whole request.
func TestBulkTaskPositionsAuthorizeEveryNamedTask(t *testing.T) {
	token := issueAccessTokenForRouterTests(t)
	project, view, blocked, other := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	grants := []iam.Grant{projectRole(project,
		allow([]string{"tasks:write"}, "project/"+project, "project/"+project+"/task/*"),
		deny([]string{"tasks:write"}, "project/"+project+"/task/"+blocked),
	)}
	var rt *routeGates
	routes := walkGates(t, coverageDeps(newTestIAM(&iamUserStore{grants: grants})))
	for i := range routes {
		if routes[i].key() == "PUT /api/v1/projects/{projectId}/views/{viewId}/task-positions" {
			rt = &routes[i]
		}
	}
	if rt == nil {
		t.Fatal("bulk task-positions route is not registered")
	}
	item := func(id string) string { return `{"task_id":"` + id + `","position":1}` }
	for name, c := range map[string]struct {
		body string
		want int
	}{
		"allowed task":                   {`{"items":[` + item(other) + `]}`, http.StatusNoContent},
		"denied task":                    {`{"items":[` + item(blocked) + `]}`, http.StatusForbidden},
		"denied among many":              {`{"items":[` + item(other) + `,` + item(blocked) + `]}`, http.StatusForbidden},
		"malformed is the handler's 400": {`not json`, http.StatusNoContent},
	} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPut,
			"/api/v1/projects/"+project+"/views/"+view+"/task-positions", strings.NewReader(c.body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		rt.handler.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: got %d, want %d", name, rec.Code, c.want)
		}
	}
}

// Creating and reordering views authorize the views they touch, not just the
// project: the sprint of a new view comes from the query, and a reorder names
// its views in the body.
var viewAttrGates = map[string]string{
	"POST /api/v1/projects/{projectId}/views/":         resViewAttrs,
	"PUT /api/v1/projects/{projectId}/views/positions": resViewReorderItems,
}

func TestRouteCoverage_ViewAttrGates(t *testing.T) {
	seen := map[string]bool{}
	for _, rt := range walkGates(t, coverageDeps(newTestIAM(&iamUserStore{}))) {
		var got []string
		for _, g := range rt.gates {
			if g.resource == resViewAttrs || g.resource == resViewReorderItems {
				got = append(got, g.resource)
			}
		}
		want, ok := viewAttrGates[rt.key()]
		switch {
		case ok && (len(got) != 1 || got[0] != want):
			t.Errorf("%s: view attribute gates %v, want [%s]", rt.key(), got, want)
		case !ok && len(got) > 0:
			t.Errorf("%s: unexpected view attribute gate %v", rt.key(), got)
		}
		if ok {
			seen[rt.key()] = true
		}
	}
	for k := range viewAttrGates {
		if !seen[k] {
			t.Errorf("%q is not a registered route", k)
		}
	}
}
