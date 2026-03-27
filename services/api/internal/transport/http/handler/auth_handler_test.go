package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	domainauth "github.com/paca/api/internal/domain/auth"
	"github.com/paca/api/internal/transport/http/handler"
)

func init() { gin.SetMode(gin.TestMode) }

// testCookieConfig is an insecure config suitable for unit tests.
var testCookieConfig = handler.CookieConfig{
	Secure:            false,
	AccessTTL:         15 * time.Minute,
	RefreshTTL:        7 * 24 * time.Hour,
	RefreshSessionTTL: 24 * time.Hour,
}

// ---------------------------------------------------------------------------
// mocks
// ---------------------------------------------------------------------------

type mockAuthSvc struct {
	login   func(ctx context.Context, username, pass string, rememberMe bool) (*domainauth.TokenPair, error)
	refresh func(ctx context.Context, token string) (*domainauth.TokenPair, error)
	logout  func(ctx context.Context, familyID string) error
}

func (m *mockAuthSvc) Login(ctx context.Context, username, pass string, rememberMe bool) (*domainauth.TokenPair, error) {
	if m.login != nil {
		return m.login(ctx, username, pass, rememberMe)
	}
	return nil, errors.New("mock: login not configured")
}
func (m *mockAuthSvc) Refresh(ctx context.Context, token string) (*domainauth.TokenPair, error) {
	if m.refresh != nil {
		return m.refresh(ctx, token)
	}
	return nil, errors.New("mock: refresh not configured")
}
func (m *mockAuthSvc) Logout(ctx context.Context, familyID string) error {
	if m.logout != nil {
		return m.logout(ctx, familyID)
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
		req = httptest.NewRequestWithContext(t.Context(), method, path, body)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequestWithContext(t.Context(), method, path, nil)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, req)
	return w
}

// doWithCookie is like do but also attaches an extra cookie to the request.
func doWithCookie(t *testing.T, engine *gin.Engine, method, path string, body *bytes.Buffer, cookieName, cookieValue string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequestWithContext(t.Context(), method, path, body)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequestWithContext(t.Context(), method, path, nil)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cookieValue})
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
func testClaims(sub, username, role string) *domainauth.Claims {
	return &domainauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: sub,
			ID:      "test-jti",
		},
		Username: username,
		Role:     role,
		Kind:     "access",
		FamilyID: "test-family",
	}
}

// errorCode decodes the error envelope from w and returns the error_code
// field. Call this only after asserting a non-2xx status.
func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	return env.ErrorCode
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
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: 7 * 24 * time.Hour}, nil
		},
	}
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "secret12"}))
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Tokens must be in cookies, not in the body.
	cookies := w.Result().Cookies()
	var hasAccess, hasRefresh bool
	for _, c := range cookies {
		if c.Name == "access_token" {
			hasAccess = true
		}
		if c.Name == "refresh_token" {
			hasRefresh = true
		}
	}
	if !hasAccess || !hasRefresh {
		t.Errorf("expected access_token and refresh_token cookies; got %v", cookies)
	}
}

func TestLogin_BadJSON(t *testing.T) {
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig).Login)

	w := do(t, r, http.MethodPost, "/auth/login", bytes.NewBufferString("not-json"))
	if w.Code == http.StatusOK {
		t.Errorf("expected non-200 for bad JSON, got 200")
	}
}

func TestLogin_MissingFields(t *testing.T) {
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig).Login)

	// username missing
	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"password": "secret12"}))
	if w.Code == http.StatusOK {
		t.Errorf("expected validation error, got 200")
	}
}

