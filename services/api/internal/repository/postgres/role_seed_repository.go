package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	roledom "github.com/Paca-AI/api/internal/domain/role"
)

// RoleSeedRepository writes what the API ships at startup: the system roles
// and the attachments the bootstrap accounts need. Every operation is
// idempotent and touches only the rows it is asked about, so it is safe to run
// on every start against a database that already holds user data.
type RoleSeedRepository struct {
	db *sqlx.DB
}

// NewRoleSeedRepository returns a RoleSeedRepository.
func NewRoleSeedRepository(db *sqlx.DB) *RoleSeedRepository {
	return &RoleSeedRepository{db: db}
}

// SeedRole is one shipped platform role.
type SeedRole struct {
	Name        string
	Description string
	Policy      json.RawMessage
}

// SeededRole reports what UpsertSystemRole did.
type SeededRole struct {
	ID uuid.UUID
	// Created: the role did not exist. Changed: it existed and its policy,
	// description or system flag was brought in line with the shipped one.
	Created, Changed bool
}

// UpsertSystemRole makes the platform role named def.Name (project_id NULL)
// a system role carrying exactly def's policy and description.
//
//   - A role of that name that does not exist is created.
//   - A role of that name that does exist is reconciled in place: this is how
//     a role migration 000064 converted from the legacy tables (matched by
//     name) becomes the shipped system role. Its id, its attachments and its
//     default flag are kept, so nobody holding it loses it.
//   - Nothing else is read or written: roles with any other name, and
//     project-owned roles, are never touched.
//
// Writes happen only when something differs, so a start with nothing to
// change leaves updated_at alone.
func (r *RoleSeedRepository) UpsertSystemRole(ctx context.Context, def SeedRole) (SeededRole, error) {
	out, err := r.upsertSystemRole(ctx, def)
	if err != nil && isRoleNameViolation(err) {
		// Another API instance created the role between our lookup and our
		// insert; now it exists, so the second attempt reconciles it.
		return r.upsertSystemRole(ctx, def)
	}
	return out, err
}

func (r *RoleSeedRepository) upsertSystemRole(ctx context.Context, def SeedRole) (SeededRole, error) {
	var out SeededRole
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		out = SeededRole{}
		var cur struct {
			ID string `db:"id"`
		}
		err := tx.GetContext(ctx, &cur, `
			SELECT id FROM roles WHERE project_id IS NULL AND name = $1 FOR UPDATE`, def.Name)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			var id string
			if err := tx.GetContext(ctx, &id, `
				INSERT INTO roles (name, description, policy, is_system)
				VALUES ($1, $2, $3::jsonb, TRUE) RETURNING id`,
				def.Name, def.Description, string(def.Policy)); err != nil {
				return fmt.Errorf("role seed: create %s: %w", def.Name, err)
			}
			parsed, perr := uuid.Parse(id)
			if perr != nil {
				return fmt.Errorf("role seed: create %s: bad id: %w", def.Name, perr)
			}
			out = SeededRole{ID: parsed, Created: true}
			return nil
		case err != nil:
			return fmt.Errorf("role seed: find %s: %w", def.Name, err)
		}

		id, perr := uuid.Parse(cur.ID)
		if perr != nil {
			return fmt.Errorf("role seed: find %s: bad id: %w", def.Name, perr)
		}
		// Reconcile the existing role: update its policy, description, and system
		// flag to match what the release ships. System roles are security-critical
		// and must match exactly; if an administrator needs a custom policy, they
		// can create a separate role with a different name.
		res, err := tx.ExecContext(ctx, `
			UPDATE roles SET policy = $2::jsonb, description = $3, is_system = TRUE, updated_at = NOW()
			WHERE id = $1::uuid AND (policy::text != $2 OR description != $3 OR NOT is_system)`,
			cur.ID, string(def.Policy), def.Description)
		if err != nil {
			return fmt.Errorf("role seed: update %s: %w", def.Name, err)
		}
		n, _ := res.RowsAffected()
		out = SeededRole{ID: id, Changed: n > 0}
		return nil
	})
	return out, err
}

// EnsureDefaultRole makes sure one platform role is the default, the role
// new accounts start with. When one already is, nothing changes — a default
// an administrator chose is never overridden. Otherwise the platform role
// named fallback becomes the default (roledom.ErrNotFound if it does not
// exist). It reports whether it changed anything.
func (r *RoleSeedRepository) EnsureDefaultRole(ctx context.Context, fallback string) (bool, error) {
	changed := false
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		changed = false
		if err := lockKey(ctx, tx, lockDefaultRole); err != nil {
			return err
		}
		var exists bool
		if err := tx.GetContext(ctx, &exists, `SELECT EXISTS (SELECT 1 FROM roles WHERE is_default)`); err != nil {
			return fmt.Errorf("role seed: find default: %w", err)
		}
		if exists {
			return nil
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE roles SET is_default = TRUE, updated_at = NOW()
			WHERE project_id IS NULL AND name = $1`, fallback)
		if err != nil {
			return fmt.Errorf("role seed: set default %s: %w", fallback, err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return roledom.ErrNotFound
		}
		changed = true
		return nil
	})
	return changed, err
}

// AttachPlatformIfMissing attaches the platform-wide role to the user without
// touching anything else the user holds. It reports whether a row was added.
func (r *RoleSeedRepository) AttachPlatformIfMissing(ctx context.Context, userID, roleID uuid.UUID) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id)
		VALUES ($1::uuid, 'user', $2::uuid, NULL)
		ON CONFLICT (role_id, principal_type, principal_id) WHERE project_id IS NULL DO NOTHING`,
		roleID.String(), userID.String())
	if err != nil {
		return false, fmt.Errorf("role seed: attach: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
