package e2e_test

import (
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"
)

func TestAuthFlow(t *testing.T) {
	env := newE2EEnv(t)
	seedUser(t, env, "alice", "supersecret", "Alice Tester")

	t.Run("login_bad_password_rejected", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"username": "alice", "password": "wrongpass"})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
		assertErrorCode(t, resp, "AUTH_INVALID_CREDENTIALS")
	})

	t.Run("login_missing_body", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/login", nil)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "BAD_REQUEST")
	})

	t.Run("login_missing_password", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"username": "alice"})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "BAD_REQUEST")
	})

	t.Run("login_short_password", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"username": "alice", "password": "short"})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "BAD_REQUEST")
	})

	t.Run("login_nonexistent_user", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"username": "nobody", "password": "supersecret"})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
		assertErrorCode(t, resp, "AUTH_INVALID_CREDENTIALS")
	})

	t.Run("login", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"username": "alice", "password": "supersecret"})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("refresh_without_cookie_rejected", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/refresh", nil)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
		assertErrorCode(t, resp, "AUTH_MISSING_TOKEN")
	})

	t.Run("refresh_token", func(t *testing.T) {
		loginResp := login(env.ctx, t, env.client, env.base, "alice", "supersecret")
		_ = loginResp.Body.Close()

		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/refresh", nil)
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var envResp envelope
		decodeJSON(t, resp, &envResp)
		data := assertDataMap(t, envResp)
		if data["message"] != "token refreshed" {
			t.Fatalf("expected message 'token refreshed', got %v", data["message"])
		}
	})

	t.Run("refresh_token_reuse_within_grace_period", func(t *testing.T) {
		isoJar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("failed to create cookie jar: %v", err)
		}
		isoClient := &http.Client{Jar: isoJar}

		loginResp := login(env.ctx, t, isoClient, env.base, "alice", "supersecret")
		oldRefreshToken := cookieValue(loginResp, "refresh_token")
		_ = loginResp.Body.Close()
		if oldRefreshToken == "" {
			t.Fatal("expected refresh_token cookie in login response")
		}

		firstRefreshReq := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/refresh", nil)
		firstRefreshResp := mustDo(t, isoClient, firstRefreshReq)
		_ = firstRefreshResp.Body.Close()
		assertStatus(t, firstRefreshResp, http.StatusOK)

		// Replay the old token immediately (within grace period).
		replayReq := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/refresh", nil)
		replayReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: oldRefreshToken})
		replayResp := mustDo(t, &http.Client{}, replayReq)
		defer func() { _ = replayResp.Body.Close() }()
		assertStatus(t, replayResp, http.StatusUnauthorized)
		assertErrorCode(t, replayResp, "AUTH_TOKEN_INVALID")

		// Family must still be valid within grace period.
		refreshAgainReq := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/refresh", nil)
		refreshAgainResp := mustDo(t, isoClient, refreshAgainReq)
		defer func() { _ = refreshAgainResp.Body.Close() }()
		assertStatus(t, refreshAgainResp, http.StatusOK)
	})

	t.Run("refresh_token_reuse_outside_grace_period", func(t *testing.T) {
		isoJar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatalf("failed to create cookie jar: %v", err)
		}
		isoClient := &http.Client{Jar: isoJar}

		loginResp := login(env.ctx, t, isoClient, env.base, "alice", "supersecret")
		oldRefreshToken := cookieValue(loginResp, "refresh_token")
		_ = loginResp.Body.Close()
		if oldRefreshToken == "" {
			t.Fatal("expected refresh_token cookie in login response")
		}

		firstRefreshReq := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/refresh", nil)
		firstRefreshResp := mustDo(t, isoClient, firstRefreshReq)
		_ = firstRefreshResp.Body.Close()
		assertStatus(t, firstRefreshResp, http.StatusOK)

		// Wait beyond the configured 5s grace period before replaying old token.
		time.Sleep(6 * time.Second)

		replayReq := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/refresh", nil)
		replayReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: oldRefreshToken})
		replayResp := mustDo(t, &http.Client{}, replayReq)
		defer func() { _ = replayResp.Body.Close() }()
		assertStatus(t, replayResp, http.StatusUnauthorized)
		assertErrorCode(t, replayResp, "AUTH_TOKEN_INVALID")

		// Family must be revoked outside grace period.
		refreshAgainReq := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/refresh", nil)
		refreshAgainResp := mustDo(t, isoClient, refreshAgainReq)
		defer func() { _ = refreshAgainResp.Body.Close() }()
		assertStatus(t, refreshAgainResp, http.StatusUnauthorized)
		assertErrorCode(t, refreshAgainResp, "AUTH_TOKEN_INVALID")
	})

	t.Run("logout_without_auth_rejected", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/logout", nil)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
		assertErrorCode(t, resp, "AUTH_MISSING_TOKEN")
	})

	t.Run("logout", func(t *testing.T) {
		loginResp := login(env.ctx, t, env.client, env.base, "alice", "supersecret")
		_ = loginResp.Body.Close()

		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/logout", nil)
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var envResp envelope
		decodeJSON(t, resp, &envResp)
		data := assertDataMap(t, envResp)
		if data["message"] != "logged out" {
			t.Fatalf("expected message 'logged out', got %v", data["message"])
		}
	})
}

