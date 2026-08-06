package router

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"

	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/transport/http/handler"
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
func (m *mockUserSvc) List(context.Context, int, int) ([]*userdom.User, int64, error) {
	return []*userdom.User{}, 0, nil
}
func (m *mockUserSvc) CountUsers(context.Context) (int64, error) {
	return 0, nil
}
func (m *mockUserSvc) ListGlobalPermissions(context.Context, uuid.UUID) ([]string, error) {
	return []string{string(authz.PermissionUsersRead)}, nil
}
func (m *mockUserSvc) Create(context.Context, userdom.CreateInput) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice", Role: userdom.RoleUser}, nil
}
func (m *mockUserSvc) UpdateProfile(context.Context, uuid.UUID, userdom.UpdateProfileInput) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice Updated", Role: userdom.RoleUser}, nil
}
func (m *mockUserSvc) AdminUpdate(context.Context, uuid.UUID, userdom.AdminUpdateInput) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice Updated", Role: userdom.RoleUser}, nil
}
func (m *mockUserSvc) ResetPassword(context.Context, uuid.UUID, string) error            { return nil }
func (m *mockUserSvc) ChangeMyPassword(context.Context, uuid.UUID, string, string) error { return nil }
func (m *mockUserSvc) Delete(context.Context, uuid.UUID) error                           { return nil }

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

// stubProjectSvc is a minimal projectdom.Service with no projects, just
// enough to exercise routing for the /projects collection endpoints.
type stubProjectSvc struct{}

func (s *stubProjectSvc) List(context.Context, int, int) ([]*projectdom.Project, int64, error) {
	return nil, 0, nil
}
func (s *stubProjectSvc) ListAccessible(context.Context, uuid.UUID, int, int) ([]*projectdom.Project, int64, error) {
	return nil, 0, nil
}
func (s *stubProjectSvc) GetByID(context.Context, uuid.UUID) (*projectdom.Project, error) {
	return nil, projectdom.ErrNotFound
}
func (s *stubProjectSvc) IsProjectPublic(context.Context, uuid.UUID) (bool, error) {
	return false, nil
}
func (s *stubProjectSvc) Create(context.Context, projectdom.CreateProjectInput) (*projectdom.Project, error) {
	return nil, nil
}
func (s *stubProjectSvc) Update(context.Context, uuid.UUID, projectdom.UpdateProjectInput) (*projectdom.Project, error) {
	return nil, nil
}
func (s *stubProjectSvc) Delete(context.Context, uuid.UUID) error { return nil }
func (s *stubProjectSvc) ListMembers(context.Context, uuid.UUID) ([]*projectdom.ProjectMember, error) {
	return nil, nil
}
func (s *stubProjectSvc) CountDistinctAgentsByProjects(context.Context, []uuid.UUID) (int64, error) {
	return 0, nil
}
func (s *stubProjectSvc) AddMember(context.Context, uuid.UUID, projectdom.AddMemberInput) (*projectdom.ProjectMember, error) {
	return nil, nil
}
func (s *stubProjectSvc) UpdateMemberRole(context.Context, uuid.UUID, uuid.UUID, projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error) {
	return nil, nil
}
func (s *stubProjectSvc) RemoveMember(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (s *stubProjectSvc) UpdateMemberRoleByMemberID(context.Context, uuid.UUID, uuid.UUID, projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error) {
	return nil, nil
}
func (s *stubProjectSvc) RemoveMemberByMemberID(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *stubProjectSvc) GetMyProjectPermissions(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID) (map[string]any, error) {
	return nil, nil
}
func (s *stubProjectSvc) AddAgentMember(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *stubProjectSvc) RemoveAgentMember(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (s *stubProjectSvc) ListRoles(context.Context, uuid.UUID) ([]*projectdom.ProjectRole, error) {
	return nil, nil
}
func (s *stubProjectSvc) CreateRole(context.Context, uuid.UUID, projectdom.CreateRoleInput) (*projectdom.ProjectRole, error) {
	return nil, nil
}
func (s *stubProjectSvc) UpdateRole(context.Context, uuid.UUID, uuid.UUID, projectdom.UpdateRoleInput) (*projectdom.ProjectRole, error) {
	return nil, nil
}
func (s *stubProjectSvc) DeleteRole(context.Context, uuid.UUID, uuid.UUID) error { return nil }

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

func newTestRouter(t *testing.T) http.Handler {
	return newTestRouterWithStore(t, &allowAllPermissionStore{})
}

func newTestRouterWithStore(t *testing.T, store authz.PermissionStore) http.Handler {
	t.Helper()

	authorizer := authz.NewAuthorizer(store)
	deps := Deps{
		TokenManager: jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour),
		Authorizer:   authorizer,
		Health:       handler.NewHealthHandler(),
		Auth: handler.NewAuthHandler(&mockAuthSvc{}, handler.CookieConfig{
			Secure:            false,
			AccessTTL:         15 * time.Minute,
			RefreshTTL:        24 * time.Hour,
			RefreshSessionTTL: 12 * time.Hour,
		}),
		User:       handler.NewUserHandler(&mockUserSvc{}),
		GlobalRole: handler.NewGlobalRoleHandler(&mockGlobalRoleSvc{}),
		Project:    handler.NewProjectHandler(&stubProjectSvc{}, authorizer),
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	return New(deps)
}

func issueAccessTokenForRouterTests(t *testing.T) string {
	t.Helper()
	tm := jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour)
	tok, err := tm.IssueAccess(uuid.NewString(), "alice", "USER", "fam-1", false)
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

func TestNew_CORSAllowList(t *testing.T) {
	deps := Deps{
		TokenManager:       jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour),
		Authorizer:         authz.NewAuthorizer(&allowAllPermissionStore{}),
		Health:             handler.NewHealthHandler(),
		Log:                slog.New(slog.NewTextHandler(io.Discard, nil)),
		CORSAllowedOrigins: []string{"https://paca.example.com"},
	}
	r := New(deps)

	t.Run("allowed origin is echoed back", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/healthz", nil)
		req.Header.Set("Origin", "https://paca.example.com")
		r.ServeHTTP(w, req)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://paca.example.com" {
			t.Fatalf("expected allowed origin echoed back, got %q", got)
		}
	})

	t.Run("unlisted origin gets no CORS header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/healthz", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		r.ServeHTTP(w, req)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("expected no CORS header for unlisted origin, got %q", got)
		}
	})
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

