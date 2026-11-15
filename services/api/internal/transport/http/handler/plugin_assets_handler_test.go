package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

func TestPluginAssetsHandler_Serve(t *testing.T) {
	root := t.TempDir()
	store, err := pluginrt.NewStore(context.Background(), pluginrt.StoreConfig{
		Store: "local", WASMDir: filepath.Join(root, "b"), FrontendDir: filepath.Join(root, "f"),
		MCPDir: filepath.Join(root, "m"), SkillsDir: filepath.Join(root, "s"),
	})
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{"assets/remoteEntry.js": "entry", "assets/chunk-abc.js": "chunk", "index.html": "home"} {
		if err := os.WriteFile(filepath.Join(src, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.PutTree(context.Background(), pluginrt.KindFrontend, "com.paca.x", src); err != nil {
		t.Fatal(err)
	}
	// A backend file that must never be reachable.
	if err := os.MkdirAll(filepath.Join(root, "b", "com.paca.x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b", "com.paca.x", "backend.wasm"), []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	r.Get("/plugin-assets/{kind}/{name}/*", handler.NewPluginAssetsHandler(store).Serve)

	get := func(path string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := get("/plugin-assets/frontend/com.paca.x/assets/chunk-abc.js", nil)
	if w.Code != http.StatusOK || w.Body.String() != "chunk" {
		t.Fatalf("chunk: %d %q", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=900, immutable" {
		t.Errorf("chunk Cache-Control = %q", cc)
	}

	w = get("/plugin-assets/frontend/com.paca.x/assets/remoteEntry.js", nil)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("remoteEntry: %d Cache-Control=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	if etag := w.Header().Get("ETag"); etag == "" {
		t.Error("expected an ETag")
	} else if w2 := get("/plugin-assets/frontend/com.paca.x/assets/remoteEntry.js", map[string]string{"If-None-Match": etag}); w2.Code != http.StatusNotModified {
		t.Errorf("conditional GET = %d, want 304", w2.Code)
	}

	// A directory URL serves its index.html, like the gateway's file_server.
	for _, p := range []string{"/plugin-assets/frontend/com.paca.x/", "/plugin-assets/frontend/com.paca.x/index.html"} {
		if w := get(p, nil); w.Code != http.StatusOK || w.Body.String() != "home" {
			t.Errorf("GET %s = %d %q, want 200 home", p, w.Code, w.Body.String())
		}
	}
	// ...and a directory without one still 404s.
	if w := get("/plugin-assets/frontend/com.paca.x/assets/", nil); w.Code != http.StatusNotFound {
		t.Errorf("GET assets/ = %d, want 404", w.Code)
	}

	for _, p := range []string{
		"/plugin-assets/frontend/com.paca.x/missing.js",
		"/plugin-assets/backend/com.paca.x/backend.wasm",
		"/plugin-assets/frontend/com.paca.x/../backend.wasm",
		"/plugin-assets/mcp/com.paca.x/mcp.js",
	} {
		if w := get(p, nil); w.Code != http.StatusNotFound && w.Code != http.StatusMovedPermanently {
			t.Errorf("GET %s = %d, want 404", p, w.Code)
		}
	}
}
