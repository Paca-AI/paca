// Package e2e contains end-to-end smoke tests for the Paca API service.
// Tests spin up real Postgres and Redis containers via testcontainers-go,
// apply migrations, wire the full service stack, and exercise the complete
// HTTP request flow against an in-process httptest.Server.
//
// Run with: PACA_E2E=1 go test ./test/e2e/... -v -timeout 120s
package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/paca/api/internal/platform/authz"
	"github.com/paca/api/internal/platform/cache"
	"github.com/paca/api/internal/platform/database"
	jwttoken "github.com/paca/api/internal/platform/token"
	pgRepo "github.com/paca/api/internal/repository/postgres"
	redisRepo "github.com/paca/api/internal/repository/redis"
	authsvc "github.com/paca/api/internal/service/auth"
	usersvc "github.com/paca/api/internal/service/user"
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	e2eJWTSecret  = "e2e-test-secret-value-that-is-at-least-32-chars"
	e2eAccessTTL  = 15 * time.Minute
	e2eRefreshTTL = 24 * time.Hour
)

// TestAPIFlow exercises the complete user lifecycle:
//
//	register → login → get own profile → update profile →
//	refresh tokens → logout → verify protected routes require auth.
//
// Subtests are sequential; each shares state via the enclosed variables.
func TestAPIFlow(t *testing.T) {
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)

	ctx := t.Context()

	// -------------------------------------------------------------------------
	// 1. Start infrastructure containers
	// -------------------------------------------------------------------------
	pgC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "postgres:16-alpine",
			Env: map[string]string{
				"POSTGRES_USER":     "test",
				"POSTGRES_PASSWORD": "test",
				"POSTGRES_DB":       "testdb",
			},
			ExposedPorts: []string{"5432/tcp"},
			WaitingFor:   wait.ForLog("database system is ready to accept connections").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })

	redisC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	t.Cleanup(func() { _ = redisC.Terminate(context.Background()) })

	// -------------------------------------------------------------------------
	// 2. Build DSNs from mapped ports
	// -------------------------------------------------------------------------
	pgHost, err := pgC.Host(ctx)
	if err != nil {
		t.Fatalf("get postgres host: %v", err)
	}
	pgPort, err := pgC.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("get postgres port: %v", err)
	}
	// Force IPv4: on macOS "localhost" resolves to [::1] first but Docker
	// only binds mapped ports on 127.0.0.1.
	if pgHost == "localhost" {
		pgHost = "127.0.0.1"
	}
	pgDSN := fmt.Sprintf(
		"postgresql://test:test@%s:%s/testdb?sslmode=disable",
		pgHost, pgPort.Port(),
	)

	redisHost, err := redisC.Host(ctx)
	if err != nil {
		t.Fatalf("get redis host: %v", err)
	}
	redisPort, err := redisC.MappedPort(ctx, "6379")
	if err != nil {
		t.Fatalf("get redis port: %v", err)
	}
	if redisHost == "localhost" {
		redisHost = "127.0.0.1"
	}
	redisURL := fmt.Sprintf("redis://%s:%s/0", redisHost, redisPort.Port())

	// -------------------------------------------------------------------------
	// 3. Open platform clients and run migrations
	// -------------------------------------------------------------------------
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	db, err := database.Open(pgDSN, log)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get underlying sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Locate migrations relative to this source file so the path is correct
	// regardless of the working directory the test binary was invoked from.
	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	if err := database.RunMigrations(db, migrationsDir); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	redisClient, err := cache.NewClient(redisURL, log)
	if err != nil {
		t.Fatalf("open redis client: %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })

	// -------------------------------------------------------------------------
	// 4. Wire up the full service stack
	// -------------------------------------------------------------------------
	tm := jwttoken.New(e2eJWTSecret, e2eAccessTTL, e2eRefreshTTL)
	policy := authz.NewPolicy()
	userRepo := pgRepo.NewUserRepository(db)
	refreshStore := redisRepo.NewRefreshTokenStore(redisClient)
	authService := authsvc.New(userRepo, tm, refreshStore, e2eRefreshTTL)
	userService := usersvc.New(userRepo)

	cookieCfg := handler.CookieConfig{
		Secure:     false,
		AccessTTL:  e2eAccessTTL,
		RefreshTTL: e2eRefreshTTL,
	}
	engine := router.New(router.Deps{
		TokenManager: tm,
		AuthzPolicy:  policy,
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, cookieCfg),
		User:         handler.NewUserHandler(userService),
		Log:          log,
	})

	// -------------------------------------------------------------------------
	// 5. Start in-process HTTP test server
	// -------------------------------------------------------------------------
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	base := srv.URL

	// Shared browser-like client with automatic cookie management.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}

	// userID is resolved after registration and reused in later subtests.
	var userID string

	// -------------------------------------------------------------------------
	// 6. Test flow
	// -------------------------------------------------------------------------

	t.Run("health_check", func(t *testing.T) {
		resp := mustDo(t, client, mustRequest(ctx, t, http.MethodGet, base+"/api/healthz", nil))
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("unauthenticated_access_rejected", func(t *testing.T) {
		resp := mustDo(t, client, mustRequest(ctx, t, http.MethodGet, base+"/api/v1/users/me", nil))
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("register", func(t *testing.T) {
		body := jsonBody(t, map[string]string{
			"username":  "alice",
			"password":  "supersecret",
			"full_name": "Alice Tester",
		})
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/users", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)

		var env envelope
		decodeJSON(t, resp, &env)
		data := assertDataMap(t, env)
		id, _ := data["id"].(string)
		if id == "" {
			t.Fatal("expected non-empty user id in response")
		}
		userID = id
	})

	t.Run("register_duplicate_rejected", func(t *testing.T) {
		body := jsonBody(t, map[string]string{
			"username":  "alice",
			"password":  "anotherpass",
			"full_name": "Duplicate",
		})
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/users", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusConflict)
	})

	t.Run("login_bad_password_rejected", func(t *testing.T) {
		// Use a cookieless client so this doesn't pollute the session jar.
		body := jsonBody(t, map[string]string{"username": "alice", "password": "wrongpass"})
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("login_missing_body", func(t *testing.T) {
		// No body at all — binding must return 400.
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/login", nil)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("login_missing_password", func(t *testing.T) {
		// Password field absent — binding validation must return 400.
		body := jsonBody(t, map[string]string{"username": "alice"})
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("login_short_password", func(t *testing.T) {
		// Password shorter than the required minimum (min=8) — must return 400.
		body := jsonBody(t, map[string]string{"username": "alice", "password": "short"})
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("login_nonexistent_user", func(t *testing.T) {
		// Username that was never registered — service must return 401.
		body := jsonBody(t, map[string]string{"username": "nobody", "password": "supersecret"})
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("login", func(t *testing.T) {
		body := jsonBody(t, map[string]string{"username": "alice", "password": "supersecret"})
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/login", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("get_me", func(t *testing.T) {
		resp := mustDo(t, client, mustRequest(ctx, t, http.MethodGet, base+"/api/v1/users/me", nil))
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env envelope
		decodeJSON(t, resp, &env)
		data := assertDataMap(t, env)
		if data["username"] != "alice" {
			t.Fatalf("expected username 'alice', got %v", data["username"])
		}
	})

	t.Run("update_me", func(t *testing.T) {
		if userID == "" {
			t.Skip("userID not set; register subtest must have failed")
		}
		body := jsonBody(t, map[string]string{"full_name": "Alice Updated"})
		req := mustRequest(ctx, t, http.MethodPatch, base+"/api/v1/users/"+userID, body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env envelope
		decodeJSON(t, resp, &env)
		data := assertDataMap(t, env)
		if data["full_name"] != "Alice Updated" {
			t.Fatalf("expected full_name 'Alice Updated', got %v", data["full_name"])
		}
	})

	t.Run("refresh_token", func(t *testing.T) {
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/refresh", nil)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env envelope
		decodeJSON(t, resp, &env)
		data := assertDataMap(t, env)
		if data["message"] != "token refreshed" {
			t.Fatalf("expected message 'token refreshed', got %v", data["message"])
		}
	})

	t.Run("access_after_refresh", func(t *testing.T) {
		// The rotated access token must still grant access to protected routes.
		resp := mustDo(t, client, mustRequest(ctx, t, http.MethodGet, base+"/api/v1/users/me", nil))
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("refresh_without_cookie_rejected", func(t *testing.T) {
		// A client that sends no cookies must not be able to refresh.
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/refresh", nil)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("refresh_token_reuse_rejected", func(t *testing.T) {
		// Use an isolated session so the main client's cookies are unaffected.
		isoJar, _ := cookiejar.New(nil)
		isoClient := &http.Client{Jar: isoJar}

		// Login and capture the original refresh token before it is consumed.
		body := jsonBody(t, map[string]string{"username": "alice", "password": "supersecret"})
		loginReq := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/login", body)
		loginReq.Header.Set("Content-Type", "application/json")
		loginResp := mustDo(t, isoClient, loginReq)
		oldRefreshToken := cookieValue(loginResp, "refresh_token")
		_ = loginResp.Body.Close()
		assertStatus(t, loginResp, http.StatusOK)
		if oldRefreshToken == "" {
			t.Fatal("expected refresh_token cookie in login response")
		}

		// First refresh: legitimate use — rotates the token stored in isoClient's jar.
		r1 := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/refresh", nil)
		r1resp := mustDo(t, isoClient, r1)
		_ = r1resp.Body.Close()
		assertStatus(t, r1resp, http.StatusOK)

		// Replay the original (now stale) refresh token — must be rejected.
		r2 := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/refresh", nil)
		r2.AddCookie(&http.Cookie{Name: "refresh_token", Value: oldRefreshToken})
		r2resp := mustDo(t, &http.Client{}, r2)
		defer func() { _ = r2resp.Body.Close() }()
		assertStatus(t, r2resp, http.StatusUnauthorized)
	})

	t.Run("logout_without_auth_rejected", func(t *testing.T) {
		// Logout is protected; a client with no access token must get 401.
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/logout", nil)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("logout", func(t *testing.T) {
		req := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/logout", nil)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env envelope
		decodeJSON(t, resp, &env)
		data := assertDataMap(t, env)
		if data["message"] != "logged out" {
			t.Fatalf("expected message 'logged out', got %v", data["message"])
		}
	})

	t.Run("protected_after_logout", func(t *testing.T) {
		resp := mustDo(t, client, mustRequest(ctx, t, http.MethodGet, base+"/api/v1/users/me", nil))
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("logout_refresh_after_logout_rejected", func(t *testing.T) {
		// Create an isolated session, capture the refresh token, logout, then
		// verify the captured token is rejected (session family revoked).
		isoJar, _ := cookiejar.New(nil)
		isoClient := &http.Client{Jar: isoJar}

		body := jsonBody(t, map[string]string{"username": "alice", "password": "supersecret"})
		loginReq := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/login", body)
		loginReq.Header.Set("Content-Type", "application/json")
		loginResp := mustDo(t, isoClient, loginReq)
		refreshToken := cookieValue(loginResp, "refresh_token")
		_ = loginResp.Body.Close()
		assertStatus(t, loginResp, http.StatusOK)
		if refreshToken == "" {
			t.Fatal("expected refresh_token cookie after login")
		}

		// Logout revokes the entire session family.
		logoutReq := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/logout", nil)
		logoutResp := mustDo(t, isoClient, logoutReq)
		_ = logoutResp.Body.Close()
		assertStatus(t, logoutResp, http.StatusOK)

		// The pre-logout refresh token must now be rejected.
		refreshReq := mustRequest(ctx, t, http.MethodPost, base+"/api/v1/auth/refresh", nil)
		refreshReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshToken})
		refreshResp := mustDo(t, &http.Client{}, refreshReq)
		defer func() { _ = refreshResp.Body.Close() }()
		assertStatus(t, refreshResp, http.StatusUnauthorized)
	})
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// envelope mirrors the presenter.envelope shape for JSON decoding.
type envelope struct {
	Success bool   `json:"success"`
	Data    any    `json:"data"`
	Error   string `json:"error"`
}

func mustRequest(ctx context.Context, t *testing.T, method, url string, body *bytes.Buffer) *http.Request {
	t.Helper()
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, method, url, body)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, url, http.NoBody)
	}
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, url, err)
	}
	return req
}

func mustDo(t *testing.T, c *http.Client, req *http.Request) *http.Response {
	t.Helper()
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", req.Method, req.URL, err)
	}
	return resp
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("expected HTTP %d, got %d", want, resp.StatusCode)
	}
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json body: %v", err)
	}
	return bytes.NewBuffer(b)
}

func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
}

func assertDataMap(t *testing.T, env envelope) map[string]any {
	t.Helper()
	m, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data to be a JSON object, got %T: %v", env.Data, env.Data)
	}
	return m
}

// cookieValue returns the value of the named cookie from the response's
// Set-Cookie headers, or "" if not present.
func cookieValue(resp *http.Response, name string) string {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// checkDockerAvailable skips the test if Docker cannot be reached.
// testcontainers-go panics rather than returning an error when no socket is
// found, so we must probe manually before handing control to the library.
// If a socket is found the function sets DOCKER_HOST so testcontainers picks
// it up automatically.
func checkDockerAvailable(t *testing.T) {
	t.Helper()

	// If DOCKER_HOST is already set and reachable, trust it.
	if dh := os.Getenv("DOCKER_HOST"); dh != "" {
		// Only treat DOCKER_HOST as a filesystem socket path when it is either:
		//   - a bare path (no scheme), or
		//   - a unix:// URL.
		if !strings.Contains(dh, "://") || strings.HasPrefix(dh, "unix://") {
			socket := strings.TrimPrefix(dh, "unix://")
			if _, err := os.Stat(socket); err == nil {
				if isColimaSock(socket) {
					// Ryuk mounts the socket path inside its own container, which
					// fails for Colima sockets on an external macOS volume.
					t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
				}
				return
			}
			// Explicitly set but not reachable — don't fall through silently.
			t.Skipf("DOCKER_HOST=%s set but socket not found; is Docker running?", dh)
		}

		// For non-unix schemes (e.g. tcp://, ssh://), assume the user has
		// configured a reachable Docker host and let testcontainers perform
		// the actual connectivity checks.
		return
	}

	// 1. Try to resolve via the active Docker context (handles Colima, Rancher
	//    Desktop, or any path configured by the user).
	if socket := socketFromDockerContext(); socket != "" {
		if _, err := os.Stat(socket); err == nil {
			t.Setenv("DOCKER_HOST", "unix://"+socket)
			if isColimaSock(socket) {
				// Ryuk mounts the socket path inside its own container, which
				// fails for Colima sockets on an external macOS volume.
				t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
			}
			return
		}
	}

	// 2. Fall back to well-known static paths.
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/var/run/docker.sock",
		filepath.Join(home, ".docker/run/docker.sock"),
		filepath.Join(home, ".docker/desktop/docker.sock"),
		filepath.Join(home, ".colima/default/docker.sock"),
		filepath.Join(home, ".colima/docker.sock"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			t.Setenv("DOCKER_HOST", "unix://"+p)
			if isColimaSock(p) {
				// Ryuk mounts the socket path inside its own container, which
				// fails for Colima sockets on an external macOS volume.
				t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
			}
			return
		}
	}

	t.Skip("Docker socket not found; install Docker Desktop or Colima and retry with PACA_E2E=1")
}

// isColimaSock reports whether p looks like a Colima-managed Docker socket.
// Ryuk cannot mount these paths inside its own container because they live on
// an external (non-Linux) volume that Colima exposes on macOS.
func isColimaSock(p string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to substring check if the home directory is unavailable.
		return strings.Contains(filepath.ToSlash(p), "/.colima/")
	}
	colimaDir := filepath.Join(home, ".colima") + string(filepath.Separator)
	return strings.HasPrefix(p, colimaDir)
}

// socketFromDockerContext reads ~/.docker/config.json to find the active
// context name, then resolves its socket path from the context metadata stored
// at ~/.docker/contexts/meta/<sha256(name)>/meta.json.
func socketFromDockerContext() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// Read currentContext from ~/.docker/config.json.
	type dockerConfig struct {
		CurrentContext string `json:"currentContext"`
	}
	cfgData, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	if err != nil {
		return ""
	}
	var cfg dockerConfig
	if err := json.Unmarshal(cfgData, &cfg); err != nil || cfg.CurrentContext == "" {
		return ""
	}

	// The context metadata lives at ~/.docker/contexts/meta/<sha256(name)>/meta.json.
	sum := sha256.Sum256([]byte(cfg.CurrentContext))
	metaPath := filepath.Join(home, ".docker", "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json")

	type contextEndpoint struct {
		Host string `json:"Host"`
	}
	type contextMeta struct {
		Endpoints map[string]contextEndpoint `json:"Endpoints"`
	}
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return ""
	}
	var meta contextMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return ""
	}

	host := meta.Endpoints["docker"].Host
	return strings.TrimPrefix(host, "unix://")
}
