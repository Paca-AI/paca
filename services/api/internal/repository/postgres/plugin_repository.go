package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// -------------------------------------------------------------------------
// sqlx models
// -------------------------------------------------------------------------

type pluginModel struct {
	ID          string          `db:"id"`
	Name        string          `db:"name"`
	Version     string          `db:"version"`
	Manifest    json.RawMessage `db:"manifest"`
	Enabled     bool            `db:"enabled"`
	InstalledAt time.Time       `db:"installed_at"`
	UpdatedAt   time.Time       `db:"updated_at"`
}

type pluginExtensionSettingModel struct {
	ID             string          `db:"id"`
	PluginID       string          `db:"plugin_id"`
	ExtensionPoint string          `db:"extension_point"`
	Settings       json.RawMessage `db:"settings"`
	UpdatedAt      time.Time       `db:"updated_at"`
}

// -------------------------------------------------------------------------
// Conversion helpers
// -------------------------------------------------------------------------

func pluginFromModel(m *pluginModel) (*plugindom.Plugin, error) {
	id, err := uuid.Parse(m.ID)
	if err != nil {
		return nil, err
	}
	var manifest plugindom.PluginManifest
	if len(m.Manifest) > 0 {
		if err := json.Unmarshal(m.Manifest, &manifest); err != nil {
			return nil, err
		}
	}
	return &plugindom.Plugin{
		ID:          id,
		Name:        m.Name,
		Version:     m.Version,
		Manifest:    manifest,
		Enabled:     m.Enabled,
		InstalledAt: m.InstalledAt,
		UpdatedAt:   m.UpdatedAt,
	}, nil
}

func pluginToModel(p *plugindom.Plugin) (*pluginModel, error) {
	manifestBytes, err := json.Marshal(p.Manifest)
	if err != nil {
		return nil, err
	}
	return &pluginModel{
		ID:          p.ID.String(),
		Name:        p.Name,
		Version:     p.Version,
		Manifest:    json.RawMessage(manifestBytes),
		Enabled:     p.Enabled,
		InstalledAt: p.InstalledAt,
		UpdatedAt:   p.UpdatedAt,
	}, nil
}

func settingFromModel(m *pluginExtensionSettingModel) (*plugindom.PluginExtensionSetting, error) {
	id, err := uuid.Parse(m.ID)
	if err != nil {
		return nil, err
	}
	pluginID, err := uuid.Parse(m.PluginID)
	if err != nil {
		return nil, err
	}
	var settings plugindom.ExtensionSettingData
	if len(m.Settings) > 0 {
		if err := json.Unmarshal(m.Settings, &settings); err != nil {
			return nil, err
		}
	}
	return &plugindom.PluginExtensionSetting{
		ID:             id,
		PluginID:       pluginID,
		ExtensionPoint: m.ExtensionPoint,
		Settings:       settings,
		UpdatedAt:      m.UpdatedAt,
	}, nil
}

// -------------------------------------------------------------------------
// PluginRepository (implements plugindom.Repository)
// -------------------------------------------------------------------------

// PluginRepository is the PostgreSQL-backed implementation of plugindom.Repository.
type PluginRepository struct {
	db *sqlx.DB
}

// NewPluginRepository creates a PluginRepository backed by the given sqlx DB.
func NewPluginRepository(db *sqlx.DB) *PluginRepository {
	return &PluginRepository{db: db}
}

const pluginSelectCols = `id, name, version, manifest, enabled, installed_at, updated_at`

