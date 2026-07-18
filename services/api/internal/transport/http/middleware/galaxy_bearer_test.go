package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type stubGalaxyBearerAuth struct {
	user      *userdom.User
	agentName string
	err       error
	called    bool
}

func (s *stubGalaxyBearerAuth) AuthenticateBearer(_ context.Context, _, _ string) (*userdom.User, string, error) {
	s.called = true
	if s.err != nil {
		return nil, "", s.err
	}
	return s.user, s.agentName, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func signTestRS256(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestGalaxyBearer_ResolvesRS256TokenToUser(t *testing.T) {
	userID := uuid.New()
	stub := &stubGalaxyBearerAuth{
		user:      &userdom.User{ID: userID, Username: "cao", Role: "USER"},
		agentName: "wiki-agent",
	}

	r := chi.NewRouter()
	r.Use(GalaxyBearer(stub, discardLogger()))
	r.With(Authn(newTestTokenManager())).Get("/protected", func(w http.ResponseWriter, req *http.Request) {
		claims := ClaimsFrom(req)
		if claims == nil || claims.Subject != userID.String() {
			http.Error(w, "wrong principal", http.StatusInternalServerError)
			return
		}
		if !IsGalaxyBearerAuth(req) {
			http.Error(w, "expected galaxy bearer auth method", http.StatusInternalServerError)
			return
		}
		if name, ok := GalaxyAgentNameFromRequest(req); !ok || name != "wiki-agent" {
			http.Error(w, "expected agent attribution", http.StatusInternalServerError)
			return
		}
		if _, hasLegacyAgent := AgentIDFromRequest(req); hasLegacyAgent {
			http.Error(w, "attribution must not populate the authz agent context", http.StatusInternalServerError)
			return
		}
		actorID, ok := ActorIDFromContext(req.Context())
		if !ok || actorID != userID {
			http.Error(w, "actor not set", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	token := signTestRS256(t, jwt.MapClaims{"sub": "vortex-sub", "exp": time.Now().Add(time.Hour).Unix()})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !stub.called {
		t.Fatal("authenticator was not invoked for RS256 bearer")
	}
}

func TestGalaxyBearer_RejectsUnresolvableToken(t *testing.T) {
	stub := &stubGalaxyBearerAuth{err: errors.New("no local user for asserted identity")}

	r := chi.NewRouter()
	r.Use(GalaxyBearer(stub, discardLogger()))
	r.With(Authn(newTestTokenManager())).Get("/protected", okHandler)

	token := signTestRS256(t, jwt.MapClaims{"sub": "unknown", "exp": time.Now().Add(time.Hour).Unix()})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 (fail closed, no fallback), got %d", w.Code)
	}
}

func TestGalaxyBearer_IgnoresHS256Tokens(t *testing.T) {
	// The API's own HS256 session tokens must fall through to Authn untouched.
	stub := &stubGalaxyBearerAuth{err: errors.New("must not be called")}
	tm := newTestTokenManager()
	at, err := tm.IssueAccess("user-id", "alice", "USER", "fam", false)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	r := chi.NewRouter()
	r.Use(GalaxyBearer(stub, discardLogger()))
	r.With(Authn(tm)).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+at)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 via HS256 path, got %d (%s)", w.Code, w.Body.String())
	}
	if stub.called {
		t.Fatal("galaxy bearer authenticator must not see HS256 tokens")
	}
}

func TestRequireJWTAuth_BlocksGalaxyBearer(t *testing.T) {
	stub := &stubGalaxyBearerAuth{user: &userdom.User{ID: uuid.New(), Username: "cao", Role: "USER"}}

	r := chi.NewRouter()
	r.Use(GalaxyBearer(stub, discardLogger()))
	r.With(Authn(newTestTokenManager()), RequireJWTAuth()).Get("/sensitive", okHandler)

	token := signTestRS256(t, jwt.MapClaims{"sub": "vortex-sub", "exp": time.Now().Add(time.Hour).Unix()})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/sensitive", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for bearer on session-only route, got %d", w.Code)
	}
}
