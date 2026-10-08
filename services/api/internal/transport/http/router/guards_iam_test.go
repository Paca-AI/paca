package router

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// iamUserStore hands every user principal the same grants (the router tests
// issue tokens for random subjects).
type iamUserStore struct {
	grants []iam.Grant
	err    error
}

func (s *iamUserStore) ListGrants(_ context.Context, p iam.Principal) ([]iam.Grant, error) {
	if s.err != nil {
		return nil, s.err
	}
	if p.Type != "user" {
		return nil, nil
	}
	return s.grants, nil
}

func newTestIAM(store iam.Store) *iam.Authorizer {
	return iam.NewAuthorizer(store, iam.NewRegistry(), iam.NewAttributeSchema())
}

func allow(actions []string, resources ...string) iam.Statement {
	return iam.Statement{Effect: iam.EffectAllow, Actions: actions, Resources: resources}
}

func deny(actions []string, resources ...string) iam.Statement {
	return iam.Statement{Effect: iam.EffectDeny, Actions: actions, Resources: resources}
}

// platformRole is a platform-wide attachment; projectRole one scoped to a
// project (its statements only ever reach inside that project).
func platformRole(sts ...iam.Statement) iam.Grant {
	return iam.Grant{RoleID: uuid.NewString(), Policy: &iam.Policy{Statements: sts}}
}

func projectRole(projectID string, sts ...iam.Statement) iam.Grant {
	return iam.Grant{RoleID: uuid.NewString(), ProjectID: projectID, Policy: &iam.Policy{Statements: sts}}
}

type iamGateCase struct {
	name          string
	grants        []iam.Grant
	storeErr      error
	authenticated bool
	publicProject bool
	envID         string // defaults to a fresh id
	want          int
}

