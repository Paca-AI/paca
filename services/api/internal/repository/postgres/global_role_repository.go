package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

type globalRoleRecord struct {
	ID          string    `db:"id"`
	Name        string    `db:"name"`
	Permissions []byte    `db:"permissions"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// GlobalRoleRepository is the sqlx implementation of globalrole.Repository.
type GlobalRoleRepository struct {
	db *sqlx.DB
}

// NewGlobalRoleRepository returns a new GlobalRoleRepository.
func NewGlobalRoleRepository(db *sqlx.DB) *GlobalRoleRepository {
	return &GlobalRoleRepository{db: db}
}

const globalRoleSelectCols = `id, name, permissions, created_at, updated_at`

// List returns all global roles sorted by name.
func (r *GlobalRoleRepository) List(ctx context.Context) ([]*globalroledom.GlobalRole, error) {
	var records []globalRoleRecord
	if err := r.db.SelectContext(ctx, &records, `SELECT `+globalRoleSelectCols+` FROM global_roles ORDER BY name ASC`); err != nil {
		return nil, fmt.Errorf("global role repo: list: %w", err)
	}

	roles := make([]*globalroledom.GlobalRole, 0, len(records))
	for i := range records {
		role, err := toGlobalRoleEntity(&records[i])
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, nil
}

// FindByID returns a role by ID.
func (r *GlobalRoleRepository) FindByID(ctx context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
	var record globalRoleRecord
	err := r.db.GetContext(ctx, &record, `SELECT `+globalRoleSelectCols+` FROM global_roles WHERE id = $1`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, globalroledom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("global role repo: find by id: %w", err)
	}
	return toGlobalRoleEntity(&record)
}

// FindByName returns a role by exact name.
func (r *GlobalRoleRepository) FindByName(ctx context.Context, name string) (*globalroledom.GlobalRole, error) {
	var record globalRoleRecord
	err := r.db.GetContext(ctx, &record, `SELECT `+globalRoleSelectCols+` FROM global_roles WHERE name = $1`, strings.TrimSpace(name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, globalroledom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("global role repo: find by name: %w", err)
	}
	return toGlobalRoleEntity(&record)
}

// Create persists a new global role.
func (r *GlobalRoleRepository) Create(ctx context.Context, role *globalroledom.GlobalRole) error {
	rec, err := fromGlobalRoleEntity(role)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO global_roles (id, name, permissions, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)`,
		rec.ID, rec.Name, rec.Permissions, rec.CreatedAt, rec.UpdatedAt,
	)
	if err != nil {
		if isSSOAdminGuardViolation(err) {
			return settingsdom.ErrSSOAdminRequired
		}
		if isUniqueViolation(err) {
			return globalroledom.ErrNameTaken
		}
		return fmt.Errorf("global role repo: create: %w", err)
	}
	return nil
}

// Update saves changes to a role.
func (r *GlobalRoleRepository) Update(ctx context.Context, role *globalroledom.GlobalRole) error {
	rec, err := fromGlobalRoleEntity(role)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE global_roles SET name = $1, permissions = $2, updated_at = $3 WHERE id = $4`,
		rec.Name, rec.Permissions, rec.UpdatedAt, rec.ID,
	)
	if err != nil {
		if isSSOAdminGuardViolation(err) {
			return settingsdom.ErrSSOAdminRequired
		}
		if isUniqueViolation(err) {
			return globalroledom.ErrNameTaken
		}
		return fmt.Errorf("global role repo: update: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return globalroledom.ErrNotFound
	}
	return nil
}

// Delete removes a role and all user-role assignments pointing to it.
func (r *GlobalRoleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM global_roles WHERE id = $1`, id.String())
	if err != nil {
		if isSSOAdminGuardViolation(err) {
			return settingsdom.ErrSSOAdminRequired
		}
		return fmt.Errorf("global role repo: delete: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return globalroledom.ErrNotFound
	}
	return nil
}

