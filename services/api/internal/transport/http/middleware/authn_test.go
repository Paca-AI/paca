package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
)

// stubAPIKeyAuth is a minimal APIKeyAuthenticator for unit tests. It also
// unconditionally implements AgentIdentityVerifier (defaulting to "deny" —
// findAgentByID nil means every agent ID is unverified) so tests exercising
// X-Agent-ID / X-Actor-User-ID must explicitly opt in to a known-good agent
// via findAgentByID/hasActiveGlobalChatSession, the same way production code
// only trusts those headers once apiKeyService.WithAgentIdentityStore is
// wired — see AgentIdentityVerifier's doc comment for why the default must
// be deny, not allow.
type stubAPIKeyAuth struct {
	key        *apikeydom.APIKey
	err        error
	isAgentKey bool

	findAgentByID              func(ctx context.Context, agentID uuid.UUID) (*agentdom.Agent, error)
	hasActiveGlobalChatSession func(ctx context.Context, agentID, actorUserID uuid.UUID) (bool, error)
}

func (s *stubAPIKeyAuth) Authenticate(_ context.Context, _ string) (*apikeydom.APIKey, error) {
	return s.key, s.err
}

func (s *stubAPIKeyAuth) IsAgentKey(_ context.Context, _ string) bool {
	return s.isAgentKey
}

func (s *stubAPIKeyAuth) FindAgentByID(ctx context.Context, agentID uuid.UUID) (*agentdom.Agent, error) {
	if s.findAgentByID != nil {
		return s.findAgentByID(ctx, agentID)
	}
	return nil, agentdom.ErrAgentNotFound
}

func (s *stubAPIKeyAuth) HasActiveGlobalChatSession(ctx context.Context, agentID, actorUserID uuid.UUID) (bool, error) {
	if s.hasActiveGlobalChatSession != nil {
		return s.hasActiveGlobalChatSession(ctx, agentID, actorUserID)
	}
	return false, nil
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
	stub := &stubAPIKeyAuth{
		key:        &apikeydom.APIKey{ID: uuid.New(), UserID: userID},
		isAgentKey: true,
		// Simulates apiKeyService.WithAgentIdentityStore finding a real
		// agent for this ID — without this, the request would be rejected
		// (see TestAuthn_APIKey_WithUnverifiableAgentID).
		findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
			if id == agentID {
				return &agentdom.Agent{ID: agentID}, nil
			}
			return nil, agentdom.ErrAgentNotFound
		},
	}

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

// TestAuthn_APIKey_WithInvalidAgentID is a regression test for the fix in
// resolveAgentClaims: a malformed X-Agent-ID on an agent-key request used to
// be silently ignored (falling back to "no agent," 200 OK) — now it must
// reject the whole request, since a well-formed API key paired with a
// garbage agent claim is more likely a bug or a probe than a legitimate
// caller who simply forgot the header.
func TestAuthn_APIKey_WithInvalidAgentID(t *testing.T) {
	userID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "test-api-key")
	req.Header.Set("X-Agent-ID", "not-a-valid-uuid")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for a malformed X-Agent-ID, got %d (%s)", w.Code, w.Body.String())
	}
}

// TestAuthn_APIKey_WithUnverifiableAgentID is the core regression test for
// the security gap: a well-formed X-Agent-ID that doesn't name a real agent
// (stubAPIKeyAuth's default findAgentByID behavior — see its doc comment)
// must be rejected, not silently trusted. Before this fix, anyone holding
// the single shared static agent API key could set X-Agent-ID to any UUID
// and have it trusted outright — this is what let a caller enumerate an
// arbitrary agent's permissions via GetMyGlobalPermissions.
func TestAuthn_APIKey_WithUnverifiableAgentID(t *testing.T) {
	userID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true}

	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/protected", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("X-API-Key", "test-api-key")
	req.Header.Set("X-Agent-ID", uuid.New().String()) // well-formed, but no findAgentByID match
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an agent ID that doesn't verify, got %d (%s)", w.Code, w.Body.String())
	}
}

