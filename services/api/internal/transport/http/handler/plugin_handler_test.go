package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// ---------------------------------------------------------------------------
// Minimal fake
// ---------------------------------------------------------------------------

type mockPluginSvc struct {
	install                func(ctx context.Context, in plugindom.InstallInput) (*plugindom.Plugin, error)
	updateExtensionSetting func(ctx context.Context, in plugindom.UpdateExtensionSettingInput) (*plugindom.PluginExtensionSetting, error)
	listPlugins            func(ctx context.Context) ([]*plugindom.Plugin, error)
}

func (m *mockPluginSvc) ListPlugins(ctx context.Context) ([]*plugindom.Plugin, error) {
	if m.listPlugins != nil {
		return m.listPlugins(ctx)
	}
	return nil, nil
}
func (m *mockPluginSvc) InstallPlugin(ctx context.Context, in plugindom.InstallInput) (*plugindom.Plugin, error) {
	if m.install != nil {
		return m.install(ctx, in)
	}
	return nil, errors.New("mock: install not configured")
}
func (m *mockPluginSvc) UpdatePlugin(_ context.Context, _ uuid.UUID, _ plugindom.UpdateInput) (*plugindom.Plugin, error) {
	return nil, errors.New("not found")
}
func (m *mockPluginSvc) DeletePlugin(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockPluginSvc) UpdateExtensionSetting(ctx context.Context, in plugindom.UpdateExtensionSettingInput) (*plugindom.PluginExtensionSetting, error) {
	if m.updateExtensionSetting != nil {
		return m.updateExtensionSetting(ctx, in)
	}
	return nil, errors.New("mock: updateExtensionSetting not configured")
}
func (m *mockPluginSvc) ListExtensionSettings(_ context.Context, _ uuid.UUID) ([]*plugindom.PluginExtensionSetting, error) {
	return nil, nil
}
func (m *mockPluginSvc) ListExtensionSettingsForPlugins(_ context.Context, _ []uuid.UUID) (map[uuid.UUID][]*plugindom.PluginExtensionSetting, error) {
	return nil, nil
}

var _ plugindom.Service = (*mockPluginSvc)(nil)

// ---------------------------------------------------------------------------
// Router helper
// ---------------------------------------------------------------------------

func newPluginValidationRouter(svc plugindom.Service) chi.Router {
	h := handler.NewPluginHandler(svc, nil, nil)
	r := chi.NewRouter()
	r.Post("/admin/plugins", h.InstallPlugin)
	r.Patch("/admin/plugin-extension-settings", h.UpdateExtensionSetting)
	return r
}

func doPluginRequest(t *testing.T, r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestInstallPlugin_MissingName_Returns400(t *testing.T) {
	r := newPluginValidationRouter(&mockPluginSvc{})

	w := doPluginRequest(t, r, http.MethodPost, "/admin/plugins",
		map[string]any{"version": "1.0.0"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing plugin name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestInstallPlugin_MissingVersion_Returns400(t *testing.T) {
	r := newPluginValidationRouter(&mockPluginSvc{})

	w := doPluginRequest(t, r, http.MethodPost, "/admin/plugins",
		map[string]any{"name": "my-plugin", "version": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing version, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateExtensionSetting_MissingPluginID_Returns400(t *testing.T) {
	r := newPluginValidationRouter(&mockPluginSvc{})

	// plugin_id absent → decodes to uuid.Nil → handler returns 400
	w := doPluginRequest(t, r, http.MethodPatch, "/admin/plugin-extension-settings",
		map[string]any{"extension_point": "sidebar.item"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing plugin_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateExtensionSetting_MissingExtensionPoint_Returns400(t *testing.T) {
	r := newPluginValidationRouter(&mockPluginSvc{})

	w := doPluginRequest(t, r, http.MethodPatch, "/admin/plugin-extension-settings",
		map[string]any{"plugin_id": uuid.New(), "extension_point": ""})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing extension_point, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ProxyRequest body-size limit
// ---------------------------------------------------------------------------

// newPluginProxyRouter mounts ProxyRequest the same way router.go does, on a
// real *pluginrt.Runtime so its configured MaxRequestBodyBytes is enforced.
// The runtime is never asked to Load a plugin: a request that is rejected
// for being oversized must never reach Runtime.HandleRequest, so no WASM
// module is needed to exercise that path.
func newPluginProxyRouter(svc plugindom.Service, rt *pluginrt.Runtime) chi.Router {
	h := handler.NewPluginHandler(svc, rt, nil)
	r := chi.NewRouter()
	r.Handle("/plugins/{pluginId}/*", http.HandlerFunc(h.ProxyRequest))
	return r
}

func TestProxyRequest_OversizedBody_Returns413(t *testing.T) {
	svc := &mockPluginSvc{
		listPlugins: func(_ context.Context) ([]*plugindom.Plugin, error) {
			return []*plugindom.Plugin{{
				Name:    "test.plugin",
				Enabled: true,
				Manifest: plugindom.PluginManifest{
					Backend: &plugindom.BackendManifest{
						Routes: []plugindom.PluginRoute{
							// Explicit empty Middlewares: skip host auth so the
							// test can focus solely on the body-size check.
							{Method: http.MethodPost, Path: "/echo", Middlewares: []plugindom.PluginRouteMiddleware{}},
						},
					},
				},
			}}, nil
		},
	}
	rt := pluginrt.NewRuntime(nil, pluginrt.HostServices{}, pluginrt.ResourceLimits{
		MaxCallDuration:     time.Second,
		MaxRequestBodyBytes: 16,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := newPluginProxyRouter(svc, rt)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/plugins/test.plugin/echo", bytes.NewReader(make([]byte, 1024)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProxyRequest_BodyWithinLimit_PassesSizeCheck(t *testing.T) {
	svc := &mockPluginSvc{
		listPlugins: func(_ context.Context) ([]*plugindom.Plugin, error) {
			return []*plugindom.Plugin{{
				Name:    "test.plugin",
				Enabled: true,
				Manifest: plugindom.PluginManifest{
					Backend: &plugindom.BackendManifest{
						Routes: []plugindom.PluginRoute{
							{Method: http.MethodPost, Path: "/echo", Middlewares: []plugindom.PluginRouteMiddleware{}},
						},
					},
				},
			}}, nil
		},
	}
	rt := pluginrt.NewRuntime(nil, pluginrt.HostServices{}, pluginrt.ResourceLimits{
		MaxCallDuration:     time.Second,
		MaxRequestBodyBytes: 16,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	r := newPluginProxyRouter(svc, rt)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost,
		"/plugins/test.plugin/echo", bytes.NewReader(make([]byte, 4)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// The body itself is within the limit, so the request must fail past the
	// size check -- on plugin dispatch (no WASM module loaded), not on 413.
	if w.Code == http.StatusRequestEntityTooLarge {
		t.Fatalf("expected the size check to pass for a body within the limit, got 413: %s", w.Body.String())
	}
}
