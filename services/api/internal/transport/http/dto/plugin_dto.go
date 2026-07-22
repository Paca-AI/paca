package dto

import (
	"time"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	pluginrt "github.com/Paca-AI/api/internal/platform/plugin"
	"github.com/google/uuid"
)

// -------------------------------------------------------------------------
// Plugin responses
// -------------------------------------------------------------------------

// PluginResponse is the JSON representation of an installed plugin.
type PluginResponse struct {
	ID                uuid.UUID                        `json:"id"`
	Name              string                           `json:"name"`
	Version           string                           `json:"version"`
	Manifest          plugindom.PluginManifest         `json:"manifest"`
	ExtensionSettings []PluginExtensionSettingResponse `json:"extension_settings,omitempty"`
	Enabled           bool                             `json:"enabled"`
	InstalledAt       time.Time                        `json:"installed_at"`
	UpdatedAt         time.Time                        `json:"updated_at"`
}

// PluginResponseFromEntity maps a domain Plugin to its DTO.
func PluginResponseFromEntity(p *plugindom.Plugin) PluginResponse {
	return PluginResponseFromEntityWithSettings(p, nil)
}

// PluginResponseFromEntityWithSettings maps a domain Plugin and its extension
// settings to a DTO payload.
func PluginResponseFromEntityWithSettings(
	p *plugindom.Plugin,
	settings []*plugindom.PluginExtensionSetting,
) PluginResponse {
	settingDTOs := make([]PluginExtensionSettingResponse, 0, len(settings))
	for _, s := range settings {
		settingDTOs = append(settingDTOs, PluginExtensionSettingFromEntity(s))
	}

	return PluginResponse{
		ID:                p.ID,
		Name:              p.Name,
		Version:           p.Version,
		Manifest:          p.Manifest,
		ExtensionSettings: settingDTOs,
		Enabled:           p.Enabled,
		InstalledAt:       p.InstalledAt,
		UpdatedAt:         p.UpdatedAt,
	}
}

// PluginListResponse is the JSON wrapper for a list of plugins.
type PluginListResponse struct {
	Plugins []PluginResponse `json:"plugins"`
}

// PluginListResponseFromEntities maps a slice of plugins to the list DTO.
func PluginListResponseFromEntities(plugins []*plugindom.Plugin) PluginListResponse {
	items := make([]PluginResponse, len(plugins))
	for i, p := range plugins {
		items[i] = PluginResponseFromEntity(p)
	}
	return PluginListResponse{Plugins: items}
}

// -------------------------------------------------------------------------
// Plugin install request (POST /admin/plugins)
// -------------------------------------------------------------------------

// InstallPluginRequest is the JSON body for installing a plugin.
type InstallPluginRequest struct {
	// Name is the reverse-DNS plugin identifier, e.g. "com.paca.checklist".
	Name string `json:"name" binding:"required"`
	// Version is the semver version string, e.g. "1.0.0".
	Version string `json:"version" binding:"required"`
	// Manifest is the full plugin.json content.
	Manifest plugindom.PluginManifest `json:"manifest" binding:"required"`
	// Enabled controls whether the plugin is active immediately after install.
	Enabled bool `json:"enabled"`
}

// -------------------------------------------------------------------------
// Plugin update request (PATCH /admin/plugins/:pluginId)
// -------------------------------------------------------------------------

// UpdatePluginRequest is the JSON body for patching a plugin.
// All fields are optional; omitted fields leave the existing value unchanged.
type UpdatePluginRequest struct {
	Version  *string                   `json:"version"`
	Manifest *plugindom.PluginManifest `json:"manifest"`
	Enabled  *bool                     `json:"enabled"`
}

// -------------------------------------------------------------------------
// Plugin preference
// -------------------------------------------------------------------------

// PluginExtensionSettingResponse is the JSON shape of a plugin_extension_settings row.
type PluginExtensionSettingResponse struct {
	ID             uuid.UUID                      `json:"id"`
	PluginID       uuid.UUID                      `json:"plugin_id"`
	ExtensionPoint string                         `json:"extension_point"`
	Settings       plugindom.ExtensionSettingData `json:"settings"`
	UpdatedAt      time.Time                      `json:"updated_at"`
}

