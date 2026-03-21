package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	domainauth "github.com/paca/api/internal/domain/auth"
	"github.com/paca/api/internal/transport/http/handler"
)

func init() { gin.SetMode(gin.TestMode) }

// ---------------------------------------------------------------------------
// mocks
// ---------------------------------------------------------------------------

type mockAuthSvc struct {
	login   func(ctx context.Context, email, pass string) (*domainauth.TokenPair, error)
	refresh func(ctx context.Context, token string) (string, error)
	logout  func(ctx context.Context, jti string) error
}

func (m *mockAuthSvc) Login(ctx context.Context, email, pass string) (*domainauth.TokenPair, error) {
	if m.login != nil {
		return m.login(ctx, email, pass)
	}
	return nil, errors.New("mock: login not configured")
}
func (m *mockAuthSvc) Refresh(ctx context.Context, token string) (string, error) {
	if m.refresh != nil {
		return m.refresh(ctx, token)
	}
	return "", errors.New("mock: refresh not configured")
}
func (m *mockAuthSvc) Logout(ctx context.Context, jti string) error {
	if m.logout != nil {
		return m.logout(ctx, jti)
	}
	return errors.New("mock: logout not configured")
}

// verify mock satisfies the interface at compile time
var _ domainauth.Service = (*mockAuthSvc)(nil)

// ---------------------------------------------------------------------------
// helpers (shared with user_handler_test.go in the same package)
// ---------------------------------------------------------------------------

// jsonBody marshals v and returns a *bytes.Buffer suitable as a request body.
func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("jsonBody: %v", err)
	}
	return bytes.NewBuffer(b)
}

// do sends method+path to engine with an optional JSON body and returns the recorder.
func do(t *testing.T, engine *gin.Engine, method, path string, body *bytes.Buffer) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, body)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// injectClaims returns a Gin middleware that sets the given claims into the context.
func injectClaims(claims *domainauth.Claims) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("claims", claims)
		c.Next()
	}
}

// testClaims returns a minimal Claims value for authenticated route tests.
func testClaims(sub, email, role string) *domainauth.Claims {
	return &domainauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: sub,
			ID:      "test-jti",
		},
		Email: email,
		Role:  role,
		Kind:  "access",
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func TestHealth_OK(t *testing.T) {
	r := gin.New()
	r.GET("/healthz", handler.NewHealthHandler().Check)

	w := do(t, r, http.MethodGet, "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt"}, nil
		},
	}
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(svc).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"email": "a@b.com", "password": "secret12"}))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_BadJSON(t *testing.T) {
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(&mockAuthSvc{}).Login)

	w := do(t, r, http.MethodPost, "/auth/login", bytes.NewBufferString("not-json"))
	if w.Code == http.StatusOK {
		t.Errorf("expected non-200 for bad JSON, got 200")
	}
}

func TestLogin_MissingFields(t *testing.T) {
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(&mockAuthSvc{}).Login)

	// email missing
	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"password": "secret12"}))
	if w.Code == http.StatusOK {
		t.Errorf("expected validation error, got 200")
	}
}

func TestLogin_InvalidCreds(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string) (*domainauth.TokenPair, error) {
			return nil, errors.New("auth: invalid credentials")
		},
	}
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(svc).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"email": "a@b.com", "password": "wrongpass"}))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

func TestRefresh_Success(t *testing.T) {
	svc := &mockAuthSvc{
		refresh: func(_ context.Context, _ string) (string, error) {
			return "new-access-token", nil
		},
	}
	r := gin.New()
	r.POST("/auth/refresh", handler.NewAuthHandler(svc).Refresh)

	w := do(t, r, http.MethodPost, "/auth/refresh",
		jsonBody(t, map[string]string{"refresh_token": "rt"}))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefresh_BadJSON(t *testing.T) {
	r := gin.New()
	r.POST("/auth/refresh", handler.NewAuthHandler(&mockAuthSvc{}).Refresh)

	w := do(t, r, http.MethodPost, "/auth/refresh", bytes.NewBufferString("{bad"))
	if w.Code == http.StatusOK {
		t.Errorf("expected non-200 for bad JSON, got 200")
	}
}

// ---------------------------------------------------------------------------
// Logout
// ---------------------------------------------------------------------------

func TestLogout_Success(t *testing.T) {
	loggedOut := false
	svc := &mockAuthSvc{
		logout: func(_ context.Context, _ string) error {
			loggedOut = true
			return nil
		},
	}
	r := gin.New()
	claims := testClaims("uid-1", "a@b.com", "USER")
	r.POST("/auth/logout", injectClaims(claims), handler.NewAuthHandler(svc).Logout)

	w := do(t, r, http.MethodPost, "/auth/logout", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !loggedOut {
		t.Error("expected svc.Logout to be called")
	}
}

func TestLogout_NoClaims(t *testing.T) {
	r := gin.New()
	// No claims-injecting middleware — claims will be nil.
	r.POST("/auth/logout", handler.NewAuthHandler(&mockAuthSvc{}).Logout)

	w := do(t, r, http.MethodPost, "/auth/logout", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
