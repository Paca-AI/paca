package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	userdom "github.com/Paca-AI/api/internal/domain/user"
)

// passwordSetTokenRow is the sqlx read/write model for password_set_tokens.
type passwordSetTokenRow struct {
	ID        string     `db:"id"`
	UserID    string     `db:"user_id"`
	TokenHash string     `db:"token_hash"`
	ExpiresAt time.Time  `db:"expires_at"`
	UsedAt    *time.Time `db:"used_at"`
	CreatedAt time.Time  `db:"created_at"`
}

// PasswordSetTokenRepository is the sqlx implementation of
// userdom.PasswordSetTokenRepository.
type PasswordSetTokenRepository struct {
	db *sqlx.DB
}

// NewPasswordSetTokenRepository returns a new PasswordSetTokenRepository.
func NewPasswordSetTokenRepository(db *sqlx.DB) *PasswordSetTokenRepository {
	return &PasswordSetTokenRepository{db: db}
}

// Create persists a new password-set token.
func (r *PasswordSetTokenRepository) Create(ctx context.Context, t *userdom.PasswordSetToken) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO password_set_tokens (id, user_id, token_hash, expires_at, used_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		t.ID.String(), t.UserID.String(), t.TokenHash, t.ExpiresAt, t.UsedAt, t.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("password set token repo: create: %w", err)
	}
	return nil
}

// FindActiveByTokenHash returns the token row for hash if it exists, is
// unused, and has not expired; otherwise userdom.ErrPasswordSetTokenInvalid.
// Filtering by "active" in the query itself (rather than in the service
// layer after a plain lookup) keeps expired/used/not-found indistinguishable
// to the caller.
func (r *PasswordSetTokenRepository) FindActiveByTokenHash(ctx context.Context, hash string) (*userdom.PasswordSetToken, error) {
	var row passwordSetTokenRow
	err := r.db.GetContext(ctx, &row, `
		SELECT id, user_id, token_hash, expires_at, used_at, created_at
		FROM password_set_tokens
		WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, userdom.ErrPasswordSetTokenInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("password set token repo: find active by token hash: %w", err)
	}

	id, _ := uuid.Parse(row.ID)
	userID, _ := uuid.Parse(row.UserID)
	return &userdom.PasswordSetToken{
		ID:        id,
		UserID:    userID,
		TokenHash: row.TokenHash,
		ExpiresAt: row.ExpiresAt,
		UsedAt:    row.UsedAt,
		CreatedAt: row.CreatedAt,
	}, nil
}

// MarkUsed sets used_at on the token so it cannot be redeemed again.
func (r *PasswordSetTokenRepository) MarkUsed(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `UPDATE password_set_tokens SET used_at = $1 WHERE id = $2`, now, id.String())
	if err != nil {
		return fmt.Errorf("password set token repo: mark used: %w", err)
	}
	return nil
}
