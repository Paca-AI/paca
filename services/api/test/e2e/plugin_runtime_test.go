package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Paca-AI/api/internal/platform/authz"
	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	pluginsvc "github.com/Paca-AI/api/internal/service/plugin"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// ---------------------------------------------------------------------------
// echoplugin fixture
// ---------------------------------------------------------------------------
//
// Unlike plugin_test.go (which exercises only the plugin management API
// against a nil runtime), these tests wire a real *pluginrt.Runtime backed
// by a real wazero-loaded WASM module, so a request actually flows:
// HTTP -> router -> PluginHandler.ProxyRequest -> Runtime.HandleRequest ->
// wasm -> back. See testdata/echoplugin for the fixture's behavior.

var (
	echoWasmOnce sync.Once
	echoWasmPath string
	echoWasmErr  error
)

// buildEchoPluginFixture compiles testdata/echoplugin into a WASI-reactor
// wasm binary the first time it's needed and reuses the result for the rest
// of the test binary's run. It isn't committed to the repo (the project's
// .gitignore excludes *.wasm everywhere), so it's built on demand with the
// standard Go toolchain -- GOOS=wasip1 GOARCH=wasm cross-compilation needs
// nothing beyond the Go distribution already required to run these tests.
func buildEchoPluginFixture(t *testing.T) string {
	t.Helper()
	echoWasmOnce.Do(func() {
		dir, err := os.MkdirTemp("", "echoplugin-*")
		if err != nil {
			echoWasmErr = err
			return
		}
		wd, err := os.Getwd()
		if err != nil {
			echoWasmErr = err
			return
		}
		out := filepath.Join(dir, "echo.wasm")
		cmd := exec.CommandContext(t.Context(), "go", "build", "-buildmode=c-shared", "-o", out, "./testdata/echoplugin")
		cmd.Dir = wd
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		if output, buildErr := cmd.CombinedOutput(); buildErr != nil {
			echoWasmErr = fmt.Errorf("build echoplugin fixture: %w: %s", buildErr, output)
			return
		}
		echoWasmPath = out
	})
	if echoWasmErr != nil {
		t.Fatalf("build echoplugin fixture: %v", echoWasmErr)
	}
	return echoWasmPath
}

// ---------------------------------------------------------------------------
// Runtime-backed plugin e2e environment
// ---------------------------------------------------------------------------

type pluginRuntimeE2EEnv struct {
	base          string
	client        *http.Client
	env           *e2eEnv
	pluginService plugindom.Service
	runtime       *pluginrt.Runtime
	wasmDir       string
}

// newPluginRuntimeE2EEnv wires a real *pluginrt.Runtime (instead of nil) into
// the router, parameterized by limits so individual tests can configure a
// small MaxRequestBodyBytes or MaxMemoryPages to exercise the size-limit and
// allocator-recovery paths.
func newPluginRuntimeE2EEnv(t *testing.T, limits pluginrt.ResourceLimits) *pluginRuntimeE2EEnv {
	t.Helper()

	env := newE2EEnv(t)
	db := env.db

	wasmDir := t.TempDir()
	store, err := pluginrt.NewStore(env.ctx, pluginrt.StoreConfig{Store: "local", WASMDir: wasmDir})
	if err != nil {
		t.Fatalf("create plugin store: %v", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	runtime := pluginrt.NewRuntime(store, pluginrt.HostServices{DB: db.DB, Log: log}, limits, log)

	authzStore := pgRepo.NewAuthzPermissionStore(db)
	pluginRepo := pgRepo.NewPluginRepository(db)
	pluginService := pluginsvc.New(pluginRepo)

	tm := jwttoken.New(e2eJWTSecret, e2eAccessTTL, e2eRefreshTTL)
	authorizer := authz.NewAuthorizer(authzStore)

	// WithRouteAuth wires the handler's own auth dependencies, used by
	// ProxyRequest's per-route middleware policy (e.g. a plugin route
	// declaring "authn" in its manifest) -- separate from the router-level
	// Authn middleware that only guards the /admin/plugins management routes.
	pluginHandler := handler.NewPluginHandler(pluginService, runtime, env.projectRepo).
		WithRouteAuth(tm, nil, authorizer)

	engine := router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authorizer,
		Health:       handler.NewHealthHandler(),
		Plugin:       pluginHandler,
		Log:          log,
	})

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	return &pluginRuntimeE2EEnv{
		base:          srv.URL,
		client:        client,
		env:           env,
		pluginService: pluginService,
		runtime:       runtime,
		wasmDir:       wasmDir,
	}
}