// List returns all plugins ordered by name.
func (r *PluginRepository) List(ctx context.Context) ([]*plugindom.Plugin, error) {
	var models []pluginModel
	if err := r.db.SelectContext(ctx, &models, `SELECT `+pluginSelectCols+` FROM plugins ORDER BY name`); err != nil {
		return nil, err
	}
	plugins := make([]*plugindom.Plugin, 0, len(models))
	for _, m := range models {
		p, err := pluginFromModel(&m)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}

// FindByID returns the plugin with the given ID.
func (r *PluginRepository) FindByID(ctx context.Context, id uuid.UUID) (*plugindom.Plugin, error) {
	var m pluginModel
	err := r.db.GetContext(ctx, &m, `SELECT `+pluginSelectCols+` FROM plugins WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, plugindom.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return pluginFromModel(&m)
}

// FindByName returns the plugin with the given name.
func (r *PluginRepository) FindByName(ctx context.Context, name string) (*plugindom.Plugin, error) {
	var m pluginModel
	err := r.db.GetContext(ctx, &m, `SELECT `+pluginSelectCols+` FROM plugins WHERE name = $1`, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, plugindom.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return pluginFromModel(&m)
}

// FindByCapability returns all enabled plugins that have the given capability.
func (r *PluginRepository) FindByCapability(ctx context.Context, capability string) ([]*plugindom.Plugin, error) {
	capJSON, _ := json.Marshal([]string{capability})
	var models []pluginModel
	err := r.db.SelectContext(ctx, &models, `
		SELECT `+pluginSelectCols+` FROM plugins
		WHERE enabled = true AND manifest->'capabilities' @> $1::jsonb
		ORDER BY name`, string(capJSON))
	if err != nil {
		return nil, err
	}
	plugins := make([]*plugindom.Plugin, 0, len(models))
	for _, m := range models {
		p, err := pluginFromModel(&m)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, p)
	}
	return plugins, nil
}

// Create inserts a new plugin into the registry.
func (r *PluginRepository) Create(ctx context.Context, p *plugindom.Plugin) error {
	m, err := pluginToModel(p)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO plugins (id, name, version, manifest, enabled, installed_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		m.ID, m.Name, m.Version, m.Manifest, m.Enabled, m.InstalledAt, m.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return plugindom.ErrNameTaken
		}
		return err
	}
	return nil
}

// Update replaces mutable fields of an existing plugin.
func (r *PluginRepository) Update(ctx context.Context, p *plugindom.Plugin) error {
	manifestBytes, err := json.Marshal(p.Manifest)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE plugins SET version = $1, manifest = $2, enabled = $3, updated_at = $4 WHERE id = $5`,
		p.Version, json.RawMessage(manifestBytes), p.Enabled, p.UpdatedAt, p.ID.String(),
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return plugindom.ErrNotFound
	}
	return nil
}

// Delete removes a plugin and all associated extension settings.
func (r *PluginRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM plugins WHERE id = $1`, id.String())
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return plugindom.ErrNotFound
	}
	return nil
}

// -------------------------------------------------------------------------
// PluginExtensionSettingRepository
// -------------------------------------------------------------------------

const settingSelectCols = `id, plugin_id, extension_point, settings, updated_at`

// ListSettings returns all extension settings for the given plugin.
func (r *PluginRepository) ListSettings(ctx context.Context, pluginID uuid.UUID) ([]*plugindom.PluginExtensionSetting, error) {
	var models []pluginExtensionSettingModel
	if err := r.db.SelectContext(ctx, &models, `SELECT `+settingSelectCols+` FROM plugin_extension_settings WHERE plugin_id = $1`, pluginID.String()); err != nil {
		return nil, err
	}
	settings := make([]*plugindom.PluginExtensionSetting, 0, len(models))
	for _, m := range models {
		s, err := settingFromModel(&m)
		if err != nil {
			return nil, err
		}
		settings = append(settings, s)
	}
	return settings, nil
}

// ListSettingsForPlugins returns all extension settings for the given plugins.
func (r *PluginRepository) ListSettingsForPlugins(
	ctx context.Context,
	pluginIDs []uuid.UUID,
) ([]*plugindom.PluginExtensionSetting, error) {
	if len(pluginIDs) == 0 {
		return []*plugindom.PluginExtensionSetting{}, nil
	}

	ids := make([]string, 0, len(pluginIDs))
	for _, id := range pluginIDs {
		ids = append(ids, id.String())
	}

	query, args, err := sqlx.In(`SELECT `+settingSelectCols+` FROM plugin_extension_settings WHERE plugin_id IN (?)`, ids)
	if err != nil {
		return nil, err
	}
	query = sqlx.Rebind(sqlx.DOLLAR, query)

	var models []pluginExtensionSettingModel
	if err := r.db.SelectContext(ctx, &models, query, args...); err != nil {
		return nil, err
	}

	settings := make([]*plugindom.PluginExtensionSetting, 0, len(models))
	for _, m := range models {
		s, err := settingFromModel(&m)
		if err != nil {
			return nil, err
		}
		settings = append(settings, s)
	}

	return settings, nil
}

// UpsertSetting creates or replaces the settings row for (plugin, extension_point).
func (r *PluginRepository) UpsertSetting(ctx context.Context, setting *plugindom.PluginExtensionSetting) error {
	settingsBytes, err := json.Marshal(setting.Settings)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO plugin_extension_settings (id, plugin_id, extension_point, settings, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (plugin_id, extension_point) DO UPDATE
		  SET settings = EXCLUDED.settings, updated_at = EXCLUDED.updated_at`,
		setting.ID.String(), setting.PluginID.String(), setting.ExtensionPoint,
		json.RawMessage(settingsBytes), setting.UpdatedAt,
	)
	return err
}
