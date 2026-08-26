package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Paca-AI/api/internal/apierr"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz"
	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// PluginHandler handles plugin management endpoints.
type PluginHandler struct {
	svc             plugindom.Service
	runtime         *pluginrt.Runtime
	memberRepo      projectdom.MemberRepository
	tokenManager    *jwttoken.Manager
	apiKeyAuth      middleware.APIKeyAuthenticator
	authorizer      *authz.Authorizer
	marketplace     *pluginrt.MarketplaceClient
	installer       *pluginrt.Installer
	migrationRunner *pluginrt.MigrationRunner
}

// NewPluginHandler creates a PluginHandler.
func NewPluginHandler(svc plugindom.Service, runtime *pluginrt.Runtime, memberRepo projectdom.MemberRepository) *PluginHandler {
	return &PluginHandler{svc: svc, runtime: runtime, memberRepo: memberRepo}
}

// WithRouteAuth wires authentication/authorization dependencies used for
// per-route plugin middleware policy enforcement.
func (h *PluginHandler) WithRouteAuth(
	tm *jwttoken.Manager,
	apiKeyAuth middleware.APIKeyAuthenticator,
	authorizer *authz.Authorizer,
) *PluginHandler {
	h.tokenManager = tm
	h.apiKeyAuth = apiKeyAuth
	h.authorizer = authorizer
	return h
}

// WithMarketplace wires marketplace dependencies onto the existing handler.
func (h *PluginHandler) WithMarketplace(
	marketplace *pluginrt.MarketplaceClient,
	installer *pluginrt.Installer,
	migrationRunner *pluginrt.MigrationRunner,
) *PluginHandler {
	h.marketplace = marketplace
	h.installer = installer
	h.migrationRunner = migrationRunner
	return h
}

// -------------------------------------------------------------------------
// PLUG-BE-10: Plugin management API
// -------------------------------------------------------------------------

// ListPlugins handles GET /api/v1/plugins.
func (h *PluginHandler) ListPlugins(w http.ResponseWriter, r *http.Request) {
	plugins, err := h.svc.ListPlugins(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	pluginIDs := make([]uuid.UUID, 0, len(plugins))
	for _, p := range plugins {
		pluginIDs = append(pluginIDs, p.ID)
	}

	settingsByPlugin, err := h.svc.ListExtensionSettingsForPlugins(r.Context(), pluginIDs)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	items := make([]dto.PluginResponse, 0, len(plugins))
	for _, p := range plugins {
		items = append(items, dto.PluginResponseFromEntityWithSettings(p, settingsByPlugin[p.ID]))
	}
	presenter.OK(w, r, dto.PluginListResponse{Plugins: items})
}

// InstallPlugin handles POST /api/v1/admin/plugins.
func (h *PluginHandler) InstallPlugin(w http.ResponseWriter, r *http.Request) {
	var req dto.InstallPluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}
	if req.Name == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "plugin name is required"))
		return
	}
	if req.Version == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "version is required"))
		return
	}
	plugin, err := h.svc.InstallPlugin(r.Context(), plugindom.InstallInput{
		Name:     req.Name,
		Version:  req.Version,
		Manifest: req.Manifest,
		Enabled:  req.Enabled,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	onFailure := func() { _ = h.svc.DeletePlugin(r.Context(), plugin.ID) }
	if !h.runMigrationsAndLoad(w, r, plugin, onFailure) {
		return
	}

	presenter.Created(w, r, dto.PluginResponseFromEntity(plugin))
}

// runMigrationsAndLoad runs any pending migrations for plugin and, if it is
// enabled, loads it into the WASM runtime. On failure it invokes onFailure
// (used by callers to roll back whatever they just persisted), writes the
// error response, and returns false; returns true on success.
func (h *PluginHandler) runMigrationsAndLoad(w http.ResponseWriter, r *http.Request, plugin *plugindom.Plugin, onFailure func()) bool {
	if h.migrationRunner != nil {
		if err := h.migrationRunner.Run(r.Context(), plugin.Name); err != nil {
			onFailure()
			presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to run plugin migrations: "+err.Error()))
			return false
		}
	}

	if plugin.Enabled && h.runtime != nil {
		if err := h.runtime.Load(r.Context(), *plugin); err != nil {
			onFailure()
			presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to load plugin runtime: "+err.Error()))
			return false
		}
	}

	return true
}

// ListMarketplacePlugins handles GET /api/v1/admin/plugins/marketplace.
func (h *PluginHandler) ListMarketplacePlugins(w http.ResponseWriter, r *http.Request) {
	if h.marketplace == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "marketplace not configured"))
		return
	}

	catalog, err := h.marketplace.List(r.Context())
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to fetch marketplace catalog"))
		return
	}

	presenter.OK(w, r, dto.MarketplacePluginListResponseFromCatalog(catalog))
}

