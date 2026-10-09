package integration_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
)

type integrationPermissionStore struct {
	globalPerms []iam.Action
}

func buildAdminTestRouter(perms []iam.Action) http.Handler {
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	store := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, store, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager: tm,
		IAM:          newIAM(&integrationPermissionStore{globalPerms: perms}),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, testCookieCfg),
		User:         handler.NewUserHandler(userService),
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

func TestIntegrationAdminRoute_ListUsers_RequiresReadPermission(t *testing.T) {
	r := buildAdminTestRouter([]iam.Action{iam.ActionUsersRead})
	tok := issueIntegrationAccessToken(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/users", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationAdminRoute_CreateUser_RequiresWritePermission(t *testing.T) {
	r := buildAdminTestRouter([]iam.Action{iam.ActionUsersRead})
	tok := issueIntegrationAccessToken(t)

	w := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"username":"carol","password":"secret12","full_name":"Carol"}`)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/users", body)
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
