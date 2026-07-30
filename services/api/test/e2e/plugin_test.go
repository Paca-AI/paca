package e2e_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	pluginsvc "github.com/Paca-AI/api/internal/service/plugin"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
)

// ---------------------------------------------------------------------------
// Plugin e2e environment
// ---------------------------------------------------------------------------

type pluginE2EEnv struct {
	base   string
	client *http.Client
	env    *e2eEnv
}

func newPluginE2EEnv(t *testing.T) *pluginE2EEnv {
	t.Helper()

	env := newE2EEnv(t)
	db := env.db

	authzStore := pgRepo.NewAuthzPermissionStore(db)
	pluginRepo := pgRepo.NewPluginRepository(db)
	pluginService := pluginsvc.New(pluginRepo)
	pluginHandler := handler.NewPluginHandler(pluginService, nil, env.projectRepo)

	tm := jwttoken.New(e2eJWTSecret, e2eAccessTTL, e2eRefreshTTL)

	engine := router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authz.NewAuthorizer(authzStore),
		Health:       handler.NewHealthHandler(),
		Plugin:       pluginHandler,
		Log:          slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

	return &pluginE2EEnv{
		base:   srv.URL,
		client: client,
		env:    env,
	}
}

// issueAdminToken mints a JWT for an admin user seeded in the base env.
func (p *pluginE2EEnv) issueAdminToken(t *testing.T) string {
	t.Helper()
	seedUser(t, p.env, "plugin-admin", "Admin1234!", "Plugin Admin")
	admin, err := p.env.userRepo.FindByUsername(p.env.ctx, "plugin-admin")
	if err != nil {
		t.Fatalf("find admin user: %v", err)
	}
	token, err := jwttoken.New(e2eJWTSecret, e2eAccessTTL, e2eRefreshTTL).
		IssueAccess(admin.ID.String(), admin.Username, "ADMIN", "fam-plugin-admin", false)
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}
	return token
}

// issueUserToken mints a JWT for a regular user seeded in the base env.
func (p *pluginE2EEnv) issueUserToken(t *testing.T) string {
	t.Helper()
	seedUser(t, p.env, "plugin-user", "User1234!", "Plugin User")
	u, err := p.env.userRepo.FindByUsername(p.env.ctx, "plugin-user")
	if err != nil {
		t.Fatalf("find user: %v", err)
	}
	token, err := jwttoken.New(e2eJWTSecret, e2eAccessTTL, e2eRefreshTTL).
		IssueAccess(u.ID.String(), u.Username, "USER", "fam-plugin-user", false)
	if err != nil {
		t.Fatalf("issue user token: %v", err)
	}
	return token
}

// doPlugin sends an authenticated request to the plugin test server.
func (p *pluginE2EEnv) doPlugin(t *testing.T, method, path, token string, body any) *http.Response {
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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func installPluginViaAPI(t *testing.T, p *pluginE2EEnv, token, name string) string {
	t.Helper()
	payload := map[string]any{
		"name":    name,
		"version": "1.0.0",
		"manifest": map[string]any{
			"id":          name,
			"displayName": name,
			"version":     "1.0.0",
		},
		"enabled": true,
	}
	resp := p.doPlugin(t, http.MethodPost, "/api/v1/admin/plugins", token, payload)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env envelope
	decodeJSON(t, resp, &env)
	data := assertDataMap(t, env)
	id, _ := data["id"].(string)
	return id
}

// ---------------------------------------------------------------------------
// GET /api/v1/plugins
// ---------------------------------------------------------------------------

func TestE2EPlugin_ListPlugins_AllowsAnonymous(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)

	resp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins", "", nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
}

func TestE2EPlugin_ListPlugins_EmptyInitially(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueUserToken(t)

	resp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins", token, nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	var env envelope
	decodeJSON(t, resp, &env)
	data := assertDataMap(t, env)
	plugins, _ := data["plugins"].([]any)
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins in fresh DB, got %d", len(plugins))
	}
}

// ---------------------------------------------------------------------------
// POST /api/v1/admin/plugins  (install)
// ---------------------------------------------------------------------------

func TestE2EPlugin_Install_RequiresAuth(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)

	payload := map[string]any{
		"name": "com.paca.test", "version": "1.0.0",
		"manifest": map[string]any{"id": "com.paca.test", "displayName": "Test", "version": "1.0.0"},
		"enabled":  true,
	}
	resp := p.doPlugin(t, http.MethodPost, "/api/v1/admin/plugins", "", payload)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestE2EPlugin_Install_Success(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueAdminToken(t)

	id := installPluginViaAPI(t, p, token, "com.paca.e2e-install")
	if id == "" {
		t.Fatal("expected non-empty plugin id after install")
	}

	// Verify it appears in the list.
	listResp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins", token, nil)
	defer func() { _ = listResp.Body.Close() }()
	assertStatus(t, listResp, http.StatusOK)
	var listEnv envelope
	decodeJSON(t, listResp, &listEnv)
	listData := assertDataMap(t, listEnv)
	plugins, _ := listData["plugins"].([]any)
	if len(plugins) != 1 {
		t.Errorf("expected 1 plugin after install, got %d", len(plugins))
	}
}

