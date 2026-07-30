package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	apikeysvc "github.com/Paca-AI/api/internal/service/apikey"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
)

// ---------------------------------------------------------------------------
// In-memory API key repository for tests
// ---------------------------------------------------------------------------

type fakeAPIKeyRepo struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]*apikeydom.APIKey
	byHash map[string]*apikeydom.APIKey
	hashes map[uuid.UUID]string // id → hash
}

func newFakeAPIKeyRepo() *fakeAPIKeyRepo {
	return &fakeAPIKeyRepo{
		byID:   make(map[uuid.UUID]*apikeydom.APIKey),
		byHash: make(map[string]*apikeydom.APIKey),
		hashes: make(map[uuid.UUID]string),
	}
}

func (r *fakeAPIKeyRepo) FindByID(_ context.Context, id uuid.UUID) (*apikeydom.APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k, ok := r.byID[id]
	if !ok {
		return nil, apikeydom.ErrNotFound
	}
	return k, nil
}

func (r *fakeAPIKeyRepo) FindByHash(_ context.Context, hash string) (*apikeydom.APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k, ok := r.byHash[hash]
	if !ok {
		return nil, apikeydom.ErrNotFound
	}
	return k, nil
}

func (r *fakeAPIKeyRepo) ListByUserID(_ context.Context, userID uuid.UUID) ([]*apikeydom.APIKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*apikeydom.APIKey
	for _, k := range r.byID {
		if k.UserID == userID && k.RevokedAt == nil {
			out = append(out, k)
		}
	}
	return out, nil
}

func (r *fakeAPIKeyRepo) Create(_ context.Context, key *apikeydom.APIKey, keyHash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[key.ID] = key
	r.byHash[keyHash] = key
	r.hashes[key.ID] = keyHash
	return nil
}

func (r *fakeAPIKeyRepo) Revoke(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k, ok := r.byID[id]
	if !ok {
		return apikeydom.ErrNotFound
	}
	now := time.Now()
	k.RevokedAt = &now
	// Remove from hash lookup so Authenticate fails after revocation.
	if h, ok2 := r.hashes[id]; ok2 {
		delete(r.byHash, h)
	}
	return nil
}

func (r *fakeAPIKeyRepo) UpdateLastUsed(_ context.Context, id uuid.UUID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if k, ok := r.byID[id]; ok {
		k.LastUsedAt = &at
	}
	return nil
}

// ---------------------------------------------------------------------------
// Router builder
// ---------------------------------------------------------------------------

func buildAPIKeyTestRouter(apiKeyRepo *fakeAPIKeyRepo) (http.Handler, *jwttoken.Manager, *fakeUserRepo) {
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	store := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, store, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo, userRepo)
	apiKeyService := apikeysvc.New(apiKeyRepo)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	r := router.New(router.Deps{
		TokenManager: tm,
		APIKeyAuth:   apiKeyService,
		Authorizer:   authz.NewAuthorizer(nil),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, testCookieCfg),
		User:         handler.NewUserHandler(userService),
		APIKey:       handler.NewAPIKeyHandler(apiKeyService),
		Log:          log,
	})
	return r, tm, userRepo
}