// InstallMarketplacePlugin handles POST /api/v1/admin/plugins/marketplace/install.
func (h *PluginHandler) InstallMarketplacePlugin(w http.ResponseWriter, r *http.Request) {
	if h.marketplace == nil || h.installer == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "marketplace installer not configured"))
		return
	}

	var req dto.InstallMarketplacePluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}

	entry, err := h.marketplace.FindPlugin(r.Context(), req.Name)
	if err != nil {
		if err == pluginrt.ErrMarketplacePluginNotFound {
			presenter.Error(w, r, apierr.New(apierr.CodePluginNotFound, "plugin not found in marketplace"))
			return
		}
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "failed to resolve marketplace plugin: "+err.Error()))
		return
	}

	manifest, err := h.installer.Install(r.Context(), *entry, h.svc.CheckHostCompatibility)
	if err != nil {
		presenter.Error(w, r, installArtifactsError(err))
		return
	}

	// Ensure downloaded artifacts are cleaned up if any subsequent step fails.
	pluginName := entry.Name
	success := false
	defer func() {
		if !success {
			if uninstallErr := h.installer.Uninstall(pluginName); uninstallErr != nil {
				slog.Error("failed to clean up plugin artifacts after install failure", "name", pluginName, "error", uninstallErr)
			}
		}
	}()

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	pl, err := h.svc.InstallPlugin(r.Context(), plugindom.InstallInput{
		Name:     entry.Name,
		Version:  entry.Version,
		Manifest: manifest,
		Enabled:  enabled,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	onFailure := func() { _ = h.svc.DeletePlugin(r.Context(), pl.ID) }
	if !h.runMigrationsAndLoad(w, r, pl, onFailure) {
		return
	}

	success = true
	presenter.Created(w, r, dto.PluginResponseFromEntity(pl))
}

// UpdatePlugin handles PATCH /api/v1/admin/plugins/:pluginId.
func (h *PluginHandler) UpdatePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := parsePluginID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdatePluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}

	// Capture the pre-update state so we can roll the DB record back if
	// migrations or the runtime reload fail below.
	plugins, err := h.svc.ListPlugins(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var previous *plugindom.Plugin
	for _, p := range plugins {
		if p.ID == id {
			previous = p
			break
		}
	}
	if previous == nil {
		presenter.Error(w, r, apierr.New(apierr.CodePluginNotFound, "plugin not found"))
		return
	}

	plugin, err := h.svc.UpdatePlugin(r.Context(), id, plugindom.UpdateInput{
		Version:  req.Version,
		Manifest: req.Manifest,
		Enabled:  req.Enabled,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	rollback := func(reason, msg string) {
		if _, rollbackErr := h.svc.UpdatePlugin(r.Context(), id, plugindom.UpdateInput{
			Version:  &previous.Version,
			Manifest: &previous.Manifest,
			Enabled:  &previous.Enabled,
		}); rollbackErr != nil {
			slog.Error("plugin update: failed to roll back DB record", "name", previous.Name, "reason", reason, "error", rollbackErr)
		}
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, msg))
	}

	if h.migrationRunner != nil {
		if err := h.migrationRunner.Run(r.Context(), plugin.Name); err != nil {
			rollback("migration failure", "failed to run plugin migrations: "+err.Error())
			return
		}
	}

	if h.runtime != nil {
		if plugin.Enabled {
			if err := h.runtime.Load(r.Context(), *plugin); err != nil {
				// Best-effort: restore the previously-loaded version/state in the
				// runtime so it doesn't keep serving the now-rolled-back plugin.
				if previous.Enabled {
					if rollbackErr := h.runtime.Load(r.Context(), *previous); rollbackErr != nil {
						slog.Error("plugin update: failed to roll back runtime", "name", previous.Name, "error", rollbackErr)
					}
				} else {
					h.runtime.Unload(r.Context(), previous.Name)
				}
				rollback("runtime reload failure", "failed to reload plugin runtime: "+err.Error())
				return
			}
		} else {
			h.runtime.Unload(r.Context(), plugin.Name)
		}
	}

	presenter.OK(w, r, dto.PluginResponseFromEntity(plugin))
}

