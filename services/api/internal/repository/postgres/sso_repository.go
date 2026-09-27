package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	ssodom "github.com/Paca-AI/api/internal/domain/sso"
)

const oidcProviderColumns = `id, slug, display_name, issuer_url, client_id, client_secret, scopes, enabled, auto_provision, link_by_email, allowed_domains, created_at, updated_at`

type oidcProviderRecord struct {
	ID             string    `db:"id"`
	Slug           string    `db:"slug"`
	DisplayName    string    `db:"display_name"`
	IssuerURL      string    `db:"issuer_url"`
	ClientID       string    `db:"client_id"`
	ClientSecret   string    `db:"client_secret"`
	Scopes         []byte    `db:"scopes"`
	Enabled        bool      `db:"enabled"`
	AutoProvision  bool      `db:"auto_provision"`
	LinkByEmail    bool      `db:"link_by_email"`
	AllowedDomains []byte    `db:"allowed_domains"`
	CreatedAt      time.Time `db:"created_at"`
	UpdatedAt      time.Time `db:"updated_at"`
}

func (r *oidcProviderRecord) toEntity() (*ssodom.Provider, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("sso repo: parse provider id %q: %w", r.ID, err)
	}
	p := &ssodom.Provider{
		ID:            id,
		Slug:          r.Slug,
		DisplayName:   r.DisplayName,
		IssuerURL:     r.IssuerURL,
		ClientID:      r.ClientID,
		ClientSecret:  r.ClientSecret,
		Enabled:       r.Enabled,
		AutoProvision: r.AutoProvision,
		LinkByEmail:   r.LinkByEmail,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
	if err := json.Unmarshal(r.Scopes, &p.Scopes); err != nil {
		return nil, fmt.Errorf("sso repo: decode scopes: %w", err)
	}
	if err := json.Unmarshal(r.AllowedDomains, &p.AllowedDomains); err != nil {
		return nil, fmt.Errorf("sso repo: decode allowed_domains: %w", err)
	}
	return p, nil
}