// issueUserToken issues a JWT for a regular user.
func issueUserToken(t *testing.T, userID string) string {
	t.Helper()
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	tok, err := tm.IssueAccess(userID, "user", "USER", "fam-"+userID, false)
	if err != nil {
		t.Fatalf("issue user token: %v", err)
	}
	return tok
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAPIKey_ListEmpty(t *testing.T) {
	repo := newFakeAPIKeyRepo()
	r, _, _ := buildAPIKeyTestRouter(repo)

	userID := uuid.NewString()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me/api-keys", nil)
	req.Header.Set("Authorization", "Bearer "+issueUserToken(t, userID))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIKey_CreateAndList(t *testing.T) {
	repo := newFakeAPIKeyRepo()
	r, _, _ := buildAPIKeyTestRouter(repo)

	userID := uuid.NewString()
	tok := issueUserToken(t, userID)

	body, _ := json.Marshal(map[string]string{"name": "My CI token"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/users/me/api-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Key should be in response body only once.
	var env struct {
		Data struct {
			Key       string `json:"key"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			KeyPrefix string `json:"key_prefix"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !hasAPIKeyPrefix(env.Data.Key, "paca_") {
		t.Errorf("key should start with paca_, got %q", env.Data.Key[:10])
	}
	if env.Data.Name != "My CI token" {
		t.Errorf("name mismatch: %q", env.Data.Name)
	}

	// List should now return one key.
	req2 := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me/api-keys", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w2.Code)
	}
	var listEnv struct {
		Data []struct{ ID string } `json:"data"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&listEnv); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listEnv.Data) != 1 {
		t.Errorf("expected 1 key, got %d", len(listEnv.Data))
	}
}

func TestAPIKey_CreateRequiresName(t *testing.T) {
	repo := newFakeAPIKeyRepo()
	r, _, _ := buildAPIKeyTestRouter(repo)

	userID := uuid.NewString()
	body, _ := json.Marshal(map[string]string{"name": ""})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/users/me/api-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueUserToken(t, userID))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAPIKey_RevokeAndCannotReuse(t *testing.T) {
	repo := newFakeAPIKeyRepo()
	r, _, _ := buildAPIKeyTestRouter(repo)

	userID := uuid.NewString()
	tok := issueUserToken(t, userID)

	// Create a key.
	body, _ := json.Marshal(map[string]string{"name": "temp key"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/users/me/api-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&env)

	// Revoke it.
	revokeReq := httptest.NewRequestWithContext(t.Context(), http.MethodDelete,
		"/api/v1/users/me/api-keys/"+env.Data.ID, nil)
	revokeReq.Header.Set("Authorization", "Bearer "+tok)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, revokeReq)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("revoke: expected 204, got %d: %s", w2.Code, w2.Body.String())
	}

	// Using the revoked key should fail with 401.
	req3 := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me/api-keys", nil)
	req3.Header.Set("Authorization", "ApiKey "+env.Data.Key)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	if w3.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key: expected 401, got %d: %s", w3.Code, w3.Body.String())
	}
}

func TestAPIKey_AuthenticateViaAPIKey(t *testing.T) {
	repo := newFakeAPIKeyRepo()
	r, _, userRepo := buildAPIKeyTestRouter(repo)

	userID := uuid.MustParse(uuid.NewString())
	_ = userRepo.Create(context.Background(), &userdom.User{
		ID:       userID,
		Username: "apikeyuser",
		Role:     userdom.RoleUser,
	})
	tok := issueUserToken(t, userID.String())

	// Create a key.
	body, _ := json.Marshal(map[string]string{"name": "sdk key"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/users/me/api-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Data struct{ Key string } `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&env)

	// Use the API key via X-API-Key header.
	req2 := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/users/me", nil)
	req2.Header.Set("X-API-Key", env.Data.Key)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("api key auth: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestAPIKey_RevokeOtherUserKeyForbidden(t *testing.T) {
	repo := newFakeAPIKeyRepo()
	r, _, _ := buildAPIKeyTestRouter(repo)

	ownerID := uuid.NewString()
	otherID := uuid.NewString()

	// Owner creates a key.
	body, _ := json.Marshal(map[string]string{"name": "owner key"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/users/me/api-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueUserToken(t, ownerID))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d", w.Code)
	}
	var env struct {
		Data struct{ ID string } `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&env)

	// Other user tries to revoke.
	revokeReq := httptest.NewRequestWithContext(t.Context(), http.MethodDelete,
		"/api/v1/users/me/api-keys/"+env.Data.ID, nil)
	revokeReq.Header.Set("Authorization", "Bearer "+issueUserToken(t, otherID))
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, revokeReq)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w2.Code, w2.Body.String())
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func hasAPIKeyPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