// UpgradeMarketplacePlugin handles POST /api/v1/admin/plugins/:pluginId/upgrade.
// It fetches the latest version from the marketplace catalog, validates that the
// marketplace version is strictly newer than the installed version and that the
// host satisfies the new manifest's minCoreVersion, downloads and replaces all
// artifacts, runs any new migrations, updates the DB record, and reloads the
// plugin in the WASM runtime.
func (h *PluginHandler) UpgradeMarketplacePlugin(w http.ResponseWriter, r *http.Request) {
	if h.marketplace == nil || h.installer == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "marketplace installer not configured"))
		return
	}

	id, err := parsePluginID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Resolve the installed plugin record.
	plugins, err := h.svc.ListPlugins(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var installed *plugindom.Plugin
	for _, p := range plugins {
		if p.ID == id {
			installed = p
			break
		}
	}
	if installed == nil {
		presenter.Error(w, r, apierr.New(apierr.CodePluginNotFound, "plugin not found"))
		return
	}

	// Look up the marketplace entry by the plugin's reverse-DNS name.
	entry, err := h.marketplace.FindPlugin(r.Context(), installed.Name)
	if err != nil {
		if err == pluginrt.ErrMarketplacePluginNotFound {
			presenter.Error(w, r, apierr.New(apierr.CodePluginNotFound, "plugin not found in marketplace"))
			return
		}
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to resolve marketplace plugin: "+err.Error()))
		return
	}

	// Enforce semver ordering: refuse no-ops and downgrades.
	cmp, err := plugindom.CompareSemver(entry.Version, installed.Version)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "version comparison failed: "+err.Error()))
		return
	}
	switch {
	case cmp == 0:
		presenter.Error(w, r, apierr.New(apierr.CodePluginAlreadyUpToDate, "plugin is already up to date"))
		return
	case cmp < 0:
		presenter.Error(w, r, apierr.New(apierr.CodePluginDowngradeNotAllowed, "marketplace version is older than installed version"))
		return
	}

	// Download and overwrite existing artifacts. CheckHostCompatibility runs
	// inside Install, before any of the currently-installed plugin's live
	// files are touched, so an incompatible upgrade leaves them untouched
	// entirely rather than requiring cleanup after the fact.
	manifest, err := h.installer.Install(r.Context(), *entry, h.svc.CheckHostCompatibility)
	if err != nil {
		presenter.Error(w, r, installArtifactsError(err))
		return
	}
	if manifest.Version != entry.Version {
		presenter.Error(w, r, apierr.New(
			apierr.CodeBadRequest,
			fmt.Sprintf(
				"downloaded plugin manifest version %q does not match marketplace version %q",
				manifest.Version,
				entry.Version,
			),
		))
		return
	}

	// Capture the currently persisted plugin state so we can delay DB writes
	// until the upgraded runtime is active and best-effort roll back on failure.
	plugins, err = h.svc.ListPlugins(r.Context())
	if err != nil {
		if h.installer != nil {
			if cleanupErr := h.installer.Uninstall(installed.Name); cleanupErr != nil {
				slog.Error("plugin upgrade: failed to clean up installed artifacts after state lookup failure", "name", installed.Name, "error", cleanupErr)
			}
		}
		presenter.Error(w, r, err)
		return
	}

	var current *plugindom.Plugin
	for i := range plugins {
		if plugins[i].ID == id {
			current = plugins[i]
			break
		}
	}
	if current == nil {
		if h.installer != nil {
			if cleanupErr := h.installer.Uninstall(installed.Name); cleanupErr != nil {
				slog.Error("plugin upgrade: failed to clean up installed artifacts after missing plugin state", "name", installed.Name, "error", cleanupErr)
			}
		}
		presenter.Error(w, r, apierr.New(apierr.CodePluginNotFound, "plugin not found"))
		return
	}

	cleanupArtifacts := func(reason string) {
		if h.installer == nil {
			return
		}
		if cleanupErr := h.installer.Uninstall(installed.Name); cleanupErr != nil {
			slog.Error("plugin upgrade: failed to clean up installed artifacts", "name", installed.Name, "reason", reason, "error", cleanupErr)
		}
	}

	// Run any new migrations introduced by the upgraded version (idempotent).
	if h.migrationRunner != nil {
		if err := h.migrationRunner.Run(r.Context(), installed.Name); err != nil {
			cleanupArtifacts("migration failure")
			presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to run plugin migrations: "+err.Error()))
			return
		}
	}

	newVersion := entry.Version

	// Reload the WASM runtime with the new binary before persisting the new
	// version so the DB only advances once the upgraded module is actually live.
	runtimePlugin := *current
	runtimePlugin.Version = newVersion
	runtimePlugin.Manifest = manifest
	if runtimePlugin.Enabled && h.runtime != nil {
		if err := h.runtime.Load(r.Context(), runtimePlugin); err != nil {
			cleanupArtifacts("runtime reload failure")
			slog.Error("plugin upgrade: failed to reload runtime", "name", runtimePlugin.Name, "error", err)
			presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "artifacts upgraded but runtime reload failed: "+err.Error()))
			return
		}
	}

	// Persist the new version and manifest only after migrations and runtime
	// reload succeed.
	updated, err := h.svc.UpdatePlugin(r.Context(), id, plugindom.UpdateInput{
		Version:  &newVersion,
		Manifest: &manifest,
	})
	if err != nil {
		if current.Enabled && h.runtime != nil {
			if rollbackErr := h.runtime.Load(r.Context(), *current); rollbackErr != nil {
				slog.Error("plugin upgrade: failed to roll back runtime after DB update failure", "name", current.Name, "error", rollbackErr)
			}
		}
		cleanupArtifacts("db update failure")
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, dto.PluginResponseFromEntity(updated))
}

