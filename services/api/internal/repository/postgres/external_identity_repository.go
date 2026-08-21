package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	extiddom "github.com/Paca-AI/api/internal/domain/externalidentity"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

// identityRow is the sqlx read model for user_external_identities.
type identityRow struct {
	ID          string    `db:"id"`
	UserID      string    `db:"user_id"`
	Provider    string    `db:"provider"`
	Issuer      string    `db:"issuer"`
	Subject     string    `db:"subject"`
	CreatedAt   time.Time `db:"created_at"`
	LastLoginAt time.Time `db:"last_login_at"`
}

// ExternalIdentityRepository is the sqlx implementation of
// externalidentity.Repository.
type ExternalIdentityRepository struct {
	db *sqlx.DB
}

// NewExternalIdentityRepository returns a new ExternalIdentityRepository.
func NewExternalIdentityRepository(db *sqlx.DB) *ExternalIdentityRepository {
	return &ExternalIdentityRepository{db: db}
}

// FindByIssuerSubject returns the identity for the (issuer, subject) pair, or
// extiddom.ErrNotFound.
func (r *ExternalIdentityRepository) FindByIssuerSubject(ctx context.Context, issuer, subject string) (*extiddom.Identity, error) {
	var row identityRow
	err := r.db.GetContext(ctx, &row, `
		SELECT id, user_id, provider, issuer, subject, created_at, last_login_at
		FROM user_external_identities
		WHERE issuer = $1 AND subject = $2`, issuer, subject)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, extiddom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("external identity repo: find by issuer+subject: %w", err)
	}
	return identityRowToEntity(&row), nil
}

// TouchLastLogin refreshes the identity's last-login timestamp.
func (r *ExternalIdentityRepository) TouchLastLogin(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE user_external_identities SET last_login_at = $1 WHERE id = $2`,
		time.Now().UTC(), id.String())
	if err != nil {
		return fmt.Errorf("external identity repo: touch last login: %w", err)
	}
	return nil
}

// ProvisionWithUser atomically inserts the new Paca user and its external
// identity in one transaction. Unique-constraint violations on the user's
// username/email map to the same userdom sentinels the user repository
// returns, so the JIT caller can retry with a different username candidate.
func (r *ExternalIdentityRepository) ProvisionWithUser(ctx context.Context, u *userdom.User, identity *extiddom.Identity) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO users (id, username, password_hash, full_name, email, role_id, must_change_password, password_login_enabled, created_at, updated_at, deleted_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			u.ID.String(), u.Username, u.PasswordHash, u.FullName, u.Email,
			u.RoleID.String(), u.MustChangePassword, u.PasswordLoginEnabled,
			u.CreatedAt, u.UpdatedAt, u.DeletedAt,
		); err != nil {
			return userRepoErr("provision user", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_external_identities (id, user_id, provider, issuer, subject, created_at, last_login_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			identity.ID.String(), identity.UserID.String(), identity.Provider,
			identity.Issuer, identity.Subject, identity.CreatedAt, identity.LastLoginAt,
		); err != nil {
			// A (issuer, subject) collision means a concurrent first login
			// already created this binding: surface it as a distinct sentinel
			// so the caller can resolve the winner's user instead of failing
			// the login.
			if constraint, ok := uniqueViolationConstraint(err); ok && constraint == "uni_external_identity_issuer_subject" {
				return extiddom.ErrIdentityTaken
			}
			return fmt.Errorf("external identity repo: provision identity: %w", err)
		}
		return nil
	})
}

// HasSSOUserWithRole reports whether at least one external identity is bound
// to an active user holding one of the named global roles. Used by the
// startup guard that refuses an SSO-only configuration until an
// administrator can actually log in via SSO.
func (r *ExternalIdentityRepository) HasSSOUserWithRole(ctx context.Context, roleNames []string) (bool, error) {
	if len(roleNames) == 0 {
		return false, nil
	}
	query, args, err := sqlx.In(`
		SELECT COUNT(*)
		FROM user_external_identities ei
		JOIN users u ON u.id = ei.user_id
		JOIN global_roles gr ON gr.id = u.role_id
		WHERE gr.name IN (?) AND u.deleted_at IS NULL`, roleNames)
	if err != nil {
		return false, fmt.Errorf("external identity repo: has sso user with role: %w", err)
	}
	var count int
	if err := r.db.GetContext(ctx, &count, r.db.Rebind(query), args...); err != nil {
		return false, fmt.Errorf("external identity repo: has sso user with role: %w", err)
	}
	return count > 0, nil
}

func identityRowToEntity(row *identityRow) *extiddom.Identity {
	id, _ := uuid.Parse(row.ID)
	userID, _ := uuid.Parse(row.UserID)
	return &extiddom.Identity{
		ID:          id,
		UserID:      userID,
		Provider:    row.Provider,
		Issuer:      row.Issuer,
		Subject:     row.Subject,
		CreatedAt:   row.CreatedAt,
		LastLoginAt: row.LastLoginAt,
	}
}
