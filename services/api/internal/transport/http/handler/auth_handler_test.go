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

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

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
	login             func(ctx context.Context, username, pass string, rememberMe bool) (*domainauth.TokenPair, error)
	refresh           func(ctx context.Context, token string) (*domainauth.TokenPair, error)
	refreshAnnotation func(ctx context.Context, token string) (*domainauth.TokenPair, error)
	logout            func(ctx context.Context, familyID string) error
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
func (m *mockAuthSvc) RefreshAnnotation(ctx context.Context, token string) (*domainauth.TokenPair, error) {
	if m.refreshAnnotation != nil {
		return m.refreshAnnotation(ctx, token)
	}
	return nil, errors.New("mock: refreshAnnotation not configured")
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
func do(t *testing.T, engine http.Handler, method, path string, body *bytes.Buffer) *httptest.ResponseRecorder {
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
func doWithCookie(t *testing.T, engine http.Handler, method, path string, body *bytes.Buffer, cookieName, cookieValue string) *httptest.ResponseRecorder {
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

// injectClaims returns a middleware that sets the given claims into the request context.
func injectClaims(claims *domainauth.Claims) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), httpmw.ClaimsContextKey(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
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
	r := chi.NewRouter()
	r.Get("/healthz", handler.NewHealthHandler().Check)

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
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

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
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig).Login)

	w := do(t, r, http.MethodPost, "/auth/login", bytes.NewBufferString("not-json"))
	if w.Code == http.StatusOK {
		t.Errorf("expected non-200 for bad JSON, got 200")
	}
}

func TestLogin_MissingFields(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig).Login)

	// username missing
	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"password": "secret12"}))
	if w.Code == http.StatusOK {
		t.Errorf("expected validation error, got 200")
	}
}

func TestLogin_EmptyUsername_Returns400(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "", "password": "secret12"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty username, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogin_InvalidCreds(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return nil, domainauth.ErrInvalidCredentials
		},
	}
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

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
	r := chi.NewRouter()
	r.Post("/auth/refresh", handler.NewAuthHandler(svc, testCookieConfig).Refresh)

	w := doWithCookie(t, r, http.MethodPost, "/auth/refresh", nil, "refresh_token", "old-rt")
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRefresh_MissingCookie(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/auth/refresh", handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig).Refresh)

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
	r := chi.NewRouter()
	r.Post("/auth/refresh", handler.NewAuthHandler(svc, testCookieConfig).Refresh)

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
	r := chi.NewRouter()
	claims := testClaims("uid-1", "alice", "USER")
	r.With(injectClaims(claims)).Post("/auth/logout", handler.NewAuthHandler(svc, testCookieConfig).Logout)

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
	r := chi.NewRouter()
	// No claims-injecting middleware — claims will be nil.
	r.Post("/auth/logout", handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig).Logout)

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
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

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
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

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
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

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
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

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
	r := chi.NewRouter()
	r.Post("/auth/refresh", handler.NewAuthHandler(svc, testCookieConfig).Refresh)

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

// ---------------------------------------------------------------------------
// paca_port cookie — read by the Paca browser extension (apps/extension)
// from a completely different page (an environment's forwarded preview),
// so it must never be HttpOnly, and must reflect the port the client
// actually used even when Paca isn't on 443/80.
// ---------------------------------------------------------------------------

func findCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("%s cookie not found; got %v", name, w.Result().Cookies())
	return nil
}

func TestLogin_SetsPacaPortCookie_NotHttpOnly(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: 7 * 24 * time.Hour}, nil
		},
	}
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "secret12"}))

	c := findCookie(t, w, "paca_port")
	if c.HttpOnly {
		t.Error("paca_port must not be HttpOnly — the extension reads it via document.cookie")
	}
}

func TestLogin_PacaPortCookie_FromHostHeader(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: time.Hour}, nil
		},
	}
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "secret12"}))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "pc.paca-ai.org:3000"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	c := findCookie(t, w, "paca_port")
	if c.Value != "3000" {
		t.Errorf("paca_port = %q, want %q (from Host header)", c.Value, "3000")
	}
}

