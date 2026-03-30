package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	domainauth "github.com/paca/api/internal/domain/auth"
	"github.com/paca/api/internal/platform/authz"
)

type mockPermissionStore struct {
	globalPerms  []authz.Permission
	projectPerms []authz.Permission
}

func (s *mockPermissionStore) ListGlobalPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return s.globalPerms, nil
}

func (s *mockPermissionStore) ListProjectPermissions(context.Context, uuid.UUID, uuid.UUID) ([]authz.Permission, error) {
	return s.projectPerms, nil
}

func withClaims(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(claimsKey, &domainauth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: uuid.NewString()},
			Role:             role,
			Kind:             "access",
		})
		c.Next()
	}
}

func TestRequirePermissions_Unauthenticated(t *testing.T) {
	r := gin.New()
	r.GET("/admin", RequirePermissions(authz.NewAuthorizer(nil), GlobalScope(), authz.PermissionUsersDelete), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRequirePermissions_Forbidden(t *testing.T) {
	r := gin.New()
	r.GET(
		"/admin",
		withClaims("USER"),
		RequirePermissions(authz.NewAuthorizer(nil), GlobalScope(), authz.PermissionUsersDelete),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestRequirePermissions_AllowedByStore(t *testing.T) {
	r := gin.New()
	store := &mockPermissionStore{globalPerms: []authz.Permission{authz.PermissionUsersDelete}}
	r.GET(
		"/admin",
		withClaims("USER"),
		RequirePermissions(authz.NewAuthorizer(store), GlobalScope(), authz.PermissionUsersDelete),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequirePermissions_ProjectScope(t *testing.T) {
	r := gin.New()
	store := &mockPermissionStore{projectPerms: []authz.Permission{authz.PermissionTasksWrite}}
	r.GET(
		"/projects/:projectId/tasks",
		withClaims("USER"),
		RequirePermissions(authz.NewAuthorizer(store), ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/"+uuid.NewString()+"/tasks", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
