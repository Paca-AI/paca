package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	pluginsvc "github.com/Paca-AI/api/internal/service/plugin"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Fake plugin repository (in-memory)
// ---------------------------------------------------------------------------

type fakeIntegPluginRepo struct {
	mu        sync.RWMutex
	plugins   map[uuid.UUID]*plugindom.Plugin
	nameIndex map[string]uuid.UUID
	settings  map[string]*plugindom.PluginExtensionSetting
}

func newFakeIntegPluginRepo() *fakeIntegPluginRepo {
	return &fakeIntegPluginRepo{
		plugins:   make(map[uuid.UUID]*plugindom.Plugin),
		nameIndex: make(map[string]uuid.UUID),
		settings:  make(map[string]*plugindom.PluginExtensionSetting),
	}
}

func (r *fakeIntegPluginRepo) List(_ context.Context) ([]*plugindom.Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*plugindom.Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (r *fakeIntegPluginRepo) FindByID(_ context.Context, id uuid.UUID) (*plugindom.Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[id]
	if !ok {
		return nil, plugindom.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (r *fakeIntegPluginRepo) FindByName(_ context.Context, name string) (*plugindom.Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.nameIndex[name]
	if !ok {
		return nil, plugindom.ErrNotFound
	}
	p := r.plugins[id]
	cp := *p
	return &cp, nil
}

func (r *fakeIntegPluginRepo) FindByCapability(_ context.Context, capability string) ([]*plugindom.Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*plugindom.Plugin, 0)
	for _, p := range r.plugins {
		if !p.Enabled {
			continue
		}
		for _, cap := range p.Manifest.Capabilities {
			if cap == capability {
				cp := *p
				out = append(out, &cp)
				break
			}
		}
	}
	return out, nil
}

func (r *fakeIntegPluginRepo) Create(_ context.Context, p *plugindom.Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.nameIndex[p.Name]; exists {
		return plugindom.ErrNameTaken
	}
	cp := *p
	r.plugins[p.ID] = &cp
	r.nameIndex[p.Name] = p.ID
	return nil
}

func (r *fakeIntegPluginRepo) Update(_ context.Context, p *plugindom.Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.plugins[p.ID]; !ok {
		return plugindom.ErrNotFound
	}
	cp := *p
	r.plugins[p.ID] = &cp
	return nil
}

func (r *fakeIntegPluginRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plugins[id]
	if !ok {
		return plugindom.ErrNotFound
	}
	delete(r.nameIndex, p.Name)
	delete(r.plugins, id)
	return nil
}

func (r *fakeIntegPluginRepo) ListSettings(_ context.Context, pluginID uuid.UUID) ([]*plugindom.PluginExtensionSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*plugindom.PluginExtensionSetting, 0)
	for _, s := range r.settings {
		if s.PluginID == pluginID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeIntegPluginRepo) ListSettingsForPlugins(_ context.Context, pluginIDs []uuid.UUID) ([]*plugindom.PluginExtensionSetting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(pluginIDs) == 0 {
		return []*plugindom.PluginExtensionSetting{}, nil
	}

	idSet := make(map[uuid.UUID]struct{}, len(pluginIDs))
	for _, id := range pluginIDs {
		idSet[id] = struct{}{}
	}

	out := make([]*plugindom.PluginExtensionSetting, 0)
	for _, s := range r.settings {
		if _, ok := idSet[s.PluginID]; ok {
			cp := *s
			out = append(out, &cp)
		}
	}

	return out, nil
}

func (r *fakeIntegPluginRepo) UpsertSetting(_ context.Context, setting *plugindom.PluginExtensionSetting) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := setting.PluginID.String() + ":" + setting.ExtensionPoint
	cp := *setting
	r.settings[key] = &cp
	return nil
}

// ---------------------------------------------------------------------------
// Router builder for plugin integration tests
// ---------------------------------------------------------------------------

type pluginTestEnv struct {
	srv    *httptest.Server
	tm     *jwttoken.Manager
	repo   *fakeIntegPluginRepo
	svc    *pluginsvc.Service
	userID uuid.UUID // seeded user
	token  string    // valid JWT for seeded user
}

func newPluginTestEnv(t *testing.T, adminPerms bool) *pluginTestEnv {
	t.Helper()

	repo := newFakeIntegPluginRepo()
	svc := pluginsvc.New(repo)

	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	userRepo := newFakeUserRepo()
	store := &fakeRefreshStore{}
	authService := authsvc.New(userRepo, tm, store, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo, userRepo)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Seed a user so we can generate a valid JWT.
	userID := uuid.New()
	perms := []authz.Permission{authz.PermissionProjectsRead}
	if adminPerms {
		perms = append(perms, authz.PermissionUsersWrite)
	}
	permStore := &integrationPermissionStore{globalPerms: perms}

	pluginHandler := handler.NewPluginHandler(svc, nil, nil)

	r := router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authz.NewAuthorizer(permStore),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, testCookieCfg),
		User:         handler.NewUserHandler(userService),
		GlobalRole:   handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}, authz.NewAuthorizer(permStore)),
		Plugin:       pluginHandler,
		Log:          log,
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	// Generate a signed JWT for userID.
	token, err := tm.IssueAccess(userID.String(), "testuser", "USER", uuid.NewString(), false)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}

	return &pluginTestEnv{
		srv:    srv,
		tm:     tm,
		repo:   repo,
		svc:    svc,
		userID: userID,
		token:  token,
	}
}

