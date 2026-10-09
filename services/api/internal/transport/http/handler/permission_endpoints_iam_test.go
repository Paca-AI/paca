package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// grantsByPrincipal serves fixed grants per principal.
type grantsByPrincipal map[iam.Principal][]iam.Grant

func (g grantsByPrincipal) ListGrants(_ context.Context, p iam.Principal) ([]iam.Grant, error) {
	return g[p], nil
}

type agentActionsReader []iam.Action

func (r agentActionsReader) ListAgentGlobalPermissions(context.Context, uuid.UUID) ([]iam.Action, error) {
	return r, nil
}

func decodeActions(t *testing.T, rec *httptest.ResponseRecorder) (map[string]bool, map[string]any) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	raw, ok := env.Data["actions"].([]any)
	if !ok {
		t.Fatalf("expected data.actions array, got %v", env.Data)
	}
	got := map[string]bool{}
	for _, a := range raw {
		got[a.(string)] = true
	}
	return got, env.Data
}

// GET /projects/{id}/members/me/permissions returns the caller's effective
// IAM actions in the project — for an agent-key caller naming an agent, the
// agent's own — never the legacy permission map.
func TestGetMyProjectPermissions_ReturnsEffectiveActions(t *testing.T) {
	projectID, userID, agentID := uuid.New(), uuid.New(), uuid.New()
	p := "project/" + projectID.String()
	inProject := func(actions ...string) iam.Grant {
		return iam.Grant{RoleID: uuid.NewString(), ProjectID: projectID.String(), Policy: &iam.Policy{Statements: []iam.Statement{
			{Effect: iam.EffectAllow, Actions: actions, Resources: []string{p + "/*"}},
		}}}
	}
	store := grantsByPrincipal{
		iam.User(userID.String()):   {inProject("tasks:read", "tasks:write")},
		iam.Agent(agentID.String()): {inProject("docs:read")},
	}
	h := handler.NewProjectHandler(nil, iam.NewAuthorizer(store, iam.NewRegistry(), iam.NewAttributeSchema()))

	serve := func(agent *uuid.UUID) *httptest.ResponseRecorder {
		claims := &domainauth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()}, Kind: "access"}
		if agent != nil {
			s := agent.String()
			claims.AgentID = &s
		}
		r := chi.NewRouter()
		r.With(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), httpmw.ClaimsContextKey(), claims)))
			})
		}).Get("/projects/{projectId}/members/me/permissions", h.GetMyProjectPermissions)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/"+projectID.String()+"/members/me/permissions", nil))
		return rec
	}

	got, data := decodeActions(t, serve(nil))
	if !got["tasks:read"] || !got["tasks:write"] || got["docs:read"] || len(got) != 2 {
		t.Errorf("user actions = %v, want exactly tasks:read, tasks:write", got)
	}
	if _, legacy := data["permissions"]; legacy {
		t.Errorf("legacy permissions map still emitted: %v", data)
	}

	got, _ = decodeActions(t, serve(&agentID))
	if !got["docs:read"] || got["tasks:read"] || len(got) != 1 {
		t.Errorf("agent actions = %v, want exactly docs:read", got)
	}
}

// GET /agents/me/global-permissions returns the agent's platform actions.
func TestAgentGetMyGlobalPermissions_ReturnsActions(t *testing.T) {
	agentID := uuid.New()
	h := handler.NewAgentHandler(nil, "", "", "").WithGlobalPermissionReader(agentActionsReader{iam.ActionAgentsRead, iam.ActionUsersRead})
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(httpmw.WithAgentID(req.Context(), agentID)))
		})
	}).Get("/agents/me/global-permissions", h.GetMyGlobalPermissions)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/agents/me/global-permissions", nil))

	got, data := decodeActions(t, rec)
	if !got["agents:read"] || !got["users:read"] || len(got) != 2 {
		t.Errorf("actions = %v", got)
	}
	if _, legacy := data["permissions"]; legacy {
		t.Errorf("legacy permissions field still emitted: %v", data)
	}
}
