package e2e_test

import (
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// apiKeyUserLogin seeds a regular user and returns a dedicated HTTP client
// with cookie-based session and the raw access token string.
func apiKeyUserLogin(t *testing.T, env *e2eEnv, username, password string) (*http.Client, string) {
	t.Helper()
	seedUser(t, env, username, password, "API Key User")
	assignGlobalRolesByName(t, env, username, "USER")
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	resp := login(env.ctx, t, client, env.base, username, password)
	defer func() { _ = resp.Body.Close() }()
	token := cookieValue(resp, "access_token")
	return client, token
}

// createAPIKey calls POST /api/v1/users/me/api-keys and returns the key ID
// and the raw key string.
func createAPIKey(t *testing.T, env *e2eEnv, client *http.Client, token, name string) (id, rawKey string) {
	t.Helper()
	body := jsonBody(t, map[string]any{"name": name})
	req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/users/me/api-keys", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	id, _ = data["id"].(string)
	rawKey, _ = data["key"].(string)
	if id == "" || rawKey == "" {
		t.Fatal("create API key: expected non-empty id and key in response")
	}
	return id, rawKey
}

// ---------------------------------------------------------------------------
// TestE2EAPIKey_CRUD exercises the full lifecycle via a real Postgres backend.
// ---------------------------------------------------------------------------

func TestE2EAPIKey_CRUD(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := apiKeyUserLogin(t, env, "apikey-crud-user", "keypass1!")

	var keyID, rawKey string

	t.Run("list_empty", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me/api-keys", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		items, _ := env2.Data.([]any)
		if len(items) != 0 {
			t.Errorf("expected 0 API keys initially, got %d", len(items))
		}
	})

	t.Run("create", func(t *testing.T) {
		keyID, rawKey = createAPIKey(t, env, client, token, "My CI Key")

		// The raw key must start with the paca_ prefix.
		if !strings.HasPrefix(rawKey, "paca_") {
			t.Errorf("expected raw key to start with paca_, got %q", rawKey[:minInt(10, len(rawKey))])
		}
	})

	t.Run("middleware_auth_with_created_key", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me", nil)
		req.Header.Set("Authorization", "ApiKey "+rawKey)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if data["username"] != "apikey-crud-user" {
			t.Errorf("expected username=apikey-crud-user from API key auth, got %v", data["username"])
		}
	})

	t.Run("list_after_create", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me/api-keys", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		items, _ := env2.Data.([]any)
		if len(items) != 1 {
			t.Errorf("expected 1 API key after create, got %d", len(items))
		}
		// The list response must NOT contain the raw key.
		item, _ := items[0].(map[string]any)
		if _, ok := item["key"]; ok {
			t.Error("list response must not expose the raw key")
		}
		if item["id"] != keyID {
			t.Errorf("expected key id %q, got %q", keyID, item["id"])
		}
	})

	t.Run("revoke", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodDelete, env.base+"/api/v1/users/me/api-keys/"+keyID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("list_after_revoke", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me/api-keys", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		items, _ := env2.Data.([]any)
		if len(items) != 0 {
			t.Errorf("expected 0 API keys after revoke, got %d", len(items))
		}
	})

	t.Run("revoked_key_cannot_authenticate", func(t *testing.T) {
		// rawKey captured during create; key is already revoked above.
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me/api-keys", nil)
		req.Header.Set("X-API-Key", rawKey)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})
}

// ---------------------------------------------------------------------------
// TestE2EAPIKey_AuthMethods verifies both supported authentication header
// formats work with a live API key against a real database.
// ---------------------------------------------------------------------------

func TestE2EAPIKey_AuthMethods(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := apiKeyUserLogin(t, env, "apikey-auth-user", "keypass2!")
	_, rawKey := createAPIKey(t, env, client, token, "sdk-key")

	t.Run("x_api_key_header", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me", nil)
		req.Header.Set("X-API-Key", rawKey)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("authorization_apikey_header", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me", nil)
		req.Header.Set("Authorization", "ApiKey "+rawKey)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})
}

// ---------------------------------------------------------------------------
// TestE2EAPIKey_Validation exercises input validation on key creation.
// ---------------------------------------------------------------------------

func TestE2EAPIKey_Validation(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := apiKeyUserLogin(t, env, "apikey-val-user", "keypass3!")

	t.Run("empty_name_rejected", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"name": ""})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/users/me/api-keys", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "API_KEY_NAME_INVALID")
	})

	t.Run("name_too_long_rejected", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"name": strings.Repeat("x", 101)})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/users/me/api-keys", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "API_KEY_NAME_TOO_LONG")
	})

	t.Run("missing_body_rejected", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/users/me/api-keys", nil)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("invalid_key_id_on_revoke", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodDelete, env.base+"/api/v1/users/me/api-keys/not-a-uuid", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})
}

// ---------------------------------------------------------------------------
// TestE2EAPIKey_Authorization verifies ownership enforcement.
// ---------------------------------------------------------------------------

func TestE2EAPIKey_Authorization(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	ownerClient, ownerToken := apiKeyUserLogin(t, env, "apikey-owner", "keypass4!")
	otherClient, otherToken := apiKeyUserLogin(t, env, "apikey-other", "keypass5!")

	keyID, _ := createAPIKey(t, env, ownerClient, ownerToken, "owner-key")

	t.Run("other_user_cannot_revoke_owner_key", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodDelete, env.base+"/api/v1/users/me/api-keys/"+keyID, nil)
		req.Header.Set("Authorization", "Bearer "+otherToken)
		resp := mustDo(t, otherClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("unauthenticated_list_rejected", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me/api-keys", nil)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("unauthenticated_create_rejected", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"name": "anon-key"})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/users/me/api-keys", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("other_user_list_sees_only_own_keys", func(t *testing.T) {
		// Owner has 1 key; other user should see 0 (their own list is empty).
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me/api-keys", nil)
		req.Header.Set("Authorization", "Bearer "+otherToken)
		resp := mustDo(t, otherClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		items, _ := env2.Data.([]any)
		if len(items) != 0 {
			t.Errorf("other user should see 0 keys, got %d", len(items))
		}
	})
}

// ---------------------------------------------------------------------------
// TestE2EAPIKey_MultipleKeys verifies a user can hold several keys at once.
// ---------------------------------------------------------------------------

func TestE2EAPIKey_MultipleKeys(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := apiKeyUserLogin(t, env, "apikey-multi-user", "keypass6!")

	names := []string{"key-alpha", "key-beta", "key-gamma"}
	for _, n := range names {
		createAPIKey(t, env, client, token, n)
	}

	req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me/api-keys", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	var env2 envelope
	decodeJSON(t, resp, &env2)
	items, _ := env2.Data.([]any)
	if len(items) != len(names) {
		t.Errorf("expected %d keys, got %d", len(names), len(items))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
