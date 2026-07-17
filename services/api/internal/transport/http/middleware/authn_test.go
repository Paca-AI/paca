package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// stubAPIKeyAuth is a minimal APIKeyAuthenticator for unit tests.
type stubAPIKeyAuth struct {
	key        *apikeydom.APIKey
	err        error
	isAgentKey bool
	// allowImpersonation mirrors the AGENT_HEADER_IMPERSONATION kill-switch
	// (ADR-038); the default false matches the production default.
	allowImpersonation bool
}

func (s *stubAPIKeyAuth) Authenticate(_ context.Context, _ string) (*apikeydom.APIKey, error) {
	return s.key, s.err
}

func (s *stubAPIKeyAuth) IsAgentKey(_ context.Context, _ string) bool {
	return s.isAgentKey
}

func (s *stubAPIKeyAuth) AgentHeaderImpersonationEnabled() bool {
	return s.allowImpersonation
}

func newTestTokenManager() *jwttoken.Manager {
	return jwttoken.New("test-secret", 15*time.Minute, 24*time.Hour)
}

func okHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func TestAuthn_MissingToken(t *testing.T) {
	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager())).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	var env struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.ErrorCode != "AUTH_MISSING_TOKEN" {
		t.Fatalf("expected AUTH_MISSING_TOKEN, got %q", env.ErrorCode)
	}
}