// runIAMGate serves one request to
// /projects/{projectId}/environments/{environmentId}/agents/{agentId} through
// a gate built from IAM-mode guards, as an authenticated user or anonymously.
func runIAMGate(t *testing.T, projectID string, tc iamGateCase, build func(g guards) func(http.Handler) http.Handler) int {
	t.Helper()
	g := newGuards(Deps{
		IAM:                  newTestIAM(&iamUserStore{grants: tc.grants, err: tc.storeErr}),
		ProjectVisibilitySvc: visibilityFake{public: tc.publicProject},
	})
	tokens := jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour)
	r := chi.NewRouter()
	r.With(httpmw.OptionalAuthn(tokens), build(g)).Get("/projects/{projectId}/environments/{environmentId}/agents/{agentId}",
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	envID := tc.envID
	if envID == "" {
		envID = uuid.NewString()
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/projects/"+projectID+"/environments/"+envID+"/agents/"+uuid.NewString(), nil)
	if tc.authenticated {
		req.Header.Set("Authorization", "Bearer "+issueAccessTokenForRouterTests(t))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

func runIAMGateCases(t *testing.T, projectID string, cases []iamGateCase, build func(g guards) func(http.Handler) http.Handler) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runIAMGate(t, projectID, tc, build); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// Global checks each action on its platform root: a role limited to user/*
// passes users:read but not roles:read (roles:read on role/*). All permissions
// of one gate must pass.
func TestGuardsIAM_Global(t *testing.T) {
	userAdmin := platformRole(allow([]string{"*"}, "user/*"))
	runIAMGateCases(t, uuid.NewString(), []iamGateCase{
		{name: "role on user/* allows users:read", grants: []iam.Grant{userAdmin}, authenticated: true, want: http.StatusNoContent},
		{name: "anonymous", grants: []iam.Grant{userAdmin}, want: http.StatusUnauthorized},
		{name: "authorizer failure is 500", storeErr: errors.New("db down"), authenticated: true, want: http.StatusInternalServerError},
	}, func(g guards) func(http.Handler) http.Handler { return g.Global(iam.ActionUsersRead) })

	runIAMGateCases(t, uuid.NewString(), []iamGateCase{
		{name: "role on user/* denies roles:read", grants: []iam.Grant{userAdmin}, authenticated: true, want: http.StatusForbidden},
		{name: "migrated named global role", grants: []iam.Grant{platformRole(allow([]string{"roles:read"}, testPlatformRoots...))}, authenticated: true, want: http.StatusNoContent},
	}, func(g guards) func(http.Handler) http.Handler { return g.Global(iam.ActionRolesRead) })

	both := platformRole(allow([]string{"users:write", "roles:assign"}, testPlatformRoots...))
	runIAMGateCases(t, uuid.NewString(), []iamGateCase{
		{name: "holds every permission", grants: []iam.Grant{both}, authenticated: true, want: http.StatusNoContent},
		{name: "one of two is not enough", grants: []iam.Grant{platformRole(allow([]string{"users:write"}, testPlatformRoots...))}, authenticated: true, want: http.StatusForbidden},
	}, func(g guards) func(http.Handler) http.Handler {
		return g.Global(iam.ActionUsersWrite, iam.ActionRolesAssign)
	})
}

// A global role holding a named permission does not reach into a project
// (GHSA-hjcj-373w-vq8m); "*" on "*" does, and so does a project role.
func TestGuardsIAM_Project(t *testing.T) {
	p := uuid.NewString()
	runIAMGateCases(t, p, []iamGateCase{
		{name: "named global permission does not reach in", grants: []iam.Grant{platformRole(allow([]string{"sprints:write"}, testPlatformRoots...))}, authenticated: true, want: http.StatusForbidden},
		{name: "star holder does", grants: []iam.Grant{platformRole(allow([]string{"*"}, "*"))}, authenticated: true, want: http.StatusNoContent},
		{name: "project role grants it", grants: []iam.Grant{projectRole(p, allow([]string{"sprints:write"}, "project/"+p+"/*"))}, authenticated: true, want: http.StatusNoContent},
		{name: "project role in another project does not", grants: []iam.Grant{projectRole(uuid.NewString(), allow([]string{"sprints:write"}, "project/*"))}, authenticated: true, want: http.StatusForbidden},
		{name: "no grants", authenticated: true, want: http.StatusForbidden},
		{name: "anonymous on a public project still needs to log in", publicProject: true, want: http.StatusUnauthorized},
	}, func(g guards) func(http.Handler) http.Handler { return g.Project(iam.ActionSprintsWrite) })
}

// ProjectOrPublic: projects:read on the project collection ("project", the
// global group) or the actions on the project; anonymous callers see public
// projects only.
func TestGuardsIAM_ProjectOrPublic(t *testing.T) {
	p := uuid.NewString()
	runIAMGateCases(t, p, []iamGateCase{
		{name: "migrated global projects.read", grants: []iam.Grant{platformRole(allow([]string{"projects:read"}, testPlatformRoots...))}, authenticated: true, want: http.StatusNoContent},
		{name: "global sprints:read alone does not", grants: []iam.Grant{platformRole(allow([]string{"sprints:read"}, testPlatformRoots...))}, authenticated: true, want: http.StatusForbidden},
		{name: "project role grants it", grants: []iam.Grant{projectRole(p, allow([]string{"sprints:read"}, "project/"+p+"/*"))}, authenticated: true, want: http.StatusNoContent},
		{name: "project projects:read does not stand in for the collection", grants: []iam.Grant{projectRole(p, allow([]string{"projects:read"}, "project/"+p+"/*"))}, authenticated: true, want: http.StatusForbidden},
		{name: "anonymous, public project", publicProject: true, want: http.StatusNoContent},
		{name: "anonymous, private project", want: http.StatusUnauthorized},
		{name: "logged in with nothing is refused even on a public project", authenticated: true, publicProject: true, want: http.StatusForbidden},
		{name: "authorizer failure is 500", storeErr: errors.New("db down"), authenticated: true, want: http.StatusInternalServerError},
	}, func(g guards) func(http.Handler) http.Handler { return g.ProjectOrPublic(iam.ActionSprintsRead) })
}

// Environment gates authorize against project/P/environment/E, so a
// Deny statement bites on that environment only.
func TestGuardsIAM_EnvironmentIsResourceScoped(t *testing.T) {
	p, locked := uuid.NewString(), uuid.NewString()
	member := projectRole(p, allow([]string{"environments:*"}, "project/"+p+"/*"))
	denyLocked := projectRole(p, deny([]string{"environments:read", "environments:write", "environments:connect"}, "project/"+p+"/environment/"+locked+"/*"))
	cases := []iamGateCase{
		{name: "member, other environment", grants: []iam.Grant{member, denyLocked}, authenticated: true, want: http.StatusNoContent},
		{name: "member, denied environment", grants: []iam.Grant{member, denyLocked}, envID: locked, authenticated: true, want: http.StatusForbidden},
		{name: "role scoped to that environment only", grants: []iam.Grant{projectRole(p, allow([]string{"environments:read"}, "project/"+p+"/environment/"+locked))}, envID: locked, authenticated: true, want: http.StatusNoContent},
	}
	runIAMGateCases(t, p, cases, func(g guards) func(http.Handler) http.Handler { return g.Environment(iam.ActionEnvironmentsRead) })
}

// AgentUse authorizes against project/P/agent/A.
func TestGuardsIAM_AgentUseIsResourceScoped(t *testing.T) {
	p := uuid.NewString()
	runIAMGateCases(t, p, []iamGateCase{
		{name: "project member", grants: []iam.Grant{projectRole(p, allow([]string{"conversations:write"}, "project/"+p+"/*"))}, authenticated: true, want: http.StatusNoContent},
		{name: "denied on every agent", grants: []iam.Grant{
			projectRole(p, allow([]string{"conversations:write"}, "project/"+p+"/*")),
			projectRole(p, deny([]string{"conversations:write"}, "project/"+p+"/agent/*")),
		}, authenticated: true, want: http.StatusForbidden},
		{name: "project-level only grant does not reach the agent", grants: []iam.Grant{projectRole(p, allow([]string{"conversations:write"}, "project/"+p))}, authenticated: true, want: http.StatusForbidden},
	}, func(g guards) func(http.Handler) http.Handler { return g.AgentUse(iam.ActionConversationsWrite) })
}

// Without an authorizer every gate fails closed with 500.
func TestGuardsIAM_NoAuthorizerFailsClosed(t *testing.T) {
	g := newGuards(Deps{})
	for name, mw := range map[string]func(http.Handler) http.Handler{
		"Global":      g.Global(iam.ActionUsersRead),
		"Project":     g.Project(iam.ActionTasksRead),
		"Environment": g.Environment(iam.ActionEnvironmentsRead),
		"AgentUse":    g.AgentUse(iam.ActionConversationsWrite),
	} {
		if code := serveWithGuard(t, mw); code != http.StatusInternalServerError {
			t.Errorf("%s: got %d, want 500", name, code)
		}
	}
}

func serveWithGuard(t *testing.T, mw func(http.Handler) http.Handler) int {
	t.Helper()
	tokens := jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour)
	r := chi.NewRouter()
	r.With(httpmw.OptionalAuthn(tokens), mw).Get("/projects/{projectId}/environments/{environmentId}/agents/{agentId}",
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
		"/projects/"+uuid.NewString()+"/environments/"+uuid.NewString()+"/agents/"+uuid.NewString(), nil)
	req.Header.Set("Authorization", "Bearer "+issueAccessTokenForRouterTests(t))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

// principalStore serves grants per principal (unlike iamUserStore, which
// hands every user the same ones), so a test can give a user and an agent
// different attachments. Project-scoped grants are only ever listed for
// principals that are members of that project (IAMStore's contract).
type principalStore map[iam.Principal][]iam.Grant

func (s principalStore) ListGrants(_ context.Context, p iam.Principal) ([]iam.Grant, error) {
	return s[p], nil
}

// serveAsPrincipal serves GET /projects/{projectId}/x through the gate as the
// user alone, or — when agentID is set — as an agent-API-key request naming
// that agent (the bot user behind the key is userID).
func serveAsPrincipal(t *testing.T, store iam.Store, build func(g guards) func(http.Handler) http.Handler, userID, agentID, projectID string) int {
	t.Helper()
	g := newGuards(Deps{IAM: newTestIAM(store)})
	inject := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), httpmw.ClaimsContextKey(), &domainauth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: userID}, Kind: "access",
			})
			if agentID != "" {
				ctx = httpmw.WithAgentID(ctx, uuid.MustParse(agentID))
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	r := chi.NewRouter()
	r.With(inject, build(g)).Get("/projects/{projectId}/x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/"+projectID+"/x", nil))
	return rec.Code
}