// jsonStrings encodes a string list for a JSONB column, never as JSON null.
func jsonStrings(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// SSORepository is the sqlx implementation of ssodom.Repository.
type SSORepository struct {
	db *sqlx.DB
}

// NewSSORepository returns a new SSORepository.
func NewSSORepository(db *sqlx.DB) *SSORepository {
	return &SSORepository{db: db}
}

// ListProviders returns every provider, ordered by display name.
func (r *SSORepository) ListProviders(ctx context.Context) ([]*ssodom.Provider, error) {
	var recs []oidcProviderRecord
	if err := r.db.SelectContext(ctx, &recs, `SELECT `+oidcProviderColumns+` FROM oidc_providers ORDER BY display_name, slug`); err != nil {
		return nil, fmt.Errorf("sso repo: list providers: %w", err)
	}
	out := make([]*ssodom.Provider, 0, len(recs))
	for i := range recs {
		p, err := recs[i].toEntity()
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// FindProviderByID returns ssodom.ErrProviderNotFound when absent.
func (r *SSORepository) FindProviderByID(ctx context.Context, id uuid.UUID) (*ssodom.Provider, error) {
	return r.findProvider(ctx, `id = $1`, id.String())
}

// FindProviderBySlug returns ssodom.ErrProviderNotFound when absent.
func (r *SSORepository) FindProviderBySlug(ctx context.Context, slug string) (*ssodom.Provider, error) {
	return r.findProvider(ctx, `slug = $1`, slug)
}

func (r *SSORepository) findProvider(ctx context.Context, where string, arg any) (*ssodom.Provider, error) {
	var rec oidcProviderRecord
	err := r.db.GetContext(ctx, &rec, `SELECT `+oidcProviderColumns+` FROM oidc_providers WHERE `+where, arg)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ssodom.ErrProviderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sso repo: find provider: %w", err)
	}
	return rec.toEntity()
}

// CreateProvider inserts p.
func (r *SSORepository) CreateProvider(ctx context.Context, p *ssodom.Provider) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO oidc_providers (`+oidcProviderColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10, $11::jsonb, $12, $13)`,
		p.ID.String(), p.Slug, p.DisplayName, p.IssuerURL, p.ClientID, p.ClientSecret,
		jsonStrings(p.Scopes), p.Enabled, p.AutoProvision, p.LinkByEmail,
		jsonStrings(p.AllowedDomains), p.CreatedAt, p.UpdatedAt,
	)
	return ssoProviderWriteErr("create provider", err)
}

// UpdateProvider saves every field of p except created_at.
func (r *SSORepository) UpdateProvider(ctx context.Context, p *ssodom.Provider) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE oidc_providers SET slug = $1, display_name = $2, issuer_url = $3, client_id = $4,
		  client_secret = $5, scopes = $6::jsonb, enabled = $7, auto_provision = $8,
		  link_by_email = $9, allowed_domains = $10::jsonb, updated_at = $11
		WHERE id = $12`,
		p.Slug, p.DisplayName, p.IssuerURL, p.ClientID, p.ClientSecret,
		jsonStrings(p.Scopes), p.Enabled, p.AutoProvision, p.LinkByEmail,
		jsonStrings(p.AllowedDomains), p.UpdatedAt, p.ID.String(),
	)
	if err != nil {
		return ssoProviderWriteErr("update provider", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ssodom.ErrProviderNotFound
	}
	return nil
}

// DeleteProvider removes the provider and, by cascade, its identity links.
func (r *SSORepository) DeleteProvider(ctx context.Context, id uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM oidc_providers WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("sso repo: delete provider: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ssodom.ErrProviderNotFound
	}
	return nil
}

func ssoProviderWriteErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if isUniqueViolation(err) {
		return ssodom.ErrSlugTaken
	}
	return fmt.Errorf("sso repo: %s: %w", op, err)
}

type userIdentityRecord struct {
	ID          string    `db:"id"`
	UserID      string    `db:"user_id"`
	ProviderID  string    `db:"provider_id"`
	Subject     string    `db:"subject"`
	Email       *string   `db:"email"`
	CreatedAt   time.Time `db:"created_at"`
	LastLoginAt time.Time `db:"last_login_at"`
}

// FindIdentity returns nil, nil when no account is linked to subject.
func (r *SSORepository) FindIdentity(ctx context.Context, providerID uuid.UUID, subject string) (*ssodom.Identity, error) {
	var rec userIdentityRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT id, user_id, provider_id, subject, email, created_at, last_login_at
		FROM user_identities WHERE provider_id = $1 AND subject = $2`, providerID.String(), subject)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sso repo: find identity: %w", err)
	}
	id, err1 := uuid.Parse(rec.ID)
	userID, err2 := uuid.Parse(rec.UserID)
	provID, err3 := uuid.Parse(rec.ProviderID)
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, fmt.Errorf("sso repo: parse identity ids: %w", err)
	}
	return &ssodom.Identity{
		ID:          id,
		UserID:      userID,
		ProviderID:  provID,
		Subject:     rec.Subject,
		Email:       rec.Email,
		CreatedAt:   rec.CreatedAt,
		LastLoginAt: rec.LastLoginAt,
	}, nil
}

// CreateIdentity inserts the link.
func (r *SSORepository) CreateIdentity(ctx context.Context, i *ssodom.Identity) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO user_identities (id, user_id, provider_id, subject, email, created_at, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		i.ID.String(), i.UserID.String(), i.ProviderID.String(), i.Subject, i.Email, i.CreatedAt, i.LastLoginAt,
	)
	if err != nil {
		return fmt.Errorf("sso repo: create identity: %w", err)
	}
	return nil
}

// TouchIdentity records a sign-in.
func (r *SSORepository) TouchIdentity(ctx context.Context, id uuid.UUID, email *string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE user_identities SET last_login_at = now(), email = $1 WHERE id = $2`, email, id.String())
	if err != nil {
		return fmt.Errorf("sso repo: touch identity: %w", err)
	}
	return nil
}