func (e *pluginTestEnv) url(path string) string {
	return e.srv.URL + path
}

func (e *pluginTestEnv) do(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
	}
	var req *http.Request
	var err error
	if bodyBytes != nil {
		req, err = http.NewRequestWithContext(context.Background(), method, e.url(path), bytes.NewReader(bodyBytes))
	} else {
		req, err = http.NewRequestWithContext(context.Background(), method, e.url(path), http.NoBody)
	}
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func decodePluginEnvelope(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var env map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return env
}

func seedPluginViaRepo(t *testing.T, repo *fakeIntegPluginRepo, name string, enabled bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_ = repo.Create(context.Background(), &plugindom.Plugin{
		ID:      id,
		Name:    name,
		Version: "1.0.0",
		Manifest: plugindom.PluginManifest{
			ID:          name,
			DisplayName: name,
			Version:     "1.0.0",
		},
		Enabled:     enabled,
		InstalledAt: time.Now(),
		UpdatedAt:   time.Now(),
	})
	return id
}

// ---------------------------------------------------------------------------
// GET /api/v1/plugins
// ---------------------------------------------------------------------------

func TestIntegPlugin_ListPlugins_Empty(t *testing.T) {
	env := newPluginTestEnv(t, false)

	resp := env.do(t, http.MethodGet, "/api/v1/plugins", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodePluginEnvelope(t, resp)
	data, ok := body["data"]
	if !ok {
		t.Fatal("expected 'data' key in response")
	}
	dataMap, _ := data.(map[string]any)
	items, _ := dataMap["plugins"].([]any)
	if len(items) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(items))
	}
}

func TestIntegPlugin_ListPlugins_ReturnsSeeded(t *testing.T) {
	env := newPluginTestEnv(t, false)
	seedPluginViaRepo(t, env.repo, "com.paca.checklist", true)
	seedPluginViaRepo(t, env.repo, "com.paca.bdd", true)

	resp := env.do(t, http.MethodGet, "/api/v1/plugins", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := decodePluginEnvelope(t, resp)
	dataMap, _ := body["data"].(map[string]any)
	data, _ := dataMap["plugins"].([]any)
	if len(data) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(data))
	}
}