// TestAuthn_APIKey_ActorUserID_RequiresGlobalAgentWithActiveSession covers
// the second half of the same gap: X-Actor-User-ID must be rejected unless
// the claimed agent is global-scope AND has an active chat session with
// that specific human — otherwise a leaked static key could attribute
// actions (e.g. CreateProject) to an arbitrary user.
func TestAuthn_APIKey_ActorUserID_RequiresGlobalAgentWithActiveSession(t *testing.T) {
	userID := uuid.New()
	agentID := uuid.New()
	actorUserID := uuid.New()

	newRouter := func(stub *stubAPIKeyAuth) chi.Router {
		r := chi.NewRouter()
		r.With(Authn(newTestTokenManager(), stub)).Get("/protected", func(w http.ResponseWriter, req *http.Request) {
			gotAgentID, agentOK := AgentIDFromRequest(req)
			gotActorUserID, actorOK := AgentActorUserIDFromRequest(req)
			resp := map[string]any{
				"agent_id_ok": agentOK, "agent_id": gotAgentID.String(),
				"actor_ok": actorOK, "actor_user_id": gotActorUserID.String(),
			}
			body, _ := json.Marshal(resp)
			_, _ = w.Write(body)
		})
		return r
	}

	doRequest := func(r chi.Router) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		req.Header.Set("X-Agent-ID", agentID.String())
		req.Header.Set("X-Actor-User-ID", actorUserID.String())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("project-scoped agent claiming an actor is rejected", func(t *testing.T) {
		stub := &stubAPIKeyAuth{
			key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true,
			findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
				return &agentdom.Agent{ID: id, AgentScope: agentdom.AgentScopeProject}, nil
			},
			// Even if a session row somehow existed, project scope alone
			// must be disqualifying.
			hasActiveGlobalChatSession: func(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
				return true, nil
			},
		}
		w := doRequest(newRouter(stub))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", w.Code, w.Body.String())
		}
	})

	t.Run("global agent with no active session for this actor is rejected", func(t *testing.T) {
		stub := &stubAPIKeyAuth{
			key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true,
			findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
				return &agentdom.Agent{ID: id, AgentScope: agentdom.AgentScopeGlobal}, nil
			},
			hasActiveGlobalChatSession: func(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
				return false, nil
			},
		}
		w := doRequest(newRouter(stub))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d (%s)", w.Code, w.Body.String())
		}
	})

	t.Run("global agent with an active session for this actor is accepted", func(t *testing.T) {
		var gotAgentID, gotActorUserID uuid.UUID
		stub := &stubAPIKeyAuth{
			key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true,
			findAgentByID: func(_ context.Context, id uuid.UUID) (*agentdom.Agent, error) {
				return &agentdom.Agent{ID: id, AgentScope: agentdom.AgentScopeGlobal}, nil
			},
			hasActiveGlobalChatSession: func(_ context.Context, id, actor uuid.UUID) (bool, error) {
				gotAgentID, gotActorUserID = id, actor
				return true, nil
			},
		}
		w := doRequest(newRouter(stub))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
		}
		if gotAgentID != agentID || gotActorUserID != actorUserID {
			t.Fatalf("expected session check for (%s, %s), got (%s, %s)", agentID, actorUserID, gotAgentID, gotActorUserID)
		}
		var resp struct {
			AgentIDOK   bool   `json:"agent_id_ok"`
			AgentID     string `json:"agent_id"`
			ActorOK     bool   `json:"actor_ok"`
			ActorUserID string `json:"actor_user_id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !resp.AgentIDOK || resp.AgentID != agentID.String() {
			t.Fatalf("expected verified agent id %s in context, got %+v", agentID, resp)
		}
		if !resp.ActorOK || resp.ActorUserID != actorUserID.String() {
			t.Fatalf("expected verified actor user id %s in context, got %+v", actorUserID, resp)
		}
	})
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

// TestOptionalAuthn_APIKey_UnverifiableAgentIDDegradesGracefully asserts
// OptionalAuthn's documented contract ("does NOT abort if... credentials
// are present [but bad]") still holds for an agent-key request carrying an
// unverifiable X-Agent-ID: the request must proceed (200), just without an
// agent identity in context — never a 401, which would break every public,
// optional-auth route an agent process happens to hit with a stale or
// spoofed claim.
func TestOptionalAuthn_APIKey_UnverifiableAgentIDDegradesGracefully(t *testing.T) {
	userID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), UserID: userID}, isAgentKey: true}

	r := chi.NewRouter()
	r.With(OptionalAuthn(newTestTokenManager(), stub)).Get("/public", func(w http.ResponseWriter, req *http.Request) {
		if _, ok := AgentIDFromRequest(req); ok {
			http.Error(w, `{"error":"agent ID should not be trusted"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/public", nil)
	req.Header.Set("X-API-Key", "test-api-key")
	req.Header.Set("X-Agent-ID", uuid.New().String()) // well-formed, but no findAgentByID match
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (OptionalAuthn must never hard-fail), got %d (%s)", w.Code, w.Body.String())
	}
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
