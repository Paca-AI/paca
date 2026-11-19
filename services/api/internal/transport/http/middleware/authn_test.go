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

	accountdom "github.com/Paca-AI/api/internal/domain/account"
	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
)

// stubAPIKeyAuth is a minimal APIKeyAuthenticator for unit tests.
type stubAPIKeyAuth struct {
	key *apikeydom.APIKey
	err error
}

func (s *stubAPIKeyAuth) Authenticate(_ context.Context, _ string) (*apikeydom.APIKey, error) {
	return s.key, s.err
}

// testUserID is the (UUID) account id the session-token tests use.
const testUserID = "6f1c2f6e-1d6e-4f3e-9d5a-0b7a6c1e2d10"

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
	at, err := tm.IssueAccess(testUserID, "alice", "USER", "fam", false)
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
	rt, err := tm.IssueRefresh(testUserID, "alice", "USER", "fam")
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

// TestAuthn_AnnotationScopedToken_AllowedOnAnnotationPath and
// _RejectedOnOtherPath together are the core security property of
// AnnotationExtensionPathPattern: a domainauth.ScopeAnnotation token is a
// real, validly-signed credential for a real user, so it must authenticate
// successfully on the narrow route set the browser extension actually
// needs, and be refused everywhere else even though the signature and
// Kind checks alone would happily accept it.

func TestAuthn_AnnotationScopedToken_AllowedOnAnnotationPath(t *testing.T) {
	tm := newTestTokenManager()
	at, err := tm.IssueAnnotationAccess(testUserID, "alice", "USER", "fam", false)
	if err != nil {
		t.Fatalf("issue annotation access token: %v", err)
	}

	r := chi.NewRouter()
	r.With(Authn(tm)).Get("/api/v1/port-forwards/resolve", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/port-forwards/resolve", nil)
	req.AddCookie(&http.Cookie{Name: "annotation_access_token", Value: at})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAuthn_AnnotationScopedToken_RejectedOnOtherPath(t *testing.T) {
	tm := newTestTokenManager()
	at, err := tm.IssueAnnotationAccess(testUserID, "alice", "USER", "fam", false)
	if err != nil {
		t.Fatalf("issue annotation access token: %v", err)
	}

	r := chi.NewRouter()
	r.With(Authn(tm)).Get("/api/v1/projects/{projectId}", okHandler)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/projects/proj1", nil)
	req.AddCookie(&http.Cookie{Name: "annotation_access_token", Value: at})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an annotation-scoped token outside its allowed paths, got %d", w.Code)
	}
}

func TestAuthn_AnnotationScopedToken_FullAccessTokenTakesPrecedence(t *testing.T) {
	tm := newTestTokenManager()
	full, err := tm.IssueAccess(testUserID, "alice", "USER", "fam", false)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	annotation, err := tm.IssueAnnotationAccess(testUserID, "alice", "USER", "fam", false)
	if err != nil {
		t.Fatalf("issue annotation access token: %v", err)
	}

	r := chi.NewRouter()
	r.With(Authn(tm)).Get("/api/v1/projects/{projectId}", okHandler)

	// Both cookies present, as they would be for a same-scheme request
	// where the browser attaches everything -- the full-scope access_token
	// must win so an out-of-scope annotation token never even gets
	// consulted here.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/projects/proj1", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: full})
	req.AddCookie(&http.Cookie{Name: "annotation_access_token", Value: annotation})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when a full-scope access_token is present alongside an annotation cookie, got %d", w.Code)
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
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), AccountID: userID, AccountKind: accountdom.KindUser}}

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
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), AccountID: userID, AccountKind: accountdom.KindUser}}

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
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), AccountID: userID, AccountKind: accountdom.KindUser}}

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
	at, err := tm.IssueAccess(testUserID, "alice", "USER", "fam", false)
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

// principalProbe reports the Principal and agent view a handler sees.
func principalProbe(w http.ResponseWriter, req *http.Request) {
	p, ok := PrincipalFrom(req)
	agentID, agentOK := AgentIDFromRequest(req)
	actor, _ := ActorIDFromContext(req.Context())
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": ok, "account_id": p.AccountID.String(), "kind": string(p.Kind), "method": p.Method,
		"agent_ok": agentOK, "agent_id": agentID.String(), "actor": actor.String(),
	})
}

type probeResp struct {
	OK        bool   `json:"ok"`
	AccountID string `json:"account_id"`
	Kind      string `json:"kind"`
	Method    string `json:"method"`
	AgentOK   bool   `json:"agent_ok"`
	AgentID   string `json:"agent_id"`
	Actor     string `json:"actor"`
}

func probe(t *testing.T, h http.Handler, req *http.Request) (int, probeResp) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var resp probeResp
	if w.Code == http.StatusOK {
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
	return w.Code, resp
}