func TestAuthFlow_RememberMe(t *testing.T) {
	env := newE2EEnv(t)
	seedUser(t, env, "bob", "supersecret", "Bob Tester")

	t.Run("remember_me_true_long_lived_cookie", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		resp := loginWithRememberMe(env.ctx, t, client, env.base, "bob", "supersecret", true)
		defer func() { _ = resp.Body.Close() }()

		c := findCookie(resp, "refresh_token")
		if c == nil {
			t.Fatal("refresh_token cookie not found")
		}
		// Persistent session: MaxAge should equal the persistent TTL (e2eRefreshTTL = 48h in e2e env),
		// which is strictly greater than the session TTL (e2eRefreshSessionTTL = 24h).
		wantMinAge := int(e2eRefreshTTL.Seconds())
		if c.MaxAge < wantMinAge {
			t.Errorf("expected refresh_token MaxAge >= %d (persistent), got %d", wantMinAge, c.MaxAge)
		}
	})

	t.Run("remember_me_false_session_cookie", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		resp := loginWithRememberMe(env.ctx, t, client, env.base, "bob", "supersecret", false)
		defer func() { _ = resp.Body.Close() }()

		c := findCookie(resp, "refresh_token")
		if c == nil {
			t.Fatal("refresh_token cookie not found")
		}
		// Session: MaxAge must equal the session TTL (e2eRefreshSessionTTL = 24h).
		wantMaxAge := int(e2eRefreshSessionTTL.Seconds())
		if c.MaxAge != wantMaxAge {
			t.Errorf("expected refresh_token MaxAge=%d (24h session), got %d", wantMaxAge, c.MaxAge)
		}
	})

	t.Run("remember_me_omitted_defaults_to_false", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		// login() helper does not set remember_me — defaults to false.
		resp := login(env.ctx, t, client, env.base, "bob", "supersecret")
		defer func() { _ = resp.Body.Close() }()

		c := findCookie(resp, "refresh_token")
		if c == nil {
			t.Fatal("refresh_token cookie not found")
		}
		wantMaxAge := int(e2eRefreshSessionTTL.Seconds())
		if c.MaxAge != wantMaxAge {
			t.Errorf("expected refresh_token MaxAge=%d (24h session), got %d", wantMaxAge, c.MaxAge)
		}
	})

	t.Run("remember_me_preserved_through_refresh_rotation", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		// Login without remember me → session TTL.
		loginResp := loginWithRememberMe(env.ctx, t, client, env.base, "bob", "supersecret", false)
		_ = loginResp.Body.Close()

		// Rotate the token.
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/auth/refresh", nil)
		refreshResp := mustDo(t, client, req)
		defer func() { _ = refreshResp.Body.Close() }()
		assertStatus(t, refreshResp, http.StatusOK)

		// The rotated cookie must still carry the session TTL.
		rotated := findCookie(refreshResp, "refresh_token")
		if rotated == nil {
			t.Fatal("refresh_token cookie not found after rotation")
		}
		wantMaxAge := int(e2eRefreshSessionTTL.Seconds())
		if rotated.MaxAge != wantMaxAge {
			t.Errorf("expected rotated refresh_token MaxAge=%d (session), got %d", wantMaxAge, rotated.MaxAge)
		}
	})
}
