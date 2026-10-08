// Package postgres provides sqlx-backed repository implementations.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	roledom "github.com/Paca-AI/api/internal/domain/role"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

// userReadRow is the result of every user read. The user's roles are not part
// of the row: they are attachments, loaded by loadUserRoles.
type userReadRow struct {
	ID                 string     `db:"id"`
	Username           string     `db:"username"`
	PasswordHash       string     `db:"password_hash"`
	FullName           string     `db:"full_name"`
	Email              *string    `db:"email"`
	MustChangePassword bool       `db:"must_change_password"`
	AvatarKey          *string    `db:"avatar_key"`
	AvatarThumbKey     *string    `db:"avatar_thumb_key"`
	CreatedAt          time.Time  `db:"created_at"`
	UpdatedAt          time.Time  `db:"updated_at"`
	DeletedAt          *time.Time `db:"deleted_at"`
}

// userReadCols is shared by all read queries.
const userReadCols = `users.id, users.username, users.password_hash, users.full_name, users.email, users.must_change_password, users.avatar_key, users.avatar_thumb_key, users.created_at, users.updated_at, users.deleted_at`

// UserRepository is the sqlx implementation of userdom.Repository.
type UserRepository struct {
	db *sqlx.DB
}

// NewUserRepository returns a new UserRepository.
func NewUserRepository(db *sqlx.DB) *UserRepository {
	return &UserRepository{db: db}
}

// userNameSortKey is the primary sort key of every user listing.
const userNameSortKey = `LOWER(COALESCE(NULLIF(users.full_name, ''), users.username))`

// userListWhere builds the WHERE clause (and its args) shared by List and
// ListAfter.
func userListWhere(filter userdom.ListFilter) (string, []any) {
	where := `users.deleted_at IS NULL AND users.username != '_paca_agent_bot'`
	var args []any
	if filter.Role != "" {
		args = append(args, filter.Role)
		where += fmt.Sprintf(` AND EXISTS (
			SELECT 1 FROM role_attachments ra JOIN roles r ON r.id = ra.role_id
			WHERE ra.principal_type = 'user' AND ra.principal_id = users.id
			  AND ra.project_id IS NULL AND r.name = $%d)`, len(args))
	}
	for _, word := range strings.Fields(filter.Search) {
		args = append(args, "%"+escapeLike(word)+"%")
		n := len(args)
		where += fmt.Sprintf(` AND (LOWER(users.username) LIKE LOWER($%[1]d) ESCAPE '\' OR LOWER(users.full_name) LIKE LOWER($%[1]d) ESCAPE '\' OR LOWER(COALESCE(users.email, '')) LIKE LOWER($%[1]d) ESCAPE '\')`, n)
	}
	return where, args
}

