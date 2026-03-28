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
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
	"golang.org/x/crypto/bcrypt"
)

// -- fakes -------------------------------------------------------------------

type fakeUserRepo struct {
	byUsername map[string]*userdom.User
	byID       map[uuid.UUID]*userdom.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		byUsername: make(map[string]*userdom.User),
		byID:       make(map[uuid.UUID]*userdom.User),
	}
}

func (r *fakeUserRepo) FindByID(_ context.Context, id uuid.UUID) (*userdom.User, error) {
	u, ok := r.byID[id]
	if !ok {
		return nil, userdom.ErrNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) FindByUsername(_ context.Context, username string) (*userdom.User, error) {
	u, ok := r.byUsername[username]
	if !ok {
		return nil, userdom.ErrNotFound
	}
	return u, nil
}

func (r *fakeUserRepo) Create(_ context.Context, u *userdom.User) error {
	r.byUsername[u.Username] = u
	r.byID[u.ID] = u
	return nil
}

func (r *fakeUserRepo) Update(_ context.Context, u *userdom.User) error {
	r.byUsername[u.Username] = u
	r.byID[u.ID] = u
	return nil
}

func (r *fakeUserRepo) Delete(_ context.Context, id uuid.UUID) error {
	if u, ok := r.byID[id]; ok {
		delete(r.byUsername, u.Username)
		delete(r.byID, id)
	}
	return nil
}

type fakeRefreshStore struct{}

func (f *fakeRefreshStore) RecordFirstUse(_ context.Context, _ string, _ time.Duration) (*time.Time, error) {
	return nil, nil // always first use
}
func (f *fakeRefreshStore) RevokeFamily(_ context.Context, _ string, _ time.Duration) error {
	return nil
}
func (f *fakeRefreshStore) IsFamilyRevoked(_ context.Context, _ string) (bool, error) {
	return false, nil
}

// -- helpers -----------------------------------------------------------------

const testSecret = "test-secret-that-is-at-least-32-chars"

var testCookieCfg = handler.CookieConfig{
	Secure:            false,
	AccessTTL:         15 * time.Minute,
	RefreshTTL:        168 * time.Hour,
	RefreshSessionTTL: 24 * time.Hour,
}

// decodeErrorCode decodes the response body and returns the error_code field.
func decodeErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode error code: %v", err)
	}
	return env.ErrorCode
}

func buildTestRouter(repo *fakeUserRepo) *gin.Engine {
	gin.SetMode(gin.TestMode)
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	store := &fakeRefreshStore{}
	authService := authsvc.New(repo, tm, store, 168*time.Hour, 24*time.Hour)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authz.NewAuthorizer(nil),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, testCookieCfg),
		User:         handler.NewUserHandler(nil),
		Log:          log,
	})
}

// -- tests -------------------------------------------------------------------

func TestLoginSuccess(t *testing.T) {
	repo := newFakeUserRepo()

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "testuser",
		PasswordHash: string(hash),
		Role:         userdom.RoleUser,
	}
	_ = repo.Create(context.Background(), u)

	r := buildTestRouter(repo)

	body, _ := json.Marshal(map[string]string{"username": "testuser", "password": "password123"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Verify tokens are delivered as cookies.
	var hasAccess, hasRefresh bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "access_token" {
			hasAccess = true
		}
		if c.Name == "refresh_token" {
			hasRefresh = true
		}
	}
	if !hasAccess || !hasRefresh {
		t.Fatalf("expected access_token and refresh_token cookies")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	repo := newFakeUserRepo()

	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "testuser",
		PasswordHash: string(hash),
		Role:         userdom.RoleUser,
	}
	_ = repo.Create(context.Background(), u)

	r := buildTestRouter(repo)

	body, _ := json.Marshal(map[string]string{"username": "testuser", "password": "wrong-password"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "AUTH_INVALID_CREDENTIALS" {
		t.Errorf("expected error_code AUTH_INVALID_CREDENTIALS, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Remember Me — cookie MaxAge
// ---------------------------------------------------------------------------

// loginRequestBody returns a JSON-encoded login body with an optional remember_me field.
func loginBody(t *testing.T, username, password string, rememberMe bool) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"username":    username,
		"password":    password,
		"remember_me": rememberMe,
	})
	if err != nil {
		t.Fatalf("failed to marshal login body: %v", err)
	}
	return bytes.NewReader(b)
}

func seedLoginUser(t *testing.T, repo *fakeUserRepo) *userdom.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to generate password hash for seed login user: %v", err)
	}
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "loginuser",
		PasswordHash: string(hash),
		Role:         userdom.RoleUser,
	}
	if err := repo.Create(context.Background(), u); err != nil {
		t.Fatalf("failed to seed login user in repo: %v", err)
	}
	return u
}

func TestLogin_RememberMe_True_LongLivedCookie(t *testing.T) {
	repo := newFakeUserRepo()
	seedLoginUser(t, repo)
	r := buildTestRouter(repo)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login",
		loginBody(t, "loginuser", "password123", true))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for _, c := range w.Result().Cookies() {
		if c.Name == "refresh_token" {
			wantMaxAge := int(testCookieCfg.RefreshTTL.Seconds()) // 168h
			if c.MaxAge != wantMaxAge {
				t.Errorf("refresh_token MaxAge: want %d (168h), got %d", wantMaxAge, c.MaxAge)
			}
			return
		}
	}
	t.Fatal("refresh_token cookie not found")
}

func TestLogin_RememberMe_False_ShortLivedCookie(t *testing.T) {
	repo := newFakeUserRepo()
	seedLoginUser(t, repo)
	r := buildTestRouter(repo)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login",
		loginBody(t, "loginuser", "password123", false))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for _, c := range w.Result().Cookies() {
		if c.Name == "refresh_token" {
			wantMaxAge := int(testCookieCfg.RefreshSessionTTL.Seconds()) // 24h
			if c.MaxAge != wantMaxAge {
				t.Errorf("refresh_token MaxAge: want %d (24h), got %d", wantMaxAge, c.MaxAge)
			}
			return
		}
	}
	t.Fatal("refresh_token cookie not found")
}

func TestLogin_RememberMe_Omitted_DefaultsToFalse(t *testing.T) {
	repo := newFakeUserRepo()
	seedLoginUser(t, repo)
	r := buildTestRouter(repo)

	// No remember_me field — should default to false → session TTL.
	body, _ := json.Marshal(map[string]string{"username": "loginuser", "password": "password123"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for _, c := range w.Result().Cookies() {
		if c.Name == "refresh_token" {
			wantMaxAge := int(testCookieCfg.RefreshSessionTTL.Seconds()) // 24h
			if c.MaxAge != wantMaxAge {
				t.Errorf("refresh_token MaxAge: want %d (24h session), got %d", wantMaxAge, c.MaxAge)
			}
			return
		}
	}
	t.Fatal("refresh_token cookie not found")
}