func TestE2EPlugin_Install_DuplicateName_409(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueAdminToken(t)

	installPluginViaAPI(t, p, token, "com.paca.dup-e2e")

	payload := map[string]any{
		"name": "com.paca.dup-e2e", "version": "2.0.0",
		"manifest": map[string]any{"id": "com.paca.dup-e2e", "displayName": "Dup", "version": "2.0.0"},
		"enabled":  true,
	}
	resp := p.doPlugin(t, http.MethodPost, "/api/v1/admin/plugins", token, payload)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusConflict)
	assertErrorCode(t, resp, "PLUGIN_NAME_TAKEN")
}

func TestE2EPlugin_Install_MissingFields_400(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueAdminToken(t)

	// Missing 'name' field.
	payload := map[string]any{
		"version":  "1.0.0",
		"manifest": map[string]any{"id": "com.paca.noname", "displayName": "No Name", "version": "1.0.0"},
	}
	resp := p.doPlugin(t, http.MethodPost, "/api/v1/admin/plugins", token, payload)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// PATCH /api/v1/admin/plugins/:pluginId (update)
// ---------------------------------------------------------------------------

func TestE2EPlugin_Update_Success(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueAdminToken(t)

	id := installPluginViaAPI(t, p, token, "com.paca.update-e2e")

	payload := map[string]any{"version": "2.0.0"}
	resp := p.doPlugin(t, http.MethodPatch, fmt.Sprintf("/api/v1/admin/plugins/%s", id), token, payload)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	var env envelope
	decodeJSON(t, resp, &env)
	data := assertDataMap(t, env)
	if data["version"] != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %v", data["version"])
	}
}

func TestE2EPlugin_Update_NotFound_404(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueAdminToken(t)

	resp := p.doPlugin(t, http.MethodPatch,
		"/api/v1/admin/plugins/00000000-0000-0000-0000-000000000001", token,
		map[string]any{"version": "9.9.9"})
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, resp, "PLUGIN_NOT_FOUND")
}

func TestE2EPlugin_Update_Disable(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueAdminToken(t)

	id := installPluginViaAPI(t, p, token, "com.paca.disable-e2e")

	disabled := false
	payload := map[string]any{"enabled": disabled}
	resp := p.doPlugin(t, http.MethodPatch, fmt.Sprintf("/api/v1/admin/plugins/%s", id), token, payload)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	var env envelope
	decodeJSON(t, resp, &env)
	data := assertDataMap(t, env)
	if enabled, _ := data["enabled"].(bool); enabled {
		t.Error("expected plugin to be disabled")
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/v1/admin/plugins/:pluginId
// ---------------------------------------------------------------------------

func TestE2EPlugin_Delete_Success(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueAdminToken(t)

	id := installPluginViaAPI(t, p, token, "com.paca.delete-e2e")

	resp := p.doPlugin(t, http.MethodDelete, fmt.Sprintf("/api/v1/admin/plugins/%s", id), token, nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusNoContent)

	// Verify it's gone from the list.
	listResp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins", token, nil)
	defer func() { _ = listResp.Body.Close() }()
	assertStatus(t, listResp, http.StatusOK)
	var listEnv envelope
	decodeJSON(t, listResp, &listEnv)
	listData := assertDataMap(t, listEnv)
	plugins, _ := listData["plugins"].([]any)
	if len(plugins) != 0 {
		t.Errorf("expected 0 plugins after delete, got %d", len(plugins))
	}
}

func TestE2EPlugin_Delete_NotFound_404(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueAdminToken(t)

	resp := p.doPlugin(t, http.MethodDelete,
		"/api/v1/admin/plugins/00000000-0000-0000-0000-000000000002", token, nil)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, resp, "PLUGIN_NOT_FOUND")
}

// ---------------------------------------------------------------------------
// PATCH /api/v1/admin/plugin-extension-settings
// ---------------------------------------------------------------------------

func TestE2EPlugin_UpdateExtensionSetting_Success(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	adminToken := p.issueAdminToken(t)

	pluginID := installPluginViaAPI(t, p, adminToken, "com.paca.pref-e2e")

	payload := map[string]any{
		"plugin_id":       pluginID,
		"extension_point": "task.detail.section",
		"settings":        map[string]any{"order": 2, "hidden": false},
	}
	resp := p.doPlugin(t, http.MethodPatch, "/api/v1/admin/plugin-extension-settings", adminToken, payload)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	var env envelope
	decodeJSON(t, resp, &env)
	data := assertDataMap(t, env)
	if data["extension_point"] != "task.detail.section" {
		t.Errorf("unexpected extension_point: %v", data["extension_point"])
	}
}

func TestE2EPlugin_UpdateExtensionSetting_RequiresAuth(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)

	resp := p.doPlugin(t, http.MethodPatch, "/api/v1/admin/plugin-extension-settings", "", map[string]any{})
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestE2EPlugin_UpdateExtensionSetting_RequiresAdmin(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	userToken := p.issueUserToken(t)

	resp := p.doPlugin(t, http.MethodPatch, "/api/v1/admin/plugin-extension-settings", userToken, map[string]any{})
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusForbidden)
}