func (p *pluginRuntimeE2EEnv) issueAdminToken(t *testing.T) string {
	t.Helper()
	seedUser(t, p.env, "plugin-rt-admin", "Admin1234!", "Plugin Runtime Admin")
	admin, err := p.env.userRepo.FindByUsername(p.env.ctx, "plugin-rt-admin")
	if err != nil {
		t.Fatalf("find admin user: %v", err)
	}
	token, err := jwttoken.New(e2eJWTSecret, e2eAccessTTL, e2eRefreshTTL).
		IssueAccess(admin.ID.String(), admin.Username, "ADMIN", "fam-plugin-rt-admin", false)
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	return token
}

func (p *pluginRuntimeE2EEnv) issueUserToken(t *testing.T, username string) string {
	t.Helper()
	seedUser(t, p.env, username, "User1234!", "Plugin Runtime User")
	u, err := p.env.userRepo.FindByUsername(p.env.ctx, username)
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	token, err := jwttoken.New(e2eJWTSecret, e2eAccessTTL, e2eRefreshTTL).
		IssueAccess(u.ID.String(), u.Username, "USER", "fam-"+username, false)
	if err != nil {
		t.Fatalf("issue user token: %v", err)
	}
	return token
}

func (p *pluginRuntimeE2EEnv) doPlugin(t *testing.T, method, path, token string, body any) *http.Response {
	t.Helper()
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(context.Background(), method, p.base+path, jsonBody(t, body))
	} else {
		req, err = http.NewRequestWithContext(context.Background(), method, p.base+path, http.NoBody)
	}
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// doRawBody sends a request with a raw byte body instead of a JSON-encoded
// one, needed for the large-request tests where the body's exact size
// matters and must not be inflated/altered by JSON encoding.
func (p *pluginRuntimeE2EEnv) doRawBody(t *testing.T, method, path, token string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, p.base+path, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// installAndLoad installs pluginName via the real admin HTTP API (proving
// the install endpoint works end to end) with a manifest declaring the given
// routes, then -- since a plain install only registers a DB row and never
// touches the runtime (only InstallMarketplacePlugin/UpgradeMarketplacePlugin
// and the process-startup LoadAll do) -- explicitly loads it into the
// runtime, mirroring what happens for a real deployment after a restart.
func (p *pluginRuntimeE2EEnv) installAndLoad(t *testing.T, token, pluginName string, routes []map[string]any, enabled bool) string {
	t.Helper()

	wasmBytes, err := os.ReadFile(buildEchoPluginFixture(t))
	if err != nil {
		t.Fatalf("read echoplugin fixture: %v", err)
	}
	pluginDir := filepath.Join(p.wasmDir, pluginName)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir plugin wasm dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "backend.wasm"), wasmBytes, 0o644); err != nil {
		t.Fatalf("write plugin wasm: %v", err)
	}

	manifest := map[string]any{
		"id":          pluginName,
		"displayName": pluginName,
		"version":     "1.0.0",
		"backend": map[string]any{
			"wasm":   "backend.wasm",
			"routes": routes,
		},
	}
	payload := map[string]any{
		"name": pluginName, "version": "1.0.0", "manifest": manifest, "enabled": enabled,
	}
	resp := p.doPlugin(t, http.MethodPost, "/api/v1/admin/plugins", token, payload)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env envelope
	decodeJSON(t, resp, &env)
	data := assertDataMap(t, env)
	id, _ := data["id"].(string)

	plugins, err := p.pluginService.ListPlugins(p.env.ctx)
	if err != nil {
		t.Fatalf("list plugins: %v", err)
	}
	var found *plugindom.Plugin
	for _, pl := range plugins {
		if pl.Name == pluginName {
			found = pl
			break
		}
	}
	if found == nil {
		t.Fatalf("installed plugin %q not found via ListPlugins", pluginName)
	}

	if found.Enabled {
		if err := p.runtime.Load(p.env.ctx, *found); err != nil {
			t.Fatalf("load plugin %q into runtime: %v", pluginName, err)
		}
		t.Cleanup(func() { p.runtime.Unload(context.Background(), pluginName) })
	}

	return id
}

func publicRoute(method, path string) map[string]any {
	return map[string]any{"method": method, "path": path, "middlewares": []map[string]any{}}
}

func authnRoute(method, path string) map[string]any {
	return map[string]any{
		"method": method, "path": path,
		"middlewares": []map[string]any{{"name": "authn"}},
	}
}

// ---------------------------------------------------------------------------
// Install -> real load -> serve a request
// ---------------------------------------------------------------------------

