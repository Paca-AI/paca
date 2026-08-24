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

// settingsColumns is shared between Get and WithLock's locked read so the
// two queries can't drift apart.
const settingsColumns = `logo_key, logo_thumb_key, favicon_key, favicon_thumb_key, primary_color_light, primary_color_dark, brand_name, oidc_configured, oidc_enabled, oidc_issuer_url, oidc_client_id, oidc_client_secret_enc, oidc_scopes, oidc_redirect_url, oidc_display_name, oidc_username_claim, local_login_enabled, updated_at, updated_by`

// workspaceSettingsRecord is the sqlx write model for the singleton
// workspace_settings row.
type workspaceSettingsRecord struct {
	LogoKey             *string   `db:"logo_key"`
	LogoThumbKey        *string   `db:"logo_thumb_key"`
	FaviconKey          *string   `db:"favicon_key"`
	FaviconThumbKey     *string   `db:"favicon_thumb_key"`
	PrimaryColorLight   *string   `db:"primary_color_light"`
	PrimaryColorDark    *string   `db:"primary_color_dark"`
	BrandName           *string   `db:"brand_name"`
	OIDCConfigured      bool      `db:"oidc_configured"`
	OIDCEnabled         bool      `db:"oidc_enabled"`
	OIDCIssuerURL       *string   `db:"oidc_issuer_url"`
	OIDCClientID        *string   `db:"oidc_client_id"`
	OIDCClientSecretEnc *string   `db:"oidc_client_secret_enc"`
	OIDCScopes          *string   `db:"oidc_scopes"`
	OIDCRedirectURL     *string   `db:"oidc_redirect_url"`
	OIDCDisplayName     *string   `db:"oidc_display_name"`
	OIDCUsernameClaim   *string   `db:"oidc_username_claim"`
	LocalLoginEnabled   bool      `db:"local_login_enabled"`
	UpdatedAt           time.Time `db:"updated_at"`
	UpdatedBy           *string   `db:"updated_by"`
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
		LogoKey:             r.LogoKey,
		LogoThumbKey:        r.LogoThumbKey,
		FaviconKey:          r.FaviconKey,
		FaviconThumbKey:     r.FaviconThumbKey,
		PrimaryColorLight:   r.PrimaryColorLight,
		PrimaryColorDark:    r.PrimaryColorDark,
		BrandName:           r.BrandName,
		OIDCConfigured:      r.OIDCConfigured,
		OIDCEnabled:         r.OIDCEnabled,
		OIDCIssuerURL:       r.OIDCIssuerURL,
		OIDCClientID:        r.OIDCClientID,
		OIDCClientSecretEnc: r.OIDCClientSecretEnc,
		OIDCScopes:          r.OIDCScopes,
		OIDCRedirectURL:     r.OIDCRedirectURL,
		OIDCDisplayName:     r.OIDCDisplayName,
		OIDCUsernameClaim:   r.OIDCUsernameClaim,
		LocalLoginEnabled:   r.LocalLoginEnabled,
		UpdatedAt:           r.UpdatedAt,
		UpdatedBy:           updatedBy,
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
	err := r.db.GetContext(ctx, &rec, `SELECT `+settingsColumns+` FROM workspace_settings WHERE id = true`)
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

// WithLock locks the singleton row with SELECT ... FOR UPDATE for the
// duration of a transaction, invokes fn with the current row, and persists
// whatever fn returns (or writes nothing if fn returns a nil row). The lock
// serializes concurrent callers so a read-modify-write from one caller can't
// be silently overwritten by another that read its snapshot just before —
// see settingsdom.Repository.WithLock's doc comment.
func (r *SettingsRepository) WithLock(ctx context.Context, fn func(*settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error)) (*settingsdom.WorkspaceSettings, error) {
	var result *settingsdom.WorkspaceSettings
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var rec workspaceSettingsRecord
		err := tx.GetContext(ctx, &rec, `SELECT `+settingsColumns+` FROM workspace_settings WHERE id = true FOR UPDATE`)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("settings repo: with lock: workspace_settings row missing")
		}
		if err != nil {
			return fmt.Errorf("settings repo: with lock: %w", err)
		}
		ws, err := workspaceSettingsToEntity(&rec)
		if err != nil {
			return err
		}

		updated, err := fn(ws)
		if err != nil {
			return err
		}
		if updated == nil {
			result = ws
			return nil
		}

		if err := updateRow(ctx, tx, updated); err != nil {
			return err
		}
		result = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// updateRow persists s, overwriting the singleton row. Takes a *sqlx.Tx so
// WithLock's write happens inside the same transaction as its lock.
func updateRow(ctx context.Context, tx *sqlx.Tx, s *settingsdom.WorkspaceSettings) error {
	var updatedBy *string
	if s.UpdatedBy != nil {
		id := s.UpdatedBy.String()
		updatedBy = &id
	}
	_, err := tx.ExecContext(ctx, `UPDATE workspace_settings SET logo_key = $1, logo_thumb_key = $2, favicon_key = $3, favicon_thumb_key = $4, primary_color_light = $5, primary_color_dark = $6, brand_name = $7, oidc_configured = $8, oidc_enabled = $9, oidc_issuer_url = $10, oidc_client_id = $11, oidc_client_secret_enc = $12, oidc_scopes = $13, oidc_redirect_url = $14, oidc_display_name = $15, oidc_username_claim = $16, local_login_enabled = $17, updated_at = $18, updated_by = $19 WHERE id = true`,
		s.LogoKey, s.LogoThumbKey, s.FaviconKey, s.FaviconThumbKey, s.PrimaryColorLight, s.PrimaryColorDark, s.BrandName,
		s.OIDCConfigured, s.OIDCEnabled, s.OIDCIssuerURL, s.OIDCClientID, s.OIDCClientSecretEnc, s.OIDCScopes, s.OIDCRedirectURL, s.OIDCDisplayName, s.OIDCUsernameClaim, s.LocalLoginEnabled,
		s.UpdatedAt, updatedBy,
	)
	if err != nil {
		return fmt.Errorf("settings repo: update: %w", err)
	}
	return nil
}
