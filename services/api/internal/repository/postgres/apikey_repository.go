package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
)

// apiKeyRecord is the sqlx write model for the api_keys table.
type apiKeyRecord struct {
	ID         string     `db:"id"`
	UserID     string     `db:"user_id"`
	Name       string     `db:"name"`
	KeyPrefix  string     `db:"key_prefix"`
	KeyHash    string     `db:"key_hash"`
	LastUsedAt *time.Time `db:"last_used_at"`
	ExpiresAt  *time.Time `db:"expires_at"`
	CreatedAt  time.Time  `db:"created_at"`
	RevokedAt  *time.Time `db:"revoked_at"`
}

func apiKeyToEntity(r *apiKeyRecord) (*apikeydom.APIKey, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("api key repo: parse record id %q: %w", r.ID, err)
	}
	userID, err := uuid.Parse(r.UserID)
	if err != nil {
		return nil, fmt.Errorf("api key repo: parse record user_id %q: %w", r.UserID, err)
	}
	return &apikeydom.APIKey{
		ID:         id,
		UserID:     userID,
		Name:       r.Name,
		KeyPrefix:  r.KeyPrefix,
		LastUsedAt: r.LastUsedAt,
		ExpiresAt:  r.ExpiresAt,
		CreatedAt:  r.CreatedAt,
		RevokedAt:  r.RevokedAt,
	}, nil
}

// APIKeyRepository is the sqlx implementation of apikeydom.Repository.
type APIKeyRepository struct {
	db *sqlx.DB
}

// NewAPIKeyRepository returns a new APIKeyRepository.
func NewAPIKeyRepository(db *sqlx.DB) *APIKeyRepository {
	return &APIKeyRepository{db: db}
}

// FindByID returns the API key with the given ID.
func (r *APIKeyRepository) FindByID(ctx context.Context, id uuid.UUID) (*apikeydom.APIKey, error) {
	var rec apiKeyRecord
	err := r.db.GetContext(ctx, &rec, `SELECT id, user_id, name, key_prefix, key_hash, last_used_at, expires_at, created_at, revoked_at FROM api_keys WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apikeydom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("api key repo: find by id: %w", err)
	}
	return apiKeyToEntity(&rec)
}

// FindByHash looks up an API key by its SHA-256 hash.
func (r *APIKeyRepository) FindByHash(ctx context.Context, keyHash string) (*apikeydom.APIKey, error) {
	var rec apiKeyRecord
	err := r.db.GetContext(ctx, &rec, `SELECT id, user_id, name, key_prefix, key_hash, last_used_at, expires_at, created_at, revoked_at FROM api_keys WHERE key_hash = $1`, keyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apikeydom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("api key repo: find by hash: %w", err)
	}
	return apiKeyToEntity(&rec)
}

// ListByUserID returns all non-revoked API keys for the given user.
func (r *APIKeyRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*apikeydom.APIKey, error) {
	var recs []apiKeyRecord
	if err := r.db.SelectContext(ctx, &recs, `SELECT id, user_id, name, key_prefix, key_hash, last_used_at, expires_at, created_at, revoked_at FROM api_keys WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC`, userID.String()); err != nil {
		return nil, fmt.Errorf("api key repo: list: %w", err)
	}
	keys := make([]*apikeydom.APIKey, 0, len(recs))
	for i := range recs {
		entity, err := apiKeyToEntity(&recs[i])
		if err != nil {
			return nil, err
		}
		keys = append(keys, entity)
	}
	return keys, nil
}

// Create persists a new API key. The keyHash is stored; the raw key is never
// persisted.
func (r *APIKeyRepository) Create(ctx context.Context, key *apikeydom.APIKey, keyHash string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO api_keys (id, user_id, name, key_prefix, key_hash, expires_at, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		key.ID.String(), key.UserID.String(), key.Name, key.KeyPrefix, keyHash, key.ExpiresAt, key.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("api key repo: create: %w", err)
	}
	return nil
}

// Revoke soft-deletes an API key by setting revoked_at to now.
func (r *APIKeyRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	result, err := r.db.ExecContext(ctx, `UPDATE api_keys SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL`, now, id.String())
	if err != nil {
		return fmt.Errorf("api key repo: revoke: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return apikeydom.ErrNotFound
	}
	return nil
}

// UpdateLastUsed sets last_used_at on the given key.
func (r *APIKeyRepository) UpdateLastUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = $1 WHERE id = $2`, at, id.String())
	if err != nil {
		return fmt.Errorf("api key repo: update last used: %w", err)
	}
	return nil
}