// A Global gate judges an agent caller by the agent's own platform grants:
// a platform role passes, a project-only role does not, and the bot user
// behind the key never stands in for the agent.
func TestGuardsIAM_AgentCaller_GlobalGate(t *testing.T) {
	user, agent, p := uuid.NewString(), uuid.NewString(), uuid.NewString()
	u, a := iam.User(user), iam.Agent(agent)
	gate := func(g guards) func(http.Handler) http.Handler { return g.Global(iam.ActionUsersRead) }
	platform := platformRole(allow([]string{"users:read"}, "user/*"))
	projectOnly := projectRole(p, allow([]string{"users:read"}, "project/"+p+"/*"))

	cases := []struct {
		name  string
		store principalStore
		agent string
		want  int
	}{
		{"agent with a platform role passes", principalStore{a: {platform}}, agent, http.StatusNoContent},
		{"agent with a project-only role fails", principalStore{a: {projectOnly}}, agent, http.StatusForbidden},
		{"agent without grants fails even though the bot user holds the role", principalStore{u: {platform}}, agent, http.StatusForbidden},
		{"the same role held by a user passes", principalStore{u: {platform}}, "", http.StatusNoContent},
		{"the same project-only role held by a user fails", principalStore{u: {projectOnly}}, "", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serveAsPrincipal(t, tc.store, gate, user, tc.agent, p); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// A Project gate judges an agent by its own project-scoped attachments: an
// agent that is not a member of the project has none there (the store lists
// project-scoped grants for members only) and fails, while a user holding the
// same role as a member passes; an agent attachment only works inside its own
// project.
func TestGuardsIAM_AgentCaller_ProjectGate(t *testing.T) {
	user, agent := uuid.NewString(), uuid.NewString()
	p1, p2 := uuid.NewString(), uuid.NewString()
	u, a := iam.User(user), iam.Agent(agent)
	gate := func(g guards) func(http.Handler) http.Handler { return g.Project(iam.ActionTasksRead) }
	roleIn := func(project string) iam.Grant {
		return projectRole(project, allow([]string{"tasks:read"}, "project/"+project+"/*"))
	}

	cases := []struct {
		name    string
		store   principalStore
		agent   string
		project string
		want    int
	}{
		{"agent that is not a member fails while the user member passes (agent)", principalStore{u: {roleIn(p1)}}, agent, p1, http.StatusForbidden},
		{"agent that is not a member fails while the user member passes (user)", principalStore{u: {roleIn(p1)}}, "", p1, http.StatusNoContent},
		{"agent attachment passes inside its project", principalStore{a: {roleIn(p1)}}, agent, p1, http.StatusNoContent},
		{"agent attachment does not reach another project", principalStore{a: {roleIn(p1)}}, agent, p2, http.StatusForbidden},
		{"agent member of one project, second project's attachment is separate", principalStore{a: {roleIn(p1), roleIn(p2)}}, agent, p2, http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serveAsPrincipal(t, tc.store, gate, user, tc.agent, tc.project); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}