func TestLogin_InvalidCreds(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return nil, domainauth.ErrInvalidCredentials
		},
	}
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "wrongpass"}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "AUTH_INVALID_CREDENTIALS" {
		t.Errorf("expected error_code AUTH_INVALID_CREDENTIALS, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

func TestRefresh_Success(t *testing.T) {
	svc := &mockAuthSvc{
		refresh: func(_ context.Context, _ string) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "new-at", RefreshToken: "new-rt", RefreshTTL: 7 * 24 * time.Hour}, nil
		},
	}
	r := gin.New()
	r.POST("/auth/refresh", handler.NewAuthHandler(svc, testCookieConfig).Refresh)

	w := doWithCookie(t, r, http.MethodPost, "/auth/refresh", nil, "refresh_token", "old-rt")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefresh_MissingCookie(t *testing.T) {
	r := gin.New()
	r.POST("/auth/refresh", handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig).Refresh)

	// No cookie — expect 401 with AUTH_MISSING_TOKEN.
	w := do(t, r, http.MethodPost, "/auth/refresh", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without cookie, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "AUTH_MISSING_TOKEN" {
		t.Errorf("expected error_code AUTH_MISSING_TOKEN, got %q", code)
	}
}

func TestRefresh_InvalidToken(t *testing.T) {
	svc := &mockAuthSvc{
		refresh: func(_ context.Context, _ string) (*domainauth.TokenPair, error) {
			return nil, domainauth.ErrTokenInvalid
		},
	}
	r := gin.New()
	r.POST("/auth/refresh", handler.NewAuthHandler(svc, testCookieConfig).Refresh)

	w := doWithCookie(t, r, http.MethodPost, "/auth/refresh", nil, "refresh_token", "bad")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "AUTH_TOKEN_INVALID" {
		t.Errorf("expected error_code AUTH_TOKEN_INVALID, got %q", code)
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
	claims := testClaims("uid-1", "alice", "USER")
	r.POST("/auth/logout", injectClaims(claims), handler.NewAuthHandler(svc, testCookieConfig).Logout)

	w := do(t, r, http.MethodPost, "/auth/logout", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !loggedOut {
		t.Error("expected svc.Logout to be called")
	}
	// Cookies must be cleared.
	for _, c := range w.Result().Cookies() {
		if (c.Name == "access_token" || c.Name == "refresh_token") && c.MaxAge != -1 {
			t.Errorf("cookie %s should be expired (MaxAge=-1), got MaxAge=%d", c.Name, c.MaxAge)
		}
	}
}

func TestLogout_NoClaims(t *testing.T) {
	r := gin.New()
	// No claims-injecting middleware — claims will be nil.
	r.POST("/auth/logout", handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig).Logout)

	w := do(t, r, http.MethodPost, "/auth/logout", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "AUTH_UNAUTHENTICATED" {
		t.Errorf("expected error_code AUTH_UNAUTHENTICATED, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Remember Me — handler propagation & cookie MaxAge
// ---------------------------------------------------------------------------

func TestLogin_RememberMe_True_ForwardedToService(t *testing.T) {
	var gotRememberMe bool
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, rememberMe bool) (*domainauth.TokenPair, error) {
			gotRememberMe = rememberMe
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: 7 * 24 * time.Hour}, nil
		},
	}
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]any{"username": "alice", "password": "secret12", "remember_me": true}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !gotRememberMe {
		t.Error("expected rememberMe=true to be forwarded to the service")
	}
}

func TestLogin_RememberMe_False_ForwardedToService(t *testing.T) {
	var gotRememberMe bool
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, rememberMe bool) (*domainauth.TokenPair, error) {
			gotRememberMe = rememberMe
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: 24 * time.Hour}, nil
		},
	}
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	// Omitting remember_me defaults to false.
	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "secret12"}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if gotRememberMe {
		t.Error("expected rememberMe=false when field is omitted")
	}
}

func TestLogin_RememberMe_True_LongCookieMaxAge(t *testing.T) {
	const wantTTL = 7 * 24 * time.Hour
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: wantTTL}, nil
		},
	}
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]any{"username": "alice", "password": "secret12", "remember_me": true}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for _, c := range w.Result().Cookies() {
		if c.Name == "refresh_token" {
			wantMaxAge := int(wantTTL.Seconds())
			if c.MaxAge != wantMaxAge {
				t.Errorf("refresh_token MaxAge: want %d, got %d", wantMaxAge, c.MaxAge)
			}
			return
		}
	}
	t.Fatal("refresh_token cookie not found")
}

func TestLogin_RememberMe_False_ShortCookieMaxAge(t *testing.T) {
	const wantTTL = 24 * time.Hour
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: wantTTL}, nil
		},
	}
	r := gin.New()
	r.POST("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]any{"username": "alice", "password": "secret12", "remember_me": false}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for _, c := range w.Result().Cookies() {
		if c.Name == "refresh_token" {
			wantMaxAge := int(wantTTL.Seconds())
			if c.MaxAge != wantMaxAge {
				t.Errorf("refresh_token MaxAge: want %d, got %d", wantMaxAge, c.MaxAge)
			}
			return
		}
	}
	t.Fatal("refresh_token cookie not found")
}

func TestRefresh_CookieMaxAge_ReflectsServiceTTL(t *testing.T) {
	const wantTTL = 24 * time.Hour
	svc := &mockAuthSvc{
		refresh: func(_ context.Context, _ string) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "new-at", RefreshToken: "new-rt", RefreshTTL: wantTTL}, nil
		},
	}
	r := gin.New()
	r.POST("/auth/refresh", handler.NewAuthHandler(svc, testCookieConfig).Refresh)

	w := doWithCookie(t, r, http.MethodPost, "/auth/refresh", nil, "refresh_token", "old-rt")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	for _, c := range w.Result().Cookies() {
		if c.Name == "refresh_token" {
			wantMaxAge := int(wantTTL.Seconds())
			if c.MaxAge != wantMaxAge {
				t.Errorf("refresh_token MaxAge after rotation: want %d, got %d", wantMaxAge, c.MaxAge)
			}
			return
		}
	}
	t.Fatal("refresh_token cookie not found in refresh response")
}
