package postgres

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
)

// The route gate runs over the real IAM store for an agent caller: an agent
// that is not a member of the project has no grants there (while a user
// member holding the same role passes), and an agent's project-scoped
// attachment only works inside its own project.
func TestIAMGateAgentPrincipalOverRealStore(t *testing.T) {
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	authz := NewIAMAuthorizer(db)
	p1, p2 := fx.project(), fx.project()
	role := fx.role(projAllowPolicy) // tasks:read on project/*

	gate := middleware.RequireActions(authz, middleware.ActionCheck{
		Action:   "tasks:read",
		Resource: middleware.ProjectResource("projectId"),
	})
	serve := func(userID, agentID uuid.UUID, project uuid.UUID) int {
		inject := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey(), &domainauth.Claims{
					RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()}, Kind: "access",
				})
				if agentID != uuid.Nil {
					ctx = middleware.WithAgentID(ctx, agentID)
				}
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		}
		r := chi.NewRouter()
		r.With(inject, gate).Get("/projects/{projectId}/x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/"+project.String()+"/x", nil))
		return rec.Code
	}

	user := fx.user(false)
	fx.attach(role, "user", user, &p1)
	fx.member(p1, user, false, false)

	agent := fx.agent(nil, false)
	fx.attach(role, "agent", agent, &p1)

	if got := serve(user, uuid.Nil, p1); got != http.StatusNoContent {
		t.Fatalf("user member: got %d, want 204", got)
	}
	if got := serve(user, agent, p1); got != http.StatusForbidden {
		t.Fatalf("agent attached but not a member of the project: got %d, want 403", got)
	}

	fx.member(p1, agent, true, false)
	if got := serve(user, agent, p1); got != http.StatusNoContent {
		t.Fatalf("agent member with its own attachment: got %d, want 204", got)
	}
	if got := serve(user, agent, p2); got != http.StatusForbidden {
		t.Fatalf("agent attachment must not reach another project: got %d, want 403", got)
	}
}
