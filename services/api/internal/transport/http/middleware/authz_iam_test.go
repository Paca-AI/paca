package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// fakeIAMStore serves fixed grants per principal, or err for every lookup.
type fakeIAMStore struct {
	grants map[iam.Principal][]iam.Grant
	err    error
	calls  int
}

func (s *fakeIAMStore) ListGrants(_ context.Context, p iam.Principal) ([]iam.Grant, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.grants[p], nil
}

func newFakeIAM(store *fakeIAMStore) *iam.Authorizer {
	return iam.NewAuthorizer(store, iam.NewRegistry(), iam.NewAttributeSchema())
}

// platformGrant is a platform-wide attachment of one Allow statement.
func platformGrant(actions, resources []string) iam.Grant {
	return iam.Grant{RoleID: uuid.NewString(), Policy: &iam.Policy{Statements: []iam.Statement{
		{Effect: iam.EffectAllow, Actions: actions, Resources: resources},
	}}}
}

// projectGrant is an attachment scoped to projectID holding the statements.
func projectGrant(projectID string, sts ...iam.Statement) iam.Grant {
	return iam.Grant{RoleID: uuid.NewString(), ProjectID: projectID, Policy: &iam.Policy{Statements: sts}}
}

// asCaller injects authenticated claims for userID and, when agentID is not
// uuid.Nil, the agent identity an agent-API-key request carries.
func asCaller(userID, agentID uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), claimsContextKey{}, &domainauth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()},
				Kind:             "access",
			})
			if agentID != uuid.Nil {
				ctx = WithAgentID(ctx, agentID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func serveGate(t *testing.T, pattern, path string, mws ...func(http.Handler) http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.With(mws...).Get(pattern, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	return rec
}

func TestRequireAction_UserAndAgentPrincipals(t *testing.T) {
	projectID := uuid.New()
	envID := uuid.New()
	userID := uuid.New()
	agentID := uuid.New()
	pattern := "/projects/{projectId}/environments/{environmentId}"
	path := "/projects/" + projectID.String() + "/environments/" + envID.String()
	res := ProjectChildResource("projectId", "environment", "environmentId")

	envReader := projectGrant(projectID.String(), iam.Statement{
		Effect: iam.EffectAllow, Actions: []string{"environments:read"}, Resources: []string{"project/" + projectID.String() + "/*"},
	})
	user := iam.Principal{Type: "user", ID: userID.String()}
	agent := iam.Principal{Type: "agent", ID: agentID.String()}

	cases := []struct {
		name    string
		agent   bool
		grants  map[iam.Principal][]iam.Grant
		err     error
		action  iam.Action
		want    int
		wantErr string
	}{
		{name: "user allowed", grants: map[iam.Principal][]iam.Grant{user: {envReader}}, action: "environments:read", want: http.StatusNoContent},
		{name: "user denied other action", grants: map[iam.Principal][]iam.Grant{user: {envReader}}, action: "environments:connect", want: http.StatusForbidden, wantErr: "FORBIDDEN"},
		{name: "user store error is 500", err: errors.New("db down"), action: "environments:read", want: http.StatusInternalServerError},
		// The agent is judged by its own grants, never the user (bot) behind its key.
		{name: "agent allowed by own grant", agent: true, grants: map[iam.Principal][]iam.Grant{agent: {envReader}}, action: "environments:read", want: http.StatusNoContent},
		{name: "agent not allowed by key user's grant", agent: true, grants: map[iam.Principal][]iam.Grant{user: {envReader}}, action: "environments:read", want: http.StatusForbidden, wantErr: "FORBIDDEN"},
		{name: "agent store error is 500", agent: true, err: errors.New("db down"), action: "environments:read", want: http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newFakeIAM(&fakeIAMStore{grants: tc.grants, err: tc.err})
			caller := asCaller(userID, uuid.Nil)
			if tc.agent {
				caller = asCaller(userID, agentID)
			}
			rec := serveGate(t, pattern, path, caller, RequireAction(a, tc.action, res))
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
			if tc.wantErr != "" {
				if got := errorCode(t, rec); got != tc.wantErr {
					t.Fatalf("error_code = %q, want %q", got, tc.wantErr)
				}
			}
		})
	}
}

func TestRequireAction_Unauthenticated(t *testing.T) {
	a := newFakeIAM(&fakeIAMStore{})
	rec := serveGate(t, "/admin", "/admin", RequireAction(a, "users:read", StaticResource("user/*")))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

func TestRequireAction_NilAuthorizerFailsClosed(t *testing.T) {
	rec := serveGate(t, "/admin", "/admin", asCaller(uuid.New(), uuid.Nil), RequireAction(nil, "users:read", StaticResource("user/*")))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rec.Code)
	}
}

func TestRequireAction_InvalidUUIDIs400WithoutConsultingStore(t *testing.T) {
	store := &fakeIAMStore{}
	a := newFakeIAM(store)
	cases := []struct{ name, path string }{
		{"bad project", "/projects/not-a-uuid/environments/" + uuid.NewString()},
		{"bad environment", "/projects/" + uuid.NewString() + "/environments/nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveGate(t, "/projects/{projectId}/environments/{environmentId}", tc.path,
				asCaller(uuid.New(), uuid.Nil),
				RequireAction(a, "environments:read", ProjectChildResource("projectId", "environment", "environmentId")))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400", rec.Code)
			}
		})
	}
	if store.calls != 0 {
		t.Fatalf("store consulted %d times for a malformed request", store.calls)
	}
}

