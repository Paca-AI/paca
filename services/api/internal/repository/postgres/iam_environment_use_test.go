package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	environmentdom "github.com/Paca-AI/api/internal/domain/environment"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
)

// An environment Deny role as an admin writes it (restricted access is no
// longer migrated; see the roles guide) — a Deny on the
// environment and its sub-paths with a NotIn principal.id condition,
// attached to every project member — blocks a member who is not listed from
// starting a chat in that environment through the real route gates (the
// agent gate, then the chat environment gate over the real agent
// repository), whether the environment is named in the body or is the
// agent's default; a listed grantee passes both ways.
func TestChatEnvironmentGateHonoursDeny(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	ctx := context.Background()
	p := fx.project()
	envRepo := NewEnvironmentRepository(db)

	newEnv := func(name string) uuid.UUID {
		id := uuid.New()
		if err := envRepo.CreateEnvironment(ctx, &environmentdom.Environment{
			ID: id, ProjectID: p, Name: name, Slug: name + "-" + id.String()[:6],
			Status: environmentdom.StatusRunning, Backend: "docker",
		}); err != nil {
			t.Fatalf("create environment: %v", err)
		}
		return id
	}
	locked, open := newEnv("locked"), newEnv("open")

	grantee, outsider := fx.user(false), fx.user(false)
	fx.member(p, grantee, false, false)
	fx.member(p, outsider, false, false)
	agentID := fx.agent(&p, false)
	fx.member(p, agentID, true, false)

	memberRole := fx.role(`{"version":"1","statements":[{"effect":"Allow","actions":["conversations:write","environments:read"],"resources":["project/*"]}]}`)
	denyPolicy, _ := json.Marshal(map[string]any{"version": "1", "statements": []any{map[string]any{
		"sid": "Restricted", "effect": "Deny",
		"actions":    []string{"environments:read", "environments:write", "environments:connect"},
		"resources":  []string{"project/" + p.String() + "/environment/" + locked.String() + "/*"},
		"conditions": map[string]any{"NotIn": map[string]any{"principal.id": []string{grantee.String()}}},
	}}})
	restrict := fx.role(string(denyPolicy))
	pgExec(t, db, `UPDATE roles SET project_id = $2 WHERE id = $1`, restrict, p)
	for _, u := range []uuid.UUID{grantee, outsider} {
		fx.attach(memberRole, "user", u, &p)
		fx.attach(restrict, "user", u, &p)
	}

	authz := NewIAMAuthorizer(db)
	lookups := middleware.AgentRepoLookups{Repo: NewAgentRepository(db)}
	chain := []func(http.Handler) http.Handler{
		middleware.RequireAction(authz, iam.ActionConversationsWrite, middleware.ProjectChildResource("projectId", "agent", "agentId")),
		middleware.RequireAction(authz, iam.ActionEnvironmentsRead, middleware.ChatEnvironmentResource(lookups, "projectId", "agentId")),
	}
	start := func(user uuid.UUID, body string) int {
		inject := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey(), &domainauth.Claims{
					RegisteredClaims: jwt.RegisteredClaims{Subject: user.String()}, Kind: "access",
				})
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		}
		r := chi.NewRouter()
		r.With(append([]func(http.Handler) http.Handler{inject}, chain...)...).
			Post("/projects/{projectId}/agents/{agentId}/chat-sessions", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) })
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			"/projects/"+p.String()+"/agents/"+agentID.String()+"/chat-sessions", strings.NewReader(body)))
		return rec.Code
	}
	named := func(env uuid.UUID) string { return `{"message":"hi","environment_id":"` + env.String() + `"}` }

	// Named in the body.
	if got := start(outsider, named(locked)); got != http.StatusForbidden {
		t.Fatalf("outsider, locked environment in the body: got %d, want 403", got)
	}
	if got := start(grantee, named(locked)); got != http.StatusCreated {
		t.Fatalf("listed grantee, locked environment in the body: got %d, want 201", got)
	}
	if got := start(outsider, named(open)); got != http.StatusCreated {
		t.Fatalf("outsider, other environment: got %d, want 201", got)
	}

	// Falling back to the agent's default environment.
	pgExec(t, db, `UPDATE agents SET default_environment_id = $2 WHERE id = $1`, agentID, locked)
	if got := start(outsider, `{"message":"hi"}`); got != http.StatusForbidden {
		t.Fatalf("outsider, locked default environment: got %d, want 403", got)
	}
	if got := start(grantee, `{"message":"hi"}`); got != http.StatusCreated {
		t.Fatalf("listed grantee, locked default environment: got %d, want 201", got)
	}
}
