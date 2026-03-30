package router

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	domainauth "github.com/paca/api/internal/domain/auth"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
	userdom "github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/platform/authz"
	jwttoken "github.com/paca/api/internal/platform/token"
	"github.com/paca/api/internal/transport/http/handler"
)

type mockAuthSvc struct{}

func (m *mockAuthSvc) Login(context.Context, string, string, bool) (*domainauth.TokenPair, error) {
	return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: 24 * time.Hour}, nil
}
func (m *mockAuthSvc) Refresh(context.Context, string) (*domainauth.TokenPair, error) {
	return &domainauth.TokenPair{AccessToken: "at2", RefreshToken: "rt2", RefreshTTL: 24 * time.Hour}, nil
}
func (m *mockAuthSvc) Logout(context.Context, string) error { return nil }

type mockUserSvc struct{}

func (m *mockUserSvc) GetByID(context.Context, uuid.UUID) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice", Role: userdom.RoleUser}, nil
}
func (m *mockUserSvc) ListGlobalPermissions(context.Context, uuid.UUID) ([]string, error) {
	return []string{string(authz.PermissionUsersRead)}, nil
}
func (m *mockUserSvc) Create(context.Context, userdom.CreateInput) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice", Role: userdom.RoleUser}, nil
}
func (m *mockUserSvc) Update(context.Context, uuid.UUID, userdom.UpdateInput) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice Updated", Role: userdom.RoleUser}, nil
}
func (m *mockUserSvc) Delete(context.Context, uuid.UUID) error { return nil }

type mockGlobalRoleSvc struct{}

func (m *mockGlobalRoleSvc) List(context.Context) ([]*globalroledom.GlobalRole, error) {
	return []*globalroledom.GlobalRole{{ID: uuid.New(), Name: "SUPER_ADMIN", Permissions: map[string]any{}}}, nil
}
func (m *mockGlobalRoleSvc) Create(context.Context, globalroledom.CreateInput) (*globalroledom.GlobalRole, error) {
	return &globalroledom.GlobalRole{ID: uuid.New(), Name: "SUPER_ADMIN", Permissions: map[string]any{}}, nil
}
func (m *mockGlobalRoleSvc) Update(context.Context, uuid.UUID, globalroledom.UpdateInput) (*globalroledom.GlobalRole, error) {
	return &globalroledom.GlobalRole{ID: uuid.New(), Name: "SUPER_ADMIN", Permissions: map[string]any{}}, nil
}
func (m *mockGlobalRoleSvc) Delete(context.Context, uuid.UUID) error { return nil }
func (m *mockGlobalRoleSvc) ReplaceUserRoles(context.Context, uuid.UUID, []uuid.UUID) ([]*globalroledom.GlobalRole, error) {
	return []*globalroledom.GlobalRole{}, nil
}

type allowAllPermissionStore struct{}

func (s *allowAllPermissionStore) ListGlobalPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return []authz.Permission{authz.PermissionAll}, nil
}

func (s *allowAllPermissionStore) ListProjectPermissions(context.Context, uuid.UUID, uuid.UUID) ([]authz.Permission, error) {
	return []authz.Permission{authz.PermissionAll}, nil
}

type staticPermissionStore struct {
	globalPerms []authz.Permission
}

func (s *staticPermissionStore) ListGlobalPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return s.globalPerms, nil
}

func (s *staticPermissionStore) ListProjectPermissions(context.Context, uuid.UUID, uuid.UUID) ([]authz.Permission, error) {
	return nil, nil
}

func newTestRouter(t *testing.T) *gin.Engine {
	return newTestRouterWithStore(t, &allowAllPermissionStore{})
}

func newTestRouterWithStore(t *testing.T, store authz.PermissionStore) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	deps := Deps{
		TokenManager: jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour),
		Authorizer:   authz.NewAuthorizer(store),
		Health:       handler.NewHealthHandler(),
		Auth: handler.NewAuthHandler(&mockAuthSvc{}, handler.CookieConfig{
			Secure:            false,
			AccessTTL:         15 * time.Minute,
			RefreshTTL:        24 * time.Hour,
			RefreshSessionTTL: 12 * time.Hour,
		}),
		User:       handler.NewUserHandler(&mockUserSvc{}),
		GlobalRole: handler.NewGlobalRoleHandler(&mockGlobalRoleSvc{}),
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	return New(deps)
}

func issueAccessTokenForRouterTests(t *testing.T) string {
	t.Helper()
	tm := jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour)
	tok, err := tm.IssueAccess(uuid.NewString(), "alice", "USER", "fam-1")
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	return tok
}

func TestNew_HealthRoute(t *testing.T) {
	r := newTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/healthz", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestNew_CORSPreflight(t *testing.T) {
	r := newTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/any", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected CORS origin '*', got %q", got)
	}
}

func TestNew_RequestIDPropagation(t *testing.T) {
	r := newTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/healthz", nil)
	req.Header.Set("X-Request-ID", "req-123")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("X-Request-ID"); got != "req-123" {
		t.Fatalf("expected echoed request id, got %q", got)
	}
}

func TestNew_ProtectedRouteRequiresAuth(t *testing.T) {
	r := newTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestNew_MeGlobalPermissionsRouteRequiresAuth(t *testing.T) {
	r := newTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me/global-permissions", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestNew_PublicCreateUserRoute(t *testing.T) {
	r := newTestRouter(t)

	body := bytes.NewBufferString(`{"username":"alice","password":"secret12","full_name":"Alice"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/users", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAdminRoute_ListGlobalRoles_RequiresReadPermission(t *testing.T) {
	r := newTestRouterWithStore(t, &staticPermissionStore{globalPerms: []authz.Permission{authz.PermissionGlobalRolesRead}})
	tok := issueAccessTokenForRouterTests(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/global-roles", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAdminRoute_CreateGlobalRole_RequiresWritePermission(t *testing.T) {
	r := newTestRouterWithStore(t, &staticPermissionStore{globalPerms: []authz.Permission{authz.PermissionGlobalRolesRead}})
	tok := issueAccessTokenForRouterTests(t)

	body := bytes.NewBufferString(`{"name":"SECURITY","permissions":{"global_roles.read":true}}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/global-roles", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without write permission, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAdminRoute_AssignGlobalRoles_RequiresAssignPermission(t *testing.T) {
	r := newTestRouterWithStore(t, &staticPermissionStore{globalPerms: []authz.Permission{authz.PermissionGlobalRolesWrite}})
	tok := issueAccessTokenForRouterTests(t)

	body := bytes.NewBufferString(`{"role_ids":[]}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/admin/users/"+uuid.NewString()+"/global-roles", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without assign permission, got %d (%s)", w.Code, w.Body.String())
	}
}