func TestLogin_PacaPortCookie_PrefersXForwardedPort(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: time.Hour}, nil
		},
	}
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "secret12"}))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "paca.example.com" // no port -- a reverse proxy fronting on 443 forwarding to an internal port
	req.Header.Set("X-Forwarded-Port", "443")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	c := findCookie(t, w, "paca_port")
	if c.Value != "443" {
		t.Errorf("paca_port = %q, want %q (X-Forwarded-Port should take precedence)", c.Value, "443")
	}
}

func TestLogin_PacaPortCookie_DefaultsWhenHostHasNoPort(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: time.Hour}, nil
		},
	}
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	// httptest.NewRequest defaults req.Host to "example.com" (no port) and
	// leaves req.TLS nil for a plain, non-HTTPS request.
	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "secret12"}))

	c := findCookie(t, w, "paca_port")
	if c.Value != "80" {
		t.Errorf("paca_port = %q, want %q (default for a plain-HTTP request with no explicit port)", c.Value, "80")
	}
}

// ---------------------------------------------------------------------------
// paca_scheme cookie — portCookieName's scheme counterpart. Same reasoning:
// read from a completely different (forwarded-preview) page, so it must
// never be HttpOnly.
// ---------------------------------------------------------------------------

func TestLogin_SetsPacaSchemeCookie_NotHttpOnly(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: time.Hour}, nil
		},
	}
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, testCookieConfig).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "secret12"}))

	c := findCookie(t, w, "paca_scheme")
	if c.HttpOnly {
		t.Error("paca_scheme must not be HttpOnly — the extension reads it via document.cookie")
	}
	if c.Value != "http" {
		t.Errorf("paca_scheme = %q, want %q (testCookieConfig has Secure: false)", c.Value, "http")
	}
}

// TestLogin_PortAndSchemeCookies_NeverSecure_EvenWithCookieSecureTrue is the
// regression test for a real bug: a deployment with COOKIE_SECURE=true (the
// main app correctly sitting behind real HTTPS, e.g. via an Ingress) was
// applying that same Secure flag to paca_port/paca_scheme — but those two
// exist specifically to be read via document.cookie from a forwarded
// environment preview, which is very commonly plain HTTP (a raw dev-server
// port, no TLS termination in that path) even when the main app is HTTPS.
// A Secure cookie is invisible to document.cookie on a non-HTTPS page,
// full stop — so with COOKIE_SECURE=true the extension silently couldn't
// see either cookie at all on exactly that common combination, even though
// every other test here (which all use testCookieConfig's Secure: false)
// passed. This is the one test in the file that deliberately uses a
// Secure:true config to catch it.
func TestLogin_PortAndSchemeCookies_NeverSecure_EvenWithCookieSecureTrue(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: time.Hour}, nil
		},
	}
	securedCfg := testCookieConfig
	securedCfg.Secure = true
	r := chi.NewRouter()
	r.Post("/auth/login", handler.NewAuthHandler(svc, securedCfg).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "secret12"}))

	// Sanity check the premise: the auth cookies really are Secure here.
	if c := findCookie(t, w, "access_token"); !c.Secure {
		t.Fatalf("test setup broken: access_token should be Secure when COOKIE_SECURE=true")
	}

	if c := findCookie(t, w, "paca_port"); c.Secure {
		t.Error("paca_port must never be Secure, even when COOKIE_SECURE=true — it would become invisible to document.cookie on a plain-HTTP forwarded preview")
	}
	if c := findCookie(t, w, "paca_scheme"); c.Secure {
		t.Error("paca_scheme must never be Secure, even when COOKIE_SECURE=true — same reasoning as paca_port")
	}
}