// DeletePlugin handles DELETE /api/v1/admin/plugins/:pluginId.
func (h *PluginHandler) DeletePlugin(w http.ResponseWriter, r *http.Request) {
	id, err := parsePluginID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var pluginName string
	if h.runtime != nil || h.installer != nil {
		plugins, err := h.svc.ListPlugins(r.Context())
		if err != nil {
			presenter.Error(w, r, err)
			return
		}
		for _, p := range plugins {
			if p.ID == id {
				pluginName = p.Name
				break
			}
		}
	}

	if err := h.svc.DeletePlugin(r.Context(), id); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if h.runtime != nil && pluginName != "" {
		h.runtime.Unload(r.Context(), pluginName)
	}
	if h.installer != nil && pluginName != "" {
		if err := h.installer.Uninstall(pluginName); err != nil {
			// Log but don't fail the request — the DB record is already gone.
			slog.Error("failed to uninstall plugin artifacts", "name", pluginName, "error", err)
		}
	}
	presenter.NoContent(w)
}

// -------------------------------------------------------------------------
// PLUG-BE-11: Plugin extension setting endpoint (admin-only)
// -------------------------------------------------------------------------

// UpdateExtensionSetting handles PATCH /api/v1/admin/plugin-extension-settings.
// Only the super admin may call this endpoint.
func (h *PluginHandler) UpdateExtensionSetting(w http.ResponseWriter, r *http.Request) {
	var req dto.UpdatePluginExtensionSettingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, err.Error()))
		return
	}
	if req.PluginID == uuid.Nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "plugin_id is required"))
		return
	}
	if req.ExtensionPoint == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "extension_point is required"))
		return
	}

	setting, err := h.svc.UpdateExtensionSetting(r.Context(), plugindom.UpdateExtensionSettingInput{
		PluginID:       req.PluginID,
		ExtensionPoint: req.ExtensionPoint,
		Settings:       req.Settings,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.PluginExtensionSettingFromEntity(setting))
}

