package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
)

// PluginAssetsHandler streams the frontend, MCP and skills bundle files of
// installed plugins out of the plugin Store.
//
// In local mode the gateway serves these straight from the shared folders. In
// s3 mode the bucket stays private and the gateway cannot sign S3 requests, so
// it forwards /plugins/*, /plugins-mcp/* and /plugins-skills/* here instead
// (see deploy/caddy/Caddyfile). Only the three bundle kinds are reachable:
// backend.wasm, plugin.json and migrations never are.
type PluginAssetsHandler struct {
	store pluginrt.Store
}

// NewPluginAssetsHandler returns a PluginAssetsHandler.
func NewPluginAssetsHandler(store pluginrt.Store) *PluginAssetsHandler {
	return &PluginAssetsHandler{store: store}
}

// Serve handles GET /plugin-assets/{kind}/{name}/*.
//
// Range requests are intentionally not supported: bundle files are small
// scripts, styles and wasm that browsers fetch whole, and an S3 body is not
// seekable. Clients get the full body with 200.
func (h *PluginAssetsHandler) Serve(w http.ResponseWriter, r *http.Request) {
	var kind pluginrt.ArtifactKind
	switch chi.URLParam(r, "kind") {
	case "frontend":
		kind = pluginrt.KindFrontend
	case "mcp":
		kind = pluginrt.KindMCP
	case "skills":
		kind = pluginrt.KindSkills
	default:
		http.NotFound(w, r)
		return
	}
	rel := chi.URLParam(r, "*")
	// Caddy's file_server answers a directory URL with its index.html, so do the
	// same: /plugins/<id>/ (or any trailing-slash path) serves the bundle's
	// index.html in both modes. A bundle without one still 404s.
	if rel == "" || strings.HasSuffix(rel, "/") {
		rel += "index.html"
	}

	asset, err := h.store.OpenAsset(r.Context(), kind, chi.URLParam(r, "name"), rel)
	if err != nil {
		if errors.Is(err, pluginrt.ErrAssetNotFound) {
			http.NotFound(w, r)
			return
		}
		slog.Error("plugin assets: open failed", "kind", kind, "name", chi.URLParam(r, "name"), "path", rel, "error", err)
		http.Error(w, "plugin asset unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = asset.Body.Close() }()

	hdr := w.Header()
	if asset.ETag != "" {
		hdr.Set("ETag", asset.ETag)
	}
	// Mirror the gateway's local-mode caching: fingerprinted files are cached
	// for 15 minutes, but remoteEntry.js keeps a stable name while its
	// contents change on every build, so it must always be revalidated.
	if strings.HasSuffix(rel, "remoteEntry.js") {
		hdr.Set("Cache-Control", "no-cache")
	} else {
		hdr.Set("Cache-Control", "public, max-age=900, immutable")
	}
	if asset.ETag != "" && ifNoneMatchHit(r.Header.Get("If-None-Match"), asset.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hdr.Set("Content-Type", asset.ContentType)
	if !asset.ModTime.IsZero() {
		hdr.Set("Last-Modified", asset.ModTime.UTC().Format(http.TimeFormat))
	}
	if asset.Size >= 0 {
		hdr.Set("Content-Length", strconv.FormatInt(asset.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, asset.Body)
	}
}

// ifNoneMatchHit implements If-None-Match's weak comparison: the header may be
// "*" or a comma-separated list, and a W/ prefix (added when a proxy such as
// Caddy's gzip encoder weakens the validator) is ignored.
func ifNoneMatchHit(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	want := strings.TrimPrefix(etag, "W/")
	for _, cand := range strings.Split(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(cand), "W/") == want {
			return true
		}
	}
	return false
}