func TestAuthn_InvalidToken(t *testing.T) {
	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager())).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthn_ValidAccessTokenInHeader(t *testing.T) {
	tm := newTestTokenManager()
	at, err := tm.IssueAccess("user-id", "alice", "USER", "fam", false)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	r := chi.NewRouter()
	r.With(Authn(tm)).Get("/protected", func(w http.ResponseWriter, req *http.Request) {
		claims := ClaimsFrom(req)
		if claims == nil {
			http.Error(w, `{"error":"claims missing"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"username": claims.Username})
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+at)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthn_RefreshTokenRejected(t *testing.T) {
	tm := newTestTokenManager()
	rt, err := tm.IssueRefresh("user-id", "alice", "USER", "fam")
	if err != nil {
		t.Fatalf("issue refresh token: %v", err)
	}

	r := chi.NewRouter()
	r.With(Authn(tm)).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+rt)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestClaimsFrom_Missing(t *testing.T) {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	if claims := ClaimsFrom(req); claims != nil {
		t.Fatal("expected nil claims when absent")
	}
}

func TestAuthn_APIKey_AuthorizationHeader(t *testing.T) {
	userID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", func(w http.ResponseWriter, req *http.Request) {
		if !IsAPIKeyAuth(req) {
			http.Error(w, `{"error":"expected API key auth"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "ApiKey test-api-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthn_APIKey_XAPIKeyHeader(t *testing.T) {
	userID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", func(w http.ResponseWriter, req *http.Request) {
		if !IsAPIKeyAuth(req) {
			http.Error(w, `{"error":"expected API key auth"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "test-api-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthn_APIKey_InvalidKey(t *testing.T) {
	stub := &stubAPIKeyAuth{err: errors.New("bad key")}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "invalid-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthn_APIKey_RevokedKey(t *testing.T) {
	stub := &stubAPIKeyAuth{err: apikeydom.ErrRevoked}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "revoked-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	var env struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.ErrorCode != "API_KEY_REVOKED" {
		t.Fatalf("expected API_KEY_REVOKED, got %q", env.ErrorCode)
	}
}

func TestAuthn_APIKey_ExpiredKey(t *testing.T) {
	stub := &stubAPIKeyAuth{err: apikeydom.ErrExpired}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "expired-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	var env struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.ErrorCode != "API_KEY_EXPIRED" {
		t.Fatalf("expected API_KEY_EXPIRED, got %q", env.ErrorCode)
	}
}

func TestAuthn_APIKey_NotConfigured(t *testing.T) {
	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager())).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "ApiKey some-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRequireJWTAuth_BlocksAPIKey(t *testing.T) {
	userID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub), RequireJWTAuth()).Get("/sensitive", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/sensitive", nil)
	req.Header.Set("X-API-Key", "some-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	var env struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.ErrorCode != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN error code, got %q", env.ErrorCode)
	}
}

func TestRequireJWTAuth_AllowsJWT(t *testing.T) {
	tm := newTestTokenManager()
	at, err := tm.IssueAccess("user-id", "alice", "USER", "fam", false)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	r := chi.NewRouter()
	r.With(Authn(tm), RequireJWTAuth()).Get("/sensitive", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/sensitive", nil)
	req.Header.Set("Authorization", "Bearer "+at)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthn_APIKey_WithValidAgentID(t *testing.T) {
	userID := uuid.New()
	agentID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true, allowImpersonation: true}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", func(w http.ResponseWriter, req *http.Request) {
		if !IsAPIKeyAuth(req) {
			http.Error(w, `{"error":"expected API key auth"}`, http.StatusInternalServerError)
			return
		}
		retrievedAgentID, ok := AgentIDFromRequest(req)
		if !ok {
			http.Error(w, `{"error":"agent ID not found in context"}`, http.StatusInternalServerError)
			return
		}
		if retrievedAgentID != agentID {
			http.Error(w, `{"error":"agent ID mismatch"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "test-api-key")
	req.Header.Set("X-Agent-ID", agentID.String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthn_APIKey_UserKeyCannotFakeAgentID(t *testing.T) {
	userID := uuid.New()
	agentID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: false}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", func(w http.ResponseWriter, req *http.Request) {
		if !IsAPIKeyAuth(req) {
			http.Error(w, `{"error":"expected API key auth"}`, http.StatusInternalServerError)
			return
		}
		retrievedAgentID, ok := AgentIDFromRequest(req)
		if ok {
			http.Error(w, `{"error":"agent ID should not be set for user API key"}`, http.StatusInternalServerError)
			return
		}
		if retrievedAgentID != uuid.Nil {
			http.Error(w, `{"error":"agent ID should be Nil"}`, http.StatusInternalServerError)
			return
		}
		_ = agentID // suppress unused variable warning
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "test-api-key")
	req.Header.Set("X-Agent-ID", agentID.String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthn_APIKey_WithInvalidAgentID(t *testing.T) {
	userID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true, allowImpersonation: true}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", func(w http.ResponseWriter, req *http.Request) {
		if !IsAPIKeyAuth(req) {
			http.Error(w, `{"error":"expected API key auth"}`, http.StatusInternalServerError)
			return
		}
		retrievedAgentID, ok := AgentIDFromRequest(req)
		if ok {
			http.Error(w, `{"error":"agent ID should not be found with invalid UUID"}`, http.StatusInternalServerError)
			return
		}
		if retrievedAgentID != uuid.Nil {
			http.Error(w, `{"error":"agent ID should be Nil"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "test-api-key")
	req.Header.Set("X-Agent-ID", "not-a-valid-uuid")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthn_APIKey_WithoutAgentID(t *testing.T) {
	userID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", func(w http.ResponseWriter, req *http.Request) {
		if !IsAPIKeyAuth(req) {
			http.Error(w, `{"error":"expected API key auth"}`, http.StatusInternalServerError)
			return
		}
		retrievedAgentID, ok := AgentIDFromRequest(req)
		if ok {
			http.Error(w, `{"error":"agent ID should not be found when header is absent"}`, http.StatusInternalServerError)
			return
		}
		if retrievedAgentID != uuid.Nil {
			http.Error(w, `{"error":"agent ID should be Nil"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "test-api-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthn_APIKey_AgentIDRejectedWhenImpersonationDisabled(t *testing.T) {
	userID := uuid.New()
	// allowImpersonation deliberately left false — the ADR-038 default.
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "shared-agent-key")
	req.Header.Set("X-Agent-ID", uuid.NewString())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when header impersonation is disabled, got %d (%s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "ADR-038") {
		t.Fatalf("expected error to mention ADR-038, got %s", body)
	}
}

func TestAuthn_APIKey_AgentIDRejectedWhenNoPolicyImplemented(t *testing.T) {
	// An authenticator that satisfies AgentAPIKeyAuthenticator but not
	// AgentImpersonationPolicy must be treated as disabled (fail closed).
	userID := uuid.New()
	stub := &legacyStubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "shared-agent-key")
	req.Header.Set("X-Agent-ID", uuid.NewString())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for policy-less authenticator, got %d", w.Code)
	}
}

func TestAuthn_APIKey_AgentKeyWithoutHeaderStillWorksWhenDisabled(t *testing.T) {
	// The kill-switch only targets the X-Agent-ID header; the shared key by
	// itself keeps authenticating the ai-agent service as the bot user.
	userID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "shared-agent-key")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 without X-Agent-ID, got %d (%s)", w.Code, w.Body.String())
	}
}

// legacyStubAPIKeyAuth implements AgentAPIKeyAuthenticator but NOT
// AgentImpersonationPolicy, mimicking a pre-ADR-038 authenticator.
type legacyStubAPIKeyAuth struct {
	key        *apikeydom.APIKey
	isAgentKey bool
}

func (s *legacyStubAPIKeyAuth) Authenticate(_ context.Context, _ string) (*apikeydom.APIKey, error) {
	return s.key, nil
}

func (s *legacyStubAPIKeyAuth) IsAgentKey(_ context.Context, _ string) bool {
	return s.isAgentKey
}

func TestAgentIDFromContext(t *testing.T) {
	ctx := context.Background()

	agentID, ok := AgentIDFromContext(ctx)
	if ok || agentID != uuid.Nil {
		t.Fatalf("expected no agent ID in empty context")
	}

	testAgentID := uuid.New()
	ctx = WithAgentID(ctx, testAgentID)

	retrievedAgentID, ok := AgentIDFromContext(ctx)
	if !ok {
		t.Fatalf("expected agent ID to be found")
	}
	if retrievedAgentID != testAgentID {
		t.Fatalf("expected agent ID %v, got %v", testAgentID, retrievedAgentID)
	}
}

func TestActorIDFromContext(t *testing.T) {
	ctx := context.Background()

	actorID, ok := ActorIDFromContext(ctx)
	if ok || actorID != uuid.Nil {
		t.Fatalf("expected no actor ID in empty context")
	}

	testActorID := uuid.New()
	ctx = WithActorID(ctx, testActorID)

	retrievedActorID, ok := ActorIDFromContext(ctx)
	if !ok {
		t.Fatalf("expected actor ID to be found")
	}
	if retrievedActorID != testActorID {
		t.Fatalf("expected actor ID %v, got %v", testActorID, retrievedActorID)
	}
}