// -------------------------------------------------------------------------
// PLUG-BE-08: Plugin route proxy
// -------------------------------------------------------------------------

// ProxyRequest handles any request under
// /api/v1/plugins/:pluginId/* and dispatches it to the
// matching plugin's HandleRequest WASM export.
func (h *PluginHandler) ProxyRequest(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "plugin runtime not available"))
		return
	}

	pluginID := chi.URLParam(r, "pluginId")

	// Validate that the plugin exists and is enabled.
	plugin, err := h.svc.ListPlugins(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var found *plugindom.Plugin
	for _, p := range plugin {
		if p.Name == pluginID && p.Enabled {
			found = p
			break
		}
	}
	if found == nil {
		presenter.Error(w, r, apierr.New(apierr.CodePluginNotFound, "plugin not found or disabled"))
		return
	}

	// The full sub-path after /plugins/:pluginId/ is captured by the wildcard param.
	// Plugins may embed /projects/:projectId/ within this path at their own discretion.
	subPath := chi.URLParam(r, "*")
	if subPath == "" {
		subPath = "/"
	}

	route, pathParams := matchPluginRoute(found.Manifest.Backend.Routes, r.Method, subPath)
	var mwOK bool
	r, mwOK = h.applyPluginRouteMiddlewares(w, r, route, pathParams)
	if !mwOK {
		return
	}

	// Determine whether the route requires project-member resolution and which
	// path-param holds the project UUID (configurable via manifest; defaults to
	// "projectId" when not specified).
	resolveProjectMember, projectParamName := projectMemberParam(route)

	// Build caller identity from JWT claims.
	claims := middleware.ClaimsFrom(r)
	callerID := ""
	userIDStr := ""
	callerRole := ""
	if claims != nil {
		callerRole = claims.Role
		userIDStr = claims.Subject

		// Resolve project member only when the route's manifest explicitly declares
		// project-scoped permission enforcement.  Plugin routes that don't carry a
		// requirePermissions(scope=project) middleware won't incur a DB lookup, and
		// plugins that choose a non-standard path structure are not forced to embed
		// :projectId at any particular position.
		if resolveProjectMember {
			if projectIDStr := pathParams[projectParamName]; projectIDStr != "" {
				if h.memberRepo == nil {
					presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "plugin member resolver not available"))
					return
				}
				projectID, err := uuid.Parse(projectIDStr)
				if err != nil {
					presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid projectId in path"))
					return
				}
				userID, err := uuid.Parse(claims.Subject)
				if err != nil {
					presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
					return
				}
				member, err := h.memberRepo.FindMemberByUserProject(r.Context(), userID, projectID)
				if err != nil {
					// API-key-authenticated callers (e.g. the agent bot user) may hold
					// SUPER_ADMIN global permissions without being a project member.
					// The requirePermissions check above already passed, so proceed with
					// an empty callerID rather than returning PROJECT_MEMBER_NOT_FOUND.
					if !middleware.IsAPIKeyAuth(r) {
						presenter.Error(w, r, err)
						return
					}
				} else {
					callerID = member.ID.String()
				}
			}
		}
	}

	// Build flattened headers map (first value per header name).
	headers := make(map[string]string, len(r.Header))
	for k, vs := range r.Header {
		if len(vs) > 0 {
			headers[k] = vs[0]
		}
	}

	// Build flattened query-param map (first value per key), same convention
	// as headers above.
	rawQuery := r.URL.Query()
	query := make(map[string]string, len(rawQuery))
	for k, vs := range rawQuery {
		if len(vs) > 0 {
			query[k] = vs[0]
		}
	}

	// Measure the envelope's exact overhead for this request (everything but
	// the body) before reading the body, so the size cap accounts for the
	// real headers/query/path instead of a guessed constant — those vary a
	// lot in practice (large cookies, forwarded headers, long query strings).
	envelopeProbe := &pluginrt.HTTPRequest{
		Method:     r.Method,
		Path:       subPath,
		Query:      query,
		ProjectID:  pathParams[projectParamName],
		CallerID:   callerID,
		UserID:     userIDStr,
		CallerRole: callerRole,
		Headers:    headers,
	}
	probeBytes, err := json.Marshal(envelopeProbe)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to serialise request"))
		return
	}

	// Read request body, capped so an oversized body can never reach the
	// plugin's WASM memory (see Runtime.HandleRequest for why that matters).
	// MaxHTTPBodyBytes (not MaxRequestBodyBytes) is used here because the raw
	// body is base64-encoded and wrapped in the JSON envelope above before it
	// reaches the runtime's own size check — using the same limit for both
	// would let a body the HTTP layer accepts still get rejected downstream.
	maxBody := h.runtime.MaxHTTPBodyBytes(int64(len(probeBytes)))
	if maxBody < 0 {
		presenter.Error(w, r, apierr.New(apierr.CodePayloadTooLarge,
			"request headers/path/query leave no room for a body under the plugin payload limit"))
		return
	}
	if maxBody > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			presenter.Error(w, r, apierr.New(apierr.CodePayloadTooLarge,
				fmt.Sprintf("request body exceeds the %d byte limit", maxBytesErr.Limit)))
			return
		}
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "failed to read request body"))
		return
	}

	req := &pluginrt.HTTPRequest{
		Method:     r.Method,
		Path:       subPath,
		Query:      query,
		ProjectID:  pathParams[projectParamName],
		CallerID:   callerID,
		UserID:     userIDStr,
		CallerRole: callerRole,
		Headers:    headers,
		Body:       bodyBytes,
	}

	// Attach request to context for HTTP host functions.
	reqCtx := pluginrt.WithPluginRequest(r.Context(), req)

	reqBytes, err := json.Marshal(req)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "failed to serialise request"))
		return
	}

	respBytes, err := h.runtime.HandleRequest(reqCtx, pluginID, reqBytes)
	if err != nil {
		if errors.Is(err, pluginrt.ErrPayloadTooLarge) {
			presenter.Error(w, r, apierr.New(apierr.CodePayloadTooLarge, "request payload too large"))
			return
		}
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "plugin execution error: "+err.Error()))
		return
	}

	// Parse the plugin response envelope. The current SDK returns:
	// {"status": number, "headers": object, "body": base64-bytes}
	var pluginResp struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    []byte            `json:"body"`
	}
	if err := json.Unmarshal(respBytes, &pluginResp); err != nil {
		// Fallback: send raw bytes as JSON.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(respBytes)
		return
	}

	statusCode := pluginResp.Status
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	contentType := ""
	if pluginResp.Headers != nil {
		contentType = pluginResp.Headers["Content-Type"]
		if contentType == "" {
			contentType = pluginResp.Headers["content-type"]
		}
	}
	if contentType == "" {
		contentType = "application/json"
	}

	for k, v := range pluginResp.Headers {
		if !strings.EqualFold(k, "Content-Type") {
			w.Header().Set(k, v)
		}
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)
	_, _ = w.Write(pluginResp.Body)
}

