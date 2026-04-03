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

// ---------------------------------------------------------------------------
// RequireAnyPermissions
// ---------------------------------------------------------------------------

func TestRequireAnyPermissions_Unauthenticated(t *testing.T) {
	r := gin.New()
	r.GET("/resource",
		RequireAnyPermissions(authz.NewAuthorizer(nil),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
		),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/resource", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_Forbidden(t *testing.T) {
	// User has neither global projects.read nor project-scoped members.read.
	r := gin.New()
	store := &mockPermissionStore{globalPerms: []authz.Permission{authz.PermissionUsersRead}}
	r.GET("/projects/:projectId/members",
		withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/"+uuid.NewString()+"/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_AllowedByFirstGroup_GlobalProjectsRead(t *testing.T) {
	// User has global projects.read → should be allowed even without project-scoped members.read.
	r := gin.New()
	store := &mockPermissionStore{globalPerms: []authz.Permission{authz.PermissionProjectsRead}}
	r.GET("/projects/:projectId/members",
		withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/"+uuid.NewString()+"/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_AllowedBySecondGroup_ProjectScopedRead(t *testing.T) {
	// User does NOT have global projects.read but does have project-scoped members.read.
	r := gin.New()
	store := &mockPermissionStore{projectPerms: []authz.Permission{authz.PermissionProjectMembersRead}}
	r.GET("/projects/:projectId/members",
		withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/"+uuid.NewString()+"/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_AllowedByWildcard_GlobalProjectsAll(t *testing.T) {
	// User has global projects.* → satisfies projects.read via wildcard expansion.
	r := gin.New()
	store := &mockPermissionStore{globalPerms: []authz.Permission{authz.PermissionProjectsAll}}
	r.GET("/projects/:projectId/members",
		withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/"+uuid.NewString()+"/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_InvalidProjectID_Returns400(t *testing.T) {
	// A request with an invalid (non-UUID) projectId should return 400, not 403.
	// The project-scoped group's resolver fails; no global-scope group is present.
	r := gin.New()
	store := &mockPermissionStore{}
	r.GET("/projects/:projectId/members",
		withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/not-a-uuid/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_InvalidProjectID_GlobalGroupSucceeds(t *testing.T) {
	// Even when the project-scoped group's resolver fails (bad UUID), a preceding
	// global-scope group that satisfies the permission check should allow access.
	r := gin.New()
	store := &mockPermissionStore{globalPerms: []authz.Permission{authz.PermissionProjectsRead}}
	r.GET("/projects/:projectId/members",
		withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/projects/not-a-uuid/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
