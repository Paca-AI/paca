package plugindom

import (
	"context"

	"github.com/google/uuid"
)

// InstallInput is the input for installing (registering) a new plugin.
type InstallInput struct {
	Name     string
	Version  string
	Manifest PluginManifest
	Enabled  bool
}

// UpdateInput is the input for patching an existing plugin registration.
// All pointer fields are optional; nil means "leave unchanged".
type UpdateInput struct {
	Version  *string
	Manifest *PluginManifest
	Enabled  *bool
}

// UpdateExtensionSettingInput is the input for upserting a system-wide
// extension point setting.  Only the super admin may call this.
type UpdateExtensionSettingInput struct {
	PluginID       uuid.UUID
	ExtensionPoint string
	Settings       ExtensionSettingData
}

// Service defines the application-level operations on the plugin domain.
type Service interface {
	// ListPlugins returns all installed plugins.
	ListPlugins(ctx context.Context) ([]*Plugin, error)

	// InstallPlugin registers a new plugin.
	InstallPlugin(ctx context.Context, input InstallInput) (*Plugin, error)

	// UpdatePlugin patches an existing plugin registration.
	UpdatePlugin(ctx context.Context, id uuid.UUID, input UpdateInput) (*Plugin, error)

	// CheckHostCompatibility reports whether the running host version
	// satisfies manifest's declared MinCoreVersion, without persisting
	// anything. Callers that install/update a plugin outside a single
	// InstallPlugin/UpdatePlugin call (e.g. a marketplace upgrade that
	// downloads artifacts and runs migrations before persisting) must call
	// this explicitly and early, before any of those side effects, so an
	// incompatible upgrade never becomes live even briefly.
	CheckHostCompatibility(manifest PluginManifest) error

	// DeletePlugin removes a plugin from the registry.
	DeletePlugin(ctx context.Context, id uuid.UUID) error

	// UpdateExtensionSetting upserts a system-wide extension-point setting.
	// Only the super admin is authorised to call this.
	UpdateExtensionSetting(ctx context.Context, input UpdateExtensionSettingInput) (*PluginExtensionSetting, error)

	// ListExtensionSettings returns all extension settings for the given plugin.
	ListExtensionSettings(ctx context.Context, pluginID uuid.UUID) ([]*PluginExtensionSetting, error)

	// ListExtensionSettingsForPlugins returns extension settings grouped by plugin ID.
	ListExtensionSettingsForPlugins(ctx context.Context, pluginIDs []uuid.UUID) (map[uuid.UUID][]*PluginExtensionSetting, error)
}