func (h *PluginHandler) applyPluginRouteMiddlewares(w http.ResponseWriter, r *http.Request, route *plugindom.PluginRoute, pathParams map[string]string) (*http.Request, bool) {
	for _, mw := range h.routeMiddlewares(route) {
		name := strings.ToLower(strings.TrimSpace(mw.Name))
		switch name {
		case "authn":
			if h.tokenManager == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "plugin route auth middleware is not configured"))
				return r, false
			}
			var ok bool
			r, ok = middleware.EnforceAuthn(w, r, h.tokenManager, h.apiKeyAuth)
			if !ok {
				return r, false
			}
		case "optionalauthn":
			if h.tokenManager == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "plugin route auth middleware is not configured"))
				return r, false
			}
			var ok bool
			r, ok = middleware.EnforceOptionalAuthn(w, r, h.tokenManager, h.apiKeyAuth)
			if !ok {
				return r, false
			}
		case "requirefreshpassword":
			var ok bool
			r, ok = middleware.EnforceFreshPassword(w, r)
			if !ok {
				return r, false
			}
		case "requirejwtauth":
			if !middleware.EnforceJWTAuth(w, r) {
				return r, false
			}
		case "requirepermissions":
			if h.authorizer == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "plugin route authorization middleware is not configured"))
				return r, false
			}
			if len(mw.Permissions) == 0 {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "plugin route requirePermissions requires at least one permission"))
				return r, false
			}
			scopeResolver := middleware.GlobalScope()
			scope := strings.ToLower(strings.TrimSpace(mw.Scope))
			switch scope {
			case "", "global":
				// keep global scope
			case "project":
				projectParam := strings.TrimSpace(mw.ProjectParam)
				if projectParam == "" {
					projectParam = "projectId"
				}
				pParam := projectParam
				scopeResolver = func(_ *http.Request) (*uuid.UUID, error) {
					v := pathParams[pParam]
					if v == "" {
						return nil, nil
					}
					id, err := uuid.Parse(v)
					if err != nil {
						return nil, err
					}
					return &id, nil
				}
			default:
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "plugin route requirePermissions has invalid scope"))
				return r, false
			}

			perms := make([]authz.Permission, 0, len(mw.Permissions))
			for _, p := range mw.Permissions {
				perms = append(perms, authz.Permission(p))
			}
			if !middleware.EnforcePermissions(w, r, h.authorizer, scopeResolver, perms...) {
				return r, false
			}
		default:
			presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "plugin route uses unsupported middleware: "+mw.Name))
			return r, false
		}
	}

	return r, true
}