func TestAdminRoute_CreateUser_RequiresAuth(t *testing.T) {
	r := newTestRouter(t)

	// Without auth token — must be rejected.
	body := bytes.NewBufferString(`{"username":"alice","password":"secret12","full_name":"Alice"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/users", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated create user, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAdminRoute_CreateUser_WithPermission(t *testing.T) {
	r := newTestRouterWithStore(t, &staticPermissionStore{globalPerms: []authz.Permission{authz.PermissionUsersWrite}})
	tok := issueAccessTokenForRouterTests(t)

	body := bytes.NewBufferString(`{"username":"alice","password":"secret12","full_name":"Alice"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/users", body)
	req.Header.Set("Authorization", "Bearer "+tok)
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

// TestProjectsRoute_WorkspaceStats_NotShadowedByProjectIDRoute guards against
// a regression where "/projects" and "/projects/{projectId}" were registered
// as two separate chi Route()/Mount() calls: chi treated the {projectId}
// mount as matching ANY sub-path of "/projects", including the static
// "/workspace-stats" route, so "workspace-stats" got bound to {projectId} and
// failed uuid.Parse with "invalid project id". See router.go's Projects
// collection block, which now uses r.Group instead of a separate r.Route.
func TestProjectsRoute_WorkspaceStats_NotShadowedByProjectIDRoute(t *testing.T) {
	r := newTestRouter(t)
	tok := issueAccessTokenForRouterTests(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/projects/workspace-stats", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from GetWorkspaceStats, got %d (%s)", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "invalid project id") {
		t.Fatalf("workspace-stats request was shadowed by the /projects/{projectId} route: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "open_task_count") {
		t.Fatalf("expected WorkspaceStatsResponse body, got %s", w.Body.String())
	}
}

// TestProjectsRoute_GetByID_StillParsesProjectID is the counterpart check:
// the {projectId} route must still receive and parse a real project ID after
// the r.Group fix, rather than always falling through to the collection
// routes.
func TestProjectsRoute_GetByID_StillParsesProjectID(t *testing.T) {
	r := newTestRouter(t)
	tok := issueAccessTokenForRouterTests(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/projects/"+uuid.NewString(), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if strings.Contains(w.Body.String(), "invalid project id") {
		t.Fatalf("valid project UUID was rejected as invalid: %s", w.Body.String())
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (unknown project id from stub service), got %d (%s)", w.Code, w.Body.String())
	}
}