func TestE2EPluginRuntime_InstallAndLoad_ServesRequest(t *testing.T) {
	t.Parallel()
	p := newPluginRuntimeE2EEnv(t, pluginrt.DefaultResourceLimits())
	token := p.issueAdminToken(t)

	p.installAndLoad(t, token, "com.paca.rt-install", []map[string]any{
		publicRoute(http.MethodGet, "/hello"),
	}, true)

	resp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins/com.paca.rt-install/hello", "", nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	var body map[string]string
	decodeJSON(t, resp, &body)
	if body["message"] != "hello from plugin" {
		t.Errorf("unexpected plugin response: %v", body)
	}
}

// ---------------------------------------------------------------------------
// API calling
// ---------------------------------------------------------------------------

func TestE2EPluginRuntime_APICall_EchoesRequestBody(t *testing.T) {
	t.Parallel()
	p := newPluginRuntimeE2EEnv(t, pluginrt.DefaultResourceLimits())
	token := p.issueAdminToken(t)

	p.installAndLoad(t, token, "com.paca.rt-echo", []map[string]any{
		publicRoute(http.MethodPost, "/echo"),
	}, true)

	payload := map[string]any{"hello": "world", "n": float64(42)}
	resp := p.doPlugin(t, http.MethodPost, "/api/v1/plugins/com.paca.rt-echo/echo", "", payload)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	var echoed map[string]any
	decodeJSON(t, resp, &echoed)
	if echoed["hello"] != "world" || echoed["n"] != float64(42) {
		t.Errorf("expected echoed body to match request, got %v", echoed)
	}
}

