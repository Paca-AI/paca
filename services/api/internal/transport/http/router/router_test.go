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

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
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
func (m *mockAuthSvc) RefreshAnnotation(context.Context, string) (*domainauth.TokenPair, error) {
	return &domainauth.TokenPair{AnnotationAccessToken: "aat2", AnnotationRefreshToken: "art2", RefreshTTL: 24 * time.Hour}, nil
}
func (m *mockAuthSvc) Logout(context.Context, string) error { return nil }

type mockUserSvc struct{}

func (m *mockUserSvc) GetByID(context.Context, uuid.UUID) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice"}, nil
}
func (m *mockUserSvc) List(context.Context, int, int, userdom.ListFilter) ([]*userdom.User, int64, error) {
	return []*userdom.User{}, 0, nil
}
func (m *mockUserSvc) CountUsers(context.Context) (int64, error) {
	return 0, nil
}
func (m *mockUserSvc) ListAfter(context.Context, int, *string, userdom.ListFilter) ([]*userdom.User, bool, error) {
	return nil, false, nil
}
func (m *mockUserSvc) CountUsersMustChangePassword(context.Context) (int64, error) {
	return 0, nil
}
func (m *mockUserSvc) ListGlobalPermissions(context.Context, uuid.UUID) ([]string, error) {
	return []string{string(iam.ActionUsersRead)}, nil
}
func (m *mockUserSvc) Create(context.Context, userdom.CreateInput) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice"}, nil
}
func (m *mockUserSvc) UpdateProfile(context.Context, uuid.UUID, userdom.UpdateProfileInput) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice Updated"}, nil
}
func (m *mockUserSvc) AdminUpdate(context.Context, uuid.UUID, userdom.AdminUpdateInput) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice Updated"}, nil
}
func (m *mockUserSvc) ResetPassword(context.Context, uuid.UUID, string) error            { return nil }
func (m *mockUserSvc) ChangeMyPassword(context.Context, uuid.UUID, string, string) error { return nil }
func (m *mockUserSvc) IssuePasswordSetToken(context.Context, uuid.UUID) (string, time.Time, error) {
	return "", time.Time{}, nil
}
func (m *mockUserSvc) SetPasswordWithToken(context.Context, string, string) error { return nil }
func (m *mockUserSvc) Delete(context.Context, uuid.UUID) error                    { return nil }
func (m *mockUserSvc) InitiateAvatarUpload(context.Context, uuid.UUID, string, string, int64) (*attachmentdom.UploadSession, error) {
	return &attachmentdom.UploadSession{}, nil
}
func (m *mockUserSvc) CompleteAvatarUpload(context.Context, uuid.UUID, uuid.UUID) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice"}, nil
}
func (m *mockUserSvc) RemoveAvatar(context.Context, uuid.UUID) (*userdom.User, error) {
	return &userdom.User{ID: uuid.New(), Username: "alice", FullName: "Alice"}, nil
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
func (s *stubProjectSvc) InitiateAvatarUpload(context.Context, uuid.UUID, string, string, int64, uuid.UUID) (*attachmentdom.UploadSession, error) {
	return &attachmentdom.UploadSession{}, nil
}
func (s *stubProjectSvc) CompleteAvatarUpload(context.Context, uuid.UUID, uuid.UUID) (*projectdom.Project, error) {
	return nil, projectdom.ErrNotFound
}
func (s *stubProjectSvc) RemoveAvatar(context.Context, uuid.UUID) (*projectdom.Project, error) {
	return nil, projectdom.ErrNotFound
}
func (s *stubProjectSvc) ListMembers(context.Context, uuid.UUID) ([]*projectdom.ProjectMember, error) {
	return nil, nil
}
func (s *stubProjectSvc) CountDistinctAgentsByProjects(context.Context, []uuid.UUID) (int64, error) {
	return 0, nil
}
func (s *stubProjectSvc) AddMember(context.Context, uuid.UUID, projectdom.AddMemberInput) (*projectdom.ProjectMember, error) {
	return nil, nil
}
func (s *stubProjectSvc) RemoveMember(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (s *stubProjectSvc) UpdateMemberDescription(context.Context, uuid.UUID, uuid.UUID, string) (*projectdom.ProjectMember, error) {
	return nil, nil
}
func (s *stubProjectSvc) RemoveMemberByMemberID(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *stubProjectSvc) AddAgentMember(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []uuid.UUID, *uuid.UUID) error {
	return nil
}
func (s *stubProjectSvc) RemoveAgentMember(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// fakeSettingsSvc is a minimal settingsdom.Service — enough to exercise
// routing/permission checks for the /admin/settings endpoints without a
// real DB.
type fakeSettingsSvc struct{}

func (f *fakeSettingsSvc) Get(context.Context) (*settingsdom.WorkspaceSettings, error) {
	return &settingsdom.WorkspaceSettings{}, nil
}
func (f *fakeSettingsSvc) InitiateImageUpload(context.Context, settingsdom.ImageSlot, string, string, int64, uuid.UUID) (*attachmentdom.UploadSession, error) {
	return &attachmentdom.UploadSession{}, nil
}
func (f *fakeSettingsSvc) CompleteImageUpload(context.Context, settingsdom.ImageSlot, uuid.UUID, uuid.UUID) (*settingsdom.WorkspaceSettings, error) {
	return &settingsdom.WorkspaceSettings{}, nil
}
func (f *fakeSettingsSvc) RemoveImage(context.Context, settingsdom.ImageSlot, uuid.UUID) (*settingsdom.WorkspaceSettings, error) {
	return &settingsdom.WorkspaceSettings{}, nil
}
func (f *fakeSettingsSvc) UpdateSettings(context.Context, *string, *string, *string, uuid.UUID) (*settingsdom.WorkspaceSettings, error) {
	return &settingsdom.WorkspaceSettings{}, nil
}

// The router tests describe callers as a platform role holding these
// actions and a project role holding those, served to the IAM engine shaped
// the way migration 000064 writes roles: actionAll as "*" on "*", every other
// platform action on the platform roots, and a project role's actions on
// every project ("project/*/*" — a stand-in for "the caller's role in
// whichever project is asked about").
const actionAll iam.Action = "*"

var testPlatformRoots = []string{"user", "user/*", "role", "role/*", "plugin", "plugin/*", "settings", "sso", "agent", "agent/*", "project"}

func roleGrants(global, project []iam.Action) []iam.Grant {
	var sts []iam.Statement
	add := func(perms []iam.Action, resources []string) {
		var actions []string
		for _, p := range perms {
			if p == actionAll {
				sts = append(sts, iam.Statement{Effect: iam.EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}})
				continue
			}
			actions = append(actions, string(p))
		}
		if len(actions) > 0 {
			sts = append(sts, iam.Statement{Effect: iam.EffectAllow, Actions: actions, Resources: resources})
		}
	}
	add(global, testPlatformRoots)
	add(project, []string{"project/*/*"})
	if len(sts) == 0 {
		return nil
	}
	return []iam.Grant{{RoleID: "legacy", Policy: &iam.Policy{Statements: sts}}}
}

type allowAllPermissionStore struct{}

func (s *allowAllPermissionStore) ListGrants(context.Context, iam.Principal) ([]iam.Grant, error) {
	return roleGrants([]iam.Action{actionAll}, nil), nil
}

type staticPermissionStore struct {
	globalPerms []iam.Action
}

func (s *staticPermissionStore) ListGrants(context.Context, iam.Principal) ([]iam.Grant, error) {
	return roleGrants(s.globalPerms, nil), nil
}

func newTestRouter(t *testing.T) http.Handler {
	return newTestRouterWithStore(t, &allowAllPermissionStore{})
}

func newTestRouterWithStore(t *testing.T, store iam.Store) http.Handler {
	t.Helper()

	authorizer := newTestIAM(store)
	deps := Deps{
		TokenManager: jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour),
		IAM:          authorizer,
		Health:       handler.NewHealthHandler(),
		Auth: handler.NewAuthHandler(&mockAuthSvc{}, handler.CookieConfig{
			Secure:            false,
			AccessTTL:         15 * time.Minute,
			RefreshTTL:        24 * time.Hour,
			RefreshSessionTTL: 12 * time.Hour,
		}),
		User:     handler.NewUserHandler(&mockUserSvc{}),
		Project:  handler.NewProjectHandler(&stubProjectSvc{}, authorizer),
		Settings: handler.NewSettingsHandler(&fakeSettingsSvc{}),
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
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
		IAM:                newTestIAM(&allowAllPermissionStore{}),
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
	r := newTestRouterWithStore(t, &staticPermissionStore{globalPerms: []iam.Action{iam.ActionUsersWrite}})
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

func TestAdminRoute_UpdateSettings_RequiresWritePermission(t *testing.T) {
	r := newTestRouterWithStore(t, &staticPermissionStore{globalPerms: []iam.Action{iam.ActionUsersRead}})
	tok := issueAccessTokenForRouterTests(t)

	body := bytes.NewBufferString(`{"brand_name":"Acme"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/admin/settings", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without settings.write permission, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAdminRoute_UpdateSettings_WithWritePermission(t *testing.T) {
	r := newTestRouterWithStore(t, &staticPermissionStore{globalPerms: []iam.Action{iam.ActionSettingsWrite}})
	tok := issueAccessTokenForRouterTests(t)

	body := bytes.NewBufferString(`{"brand_name":"Acme"}`)
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/admin/settings", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with settings.write permission, got %d (%s)", w.Code, w.Body.String())
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

func TestNew_AuthRateLimit(t *testing.T) {
	deps := Deps{
		TokenManager: jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour),
		IAM:          newTestIAM(&allowAllPermissionStore{}),
		Health:       handler.NewHealthHandler(),
		Auth: handler.NewAuthHandler(&mockAuthSvc{}, handler.CookieConfig{
			AccessTTL: 15 * time.Minute, RefreshTTL: 24 * time.Hour, RefreshSessionTTL: 12 * time.Hour,
		}),
		User:          handler.NewUserHandler(&mockUserSvc{}),
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		AuthRateLimit: 2,
	}
	r := New(deps)
	login := func(ip string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login",
			strings.NewReader(`{"username":"alice","password":"pw"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip + ":1234"
		r.ServeHTTP(w, req)
		return w.Code
	}
	for i := range 2 {
		if code := login("203.0.113.1"); code == http.StatusTooManyRequests {
			t.Fatalf("login %d rate-limited too early", i+1)
		}
	}
	if code := login("203.0.113.1"); code != http.StatusTooManyRequests {
		t.Fatalf("third login: got %d, want 429", code)
	}
	if code := login("203.0.113.2"); code == http.StatusTooManyRequests {
		t.Fatal("another client was rate-limited")
	}

	// Refresh has its own, larger budget: exhausting login doesn't block it.
	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/refresh", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	r.ServeHTTP(w, req)
	if w.Code == http.StatusTooManyRequests {
		t.Fatal("refresh shares login's budget")
	}
}
