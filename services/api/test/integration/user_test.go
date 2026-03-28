package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/platform/authz"
	jwttoken "github.com/paca/api/internal/platform/token"
	authsvc "github.com/paca/api/internal/service/auth"
	usersvc "github.com/paca/api/internal/service/user"
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
)

func buildUserTestRouter(repo *fakeUserRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	store := &fakeRefreshStore{}
	authService := authsvc.New(repo, tm, store, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(repo)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authz.NewAuthorizer(nil),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, testCookieCfg),
		User:         handler.NewUserHandler(userService),
		Log:          log,
	})
}

func TestCreateUser(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)

	body, _ := json.Marshal(map[string]string{
		"username":  "newuser",
		"password":  "securepass",
		"full_name": "Test User",
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateUserDuplicateUsername(t *testing.T) {
	repo := newFakeUserRepo()
	existing := &userdom.User{ID: uuid.New(), Username: "existing", Role: userdom.RoleUser}
	_ = repo.Create(context.Background(), existing)

	r := buildUserTestRouter(repo)

	body, _ := json.Marshal(map[string]string{
		"username":  "existing",
		"password":  "securepass",
		"full_name": "Duplicate",
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "USER_USERNAME_TAKEN" {
		t.Errorf("expected error_code USER_USERNAME_TAKEN, got %q", code)
	}
}