// TestLogin_PacaSchemeCookie_ReflectsCookieSecure_NotHeaders confirms
// paca_scheme is derived purely from h.cookie.Secure (COOKIE_SECURE) — not
// from X-Forwarded-Proto or any other per-request signal. An earlier
// version read X-Forwarded-Proto, which broke silently whenever a reverse
// proxy in front of this service (e.g. Caddy's reverse_proxy, which
// recomputes that header from its own connection by default) didn't
// faithfully forward it — see requestScheme's own doc comment for why
// h.cookie.Secure has no equivalent failure mode.
func TestLogin_PacaSchemeCookie_ReflectsCookieSecure_NotHeaders(t *testing.T) {
	cases := []struct {
		name       string
		secure     bool
		wantScheme string
	}{
		{"COOKIE_SECURE=false", false, "http"},
		{"COOKIE_SECURE=true", true, "https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &mockAuthSvc{
				login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
					return &domainauth.TokenPair{AccessToken: "at", RefreshToken: "rt", RefreshTTL: time.Hour}, nil
				},
			}
			cfg := testCookieConfig
			cfg.Secure = tc.secure
			r := chi.NewRouter()
			r.Post("/auth/login", handler.NewAuthHandler(svc, cfg).Login)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/login",
				jsonBody(t, map[string]string{"username": "alice", "password": "secret12"}))
			req.Header.Set("Content-Type", "application/json")
			// A header claiming the opposite scheme must be ignored entirely.
			opposite := map[string]string{"http": "https", "https": "http"}[tc.wantScheme]
			req.Header.Set("X-Forwarded-Proto", opposite)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			c := findCookie(t, w, "paca_scheme")
			if c.Value != tc.wantScheme {
				t.Errorf("paca_scheme = %q, want %q (from h.cookie.Secure=%v, ignoring X-Forwarded-Proto: %q)", c.Value, tc.wantScheme, tc.secure, opposite)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// annotation_access_token / annotation_refresh_token — the
// domainauth.ScopeAnnotation pair the browser extension relies on when the
// forwarded preview page and this API don't share a scheme (see
// AuthHandler.setAnnotationTokenCookies's doc comment).
// ---------------------------------------------------------------------------

func TestLogin_SetsAnnotationCookies_SameSiteNone(t *testing.T) {
	svc := &mockAuthSvc{
		login: func(_ context.Context, _, _ string, _ bool) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{
				AccessToken: "at", RefreshToken: "rt", RefreshTTL: time.Hour,
				AnnotationAccessToken: "aat", AnnotationRefreshToken: "art",
			}, nil
		},
	}
	r := chi.NewRouter()
	// SameSite=None requires Secure, or the browser drops the cookie
	// outright -- exercise this with Secure:true so the cookie is actually
	// well-formed, matching a real HTTPS deployment.
	securedCfg := testCookieConfig
	securedCfg.Secure = true
	r.Post("/auth/login", handler.NewAuthHandler(svc, securedCfg).Login)

	w := do(t, r, http.MethodPost, "/auth/login",
		jsonBody(t, map[string]string{"username": "alice", "password": "secret12"}))

	access := findCookie(t, w, "annotation_access_token")
	if access.Value != "aat" || !access.HttpOnly || !access.Secure || access.SameSite != http.SameSiteNoneMode {
		t.Errorf("annotation_access_token = %+v, want HttpOnly+Secure+SameSite=None with value %q", access, "aat")
	}
	refresh := findCookie(t, w, "annotation_refresh_token")
	if refresh.Value != "art" || refresh.Path != "/api/v1/auth/annotation-refresh" {
		t.Errorf("annotation_refresh_token = %+v, want value %q scoped to the annotation-refresh path", refresh, "art")
	}
}

