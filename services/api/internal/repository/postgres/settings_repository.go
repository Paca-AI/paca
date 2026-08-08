package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
)

// workspaceSettingsRecord is the sqlx write model for the singleton
// workspace_settings row.
type workspaceSettingsRecord struct {
	LogoKey           *string   `db:"logo_key"`
	LogoThumbKey      *string   `db:"logo_thumb_key"`
	FaviconKey        *string   `db:"favicon_key"`
	FaviconThumbKey   *string   `db:"favicon_thumb_key"`
	PrimaryColorLight *string   `db:"primary_color_light"`
	PrimaryColorDark  *string   `db:"primary_color_dark"`
	BrandName         *string   `db:"brand_name"`
	UpdatedAt         time.Time `db:"updated_at"`
	UpdatedBy         *string   `db:"updated_by"`
}

func workspaceSettingsToEntity(r *workspaceSettingsRecord) (*settingsdom.WorkspaceSettings, error) {
	var updatedBy *uuid.UUID
	if r.UpdatedBy != nil {
		id, err := uuid.Parse(*r.UpdatedBy)
		if err != nil {
			return nil, fmt.Errorf("settings repo: parse record updated_by %q: %w", *r.UpdatedBy, err)
		}
		updatedBy = &id
	}
	return &settingsdom.WorkspaceSettings{
		LogoKey:           r.LogoKey,
		LogoThumbKey:      r.LogoThumbKey,
		FaviconKey:        r.FaviconKey,
		FaviconThumbKey:   r.FaviconThumbKey,
		PrimaryColorLight: r.PrimaryColorLight,
		PrimaryColorDark:  r.PrimaryColorDark,
		BrandName:         r.BrandName,
		UpdatedAt:         r.UpdatedAt,
		UpdatedBy:         updatedBy,
	}, nil
}

// SettingsRepository is the sqlx implementation of settingsdom.Repository,
// operating on the singleton workspace_settings row (id = true, seeded by
// migration 000035).
type SettingsRepository struct {
	db *sqlx.DB
}

// NewSettingsRepository returns a new SettingsRepository.
func NewSettingsRepository(db *sqlx.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

// Get returns the workspace settings row.
func (r *SettingsRepository) Get(ctx context.Context) (*settingsdom.WorkspaceSettings, error) {
	var rec workspaceSettingsRecord
	err := r.db.GetContext(ctx, &rec, `SELECT logo_key, logo_thumb_key, favicon_key, favicon_thumb_key, primary_color_light, primary_color_dark, brand_name, updated_at, updated_by FROM workspace_settings WHERE id = true`)
	if errors.Is(err, sql.ErrNoRows) {
		// The seed row (migration 000035) always exists; ErrNoRows here would
		// mean the table was somehow emptied out from under the app.
		return nil, fmt.Errorf("settings repo: get: workspace_settings row missing")
	}
	if err != nil {
		return nil, fmt.Errorf("settings repo: get: %w", err)
	}
	return workspaceSettingsToEntity(&rec)
}

// Update persists s, overwriting the singleton row.
func (r *SettingsRepository) Update(ctx context.Context, s *settingsdom.WorkspaceSettings) error {
	var updatedBy *string
	if s.UpdatedBy != nil {
		id := s.UpdatedBy.String()
		updatedBy = &id
	}
	_, err := r.db.ExecContext(ctx, `UPDATE workspace_settings SET logo_key = $1, logo_thumb_key = $2, favicon_key = $3, favicon_thumb_key = $4, primary_color_light = $5, primary_color_dark = $6, brand_name = $7, updated_at = $8, updated_by = $9 WHERE id = true`,
		s.LogoKey, s.LogoThumbKey, s.FaviconKey, s.FaviconThumbKey, s.PrimaryColorLight, s.PrimaryColorDark, s.BrandName, s.UpdatedAt, updatedBy,
	)
	if err != nil {
		return fmt.Errorf("settings repo: update: %w", err)
	}
	return nil
}
