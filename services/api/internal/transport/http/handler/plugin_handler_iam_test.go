package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
)

type pluginGrantStore []iam.Grant

func (s pluginGrantStore) ListGrants(context.Context, iam.Principal) ([]iam.Grant, error) {
	return s, nil
}

// A plugin route's requireActions actions are all checked on project/<P> (project scope with the project in the path), the
// action's platform root, or plugin/<pluginID> for a plugin's own action.
func TestApplyPluginRouteMiddlewares_RequireActions(t *testing.T) {
	pluginID := uuid.NewString()
	projectID := uuid.NewString()
	allow := func(actions []string, resources ...string) iam.Grant {
		return iam.Grant{RoleID: uuid.NewString(), Policy: &iam.Policy{Statements: []iam.Statement{
			{Effect: iam.EffectAllow, Actions: actions, Resources: resources},
		}}}
	}
	inProject := iam.Grant{RoleID: "p", ProjectID: projectID, Policy: &iam.Policy{Statements: []iam.Statement{
		{Effect: iam.EffectAllow, Actions: []string{"tasks:read", "time_logging:log"}, Resources: []string{"project/" + projectID + "/*"}},
	}}}

	cases := []struct {
		name   string
		mw     plugindom.PluginRouteMiddleware
		params map[string]string
		grants []iam.Grant
		want   int
	}{
		{"project scope, project role", plugindom.PluginRouteMiddleware{Name: "requireActions", Scope: "project", Actions: []string{"time_logging:log"}},
			map[string]string{"projectId": projectID}, []iam.Grant{inProject}, http.StatusNoContent},
		{"project scope, other project", plugindom.PluginRouteMiddleware{Name: "requireActions", Scope: "project", Actions: []string{"time_logging:log"}},
			map[string]string{"projectId": uuid.NewString()}, []iam.Grant{inProject}, http.StatusForbidden},
		{"project scope, malformed id", plugindom.PluginRouteMiddleware{Name: "requireActions", Scope: "project", Actions: []string{"tasks:read"}},
			map[string]string{"projectId": "nope"}, []iam.Grant{inProject}, http.StatusBadRequest},
		{"global builtin on its platform root", plugindom.PluginRouteMiddleware{Name: "requireActions", Actions: []string{"users:read"}},
			nil, []iam.Grant{allow([]string{"users:read"}, "user/*")}, http.StatusNoContent},
		{"global plugin action on plugin/<id>", plugindom.PluginRouteMiddleware{Name: "requireActions", Actions: []string{"time_logging:manage_all"}},
			nil, []iam.Grant{allow([]string{"time_logging:manage_all"}, "plugin/*")}, http.StatusNoContent},
		{"global plugin action, project grant does not count", plugindom.PluginRouteMiddleware{Name: "requireActions", Actions: []string{"time_logging:log"}},
			nil, []iam.Grant{inProject}, http.StatusForbidden},
		{"all actions must pass", plugindom.PluginRouteMiddleware{Name: "requireActions", Scope: "project", Actions: []string{"tasks:read", "tasks:write"}},
			map[string]string{"projectId": projectID}, []iam.Grant{inProject}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &PluginHandler{authorizer: iam.NewAuthorizer(pluginGrantStore(tc.grants), iam.NewRegistry(), iam.NewAttributeSchema())}
			route := &plugindom.PluginRoute{Middlewares: []plugindom.PluginRouteMiddleware{tc.mw}}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
			req = req.WithContext(context.WithValue(req.Context(), middleware.ClaimsContextKey(), &domainauth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: uuid.NewString()}, Kind: "access",
			}))
			rec := httptest.NewRecorder()
			if _, ok := h.applyPluginRouteMiddlewares(rec, req, pluginID, route, tc.params); ok {
				rec.WriteHeader(http.StatusNoContent)
			}
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// A manifest still declaring requirePermissions fails closed with a clear
// error (Validate rejects it at install; migration 000065 converts stored
// manifests).
func TestApplyPluginRouteMiddlewares_RequirePermissionsRejected(t *testing.T) {
	h := &PluginHandler{authorizer: iam.NewAuthorizer(pluginGrantStore{}, iam.NewRegistry(), iam.NewAttributeSchema())}
	route := &plugindom.PluginRoute{Middlewares: []plugindom.PluginRouteMiddleware{{Name: "requirePermissions", Permissions: []string{"tasks.read"}}}}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	if _, ok := h.applyPluginRouteMiddlewares(rec, req, uuid.NewString(), route, nil); ok {
		t.Fatal("requirePermissions route was admitted")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", rec.Code)
	}
}

// A route that omits `middlewares` keeps the long-standing default policy:
// authn + requireFreshPassword and nothing else (no action check — a plugin
// declares requireActions explicitly). Anonymous callers are refused; any
// authenticated caller is admitted. An explicit empty list or a public route
// applies no host middleware at all.
func TestApplyPluginRouteMiddlewares_OmittedMiddlewaresDefaultPolicy(t *testing.T) {
	tm := jwttoken.New("test-secret-test-secret-test-secret", time.Minute, time.Hour)
	// An authorizer that denies everything proves the default adds no action check.
	h := &PluginHandler{
		tokenManager: tm,
		authorizer:   iam.NewAuthorizer(pluginGrantStore{}, iam.NewRegistry(), iam.NewAttributeSchema()),
	}
	access, err := tm.IssueAccess(uuid.NewString(), "alice", "USER", uuid.NewString(), false)
	if err != nil {
		t.Fatal(err)
	}
	mustChange, err := tm.IssueAccess(uuid.NewString(), "bob", "USER", uuid.NewString(), true)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		route *plugindom.PluginRoute
		token string
		want  int
	}{
		{"omitted, anonymous", &plugindom.PluginRoute{}, "", http.StatusUnauthorized},
		{"omitted, authenticated without any grant", &plugindom.PluginRoute{}, access, http.StatusNoContent},
		{"omitted, must change password", &plugindom.PluginRoute{}, mustChange, http.StatusForbidden},
		{"explicit empty list, anonymous", &plugindom.PluginRoute{Middlewares: []plugindom.PluginRouteMiddleware{}}, "", http.StatusNoContent},
		{"public route, anonymous", &plugindom.PluginRoute{Public: true}, "", http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil)
			if tc.token != "" {
				req.AddCookie(&http.Cookie{Name: "access_token", Value: tc.token})
			}
			rec := httptest.NewRecorder()
			if _, ok := h.applyPluginRouteMiddlewares(rec, req, uuid.NewString(), tc.route, nil); ok {
				rec.WriteHeader(http.StatusNoContent)
			}
			if rec.Code != tc.want {
				t.Fatalf("got %d, want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
