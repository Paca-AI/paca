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
	"github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/platform/authz"
	"github.com/paca/api/internal/platform/token"
	authsvc "github.com/paca/api/internal/service/auth"
	usersvc "github.com/paca/api/internal/service/user"
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
)

func buildUserTestRouter(repo *fakeUserRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	tm := token.New(testSecret, 15*time.Minute, 168*time.Hour)
	bl := newFakeBlacklist()
	authService := authsvc.New(repo, tm, bl, 168*time.Hour)
	userService := usersvc.New(repo)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager: tm,
		AuthzPolicy:  authz.NewPolicy(),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService),
		User:         handler.NewUserHandler(userService),
		Log:          log,
	})
}

func TestCreateUser(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)

	body, _ := json.Marshal(map[string]string{
		"email":    "newuser@example.com",
		"password": "securepass",
		"name":     "Test User",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateUserDuplicateEmail(t *testing.T) {
	repo := newFakeUserRepo()
	existing := &user.User{ID: uuid.New(), Email: "dup@example.com", Role: user.RoleUser}
	_ = repo.Create(context.Background(), existing)

	r := buildUserTestRouter(repo)

	body, _ := json.Marshal(map[string]string{
		"email":    "dup@example.com",
		"password": "securepass",
		"name":     "Duplicate",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}
