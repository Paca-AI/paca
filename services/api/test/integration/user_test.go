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
	"golang.org/x/crypto/bcrypt"
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

func TestGetMyGlobalPermissions(t *testing.T) {
	repo := newFakeUserRepo()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "perm-user",
		PasswordHash: string(hash),
		Role:         userdom.RoleUser,
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	r := buildUserTestRouter(repo)

	loginBody, _ := json.Marshal(map[string]string{"username": "perm-user", "password": "secret123"})
	loginReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d: %s", loginW.Code, loginW.Body.String())
	}

	var accessToken string
	for _, c := range loginW.Result().Cookies() {
		if c.Name == "access_token" {
			accessToken = c.Value
			break
		}
	}
	if accessToken == "" {
		t.Fatal("missing access_token cookie")
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me/global-permissions", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Permissions []string `json:"permissions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !env.Success {
		t.Fatal("expected success response")
	}

	foundUsersRead := false
	for _, p := range env.Data.Permissions {
		if p == string(authz.PermissionUsersRead) {
			foundUsersRead = true
		}
	}
	if !foundUsersRead {
		t.Fatalf("expected %q in permissions, got %v", authz.PermissionUsersRead, env.Data.Permissions)
	}
}

func TestGetMyGlobalPermissions_Unauthorized(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me/global-permissions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "AUTH_MISSING_TOKEN" {
		t.Fatalf("expected error_code AUTH_MISSING_TOKEN, got %q", code)
	}
}

func TestGetMyGlobalPermissions_AdminRoleIncludesWildcard(t *testing.T) {
	repo := newFakeUserRepo()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "admin-user",
		PasswordHash: string(hash),
		Role:         userdom.RoleAdmin,
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	r := buildUserTestRouter(repo)

	loginBody, _ := json.Marshal(map[string]string{"username": "admin-user", "password": "secret123"})
	loginReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("expected 200 on login, got %d: %s", loginW.Code, loginW.Body.String())
	}

	var accessToken string
	for _, c := range loginW.Result().Cookies() {
		if c.Name == "access_token" {
			accessToken = c.Value
			break
		}
	}
	if accessToken == "" {
		t.Fatal("missing access_token cookie")
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me/global-permissions", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Permissions []string `json:"permissions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	foundWildcard := false
	for _, p := range env.Data.Permissions {
		if p == string(authz.PermissionAll) {
			foundWildcard = true
		}
	}
	if !foundWildcard {
		t.Fatalf("expected %q in permissions, got %v", authz.PermissionAll, env.Data.Permissions)
	}
}