// PluginExtensionSettingFromEntity maps a domain PluginExtensionSetting to its DTO.
func PluginExtensionSettingFromEntity(s *plugindom.PluginExtensionSetting) PluginExtensionSettingResponse {
	return PluginExtensionSettingResponse{
		ID:             s.ID,
		PluginID:       s.PluginID,
		ExtensionPoint: s.ExtensionPoint,
		Settings:       s.Settings,
		UpdatedAt:      s.UpdatedAt,
	}
}

// UpdatePluginExtensionSettingRequest is the JSON body for
// PATCH /admin/plugin-extension-settings.
type UpdatePluginExtensionSettingRequest struct {
	PluginID       uuid.UUID                      `json:"plugin_id" binding:"required"`
	ExtensionPoint string                         `json:"extension_point" binding:"required"`
	Settings       plugindom.ExtensionSettingData `json:"settings"`
}

// -------------------------------------------------------------------------
// Marketplace
// -------------------------------------------------------------------------

// MarketplacePluginResponse is one plugin listing item from the marketplace catalog.
type MarketplacePluginResponse struct {
	Name          string                            `json:"name"`
	DisplayName   string                            `json:"display_name"`
	Description   string                            `json:"description"`
	Version       string                            `json:"version"`
	AvatarURL     string                            `json:"avatar_url,omitempty"`
	RepositoryURL string                            `json:"repository_url,omitempty"`
	Artifacts     MarketplacePluginArtifactResponse `json:"artifacts"`
}

// MarketplacePluginArtifactResponse contains downloadable tar.gz URLs.
type MarketplacePluginArtifactResponse struct {
	BackendTarGzURL    string `json:"backend_tar_gz_url,omitempty"`
	FrontendTarGzURL   string `json:"frontend_tar_gz_url,omitempty"`
	MigrationsTarGzURL string `json:"migrations_tar_gz_url,omitempty"`
	ManifestTarGzURL   string `json:"manifest_tar_gz_url"`
	MCPTarGzURL        string `json:"mcp_tar_gz_url,omitempty"`
	SkillsTarGzURL     string `json:"skills_tar_gz_url,omitempty"`
}

// MarketplacePluginListResponse wraps marketplace plugin list payload.
type MarketplacePluginListResponse struct {
	Plugins []MarketplacePluginResponse `json:"plugins"`
}

// MarketplacePluginListResponseFromCatalog maps the marketplace catalog model to DTO.
func MarketplacePluginListResponseFromCatalog(catalog *pluginrt.MarketplaceCatalog) MarketplacePluginListResponse {
	items := make([]MarketplacePluginResponse, 0, len(catalog.Plugins))
	for _, p := range catalog.Plugins {
		items = append(items, MarketplacePluginResponse{
			Name:          p.Name,
			DisplayName:   p.DisplayName,
			Description:   p.Description,
			Version:       p.Version,
			AvatarURL:     p.AvatarURL,
			RepositoryURL: p.RepositoryURL,
			Artifacts: MarketplacePluginArtifactResponse{
				BackendTarGzURL:    p.Artifacts.BackendTarGzURL,
				FrontendTarGzURL:   p.Artifacts.FrontendTarGzURL,
				MigrationsTarGzURL: p.Artifacts.MigrationsTarGzURL,
				ManifestTarGzURL:   p.Artifacts.ManifestTarGzURL,
				MCPTarGzURL:        p.Artifacts.MCPTarGzURL,
				SkillsTarGzURL:     p.Artifacts.SkillsTarGzURL,
			},
		})
	}
	return MarketplacePluginListResponse{Plugins: items}
}

// InstallMarketplacePluginRequest is the JSON body for marketplace installation.
type InstallMarketplacePluginRequest struct {
	Name    string `json:"name" binding:"required"`
	Enabled *bool  `json:"enabled,omitempty"`
}
