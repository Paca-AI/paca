// Package postgres provides sqlx-backed repository implementations.
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

// userRecord is the sqlx write model for the users table. It mirrors the
// columns defined in 000001_init.sql.
type userRecord struct {
	ID                 string     `db:"id"`
	Username           string     `db:"username"`
	PasswordHash       string     `db:"password_hash"`
	FullName           string     `db:"full_name"`
	RoleID             string     `db:"role_id"`
	MustChangePassword bool       `db:"must_change_password"`
	CreatedAt          time.Time  `db:"created_at"`
	UpdatedAt          time.Time  `db:"updated_at"`
	DeletedAt          *time.Time `db:"deleted_at"`
}

// userReadRow is the result of a SELECT … JOIN global_roles used for all read
// operations so that the role name is always available alongside the FK.
type userReadRow struct {
	ID                 string     `db:"id"`
	Username           string     `db:"username"`
	PasswordHash       string     `db:"password_hash"`
	FullName           string     `db:"full_name"`
	RoleID             string     `db:"role_id"`
	RoleName           string     `db:"role_name"`
	MustChangePassword bool       `db:"must_change_password"`
	CreatedAt          time.Time  `db:"created_at"`
	UpdatedAt          time.Time  `db:"updated_at"`
	DeletedAt          *time.Time `db:"deleted_at"`
}

// userReadCols and userReadJoin are shared by all read queries.
const (
	userReadCols = `users.id, users.username, users.password_hash, users.full_name, users.role_id, users.must_change_password, users.created_at, users.updated_at, users.deleted_at, gr.name AS role_name`
	userReadJoin = `JOIN global_roles gr ON gr.id = users.role_id`
)

// UserRepository is the sqlx implementation of userdom.Repository.
type UserRepository struct {
	db *sqlx.DB
}

// NewUserRepository returns a new UserRepository.
func NewUserRepository(db *sqlx.DB) *UserRepository {
	return &UserRepository{db: db}
}

// List returns a page of non-deleted, non-system users ordered by creation
// date plus the total count across all pages.  The built-in agent bot account
// is excluded because it is an internal system identity, not a real user.
func (r *UserRepository) List(ctx context.Context, offset, limit int) ([]*userdom.User, int64, error) {
	total, err := r.CountUsers(ctx)
	if err != nil {
		return nil, 0, err
	}

	var rows []userReadRow
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT `+userReadCols+`
		FROM users
		`+userReadJoin+`
		WHERE users.deleted_at IS NULL AND users.username != '_paca_agent_bot'
		ORDER BY users.created_at ASC
		OFFSET $1 LIMIT $2`, offset, limit); err != nil {
		return nil, 0, fmt.Errorf("user repo: list: %w", err)
	}

	users := make([]*userdom.User, 0, len(rows))
	for i := range rows {
		users = append(users, rowToEntity(&rows[i]))
	}
	return users, total, nil
}

// CountUsers returns the total count of non-deleted, non-system users. The
// built-in agent bot account is excluded because it is an internal system
// identity, not a real user — matching List's filter.
func (r *UserRepository) CountUsers(ctx context.Context) (int64, error) {
	var total int64
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND username != '_paca_agent_bot'`); err != nil {
		return 0, fmt.Errorf("user repo: count users: %w", err)
	}
	return total, nil
}

// FindByID returns the user with the given primary key, or userdom.ErrNotFound.
func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*userdom.User, error) {
	var row userReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+userReadCols+`
		FROM users
		`+userReadJoin+`
		WHERE users.id = $1 AND users.deleted_at IS NULL`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, userdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user repo: find by id: %w", err)
	}
	return rowToEntity(&row), nil
}

// FindByUsername returns the user with the given username, or userdom.ErrNotFound.
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*userdom.User, error) {
	var row userReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+userReadCols+`
		FROM users
		`+userReadJoin+`
		WHERE users.username = $1 AND users.deleted_at IS NULL`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, userdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user repo: find by username: %w", err)
	}
	return rowToEntity(&row), nil
}

// FindByUsernameIncludingDeleted returns the user with the given username,
// including rows that were soft-deleted.
func (r *UserRepository) FindByUsernameIncludingDeleted(ctx context.Context, username string) (*userdom.User, error) {
	var row userReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+userReadCols+`
		FROM users
		`+userReadJoin+`
		WHERE users.username = $1`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, userdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user repo: find by username including deleted: %w", err)
	}
	return rowToEntity(&row), nil
}

// Create persists a new user record.
func (r *UserRepository) Create(ctx context.Context, u *userdom.User) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, full_name, role_id, must_change_password, created_at, updated_at, deleted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		u.ID.String(), u.Username, u.PasswordHash, u.FullName,
		u.RoleID.String(), u.MustChangePassword, u.CreatedAt, u.UpdatedAt, u.DeletedAt,
	)
	if err != nil {
		return fmt.Errorf("user repo: create: %w", err)
	}
	return nil
}

// Update saves changes to an existing user record.
func (r *UserRepository) Update(ctx context.Context, u *userdom.User) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET username = $1, password_hash = $2, full_name = $3, role_id = $4,
		  must_change_password = $5, updated_at = $6, deleted_at = $7
		WHERE id = $8`,
		u.Username, u.PasswordHash, u.FullName, u.RoleID.String(),
		u.MustChangePassword, u.UpdatedAt, u.DeletedAt, u.ID.String(),
	)
	if err != nil {
		return fmt.Errorf("user repo: update: %w", err)
	}
	return nil
}

// Delete soft-deletes the user by setting deleted_at.
func (r *UserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx, `UPDATE users SET deleted_at = $1 WHERE id = $2 AND deleted_at IS NULL`, now, id.String())
	if err != nil {
		return fmt.Errorf("user repo: delete: %w", err)
	}
	return nil
}

// -- mapping helpers ---------------------------------------------------------

func rowToEntity(row *userReadRow) *userdom.User {
	id, _ := uuid.Parse(row.ID)
	roleID, _ := uuid.Parse(row.RoleID)
	return &userdom.User{
		ID:                 id,
		Username:           row.Username,
		PasswordHash:       row.PasswordHash,
		FullName:           row.FullName,
		RoleID:             roleID,
		Role:               row.RoleName,
		MustChangePassword: row.MustChangePassword,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		DeletedAt:          row.DeletedAt,
	}
}