func TestRefresh_ReissuesAnnotationCookies(t *testing.T) {
	// Refresh now reissues the annotation pair on every call (see
	// domainauth.Service.Refresh's doc comment) so its lifetime piggybacks
	// on ordinary web-app usage instead of requiring the extension to
	// independently keep itself alive.
	svc := &mockAuthSvc{
		refresh: func(_ context.Context, _ string) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{
				AccessToken: "at2", RefreshToken: "rt2", RefreshTTL: time.Hour,
				AnnotationAccessToken: "aat2", AnnotationRefreshToken: "art2",
			}, nil
		},
	}
	r := chi.NewRouter()
	securedCfg := testCookieConfig
	securedCfg.Secure = true // SameSite=None cookies require Secure to actually be stored
	r.Post("/auth/refresh", handler.NewAuthHandler(svc, securedCfg).Refresh)

	w := doWithCookie(t, r, http.MethodPost, "/auth/refresh", nil, "refresh_token", "rt")

	access := findCookie(t, w, "annotation_access_token")
	if access.Value != "aat2" || access.SameSite != http.SameSiteNoneMode {
		t.Errorf("annotation_access_token = %+v, want value %q with SameSite=None", access, "aat2")
	}
	refresh := findCookie(t, w, "annotation_refresh_token")
	if refresh.Value != "art2" {
		t.Errorf("annotation_refresh_token = %+v, want value %q", refresh, "art2")
	}
}

func TestRefresh_EmptyAnnotationFields_SetsNoAnnotationCookies(t *testing.T) {
	// Guards setTokenCookies's own guard: a TokenPair with no annotation
	// fields populated (never actually returned by the real service today,
	// but the contract setTokenCookies relies on) must not emit annotation
	// cookies at all, rather than emitting them empty.
	svc := &mockAuthSvc{
		refresh: func(_ context.Context, _ string) (*domainauth.TokenPair, error) {
			return &domainauth.TokenPair{AccessToken: "at2", RefreshToken: "rt2", RefreshTTL: time.Hour}, nil
		},
	}
	r := chi.NewRouter()
	r.Post("/auth/refresh", handler.NewAuthHandler(svc, testCookieConfig).Refresh)

	w := doWithCookie(t, r, http.MethodPost, "/auth/refresh", nil, "refresh_token", "rt")

	for _, name := range []string{"annotation_access_token", "annotation_refresh_token"} {
		for _, c := range w.Result().Cookies() {
			if c.Name == name {
				t.Errorf("expected no %s cookie for an empty-annotation-fields TokenPair, got %+v", name, c)
			}
		}
	}
}

func TestAnnotationRefresh_Success(t *testing.T) {
	svc := &mockAuthSvc{
		refreshAnnotation: func(_ context.Context, token string) (*domainauth.TokenPair, error) {
			if token != "art" {
				t.Errorf("expected annotation_refresh_token value %q, got %q", "art", token)
			}
			return &domainauth.TokenPair{
				RefreshTTL:             time.Hour,
				AnnotationAccessToken:  "aat2",
				AnnotationRefreshToken: "art2",
			}, nil
		},
	}
	r := chi.NewRouter()
	r.Post("/auth/annotation-refresh", handler.NewAuthHandler(svc, testCookieConfig).AnnotationRefresh)

	w := doWithCookie(t, r, http.MethodPost, "/auth/annotation-refresh", nil, "annotation_refresh_token", "art")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	access := findCookie(t, w, "annotation_access_token")
	if access.Value != "aat2" {
		t.Errorf("annotation_access_token = %q, want %q", access.Value, "aat2")
	}
	// Must not touch the main session's cookies at all.
	for _, c := range w.Result().Cookies() {
		if c.Name == "access_token" || c.Name == "refresh_token" {
			t.Errorf("AnnotationRefresh must not set %s, got %+v", c.Name, c)
		}
	}
}

func TestAnnotationRefresh_MissingCookie(t *testing.T) {
	r := chi.NewRouter()
	r.Post("/auth/annotation-refresh", handler.NewAuthHandler(&mockAuthSvc{}, testCookieConfig).AnnotationRefresh)

	w := do(t, r, http.MethodPost, "/auth/annotation-refresh", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without cookie, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "AUTH_MISSING_TOKEN" {
		t.Errorf("expected error_code AUTH_MISSING_TOKEN, got %q", code)
	}
}