func TestRequireActions_AllMustPass(t *testing.T) {
	projectID := uuid.New()
	userID := uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	grant := projectGrant(projectID.String(), iam.Statement{
		Effect: iam.EffectAllow, Actions: []string{"agents:write"}, Resources: []string{"project/" + projectID.String() + "/*"},
	})
	a := newFakeIAM(&fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: {grant}}})
	res := ProjectResource("projectId")
	path := "/projects/" + projectID.String()

	rec := serveGate(t, "/projects/{projectId}", path, asCaller(userID, uuid.Nil),
		RequireActions(a, ActionCheck{Action: "agents:write", Resource: res}, ActionCheck{Action: "project.members:write", Resource: res}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("one of two actions: got %d, want 403", rec.Code)
	}
	rec = serveGate(t, "/projects/{projectId}", path, asCaller(userID, uuid.Nil),
		RequireActions(a, ActionCheck{Action: "agents:write", Resource: res}))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("the held action: got %d, want 204", rec.Code)
	}
}

// A Deny on one environment carves one environment
// out of a broad project Allow; the resource-aware gate is what makes it bite.
func TestRequireAction_DenyOnOneEnvironment(t *testing.T) {
	projectID, locked, open := uuid.New(), uuid.New(), uuid.New()
	userID := uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	p := "project/" + projectID.String()
	member := projectGrant(projectID.String(), iam.Statement{
		Effect: iam.EffectAllow, Actions: []string{"environments:*"}, Resources: []string{p + "/*"},
	})
	denyLocked := projectGrant(projectID.String(), iam.Statement{
		Effect: iam.EffectDeny, Actions: []string{"environments:connect"}, Resources: []string{p + "/environment/" + locked.String() + "/*"},
	})
	a := newFakeIAM(&fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: {member, denyLocked}}})
	gate := RequireAction(a, "environments:connect", ProjectChildResource("projectId", "environment", "environmentId"))
	pattern := "/projects/{projectId}/environments/{environmentId}"

	if rec := serveGate(t, pattern, "/projects/"+projectID.String()+"/environments/"+locked.String(), asCaller(userID, uuid.Nil), gate); rec.Code != http.StatusForbidden {
		t.Fatalf("denied environment: got %d, want 403", rec.Code)
	}
	if rec := serveGate(t, pattern, "/projects/"+projectID.String()+"/environments/"+open.String(), asCaller(userID, uuid.Nil), gate); rec.Code != http.StatusNoContent {
		t.Fatalf("other environment: got %d, want 204", rec.Code)
	}
}

