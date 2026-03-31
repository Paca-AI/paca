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

// issueAdminToken issues a JWT for an admin user to authenticate admin routes.
func issueAdminToken(t *testing.T) string {
	t.Helper()
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	tok, err := tm.IssueAccess(uuid.NewString(), "admin-user", "ADMIN", "fam-admin", false)
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	return tok
}

func TestCreateUser(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)

	body, _ := json.Marshal(map[string]string{
		"username":  "newuser",
		"password":  "securepass",
		"full_name": "Test User",
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueAdminToken(t))
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

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueAdminToken(t))
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

// ---------------------------------------------------------------------------
// GetMe / UpdateMe (self-service)
// ---------------------------------------------------------------------------

func seedAndLogin(t *testing.T, r *gin.Engine, repo *fakeUserRepo, username, password string) (string, *userdom.User) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     username,
		PasswordHash: string(hash),
		Role:         userdom.RoleUser,
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	loginBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var accessToken string
	for _, c := range w.Result().Cookies() {
		if c.Name == "access_token" {
			accessToken = c.Value
			break
		}
	}
	if accessToken == "" {
		t.Fatal("missing access_token cookie after login")
	}
	return accessToken, u
}

func TestGetMe(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)
	token, u := seedAndLogin(t, r, repo, "getme-user", "password123")

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.ID != u.ID.String() {
		t.Errorf("expected id=%s, got %s", u.ID, env.Data.ID)
	}
	if env.Data.Username != u.Username {
		t.Errorf("expected username=%s, got %s", u.Username, env.Data.Username)
	}
}

func TestGetMe_Unauthenticated(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "AUTH_MISSING_TOKEN" {
		t.Errorf("expected AUTH_MISSING_TOKEN, got %q", code)
	}
}