func TestIntegPlugin_ListPlugins_AllowsAnonymous(t *testing.T) {
	env := newPluginTestEnv(t, false)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, env.url("/api/v1/plugins"), http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// POST /api/v1/admin/plugins
// ---------------------------------------------------------------------------

func TestIntegPlugin_InstallPlugin_RequiresAdminPerm(t *testing.T) {
	env := newPluginTestEnv(t, false) // no admin perms

	payload := map[string]any{
		"name":    "com.paca.test",
		"version": "1.0.0",
		"manifest": map[string]any{
			"id": "com.paca.test", "displayName": "Test", "version": "1.0.0",
		},
		"enabled": true,
	}
	resp := env.do(t, http.MethodPost, "/api/v1/admin/plugins", payload)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestIntegPlugin_InstallPlugin_Success(t *testing.T) {
	env := newPluginTestEnv(t, true) // admin perms

	payload := map[string]any{
		"name":    "com.paca.install-test",
		"version": "1.0.0",
		"manifest": map[string]any{
			"id": "com.paca.install-test", "displayName": "Install Test", "version": "1.0.0",
		},
		"enabled": true,
	}
	resp := env.do(t, http.MethodPost, "/api/v1/admin/plugins", payload)
	defer func() { _ = resp.Body.Close() }()
	body := decodePluginEnvelope(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %v", resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["name"] != "com.paca.install-test" {
		t.Errorf("expected name %q, got %v", "com.paca.install-test", data["name"])
	}
}

func TestIntegPlugin_InstallPlugin_DuplicateName_Conflict(t *testing.T) {
	env := newPluginTestEnv(t, true)
	seedPluginViaRepo(t, env.repo, "com.paca.dup", true)

	payload := map[string]any{
		"name":    "com.paca.dup",
		"version": "1.0.0",
		"manifest": map[string]any{
			"id": "com.paca.dup", "displayName": "Dup", "version": "1.0.0",
		},
		"enabled": true,
	}
	resp := env.do(t, http.MethodPost, "/api/v1/admin/plugins", payload)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", resp.StatusCode)
	}
}

func TestIntegPlugin_InstallPlugin_BadRequest_MissingName(t *testing.T) {
	env := newPluginTestEnv(t, true)

	payload := map[string]any{
		"version": "1.0.0",
		"manifest": map[string]any{
			"id": "com.paca.noname", "displayName": "No Name", "version": "1.0.0",
		},
	}
	resp := env.do(t, http.MethodPost, "/api/v1/admin/plugins", payload)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PATCH /api/v1/admin/plugins/:pluginId
// ---------------------------------------------------------------------------

func TestIntegPlugin_UpdatePlugin_Success(t *testing.T) {
	env := newPluginTestEnv(t, true)
	id := seedPluginViaRepo(t, env.repo, "com.paca.upd", true)

	payload := map[string]any{"version": "2.0.0"}
	resp := env.do(t, http.MethodPatch, fmt.Sprintf("/api/v1/admin/plugins/%s", id), payload)
	defer func() { _ = resp.Body.Close() }()
	body := decodePluginEnvelope(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %v", resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["version"] != "2.0.0" {
		t.Errorf("expected version 2.0.0, got %v", data["version"])
	}
}

func TestIntegPlugin_UpdatePlugin_NotFound(t *testing.T) {
	env := newPluginTestEnv(t, true)

	payload := map[string]any{"version": "2.0.0"}
	resp := env.do(t, http.MethodPatch, fmt.Sprintf("/api/v1/admin/plugins/%s", uuid.New()), payload)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestIntegPlugin_UpdatePlugin_InvalidID(t *testing.T) {
	env := newPluginTestEnv(t, true)

	payload := map[string]any{"version": "2.0.0"}
	resp := env.do(t, http.MethodPatch, "/api/v1/admin/plugins/not-a-uuid", payload)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/v1/admin/plugins/:pluginId
// ---------------------------------------------------------------------------

func TestIntegPlugin_DeletePlugin_Success(t *testing.T) {
	env := newPluginTestEnv(t, true)
	id := seedPluginViaRepo(t, env.repo, "com.paca.del", true)

	resp := env.do(t, http.MethodDelete, fmt.Sprintf("/api/v1/admin/plugins/%s", id), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	// Verify it's gone.
	listResp := env.do(t, http.MethodGet, "/api/v1/plugins", nil)
	defer func() { _ = listResp.Body.Close() }()
	listBody := decodePluginEnvelope(t, listResp)
	dataMap, _ := listBody["data"].(map[string]any)
	data, _ := dataMap["plugins"].([]any)
	if len(data) != 0 {
		t.Errorf("expected empty list after delete, got %d plugins", len(data))
	}
}

func TestIntegPlugin_DeletePlugin_NotFound(t *testing.T) {
	env := newPluginTestEnv(t, true)

	resp := env.do(t, http.MethodDelete, fmt.Sprintf("/api/v1/admin/plugins/%s", uuid.New()), nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PATCH /api/v1/admin/plugin-extension-settings
// ---------------------------------------------------------------------------

func TestIntegPlugin_UpdateExtensionSetting_Success(t *testing.T) {
	env := newPluginTestEnv(t, true) // admin perms required
	pluginID := seedPluginViaRepo(t, env.repo, "com.paca.pref-integ", true)

	payload := map[string]any{
		"plugin_id":       pluginID.String(),
		"extension_point": "task.detail.section",
		"settings":        map[string]any{"order": 1, "hidden": false},
	}
	resp := env.do(t, http.MethodPatch, "/api/v1/admin/plugin-extension-settings", payload)
	defer func() { _ = resp.Body.Close() }()
	body := decodePluginEnvelope(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %v", resp.StatusCode, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["extension_point"] != "task.detail.section" {
		t.Errorf("unexpected extension_point: %v", data["extension_point"])
	}
}

func TestIntegPlugin_UpdateExtensionSetting_BadBody(t *testing.T) {
	env := newPluginTestEnv(t, true)

	// Malformed JSON body.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPatch, env.url("/api/v1/admin/plugin-extension-settings"),
		bytes.NewBufferString(`{invalid}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestIntegPlugin_UpdateExtensionSetting_RequiresAuth(t *testing.T) {
	env := newPluginTestEnv(t, true)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPatch, env.url("/api/v1/admin/plugin-extension-settings"), http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestIntegPlugin_UpdateExtensionSetting_RequiresAdmin(t *testing.T) {
	env := newPluginTestEnv(t, false) // no admin perms

	payload := map[string]any{
		"plugin_id":       uuid.New().String(),
		"extension_point": "task.detail.section",
		"settings":        map[string]any{"order": 1, "hidden": false},
	}
	resp := env.do(t, http.MethodPatch, "/api/v1/admin/plugin-extension-settings", payload)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}