// ReplaceUserRoles sets the single global role for a user (users.role_id).
// Exactly one roleID must be provided; the schema does not support multiple roles per user.
func (r *GlobalRoleRepository) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	normalized := normalizeUUIDs(roleIDs)
	if len(normalized) != 1 {
		return fmt.Errorf("global role repo: exactly one role id required, got %d", len(normalized))
	}
	roleID := normalized[0]

	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		var userCount int64
		if err := tx.GetContext(ctx, &userCount, `SELECT COUNT(*) FROM users WHERE id = $1 AND deleted_at IS NULL`, userID.String()); err != nil {
			return fmt.Errorf("global role repo: check user exists: %w", err)
		}
		if userCount == 0 {
			return userdom.ErrNotFound
		}

		var roleCount int64
		if err := tx.GetContext(ctx, &roleCount, `SELECT COUNT(*) FROM global_roles WHERE id = $1`, roleID); err != nil {
			return fmt.Errorf("global role repo: check role id: %w", err)
		}
		if roleCount == 0 {
			return globalroledom.ErrNotFound
		}

		result, err := tx.ExecContext(ctx, `UPDATE users SET role_id = $1, updated_at = $2 WHERE id = $3`, roleID, time.Now(), userID.String())
		if err != nil {
			return fmt.Errorf("global role repo: set user role: %w", err)
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			return userdom.ErrNotFound
		}
		return nil
	})
	if isSSOAdminGuardViolation(err) {
		return settingsdom.ErrSSOAdminRequired
	}
	return err
}

// ListUserRoles returns the single global role assigned to the provided user via users.role_id.
func (r *GlobalRoleRepository) ListUserRoles(ctx context.Context, userID uuid.UUID) ([]*globalroledom.GlobalRole, error) {
	var record globalRoleRecord
	err := r.db.GetContext(ctx, &record, `
		SELECT gr.id, gr.name, gr.permissions, gr.created_at, gr.updated_at
		FROM global_roles gr
		JOIN users u ON u.role_id = gr.id
		WHERE u.id = $1 AND u.deleted_at IS NULL`, userID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return []*globalroledom.GlobalRole{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("global role repo: list user roles: %w", err)
	}
	role, err := toGlobalRoleEntity(&record)
	if err != nil {
		return nil, err
	}
	return []*globalroledom.GlobalRole{role}, nil
}

// CountUsersWithRole returns the total number of non-deleted users with the given role_id.
func (r *GlobalRoleRepository) CountUsersWithRole(ctx context.Context, id uuid.UUID) (int64, error) {
	var count int64
	if err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM users WHERE role_id = $1 AND deleted_at IS NULL`, id.String()); err != nil {
		return 0, fmt.Errorf("global role repo: count users with role: %w", err)
	}
	return count, nil
}

func fromGlobalRoleEntity(role *globalroledom.GlobalRole) (*globalRoleRecord, error) {
	permissions := role.Permissions
	if permissions == nil {
		permissions = map[string]any{}
	}
	permissionsRaw, err := json.Marshal(permissions)
	if err != nil {
		return nil, fmt.Errorf("global role repo: marshal permissions: %w", err)
	}
	return &globalRoleRecord{
		ID:          role.ID.String(),
		Name:        strings.TrimSpace(role.Name),
		Permissions: permissionsRaw,
		CreatedAt:   role.CreatedAt,
		UpdatedAt:   role.UpdatedAt,
	}, nil
}

func toGlobalRoleEntity(record *globalRoleRecord) (*globalroledom.GlobalRole, error) {
	id, err := uuid.Parse(record.ID)
	if err != nil {
		return nil, fmt.Errorf("global role repo: parse id: %w", err)
	}
	permissions := map[string]any{}
	if len(record.Permissions) > 0 {
		if err := json.Unmarshal(record.Permissions, &permissions); err != nil {
			return nil, fmt.Errorf("global role repo: unmarshal permissions: %w", err)
		}
	}
	return &globalroledom.GlobalRole{
		ID:          id,
		Name:        record.Name,
		Permissions: permissions,
		CreatedAt:   record.CreatedAt,
		UpdatedAt:   record.UpdatedAt,
	}, nil
}

func normalizeUUIDs(ids []uuid.UUID) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		s := id.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique")
}

// uniqueViolationConstraint reports whether err is a Postgres unique-
// constraint violation (SQLSTATE 23505) and, if so, which constraint it
// violated — letting a repository disambiguate between two unique indexes
// on the same table (e.g. users has separate ones for username and email)
// instead of isUniqueViolation's cruder single yes/no, which can't tell them
// apart. Unlike isUniqueViolation's string match, this asserts the
// concrete *pgconn.PgError (via errors.As, which unwraps through
// database/sql's error wrapping) so it can read the constraint name pgx
// reports directly, without guessing from the message text.
func uniqueViolationConstraint(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName, true
	}
	return "", false
}

func isSSOAdminGuardViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514" && pgErr.ConstraintName == "paca_sso_admin_required"
}
