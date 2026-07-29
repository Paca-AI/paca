package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/platform/authz"
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

func withClaims(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), claimsContextKey{}, &domainauth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: uuid.NewString()},
				Role:             role,
				Kind:             "access",
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func TestRequirePermissions_Unauthenticated(t *testing.T) {
	r := chi.NewRouter()
	r.With(RequirePermissions(authz.NewAuthorizer(nil), GlobalScope(), authz.PermissionUsersDelete)).
		Get("/admin", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRequirePermissions_Forbidden(t *testing.T) {
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequirePermissions(authz.NewAuthorizer(nil), GlobalScope(), authz.PermissionUsersDelete)).
		Get("/admin", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestRequirePermissions_AllowedByStore(t *testing.T) {
	store := &mockPermissionStore{globalPerms: []authz.Permission{authz.PermissionUsersDelete}}
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequirePermissions(authz.NewAuthorizer(store), GlobalScope(), authz.PermissionUsersDelete)).
		Get("/admin", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/admin", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequirePermissions_ProjectScope(t *testing.T) {
	store := &mockPermissionStore{projectPerms: []authz.Permission{authz.PermissionTasksWrite}}
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequirePermissions(authz.NewAuthorizer(store), ProjectScopeFromParam("projectId"), authz.PermissionTasksWrite)).
		Get("/projects/{projectId}/tasks", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+uuid.NewString()+"/tasks", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// RequireAnyPermissions
// ---------------------------------------------------------------------------

func TestRequireAnyPermissions_Unauthenticated(t *testing.T) {
	r := chi.NewRouter()
	r.With(RequireAnyPermissions(authz.NewAuthorizer(nil),
		PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
	)).Get("/resource", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/resource", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_Forbidden(t *testing.T) {
	store := &mockPermissionStore{globalPerms: []authz.Permission{authz.PermissionUsersRead}}
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		)).Get("/projects/{projectId}/members", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+uuid.NewString()+"/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_AllowedByFirstGroup_GlobalProjectsRead(t *testing.T) {
	store := &mockPermissionStore{globalPerms: []authz.Permission{authz.PermissionProjectsRead}}
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		)).Get("/projects/{projectId}/members", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+uuid.NewString()+"/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_AllowedBySecondGroup_ProjectScopedRead(t *testing.T) {
	store := &mockPermissionStore{projectPerms: []authz.Permission{authz.PermissionProjectMembersRead}}
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		)).Get("/projects/{projectId}/members", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+uuid.NewString()+"/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_AllowedByWildcard_GlobalProjectsAll(t *testing.T) {
	store := &mockPermissionStore{globalPerms: []authz.Permission{authz.PermissionProjectsAll}}
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		)).Get("/projects/{projectId}/members", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+uuid.NewString()+"/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_InvalidProjectID_Returns400(t *testing.T) {
	store := &mockPermissionStore{}
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		)).Get("/projects/{projectId}/members", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/not-a-uuid/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRequireAnyPermissions_InvalidProjectID_GlobalGroupSucceeds(t *testing.T) {
	store := &mockPermissionStore{globalPerms: []authz.Permission{authz.PermissionProjectsRead}}
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequireAnyPermissions(authz.NewAuthorizer(store),
			PermissionGroup{Scope: GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionProjectMembersRead}},
		)).Get("/projects/{projectId}/members", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/not-a-uuid/members", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// RequirePublicProjectOrPermissions
// ---------------------------------------------------------------------------

type mockVisibilityChecker struct {
	isPublic bool
	err      error
}

func (m *mockVisibilityChecker) IsProjectPublic(_ context.Context, _ uuid.UUID) (bool, error) {
	return m.isPublic, m.err
}

func TestRequirePublicProjectOrPermissions_AnonymousPublicProject_Allows(t *testing.T) {
	checker := &mockVisibilityChecker{isPublic: true}
	r := chi.NewRouter()
	r.With(RequirePublicProjectOrPermissions(checker, authz.NewAuthorizer(nil),
		PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
	)).Get("/projects/{projectId}/tasks", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+uuid.NewString()+"/tasks", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequirePublicProjectOrPermissions_AnonymousPrivateProject_Returns401(t *testing.T) {
	checker := &mockVisibilityChecker{isPublic: false}
	r := chi.NewRouter()
	r.With(RequirePublicProjectOrPermissions(checker, authz.NewAuthorizer(nil),
		PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
	)).Get("/projects/{projectId}/tasks", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+uuid.NewString()+"/tasks", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRequirePublicProjectOrPermissions_AnonymousInvalidProjectID_Returns400(t *testing.T) {
	checker := &mockVisibilityChecker{isPublic: false}
	r := chi.NewRouter()
	r.With(RequirePublicProjectOrPermissions(checker, authz.NewAuthorizer(nil),
		PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
	)).Get("/projects/{projectId}/tasks", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/not-a-uuid/tasks", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRequirePublicProjectOrPermissions_AuthenticatedWithPermission_Allows(t *testing.T) {
	store := &mockPermissionStore{projectPerms: []authz.Permission{authz.PermissionTasksRead}}
	checker := &mockVisibilityChecker{isPublic: false}
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequirePublicProjectOrPermissions(checker, authz.NewAuthorizer(store),
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
		)).Get("/projects/{projectId}/tasks", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+uuid.NewString()+"/tasks", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRequirePublicProjectOrPermissions_AuthenticatedWithoutPermission_Returns403(t *testing.T) {
	store := &mockPermissionStore{}
	checker := &mockVisibilityChecker{isPublic: false}
	r := chi.NewRouter()
	r.With(withClaims("USER"),
		RequirePublicProjectOrPermissions(checker, authz.NewAuthorizer(store),
			PermissionGroup{Scope: ProjectScopeFromParam("projectId"), Permissions: []authz.Permission{authz.PermissionTasksRead}},
		)).Get("/projects/{projectId}/tasks", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/projects/"+uuid.NewString()+"/tasks", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}