// An API key authenticates as exactly the account that owns it, whatever its
// kind: the principal carries the account id and kind, and an agent's key makes
// the caller that agent.
func TestAuthn_APIKey_PrincipalIsTheOwningAccount(t *testing.T) {
	for _, kind := range []accountdom.Kind{accountdom.KindUser, accountdom.KindAgent, accountdom.KindServiceAccount} {
		t.Run(string(kind), func(t *testing.T) {
			id := uuid.New()
			stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), AccountID: id, AccountKind: kind}}
			r := chi.NewRouter()
			r.With(Authn(newTestTokenManager(), stub)).Get("/p", principalProbe)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/p", nil)
			req.Header.Set("X-API-Key", "paca_somekey")
			code, got := probe(t, r, req)
			if code != http.StatusOK {
				t.Fatalf("code %d", code)
			}
			if got.AccountID != id.String() || got.Kind != string(kind) || got.Method != MethodAPIKey || got.Actor != id.String() {
				t.Fatalf("principal = %+v", got)
			}
			if wantAgent := kind == accountdom.KindAgent; got.AgentOK != wantAgent {
				t.Fatalf("agent view = %v, want %v", got.AgentOK, wantAgent)
			}
		})
	}
}

// X-Agent-ID and X-Actor-User-ID mean nothing: a caller is the account its
// credential proves, never one named by a header, and acts with its own
// permissions only.
func TestAuthn_APIKey_IgnoresAgentHeaders(t *testing.T) {
	userID := uuid.New()
	stub := &stubAPIKeyAuth{key: &apikeydom.APIKey{ID: uuid.New(), AccountID: userID, AccountKind: accountdom.KindUser}}
	r := chi.NewRouter()
	r.With(Authn(newTestTokenManager(), stub)).Get("/p", principalProbe)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/p", nil)
	req.Header.Set("X-API-Key", "paca_somekey")
	req.Header.Set("X-Agent-ID", uuid.NewString())
	req.Header.Set("X-Actor-User-ID", uuid.NewString())
	code, got := probe(t, r, req)
	if code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if got.AccountID != userID.String() || got.Kind != string(accountdom.KindUser) || got.AgentOK {
		t.Fatalf("headers must not change the principal: %+v", got)
	}
}

// A session token (every token Login issues) is a user.
func TestAuthn_SessionToken_IsAUser(t *testing.T) {
	tm := newTestTokenManager()
	userID := uuid.New()
	at, err := tm.IssueAccess(userID.String(), "alice", "USER", "fam", false)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.With(Authn(tm)).Get("/p", principalProbe)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/p", nil)
	req.Header.Set("Authorization", "Bearer "+at)
	code, got := probe(t, r, req)
	if code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if got.AccountID != userID.String() || got.Kind != string(accountdom.KindUser) || got.Method != MethodSession || got.AgentOK {
		t.Fatalf("principal = %+v", got)
	}
}

// OptionalAuthn never aborts on a bad API key: the request proceeds
// unauthenticated.
func TestOptionalAuthn_BadAPIKeyProceedsUnauthenticated(t *testing.T) {
	stub := &stubAPIKeyAuth{err: apikeydom.ErrNotFound}
	r := chi.NewRouter()
	r.With(OptionalAuthn(newTestTokenManager(), stub)).Get("/public", func(w http.ResponseWriter, req *http.Request) {
		if _, ok := PrincipalFrom(req); ok {
			http.Error(w, `{"error":"must be unauthenticated"}`, http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/public", nil)
	req.Header.Set("X-API-Key", "paca_bad")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
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

func TestAuthn_RevokedSessionAccessTokenRejected(t *testing.T) {
	tm := newTestTokenManager()
	revoked := map[string]bool{}
	tm.WithRevocationCheck(func(_ context.Context, fid string) (bool, error) { return revoked[fid], nil })

	token, err := tm.IssueAccess(testUserID, "alice", "USER", "fam-1", false)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.With(Authn(tm)).Get("/protected", okHandler)
	call := func() int {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if got := call(); got != http.StatusOK {
		t.Fatalf("live session: %d, want 200", got)
	}
	revoked["fam-1"] = true
	if got := call(); got != http.StatusUnauthorized {
		t.Fatalf("revoked session: %d, want 401", got)
	}
}

func TestAuthn_RevocationLookupFailureFailsOpen(t *testing.T) {
	tm := newTestTokenManager()
	tm.WithRevocationCheck(func(context.Context, string) (bool, error) { return false, errors.New("redis down") })
	token, _ := tm.IssueAccess(testUserID, "alice", "USER", "fam-1", false)

	r := chi.NewRouter()
	r.With(Authn(tm)).Get("/protected", okHandler)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
}
