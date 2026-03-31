package integration_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
	"github.com/paca/api/internal/platform/authz"
	jwttoken "github.com/paca/api/internal/platform/token"
	authsvc "github.com/paca/api/internal/service/auth"
	usersvc "github.com/paca/api/internal/service/user"
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
)

type integrationPermissionStore struct {
	globalPerms []authz.Permission
}

func (s *integrationPermissionStore) ListGlobalPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return append([]authz.Permission(nil), s.globalPerms...), nil
}

func (s *integrationPermissionStore) ListProjectPermissions(context.Context, uuid.UUID, uuid.UUID) ([]authz.Permission, error) {
	return nil, nil
}

type fakeGlobalRoleService struct{}

func (s *fakeGlobalRoleService) List(context.Context) ([]*globalroledom.GlobalRole, error) {
	return []*globalroledom.GlobalRole{{
		ID:          uuid.New(),
		Name:        "TEST",
		Permissions: map[string]any{"global_roles.read": true},
	}}, nil
}

func (s *fakeGlobalRoleService) Create(context.Context, globalroledom.CreateInput) (*globalroledom.GlobalRole, error) {
	return &globalroledom.GlobalRole{ID: uuid.New(), Name: "CREATED", Permissions: map[string]any{}}, nil
}

func (s *fakeGlobalRoleService) Update(context.Context, uuid.UUID, globalroledom.UpdateInput) (*globalroledom.GlobalRole, error) {
	return &globalroledom.GlobalRole{ID: uuid.New(), Name: "UPDATED", Permissions: map[string]any{}}, nil
}

func (s *fakeGlobalRoleService) Delete(context.Context, uuid.UUID) error {
	return nil
}

func (s *fakeGlobalRoleService) ReplaceUserRoles(context.Context, uuid.UUID, []uuid.UUID) ([]*globalroledom.GlobalRole, error) {
	return []*globalroledom.GlobalRole{}, nil
}

func buildAdminTestRouter(perms []authz.Permission) *gin.Engine {
	gin.SetMode(gin.TestMode)
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	store := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, store, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authz.NewAuthorizer(&integrationPermissionStore{globalPerms: perms}),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, testCookieCfg),
		User:         handler.NewUserHandler(userService),
		GlobalRole:   handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}),
		Log:          log,
	})
}

func issueIntegrationAccessToken(t *testing.T) string {
	t.Helper()
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	tok, err := tm.IssueAccess(uuid.NewString(), "integration-user", "USER", "fam-it", false)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	return tok
}

func TestIntegrationAdminRoute_ListGlobalRoles_RequiresReadPermission(t *testing.T) {
	r := buildAdminTestRouter([]authz.Permission{authz.PermissionGlobalRolesRead})
	tok := issueIntegrationAccessToken(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/global-roles", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationAdminRoute_CreateGlobalRole_RequiresWritePermission(t *testing.T) {
	r := buildAdminTestRouter([]authz.Permission{authz.PermissionGlobalRolesRead})
	tok := issueIntegrationAccessToken(t)

	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"name":"SECURITY","permissions":{"global_roles.read":true}}`)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/global-roles", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without write permission, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "FORBIDDEN" {
		t.Fatalf("expected error_code FORBIDDEN, got %q", code)
	}
}

func TestIntegrationAdminRoute_AssignGlobalRoles_RequiresAssignPermission(t *testing.T) {
	r := buildAdminTestRouter([]authz.Permission{authz.PermissionGlobalRolesWrite})
	tok := issueIntegrationAccessToken(t)

	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"role_ids":[]}`)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/admin/users/"+uuid.NewString()+"/global-roles", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without assign permission, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "FORBIDDEN" {
		t.Fatalf("expected error_code FORBIDDEN, got %q", code)
	}
}
