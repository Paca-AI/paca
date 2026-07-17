package postgres

// Galaxy identity (ADR-038) extensions to UserRepository: lookups by the
// issuer-assigned OIDC subject / email and account linking.  Kept in a
// separate file so the upstream user_repository.go stays rebase-friendly.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/google/uuid"
)

// FindByOIDCSub returns the active user linked to the given OIDC subject, or
// userdom.ErrNotFound.
func (r *UserRepository) FindByOIDCSub(ctx context.Context, sub string) (*userdom.User, error) {
	var row userReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+userReadCols+`
		FROM users
		`+userReadJoin+`
		WHERE users.oidc_sub = $1 AND users.deleted_at IS NULL`, sub)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, userdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user repo: find by oidc sub: %w", err)
	}
	return rowToEntity(&row), nil
}

// FindByEmail returns the active user with the given email, or
// userdom.ErrNotFound.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*userdom.User, error) {
	var row userReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+userReadCols+`
		FROM users
		`+userReadJoin+`
		WHERE users.email = $1 AND users.deleted_at IS NULL`, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, userdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user repo: find by email: %w", err)
	}
	return rowToEntity(&row), nil
}

// SetOIDCIdentity sets or corrects the identity link on an existing user
// (admin/directory-sync path — ADR-038). Unlike LinkOIDC (the JIT login
// linker, which never overwrites an existing email), this OVERWRITES the
// stored values with the provided non-empty ones; an empty argument leaves
// that column untouched. It can never clear a link. A unique-index conflict
// (email or subject already on another row) maps to userdom.ErrIdentityTaken.
func (r *UserRepository) SetOIDCIdentity(ctx context.Context, userID uuid.UUID, email, sub string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET email = COALESCE(NULLIF($1, ''), email),
		    oidc_sub = COALESCE(NULLIF($2, ''), oidc_sub),
		    updated_at = $3
		WHERE id = $4 AND deleted_at IS NULL`,
		email, sub, time.Now(), userID.String(),
	)
	if err != nil {
		if mapped := mapUserUniqueViolation(err); mapped != nil {
			return mapped
		}
		return fmt.Errorf("user repo: set oidc identity: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return userdom.ErrNotFound
	}
	return nil
}

// LinkOIDC stores the OIDC subject (and email, when non-empty and not already
// set) on an existing user, linking the local account to the identity
// provider.
func (r *UserRepository) LinkOIDC(ctx context.Context, userID uuid.UUID, sub, email string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users
		SET oidc_sub = $1,
		    email = COALESCE(email, NULLIF($2, '')),
		    updated_at = $3
		WHERE id = $4 AND deleted_at IS NULL`,
		sub, email, time.Now(), userID.String(),
	)
	if err != nil {
		return fmt.Errorf("user repo: link oidc: %w", err)
	}
	return nil
}