func (h *PluginHandler) routeMiddlewares(route *plugindom.PluginRoute) []plugindom.PluginRouteMiddleware {
	if route != nil {
		// Distinguish omitted (nil) vs explicit empty list:
		// nil -> apply default policy, [] -> no middlewares.
		if route.Middlewares != nil {
			return route.Middlewares
		}
		if route.Public {
			return nil
		}
	}

	// Default policy for plugin routes: optional authn + fresh-password check.
	// Project-scoped permission checks must be declared explicitly in the
	// plugin route's middlewares list.
	return []plugindom.PluginRouteMiddleware{
		{Name: "optionalAuthn"},
		{Name: "requireFreshPassword"},
	}
}

func matchPluginRoute(routes []plugindom.PluginRoute, method, path string) (*plugindom.PluginRoute, map[string]string) {
	method = strings.ToUpper(strings.TrimSpace(method))
	var matched *plugindom.PluginRoute
	var matchedParams map[string]string
	var matchedScore routePatternScore
	for i := range routes {
		r := &routes[i]
		if strings.ToUpper(strings.TrimSpace(r.Method)) != method {
			continue
		}
		if params, ok := matchPathPattern(r.Path, path); ok {
			score := scoreRoutePattern(r.Path)
			// Prefer more specific patterns (fewer wildcards, then more static segments).
			// Ties keep manifest order for deterministic fallback behavior.
			if matched == nil || score.moreSpecificThan(matchedScore) {
				matched = r
				matchedScore = score
				matchedParams = params
			}
		}
	}
	if matchedParams == nil {
		matchedParams = make(map[string]string)
	}
	return matched, matchedParams
}