func TestUpdateMe(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)
	token, _ := seedAndLogin(t, r, repo, "updateme-user", "password123")

	body, _ := json.Marshal(map[string]string{"full_name": "Updated Name"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/users/me", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			FullName string `json:"full_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.FullName != "Updated Name" {
		t.Errorf("expected full_name='Updated Name', got %q", env.Data.FullName)
	}
}

// ---------------------------------------------------------------------------
// ChangeMyPassword
// ---------------------------------------------------------------------------

func TestChangeMyPassword(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)
	token, _ := seedAndLogin(t, r, repo, "changepw-user", "password123")

	body, _ := json.Marshal(map[string]string{
		"current_password": "password123",
		"new_password":     "newpassword456",
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/users/me/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestChangeMyPassword_WrongCurrentPassword(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)
	token, _ := seedAndLogin(t, r, repo, "changepw-wrong-user", "password123")

	body, _ := json.Marshal(map[string]string{
		"current_password": "wrongpassword",
		"new_password":     "newpassword456",
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/users/me/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "USER_INVALID_CURRENT_PASSWORD" {
		t.Errorf("expected USER_INVALID_CURRENT_PASSWORD, got %q", code)
	}
}

func TestChangeMyPassword_Unauthenticated(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)

	body, _ := json.Marshal(map[string]string{"current_password": "old12345", "new_password": "new12345"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/users/me/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Admin — list, get, update, delete, reset password
// ---------------------------------------------------------------------------

func TestListUsers(t *testing.T) {
	repo := newFakeUserRepo()
	// Seed a couple of users.
	for _, name := range []string{"user-a", "user-b"} {
		_ = repo.Create(context.Background(), &userdom.User{
			ID: uuid.New(), Username: name, Role: userdom.RoleUser,
		})
	}
	r := buildUserTestRouter(repo)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/users?page=1&page_size=10", nil)
	req.Header.Set("Authorization", "Bearer "+issueAdminToken(t))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			Items    []any `json:"items"`
			Total    int64 `json:"total"`
			Page     int   `json:"page"`
			PageSize int   `json:"page_size"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.Total < 2 {
		t.Errorf("expected total >= 2, got %d", env.Data.Total)
	}
}

func TestListUsers_RequiresAuth(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestGetUserByID_Admin(t *testing.T) {
	repo := newFakeUserRepo()
	u := &userdom.User{ID: uuid.New(), Username: "target-user", Role: userdom.RoleUser}
	_ = repo.Create(context.Background(), u)
	r := buildUserTestRouter(repo)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/users/"+u.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+issueAdminToken(t))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.ID != u.ID.String() {
		t.Errorf("expected id=%s, got %s", u.ID, env.Data.ID)
	}
}

func TestGetUserByID_NotFound(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/users/"+uuid.NewString(), nil)
	req.Header.Set("Authorization", "Bearer "+issueAdminToken(t))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected USER_NOT_FOUND, got %q", code)
	}
}

func TestAdminUpdateUser(t *testing.T) {
	repo := newFakeUserRepo()
	u := &userdom.User{ID: uuid.New(), Username: "update-target", FullName: "Old Name", Role: userdom.RoleUser}
	_ = repo.Create(context.Background(), u)
	r := buildUserTestRouter(repo)

	body, _ := json.Marshal(map[string]string{"full_name": "New Name"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/admin/users/"+u.ID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueAdminToken(t))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			FullName string `json:"full_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.FullName != "New Name" {
		t.Errorf("expected full_name='New Name', got %q", env.Data.FullName)
	}
}

func TestDeleteUser(t *testing.T) {
	repo := newFakeUserRepo()
	u := &userdom.User{ID: uuid.New(), Username: "delete-target", Role: userdom.RoleUser}
	_ = repo.Create(context.Background(), u)
	r := buildUserTestRouter(repo)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/admin/users/"+u.ID.String(), nil)
	req.Header.Set("Authorization", "Bearer "+issueAdminToken(t))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the user is gone from the repo.
	if _, err := repo.FindByID(context.Background(), u.ID); err != userdom.ErrNotFound {
		t.Error("expected user to be deleted from repo")
	}
}

func TestDeleteUser_NotFound(t *testing.T) {
	repo := newFakeUserRepo()
	r := buildUserTestRouter(repo)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/api/v1/admin/users/"+uuid.NewString(), nil)
	req.Header.Set("Authorization", "Bearer "+issueAdminToken(t))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "USER_NOT_FOUND" {
		t.Errorf("expected USER_NOT_FOUND, got %q", code)
	}
}

func TestAdminResetPassword(t *testing.T) {
	repo := newFakeUserRepo()
	hash, _ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.MinCost)
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "reset-target",
		PasswordHash: string(hash),
		Role:         userdom.RoleUser,
	}
	_ = repo.Create(context.Background(), u)
	r := buildUserTestRouter(repo)

	body, _ := json.Marshal(map[string]string{"new_password": "brandnewpass"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/admin/users/"+u.ID.String()+"/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueAdminToken(t))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminResetPassword_SetsMusChangePassword(t *testing.T) {
	repo := newFakeUserRepo()
	hash, _ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.MinCost)
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "must-change-user",
		PasswordHash: string(hash),
		Role:         userdom.RoleUser,
	}
	_ = repo.Create(context.Background(), u)

	// Build router with real user service so MustChangePassword is set.
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	store := &fakeRefreshStore{}
	authService := authsvc.New(repo, tm, store, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(repo)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r := router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authz.NewAuthorizer(nil),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, testCookieCfg),
		User:         handler.NewUserHandler(userService, authService),
		Log:          log,
	})

	body, _ := json.Marshal(map[string]string{"new_password": "brandnewpass"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/admin/users/"+u.ID.String()+"/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueAdminToken(t))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Verify the flag is set in the repo.
	updated, err := repo.FindByID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if !updated.MustChangePassword {
		t.Error("expected MustChangePassword=true after admin reset")
	}
}

// ---------------------------------------------------------------------------
// MustChangePassword flow
// ---------------------------------------------------------------------------

func TestMustChangePassword_BlocksOtherRoutes(t *testing.T) {
	repo := newFakeUserRepo()
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	u := &userdom.User{
		ID:                 uuid.New(),
		Username:           "forced-user",
		PasswordHash:       string(hash),
		Role:               userdom.RoleUser,
		MustChangePassword: true,
	}
	_ = repo.Create(context.Background(), u)
	r := buildUserTestRouter(repo)

	// Log in — token must embed must_change_password=true.
	loginBody, _ := json.Marshal(map[string]string{"username": "forced-user", "password": "password123"})
	loginReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", loginW.Code, loginW.Body.String())
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

	// GET /users/me should be blocked with 403.
	meReq := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+accessToken)
	meW := httptest.NewRecorder()
	r.ServeHTTP(meW, meReq)

	if meW.Code != http.StatusForbidden {
		t.Errorf("expected 403 (must change password), got %d: %s", meW.Code, meW.Body.String())
	}
	if code := decodeErrorCode(t, meW); code != "AUTH_PASSWORD_CHANGE_REQUIRED" {
		t.Errorf("expected AUTH_PASSWORD_CHANGE_REQUIRED, got %q", code)
	}
}

func TestMustChangePassword_ChangeAllowedAndUnblocks(t *testing.T) {
	repo := newFakeUserRepo()
	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	u := &userdom.User{
		ID:                 uuid.New(),
		Username:           "must-change",
		PasswordHash:       string(hash),
		Role:               userdom.RoleUser,
		MustChangePassword: true,
	}
	_ = repo.Create(context.Background(), u)

	// Build a router that wires authService into UserHandler so session is revoked after change.
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	store := &fakeRefreshStore{}
	authService := authsvc.New(repo, tm, store, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(repo)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	r := router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authz.NewAuthorizer(nil),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, testCookieCfg),
		User:         handler.NewUserHandler(userService, authService),
		Log:          log,
	})

	// Login.
	loginBody, _ := json.Marshal(map[string]string{"username": "must-change", "password": "password123"})
	loginReq := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", loginW.Code)
	}
	var accessToken string
	for _, c := range loginW.Result().Cookies() {
		if c.Name == "access_token" {
			accessToken = c.Value
		}
	}

	// PATCH /users/me/password must be accessible (not blocked by RequireFreshPassword).
	changeBody, _ := json.Marshal(map[string]string{
		"current_password": "password123",
		"new_password":     "newpassword456",
	})
	changeReq := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/api/v1/users/me/password", bytes.NewReader(changeBody))
	changeReq.Header.Set("Content-Type", "application/json")
	changeReq.Header.Set("Authorization", "Bearer "+accessToken)
	changeW := httptest.NewRecorder()
	r.ServeHTTP(changeW, changeReq)

	if changeW.Code != http.StatusNoContent {
		t.Fatalf("change password: expected 204, got %d: %s", changeW.Code, changeW.Body.String())
	}

	// Verify MustChangePassword is cleared in repo.
	updated, err := repo.FindByID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	if updated.MustChangePassword {
		t.Error("expected MustChangePassword=false after change")
	}
}