// List returns a page of non-deleted, non-system users matching filter,
// ordered by name, plus the count of matches across all pages. Every
// whitespace-separated search word must appear (case-insensitively) in the
// username, full name or email; Role is the exact name of a platform role
// attached to the user. The
// built-in agent bot account is excluded because it is an internal system
// identity, not a real user.
func (r *UserRepository) List(ctx context.Context, offset, limit int, filter userdom.ListFilter) ([]*userdom.User, int64, error) {
	where, args := userListWhere(filter)

	var total int64
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM users WHERE `+where, args...); err != nil {
		return nil, 0, fmt.Errorf("user repo: list count: %w", err)
	}

	args = append(args, limit, offset)
	var rows []userReadRow
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT `+userReadCols+`
		FROM users
		WHERE `+where+fmt.Sprintf(`
		ORDER BY `+userNameSortKey+`, LOWER(users.username), users.id
		LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...); err != nil {
		return nil, 0, fmt.Errorf("user repo: list: %w", err)
	}

	users, err := r.entitiesWithRoles(ctx, rows)
	if err != nil {
		return nil, 0, err
	}
	return users, total, nil
}

// ListAfter is the keyset-paginated counterpart of List: same filter and
// ordering, but it resumes after the user named by cursorAfter instead of
// using an offset, so pages stay stable while users are added or removed.
func (r *UserRepository) ListAfter(ctx context.Context, limit int, cursorAfter *string, filter userdom.ListFilter) ([]*userdom.User, bool, error) {
	if limit <= 0 {
		limit = 20
	}
	where, args := userListWhere(filter)
	if cursorAfter != nil {
		cur, err := userdom.DecodeCursor(*cursorAfter)
		if err != nil {
			return nil, false, err
		}
		// Soft-deleted rows still count: a user removed between page loads
		// must not invalidate the cursor. An id that never existed does.
		var exists bool
		if err := r.db.GetContext(ctx, &exists, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, cur.ID); err != nil {
			return nil, false, fmt.Errorf("user repo: list after: cursor lookup: %w", err)
		}
		if !exists {
			return nil, false, fmt.Errorf("%w: unknown user", userdom.ErrInvalidCursor)
		}
		args = append(args, cur.ID)
		where += fmt.Sprintf(` AND (`+userNameSortKey+`, LOWER(users.username), users.id) > (
			SELECT `+userNameSortKey+`, LOWER(users.username), users.id FROM users WHERE users.id = $%d)`, len(args))
	}
	args = append(args, limit+1)

	var rows []userReadRow
	if err := r.db.SelectContext(ctx, &rows, `
		SELECT `+userReadCols+`
		FROM users
		WHERE `+where+fmt.Sprintf(`
		ORDER BY `+userNameSortKey+`, LOWER(users.username), users.id
		LIMIT $%d`, len(args)), args...); err != nil {
		return nil, false, fmt.Errorf("user repo: list after: %w", err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	users, err := r.entitiesWithRoles(ctx, rows)
	if err != nil {
		return nil, false, err
	}
	return users, hasMore, nil
}

// escapeLike escapes the LIKE wildcards in s so it matches literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
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

// CountUsersMustChangePassword returns the total count of non-deleted,
// non-system users with must_change_password set. Matches CountUsers'
// filter (see its doc comment) plus the must_change_password condition.
func (r *UserRepository) CountUsersMustChangePassword(ctx context.Context) (int64, error) {
	var total int64
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND username != '_paca_agent_bot' AND must_change_password`); err != nil {
		return 0, fmt.Errorf("user repo: count users must change password: %w", err)
	}
	return total, nil
}

// FindByID returns the user with the given primary key, or userdom.ErrNotFound.
func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*userdom.User, error) {
	var row userReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+userReadCols+`
		FROM users
		WHERE users.id = $1 AND users.deleted_at IS NULL`, id.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, userdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user repo: find by id: %w", err)
	}
	return r.entityWithRoles(ctx, &row)
}

// FindByUsername returns the user with the given username, or userdom.ErrNotFound.
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*userdom.User, error) {
	var row userReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+userReadCols+`
		FROM users
		WHERE users.username = $1 AND users.deleted_at IS NULL`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, userdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user repo: find by username: %w", err)
	}
	return r.entityWithRoles(ctx, &row)
}

// FindByEmail returns the user with the given email, or userdom.ErrNotFound.
// Scoped to active users only, matching uni_users_email_active — a
// soft-deleted user's email is freed up for reuse, same as username. The
// match ignores case (addresses are case-insensitive in practice); should
// legacy rows differ only by case, an exact match wins.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*userdom.User, error) {
	var row userReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+userReadCols+`
		FROM users
		WHERE lower(users.email) = lower($1) AND users.deleted_at IS NULL
		ORDER BY users.email = $1 DESC, users.created_at
		LIMIT 1`, email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, userdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user repo: find by email: %w", err)
	}
	return r.entityWithRoles(ctx, &row)
}

// FindByUsernameIncludingDeleted returns the user with the given username,
// including rows that were soft-deleted.
func (r *UserRepository) FindByUsernameIncludingDeleted(ctx context.Context, username string) (*userdom.User, error) {
	var row userReadRow
	err := r.db.GetContext(ctx, &row, `
		SELECT `+userReadCols+`
		FROM users
		WHERE users.username = $1`, username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, userdom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user repo: find by username including deleted: %w", err)
	}
	return r.entityWithRoles(ctx, &row)
}