func TestResourceResolvers(t *testing.T) {
	p, e := uuid.NewString(), uuid.NewString()
	cases := []struct {
		name    string
		pattern string
		path    string
		res     ResourceResolver
		want    string
		wantErr bool
	}{
		{"project", "/p/{projectId}", "/p/" + p, ProjectResource("projectId"), "project/" + p, false},
		{"project child", "/p/{projectId}/e/{environmentId}", "/p/" + p + "/e/" + e, ProjectChildResource("projectId", "environment", "environmentId"), "project/" + p + "/environment/" + e, false},
		{"project child agent", "/p/{projectId}/a/{agentId}", "/p/" + p + "/a/" + e, ProjectChildResource("projectId", "agent", "agentId"), "project/" + p + "/agent/" + e, false},
		{"static", "/s", "/s", StaticResource("settings"), "settings", false},
		{"uppercase uuid is canonicalised", "/p/{projectId}", "/p/" + "A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11", ProjectResource("projectId"), "project/a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", false},
		{"invalid project", "/p/{projectId}", "/p/zzz", ProjectResource("projectId"), "", true},
		{"invalid child", "/p/{projectId}/e/{environmentId}", "/p/" + p + "/e/zzz", ProjectChildResource("projectId", "environment", "environmentId"), "", true},
		{"invalid child project", "/p/{projectId}/e/{environmentId}", "/p/zzz/e/" + e, ProjectChildResource("projectId", "environment", "environmentId"), "", true},
		{"missing param", "/p", "/p", ProjectResource("projectId"), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			var gotErr error
			r := chi.NewRouter()
			r.Get(tc.pattern, func(_ http.ResponseWriter, req *http.Request) { got, gotErr = tc.res(req) })
			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil))
			if tc.wantErr {
				if gotErr == nil {
					t.Fatalf("got %q, want an error", got)
				}
				if status := statusOf(t, gotErr); status != http.StatusBadRequest {
					t.Fatalf("error maps to %d, want 400", status)
				}
				return
			}
			if gotErr != nil || got != tc.want {
				t.Fatalf("got (%q, %v), want %q", got, gotErr, tc.want)
			}
		})
	}
}

func TestRequirePublicProjectOrActions(t *testing.T) {
	projectID := uuid.New()
	userID := uuid.New()
	user := iam.Principal{Type: "user", ID: userID.String()}
	p := "project/" + projectID.String()
	pattern := "/projects/{projectId}"
	path := "/projects/" + projectID.String()

	cases := []struct {
		name          string
		grants        []iam.Grant
		authenticated bool
		public        bool
		path          string
		want          int
	}{
		{name: "global projects:read on the collection", authenticated: true, grants: []iam.Grant{platformGrant([]string{"projects:read"}, []string{"project"})}, want: http.StatusNoContent},
		{name: "project role holding the action", authenticated: true, grants: []iam.Grant{projectGrant(projectID.String(), iam.Statement{Effect: iam.EffectAllow, Actions: []string{"tasks:read"}, Resources: []string{p + "/*"}})}, want: http.StatusNoContent},
		{name: "global tasks:read on platform roots only", authenticated: true, grants: []iam.Grant{platformGrant([]string{"tasks:read"}, []string{"user/*", "role/*", "project"})}, want: http.StatusForbidden},
		{name: "star holder", authenticated: true, grants: []iam.Grant{platformGrant([]string{"*"}, []string{"*"})}, want: http.StatusNoContent},
		{name: "nothing held, public project: logged in does not fall back", authenticated: true, public: true, want: http.StatusForbidden},
		{name: "global group still admits on malformed project id", authenticated: true, path: "/projects/zzz", grants: []iam.Grant{platformGrant([]string{"projects:read"}, []string{"project"})}, want: http.StatusNoContent},
		{name: "malformed project id and nothing held", authenticated: true, path: "/projects/zzz", want: http.StatusBadRequest},
		{name: "anonymous on public project", public: true, want: http.StatusNoContent},
		{name: "anonymous on private project", want: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newFakeIAM(&fakeIAMStore{grants: map[iam.Principal][]iam.Grant{user: tc.grants}})
			var mws []func(http.Handler) http.Handler
			if tc.authenticated {
				mws = append(mws, asCaller(userID, uuid.Nil))
			}
			mws = append(mws, RequirePublicProjectOrActions(publicFake{public: tc.public}, a,
				[]ActionCheck{{Action: "projects:read", Resource: StaticResource("project")}},
				[]ActionCheck{{Action: "tasks:read", Resource: ProjectResource("projectId")}},
			))
			reqPath := path
			if tc.path != "" {
				reqPath = tc.path
			}
			if rec := serveGate(t, pattern, reqPath, mws...); rec.Code != tc.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

type publicFake struct{ public bool }

func (f publicFake) IsProjectPublic(context.Context, uuid.UUID) (bool, error) { return f.public, nil }

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		ErrorCode string `json:"error_code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return env.ErrorCode
}

// statusOf renders err the way every gate does and returns the status.
func statusOf(t *testing.T, err error) int {
	t.Helper()
	rec := httptest.NewRecorder()
	proceedIfAllowed(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), false, err)
	return rec.Code
}
