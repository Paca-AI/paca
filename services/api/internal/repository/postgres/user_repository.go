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
	Email              *string    `db:"email"`
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
	Email              *string    `db:"email"`
	RoleID             string     `db:"role_id"`
	RoleName           string     `db:"role_name"`
	MustChangePassword bool       `db:"must_change_password"`
	AvatarKey          *string    `db:"avatar_key"`
	AvatarThumbKey     *string    `db:"avatar_thumb_key"`
	CreatedAt          time.Time  `db:"created_at"`
	UpdatedAt          time.Time  `db:"updated_at"`
	DeletedAt          *time.Time `db:"deleted_at"`
}

// userReadCols and userReadJoin are shared by all read queries.
const (
	userReadCols = `users.id, users.username, users.password_hash, users.full_name, users.email, users.role_id, users.must_change_password, users.avatar_key, users.avatar_thumb_key, users.created_at, users.updated_at, users.deleted_at, gr.name AS role_name`
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

// FindByEmail returns the user with the given email, or userdom.ErrNotFound.
// Scoped to active users only, matching uni_users_email_active — a
// soft-deleted user's email is freed up for reuse, same as username.
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

// Create persists a new user record. A username/email that collides with an
// active row surfaces as the corresponding userdom sentinel (checked
// up front by the service layer already, but that pre-check can still lose
// a race to a concurrent request — see userRepoErr) rather than a raw
// constraint-violation error.
func (r *UserRepository) Create(ctx context.Context, u *userdom.User) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, full_name, email, role_id, must_change_password, created_at, updated_at, deleted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		u.ID.String(), u.Username, u.PasswordHash, u.FullName, u.Email,
		u.RoleID.String(), u.MustChangePassword, u.CreatedAt, u.UpdatedAt, u.DeletedAt,
	)
	if err != nil {
		return userRepoErr("create", err)
	}
	return nil
}

// Update saves changes to an existing user record. See Create's doc comment
// re: username/email uniqueness errors.
func (r *UserRepository) Update(ctx context.Context, u *userdom.User) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET username = $1, password_hash = $2, full_name = $3, email = $4, role_id = $5,
		  must_change_password = $6, avatar_key = $7, avatar_thumb_key = $8, updated_at = $9, deleted_at = $10
		WHERE id = $11`,
		u.Username, u.PasswordHash, u.FullName, u.Email, u.RoleID.String(),
		u.MustChangePassword, u.AvatarKey, u.AvatarThumbKey, u.UpdatedAt, u.DeletedAt, u.ID.String(),
	)
	if err != nil {
		return userRepoErr("update", err)
	}
	return nil
}

// userRepoErr maps a users-table write error to the matching userdom
// sentinel when it's a unique-constraint violation on username or email
// (see uniqueViolationConstraint), otherwise wrapping it generically. This
// is what makes the uniqueness pre-checks in service/user race-safe:
// usersvc.Service.checkEmailAvailable and its username equivalent check
// before the write, but two concurrent requests can both pass that check
// for the same value — the database's unique index is the actual guarantee,
// and this turns its violation into the same userdom.ErrUsernameTaken /
// userdom.ErrEmailTaken the pre-check itself returns, instead of an
// unhandled 500 built from a raw driver error string.
func userRepoErr(op string, err error) error {
	if constraint, ok := uniqueViolationConstraint(err); ok {
		switch constraint {
		case "uni_users_username_active":
			return userdom.ErrUsernameTaken
		case "uni_users_email_active":
			return userdom.ErrEmailTaken
		}
	}
	return fmt.Errorf("user repo: %s: %w", op, err)
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
		Email:              row.Email,
		RoleID:             roleID,
		Role:               row.RoleName,
		MustChangePassword: row.MustChangePassword,
		AvatarKey:          row.AvatarKey,
		AvatarThumbKey:     row.AvatarThumbKey,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		DeletedAt:          row.DeletedAt,
	}
}