// Create persists a new user record together with its role attachments, in
// one transaction. u.Roles names the platform roles to attach; left empty, the
// account starts with the default role (roledom.ErrNoDefault when none is
// set). u.Roles is replaced by the roles actually attached.
//
// A username/email that collides with an active row surfaces as the
// corresponding userdom sentinel (checked up front by the service layer
// already, but that pre-check can still lose a race to a concurrent request —
// see userRepoErr) rather than a raw constraint-violation error.
func (r *UserRepository) Create(ctx context.Context, u *userdom.User) error {
	var attached []roledom.Summary
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO users (id, username, password_hash, full_name, email, must_change_password, created_at, updated_at, deleted_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			u.ID.String(), u.Username, u.PasswordHash, u.FullName, u.Email,
			u.MustChangePassword, u.CreatedAt, u.UpdatedAt, u.DeletedAt,
		); err != nil {
			return userRepoErr("create", err)
		}
		if len(u.Roles) == 0 {
			if _, err := attachDefaultRoleTx(ctx, tx, roledom.PrincipalUser, u.ID, true); err != nil {
				return err
			}
		} else {
			ids := make([]uuid.UUID, 0, len(u.Roles))
			for _, role := range u.Roles {
				ids = append(ids, role.ID)
			}
			if _, err := attachRolesTx(ctx, tx, roledom.PrincipalUser, u.ID, nil, ids, nil); err != nil {
				return err
			}
		}
		var err error
		attached, err = summariesTx(ctx, tx, roledom.PrincipalUser, u.ID, nil)
		return err
	})
	if err != nil {
		return err
	}
	u.Roles = attached
	return nil
}

// Update saves changes to an existing user record. It never touches the
// user's role attachments. See Create's doc comment re: username/email
// uniqueness errors.
func (r *UserRepository) Update(ctx context.Context, u *userdom.User) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE users SET username = $1, password_hash = $2, full_name = $3, email = $4,
		  must_change_password = $5, avatar_key = $6, avatar_thumb_key = $7, updated_at = $8, deleted_at = $9
		WHERE id = $10`,
		u.Username, u.PasswordHash, u.FullName, u.Email,
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
	return &userdom.User{
		ID:                 id,
		Username:           row.Username,
		PasswordHash:       row.PasswordHash,
		FullName:           row.FullName,
		Email:              row.Email,
		Roles:              []roledom.Summary{},
		MustChangePassword: row.MustChangePassword,
		AvatarKey:          row.AvatarKey,
		AvatarThumbKey:     row.AvatarThumbKey,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		DeletedAt:          row.DeletedAt,
	}
}

// entityWithRoles maps row and loads the user's platform roles.
func (r *UserRepository) entityWithRoles(ctx context.Context, row *userReadRow) (*userdom.User, error) {
	users, err := r.entitiesWithRoles(ctx, []userReadRow{*row})
	if err != nil {
		return nil, err
	}
	return users[0], nil
}

// entitiesWithRoles maps rows (keeping their order) and loads the platform
// roles of all of them in one query: the platform-wide attachments of each
// user, sorted by role name.
func (r *UserRepository) entitiesWithRoles(ctx context.Context, rows []userReadRow) ([]*userdom.User, error) {
	users := make([]*userdom.User, 0, len(rows))
	if len(rows) == 0 {
		return users, nil
	}
	ids := make([]string, 0, len(rows))
	byID := make(map[string]*userdom.User, len(rows))
	for i := range rows {
		u := rowToEntity(&rows[i])
		users = append(users, u)
		ids = append(ids, rows[i].ID)
		byID[rows[i].ID] = u
	}
	var attached []struct {
		UserID string `db:"user_id"`
		ID     string `db:"id"`
		Name   string `db:"name"`
	}
	if err := r.db.SelectContext(ctx, &attached, `
		SELECT ra.principal_id::text AS user_id, r.id::text AS id, r.name
		FROM role_attachments ra JOIN roles r ON r.id = ra.role_id
		WHERE ra.principal_type = 'user' AND ra.project_id IS NULL AND ra.principal_id = ANY($1::uuid[])
		ORDER BY r.name, r.id`, ids); err != nil {
		return nil, fmt.Errorf("user repo: load roles: %w", err)
	}
	for _, a := range attached {
		u, ok := byID[a.UserID]
		if !ok {
			continue
		}
		rid, err := uuid.Parse(a.ID)
		if err != nil {
			return nil, fmt.Errorf("user repo: load roles: bad role id %q: %w", a.ID, err)
		}
		u.Roles = append(u.Roles, roledom.Summary{ID: rid, Name: a.Name})
	}
	return users, nil
}