type routePatternScore struct {
	staticSegments   int
	paramSegments    int
	wildcardSegments int
}

// scoreRoutePattern computes path-pattern specificity by counting static,
// param, and wildcard segments.
func scoreRoutePattern(pattern string) routePatternScore {
	var score routePatternScore
	for _, segment := range splitPathSegments(pattern) {
		switch {
		case strings.HasPrefix(segment, "*"):
			score.wildcardSegments++
		case strings.HasPrefix(segment, ":"):
			score.paramSegments++
		default:
			score.staticSegments++
		}
	}
	return score
}

// moreSpecificThan orders routes by specificity:
// fewer wildcards, then more static segments, then fewer param segments.
func (score routePatternScore) moreSpecificThan(other routePatternScore) bool {
	if score.wildcardSegments != other.wildcardSegments {
		return score.wildcardSegments < other.wildcardSegments
	}
	if score.staticSegments != other.staticSegments {
		return score.staticSegments > other.staticSegments
	}
	if score.paramSegments != other.paramSegments {
		return score.paramSegments < other.paramSegments
	}
	return false
}

func matchPathPattern(pattern, path string) (map[string]string, bool) {
	patternSegments := splitPathSegments(pattern)
	pathSegments := splitPathSegments(path)
	params := make(map[string]string)
	pathIdx := 0
	for i := range patternSegments {
		patternSegment := patternSegments[i]

		if strings.HasPrefix(patternSegment, "*") {
			name := strings.TrimPrefix(patternSegment, "*")
			// Catch-all wildcards are only supported as the last path segment.
			if name == "" || i != len(patternSegments)-1 {
				// Empty wildcard names are invalid ("/items/*" has no param key).
				return nil, false
			}
			if pathIdx >= len(pathSegments) {
				return nil, false
			}
			params[name] = strings.Join(pathSegments[pathIdx:], "/")
			return params, true
		}

		if pathIdx >= len(pathSegments) {
			return nil, false
		}
		pathSegment := pathSegments[pathIdx]

		if strings.HasPrefix(patternSegment, ":") {
			name := strings.TrimPrefix(patternSegment, ":")
			if name == "" {
				return nil, false
			}
			params[name] = pathSegment
			pathIdx++
			continue
		}

		if patternSegment != pathSegment {
			return nil, false
		}
		pathIdx++
	}

	if pathIdx != len(pathSegments) {
		return nil, false
	}

	return params, true
}

// projectMemberParam returns whether the route manifest requires project-member
// resolution and, if so, which path-param name holds the project UUID.
// The param name is taken from the first requirePermissions(scope=project)
// middleware's ProjectParam field, defaulting to "projectId" when not set.
func projectMemberParam(route *plugindom.PluginRoute) (bool, string) {
	if route == nil {
		return false, ""
	}
	for _, mw := range route.Middlewares {
		if strings.ToLower(strings.TrimSpace(mw.Name)) == "requirepermissions" &&
			strings.ToLower(strings.TrimSpace(mw.Scope)) == "project" {
			param := strings.TrimSpace(mw.ProjectParam)
			if param == "" {
				param = "projectId"
			}
			return true, param
		}
	}
	return false, ""
}

func splitPathSegments(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

func parsePluginID(r *http.Request) (uuid.UUID, error) {
	raw := chi.URLParam(r, "pluginId")
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid pluginId: "+raw)
	}
	return id, nil
}

// installArtifactsError maps an error from Installer.Install to a response
// error. A *apierr.Error (from the checkManifest callback, e.g.
// PLUGIN_INCOMPATIBLE_HOST_VERSION with its structured Details) is passed
// through unchanged so the client still gets the specific code/details;
// anything else (download/extract/parse failures) is a generic bad request.
func installArtifactsError(err error) error {
	var apiErr *apierr.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return apierr.New(apierr.CodeBadRequest, "failed to install plugin artifacts: "+err.Error())
}
