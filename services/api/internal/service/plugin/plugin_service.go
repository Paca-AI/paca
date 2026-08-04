// Package pluginsvc implements the plugin domain service.
package pluginsvc

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// Service implements plugindom.Service.
type Service struct {
	repo        plugindom.Repository
	hostVersion string
}

// New creates a Service wired to the given repository.
func New(repo plugindom.Repository) *Service {
	return &Service{repo: repo}
}

// WithHostVersion sets the running Paca build's version, used to enforce each
// manifest's declared MinCoreVersion at install/update time. Left unset (or
// set to a non-release string like "dev"), the check never rejects a plugin
// — see plugindom.PluginManifest.CheckMinCoreVersion.
func (s *Service) WithHostVersion(v string) *Service {
	s.hostVersion = v
	return s
}

// ListPlugins returns all installed plugins.
func (s *Service) ListPlugins(ctx context.Context) ([]*plugindom.Plugin, error) {
	return s.repo.List(ctx)
}

// CheckHostCompatibility reports whether the running host version satisfies
// manifest's declared MinCoreVersion, as a *apierr.Error ready to return to
// the caller. It performs no validation or persistence — callers that also
// need manifest.Validate() (InstallPlugin, UpdatePlugin) run that first.
//
// Handlers that install/upgrade a plugin outside of a single InstallPlugin/
// UpdatePlugin call (e.g. marketplace upgrade, which downloads artifacts and
// runs migrations before persisting) must call this explicitly, and early —
// before running migrations or loading the new manifest into the runtime —
// so an incompatible upgrade never becomes live even briefly.
func (s *Service) CheckHostCompatibility(manifest plugindom.PluginManifest) error {
	if err := manifest.CheckMinCoreVersion(s.hostVersion); err != nil {
		return apierr.NewWithDetails(apierr.CodePluginIncompatibleHostVersion, err.Error(), map[string]string{
			"plugin_id":        manifest.ID,
			"required_version": manifest.MinCoreVersion,
			"host_version":     s.hostVersion,
		})
	}
	return nil
}

// InstallPlugin validates and inserts a new plugin into the registry.
func (s *Service) InstallPlugin(ctx context.Context, input plugindom.InstallInput) (*plugindom.Plugin, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("plugin name is required")
	}
	if err := input.Manifest.Validate(); err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "invalid plugin manifest: "+err.Error())
	}
	if err := s.CheckHostCompatibility(input.Manifest); err != nil {
		return nil, err
	}
	now := time.Now()
	p := &plugindom.Plugin{
		ID:          uuid.New(),
		Name:        input.Name,
		Version:     input.Version,
		Manifest:    input.Manifest,
		Enabled:     input.Enabled,
		InstalledAt: now,
		UpdatedAt:   now,
	}
	if err := s.repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// UpdatePlugin patches an existing plugin's mutable fields.
func (s *Service) UpdatePlugin(ctx context.Context, id uuid.UUID, input plugindom.UpdateInput) (*plugindom.Plugin, error) {
	p, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Version != nil {
		p.Version = *input.Version
	}
	if input.Manifest != nil {
		if err := input.Manifest.Validate(); err != nil {
			return nil, apierr.New(apierr.CodeBadRequest, "invalid plugin manifest: "+err.Error())
		}
		if err := s.CheckHostCompatibility(*input.Manifest); err != nil {
			return nil, err
		}
		p.Manifest = *input.Manifest
	}
	if input.Enabled != nil {
		p.Enabled = *input.Enabled
	}
	p.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// DeletePlugin removes a plugin from the registry.
func (s *Service) DeletePlugin(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

// UpdateExtensionSetting upserts a system-wide extension-point setting.
func (s *Service) UpdateExtensionSetting(ctx context.Context, input plugindom.UpdateExtensionSettingInput) (*plugindom.PluginExtensionSetting, error) {
	setting := &plugindom.PluginExtensionSetting{
		ID:             uuid.New(),
		PluginID:       input.PluginID,
		ExtensionPoint: input.ExtensionPoint,
		Settings:       input.Settings,
		UpdatedAt:      time.Now(),
	}
	if err := s.repo.UpsertSetting(ctx, setting); err != nil {
		return nil, err
	}
	// Re-query to get the actual persisted ID (upsert may have kept existing ID)
	settings, err := s.repo.ListSettings(ctx, input.PluginID)
	if err != nil {
		return nil, err
	}
	for _, setting := range settings {
		if setting.ExtensionPoint == input.ExtensionPoint {
			return setting, nil
		}
	}
	// Fallback: return what we tried to insert (should not happen)
	return setting, nil
}

// ListExtensionSettings returns all extension settings for the given plugin.
func (s *Service) ListExtensionSettings(ctx context.Context, pluginID uuid.UUID) ([]*plugindom.PluginExtensionSetting, error) {
	return s.repo.ListSettings(ctx, pluginID)
}

// ListExtensionSettingsForPlugins returns extension settings grouped by plugin ID.
func (s *Service) ListExtensionSettingsForPlugins(
	ctx context.Context,
	pluginIDs []uuid.UUID,
) (map[uuid.UUID][]*plugindom.PluginExtensionSetting, error) {
	grouped := make(map[uuid.UUID][]*plugindom.PluginExtensionSetting, len(pluginIDs))
	if len(pluginIDs) == 0 {
		return grouped, nil
	}

	settings, err := s.repo.ListSettingsForPlugins(ctx, pluginIDs)
	if err != nil {
		return nil, err
	}

	for _, s := range settings {
		grouped[s.PluginID] = append(grouped[s.PluginID], s)
	}

	return grouped, nil
}