// ---------------------------------------------------------------------------
// Full lifecycle: install → list → update → delete
// ---------------------------------------------------------------------------

func TestE2EPlugin_FullLifecycle(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueAdminToken(t)

	const pluginName = "com.paca.lifecycle-e2e"

	// 1. Install.
	id := installPluginViaAPI(t, p, token, pluginName)
	if id == "" {
		t.Fatal("install: expected non-empty plugin id")
	}

	// 2. List — should contain exactly 1.
	listResp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins", token, nil)
	defer func() { _ = listResp.Body.Close() }()
	assertStatus(t, listResp, http.StatusOK)
	var listEnv envelope
	decodeJSON(t, listResp, &listEnv)
	listData := assertDataMap(t, listEnv)
	plugins, _ := listData["plugins"].([]any)
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin after install, got %d", len(plugins))
	}

	// 3. Update version.
	updateResp := p.doPlugin(t, http.MethodPatch, fmt.Sprintf("/api/v1/admin/plugins/%s", id),
		token, map[string]any{"version": "1.1.0"})
	assertStatus(t, updateResp, http.StatusOK)
	var updateEnv envelope
	decodeJSON(t, updateResp, &updateEnv)
	updateData := assertDataMap(t, updateEnv)
	_ = updateResp.Body.Close()
	if updateData["version"] != "1.1.0" {
		t.Errorf("expected version 1.1.0, got %v", updateData["version"])
	}

	// 4. Save extension setting (admin-only).
	prefResp := p.doPlugin(t, http.MethodPatch, "/api/v1/admin/plugin-extension-settings", token,
		map[string]any{
			"plugin_id":       id,
			"extension_point": "sidebar.project.section",
			"settings":        map[string]any{"order": 1},
		})
	assertStatus(t, prefResp, http.StatusOK)
	_ = prefResp.Body.Close()

	// 5. Delete.
	delResp := p.doPlugin(t, http.MethodDelete, fmt.Sprintf("/api/v1/admin/plugins/%s", id), token, nil)
	assertStatus(t, delResp, http.StatusNoContent)
	_ = delResp.Body.Close()

	// 6. List — should be empty again.
	list2Resp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins", token, nil)
	defer func() { _ = list2Resp.Body.Close() }()
	assertStatus(t, list2Resp, http.StatusOK)
	var list2Env envelope
	decodeJSON(t, list2Resp, &list2Env)
	list2Data := assertDataMap(t, list2Env)
	plugins2, _ := list2Data["plugins"].([]any)
	if len(plugins2) != 0 {
		t.Errorf("expected empty plugin list after delete, got %d", len(plugins2))
	}
}

// ---------------------------------------------------------------------------
// Multi-plugin: list returns all installed plugins
// ---------------------------------------------------------------------------

func TestE2EPlugin_ListPlugins_Multiple(t *testing.T) {
	t.Parallel()
	p := newPluginE2EEnv(t)
	token := p.issueAdminToken(t)

	installPluginViaAPI(t, p, token, "com.paca.multi-1")
	installPluginViaAPI(t, p, token, "com.paca.multi-2")
	installPluginViaAPI(t, p, token, "com.paca.multi-3")

	listResp := p.doPlugin(t, http.MethodGet, "/api/v1/plugins", token, nil)
	defer func() { _ = listResp.Body.Close() }()
	assertStatus(t, listResp, http.StatusOK)

	var listEnv envelope
	decodeJSON(t, listResp, &listEnv)
	listData := assertDataMap(t, listEnv)
	plugins, _ := listData["plugins"].([]any)
	if len(plugins) != 3 {
		t.Errorf("expected 3 plugins, got %d", len(plugins))
	}
}
