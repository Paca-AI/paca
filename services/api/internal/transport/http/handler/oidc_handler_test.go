package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	oidcsvc "github.com/Paca-AI/api/internal/service/oidc"
	handler "github.com/Paca-AI/api/internal/transport/http/handler"
)

// memTxStore is a minimal in-memory oidc.LoginTxStore for handler tests.
type memTxStore struct {
	mu  sync.Mutex
	txs map[string][]byte
}

func (s *memTxStore) Save(_ context.Context, state string, payload []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.txs[state] = payload
	return nil
}

func (s *memTxStore) Consume(_ context.Context, state string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, ok := s.txs[state]
	if !ok {
		return nil, nil
	}
	delete(s.txs, state)
	return payload, nil
}

// discoveryOnlyIdP is an httptest server that serves just OIDC discovery —
// enough to construct a real oidc.Service. The handler tests below only
// exercise login initiation and callback rejection paths, none of which
// reach the token endpoint.
func discoveryOnlyIdP(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 srvURL(r),
				"authorization_endpoint": srvURL(r) + "/authorize",
				"token_endpoint":         srvURL(r) + "/token",
				"jwks_uri":               srvURL(r) + "/keys",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	return srv
}

func srvURL(r *http.Request) string {
	return "http://" + r.Host
}

// newOIDCTestHandler builds an OIDCHandler backed by a real oidc.Service
// pointed at a discovery-only IdP.
func newOIDCTestHandler(t *testing.T) *handler.OIDCHandler {
	t.Helper()
	idp := discoveryOnlyIdP(t)
	t.Cleanup(idp.Close)
	svc, err := oidcsvc.New(context.Background(), oidcsvc.Options{
		IssuerURL:    idp.URL,
		ClientID:     "paca-client",
		ClientSecret: "s3cret",
		RedirectURL:  "https://paca.example.com/api/v1/auth/oidc/callback",
		Scopes:       []string{"openid", "profile", "email"},
		DefaultRole:  "USER",
	},
		&memTxStore{txs: map[string][]byte{}},
		nil, nil, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("new oidc service: %v", err)
	}
	return handler.NewOIDCHandler(svc, testCookieConfig, "https://paca.example.com")
}

func findSetCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// Login must bind the login transaction to the initiating browser: the state
// in the authorization URL is mirrored into a short-lived HttpOnly cookie
// scoped to the callback path.
func TestOIDCLogin_SetsBrowserBindingStateCookie(t *testing.T) {
	h := newOIDCTestHandler(t)
	r := chi.NewRouter()
	r.Get("/auth/oidc/login", h.Login)

	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	q := strings.SplitN(loc, "?", 2)[1]
	var state string
	for _, kv := range strings.Split(q, "&") {
		if strings.HasPrefix(kv, "state=") {
			state = strings.TrimPrefix(kv, "state=")
		}
	}
	if state == "" {
		t.Fatalf("no state in authorization URL: %s", loc)
	}

	cookie := findSetCookie(w, "oidc_login_state")
	if cookie == nil {
		t.Fatal("expected oidc_login_state cookie on login redirect")
	}
	if cookie.Value != state {
		t.Errorf("cookie state %q must match URL state %q", cookie.Value, state)
	}
	if !cookie.HttpOnly {
		t.Error("state cookie must be HttpOnly")
	}
	if cookie.Path != "/api/v1/auth/oidc/callback" {
		t.Errorf("state cookie must be scoped to the callback path, got %q", cookie.Path)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("state cookie must be SameSite=Lax (top-level redirect back from IdP), got %v", cookie.SameSite)
	}
	if cookie.MaxAge != 600 {
		t.Errorf("state cookie MaxAge must match the login tx TTL (600s), got %d", cookie.MaxAge)
	}
}

// A callback without the binding cookie (e.g. a forwarded callback URL opened
// in another browser) must be rejected generically — this is the login-CSRF
// defense.
func TestOIDCCallback_WithoutStateCookieRejected(t *testing.T) {
	h := newOIDCTestHandler(t)
	r := chi.NewRouter()
	r.Get("/auth/oidc/callback", h.Callback)

	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=x&state=y", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "sso_error=1") {
		t.Fatalf("expected generic error redirect, got %d %s", w.Code, w.Header().Get("Location"))
	}
}

// A callback whose cookie value does not match the state parameter (an
// attacker's callback URL forced onto a browser with its own, different
// pending login) must be rejected.
func TestOIDCCallback_MismatchedStateCookieRejected(t *testing.T) {
	h := newOIDCTestHandler(t)
	r := chi.NewRouter()
	r.Get("/auth/oidc/callback", h.Callback)

	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=x&state=attacker-state", nil)
	req.AddCookie(&http.Cookie{Name: "oidc_login_state", Value: "victims-own-state"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), "sso_error=1") {
		t.Fatalf("expected generic error redirect, got %d %s", w.Code, w.Header().Get("Location"))
	}
}

// The binding cookie must be cleared on every exit path so a state value
// cannot be retried in this browser.
func TestOIDCCallback_ClearsStateCookieOnError(t *testing.T) {
	h := newOIDCTestHandler(t)
	r := chi.NewRouter()
	r.Get("/auth/oidc/callback", h.Callback)

	// IdP-reported error (e.g. consent denied).
	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?error=access_denied", nil)
	req.AddCookie(&http.Cookie{Name: "oidc_login_state", Value: "some-state"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	cookie := findSetCookie(w, "oidc_login_state")
	if cookie == nil || cookie.MaxAge >= 0 {
		t.Fatalf("expected state cookie to be expired, got %+v", cookie)
	}
}