// TestE2EPluginRuntime_APICall_QueryParamsPropagate is a regression test for
// a bug where the host silently dropped the incoming request's URL query
// string instead of forwarding it to the plugin: PluginHandler.ProxyRequest
// built pluginrt.HTTPRequest without ever reading r.URL.Query(), so
// req.QueryParam(...) inside every plugin always saw an empty map regardless
// of what the caller sent. Query-string-driven filtering/pagination on any
// plugin route was silently a no-op until this was fixed.
func TestE2EPluginRuntime_APICall_QueryParamsPropagate(t *testing.T) {
	t.Parallel()
	p := newPluginRuntimeE2EEnv(t, pluginrt.DefaultResourceLimits())
	token := p.issueAdminToken(t)

	p.installAndLoad(t, token, "com.paca.rt-query", []map[string]any{
		publicRoute(http.MethodGet, "/query"),
	}, true)

	resp := p.doPlugin(t, http.MethodGet,
		"/api/v1/plugins/com.paca.rt-query/query?member_id=abc&date_from=2026-07-01", "", nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	var got map[string]string
	decodeJSON(t, resp, &got)
	if got["member_id"] != "abc" {
		t.Errorf("expected member_id=abc, got %v", got)
	}
	if got["date_from"] != "2026-07-01" {
		t.Errorf("expected date_from=2026-07-01, got %v", got)
	}
}

func TestE2EPluginRuntime_APICall_CallerIdentityPropagates(t *testing.T) {
	t.Parallel()
	p := newPluginRuntimeE2EEnv(t, pluginrt.DefaultResourceLimits())
	adminToken := p.issueAdminToken(t)

	p.installAndLoad(t, adminToken, "com.paca.rt-whoami", []map[string]any{
		authnRoute(http.MethodGet, "/whoami"),
	}, true)

	userToken := p.issueUserToken(t, "plugin-rt-whoami-user")
	resp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins/com.paca.rt-whoami/whoami", userToken, nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	var who map[string]string
	decodeJSON(t, resp, &who)
	if who["caller_role"] != "USER" {
		t.Errorf("expected caller_role USER, got %v", who)
	}
	if who["user_id"] == "" {
		t.Errorf("expected non-empty user_id, got %v", who)
	}
}

func TestE2EPluginRuntime_APICall_AuthnRoute_RequiresToken(t *testing.T) {
	t.Parallel()
	p := newPluginRuntimeE2EEnv(t, pluginrt.DefaultResourceLimits())
	token := p.issueAdminToken(t)

	p.installAndLoad(t, token, "com.paca.rt-authn", []map[string]any{
		authnRoute(http.MethodGet, "/whoami"),
	}, true)

	resp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins/com.paca.rt-authn/whoami", "", nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestE2EPluginRuntime_APICall_UnmatchedPath_PluginReturns404(t *testing.T) {
	t.Parallel()
	p := newPluginRuntimeE2EEnv(t, pluginrt.DefaultResourceLimits())
	token := p.issueAdminToken(t)

	p.installAndLoad(t, token, "com.paca.rt-nomatch", []map[string]any{
		publicRoute(http.MethodGet, "/hello"),
	}, true)

	resp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins/com.paca.rt-nomatch/does-not-exist", "", nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// Other cases: disabled / nonexistent plugin
// ---------------------------------------------------------------------------

func TestE2EPluginRuntime_DisabledPlugin_Returns404(t *testing.T) {
	t.Parallel()
	p := newPluginRuntimeE2EEnv(t, pluginrt.DefaultResourceLimits())
	token := p.issueAdminToken(t)

	p.installAndLoad(t, token, "com.paca.rt-disabled", []map[string]any{
		publicRoute(http.MethodGet, "/hello"),
	}, false) // enabled=false: never loaded into the runtime, same as production

	resp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins/com.paca.rt-disabled/hello", "", nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, resp, "PLUGIN_NOT_FOUND")
}

func TestE2EPluginRuntime_NonexistentPlugin_Returns404(t *testing.T) {
	t.Parallel()
	p := newPluginRuntimeE2EEnv(t, pluginrt.DefaultResourceLimits())

	resp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins/com.paca.does-not-exist/hello", "", nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, resp, "PLUGIN_NOT_FOUND")
}

// ---------------------------------------------------------------------------
// Large request body
// ---------------------------------------------------------------------------

// TestE2EPluginRuntime_OversizedBody_Returns413AndStaysHealthy proves the
// HTTP-edge defense end to end: a body over the configured limit is rejected
// with 413 before it ever reaches the plugin, and the plugin keeps serving
// normal requests afterward.
func TestE2EPluginRuntime_OversizedBody_Returns413AndStaysHealthy(t *testing.T) {
	t.Parallel()
	limits := pluginrt.DefaultResourceLimits()
	limits.MaxRequestBodyBytes = 1024
	p := newPluginRuntimeE2EEnv(t, limits)
	token := p.issueAdminToken(t)

	p.installAndLoad(t, token, "com.paca.rt-oversized", []map[string]any{
		publicRoute(http.MethodPost, "/echo"),
	}, true)

	oversized := make([]byte, 4096)
	resp := p.doRawBody(t, http.MethodPost, "/api/v1/plugins/com.paca.rt-oversized/echo", "", oversized)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusRequestEntityTooLarge)
	assertErrorCode(t, resp, "PAYLOAD_TOO_LARGE")

	// The plugin must still serve a normal request right after.
	resp2 := p.doPlugin(t, http.MethodPost, "/api/v1/plugins/com.paca.rt-oversized/echo", "", map[string]any{"ok": true})
	defer func() { _ = resp2.Body.Close() }()
	assertStatus(t, resp2, http.StatusOK)
}

// TestE2EPluginRuntime_AllocatorRecovery_AfterOversizedWrite reproduces the
// originally reported bug end to end: a request large enough to exceed the
// plugin module's actual WASM memory (not just the HTTP-level cap, which is
// disabled here) makes the host's write fail inside Runtime.HandleRequest.
// Before the fix, that failure was returned without ever resetting the
// plugin's bump allocator, permanently poisoning the instance -- every
// request after, even a tiny one, failed the same way. This test fails
// against the pre-fix runtime.go and passes against the fix.
func TestE2EPluginRuntime_AllocatorRecovery_AfterOversizedWrite(t *testing.T) {
	t.Parallel()
	limits := pluginrt.ResourceLimits{
		MaxCallDuration: 5 * time.Second,
		// The echoplugin's actual WASM memory is ~7.25 MiB (116 pages) once
		// loaded. 200 pages (12.5 MiB) is comfortably above that so the
		// module still loads, while leaving room for a request that
		// exceeds the module's real memory without exceeding this ceiling.
		MaxMemoryPages:      200,
		MaxRequestBodyBytes: 0, // disabled: this test targets the deeper wazero-level bounds check, not the HTTP-edge cap
	}
	p := newPluginRuntimeE2EEnv(t, limits)
	token := p.issueAdminToken(t)

	p.installAndLoad(t, token, "com.paca.rt-recovery", []map[string]any{
		publicRoute(http.MethodPost, "/echo"),
	}, true)

	// 20 MiB comfortably exceeds both the module's actual ~7.25 MiB memory
	// and the configured 12.5 MiB ceiling, so wazero's own bounds check
	// rejects the write regardless of which limit is binding.
	huge := make([]byte, 20*1024*1024)
	resp := p.doRawBody(t, http.MethodPost, "/api/v1/plugins/com.paca.rt-recovery/echo", "", huge)
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("expected the oversized write to fail, got 200")
	}

	for i := 0; i < 3; i++ {
		resp := p.doPlugin(t, http.MethodPost, "/api/v1/plugins/com.paca.rt-recovery/echo", "", map[string]any{"attempt": i})
		status := resp.StatusCode
		_ = resp.Body.Close()
		if status != http.StatusOK {
			t.Fatalf("request %d after the oversized write failed (instance still poisoned): got %d", i, status)
		}
	}
}
