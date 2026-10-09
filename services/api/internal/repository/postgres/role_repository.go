package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"

	roledom "github.com/Paca-AI/api/internal/domain/role"
)

// RoleRepository is the sqlx implementation of roledom.Repository over the
// roles and role_attachments tables.
type RoleRepository struct {
	db *sqlx.DB
}

// NewRoleRepository returns a RoleRepository.
func NewRoleRepository(db *sqlx.DB) *RoleRepository { return &RoleRepository{db: db} }

var _ roledom.Repository = (*RoleRepository)(nil)

// Advisory lock keys (transaction-scoped) that serialise the operations whose
// invariants span rows: the set of platform-wide full-access holders, and the
// single default role.
const (
	lockWildcardHolders = "paca.iam.wildcard_holders"
	lockDefaultRole     = "paca.iam.default_role"
)

type roleRecord struct {
	ID              string    `db:"id"`
	Name            string    `db:"name"`
	Description     string    `db:"description"`
	Policy          []byte    `db:"policy"`
	ProjectID       *string   `db:"project_id"`
	IsSystem        bool      `db:"is_system"`
	IsDefault       bool      `db:"is_default"`
	AttachmentCount int       `db:"attachment_count"`
	CreatedAt       time.Time `db:"created_at"`
	UpdatedAt       time.Time `db:"updated_at"`
}

const roleCols = `r.id, r.name, r.description, r.policy, r.project_id, r.is_system, r.is_default, r.created_at, r.updated_at`

func (rec *roleRecord) entity() (*roledom.Role, error) {
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return nil, fmt.Errorf("role repo: bad role id %q: %w", rec.ID, err)
	}
	r := &roledom.Role{
		ID: id, Name: rec.Name, Description: rec.Description, Policy: rec.Policy,
		IsSystem: rec.IsSystem, IsDefault: rec.IsDefault, AttachmentCount: rec.AttachmentCount,
		CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt,
	}
	if rec.ProjectID != nil {
		p, err := uuid.Parse(*rec.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("role repo: bad project id %q: %w", *rec.ProjectID, err)
		}
		r.ProjectID = &p
	}
	return r, nil
}

func entities(recs []roleRecord) ([]*roledom.Role, error) {
	out := make([]*roledom.Role, 0, len(recs))
	for i := range recs {
		r, err := recs[i].entity()
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// ListPlatform implements roledom.Repository.
func (r *RoleRepository) ListPlatform(ctx context.Context) ([]*roledom.Role, error) {
	var recs []roleRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+roleCols+`,
		       (SELECT COUNT(*) FROM role_attachments ra WHERE ra.role_id = r.id) AS attachment_count
		FROM roles r WHERE r.project_id IS NULL ORDER BY r.name`)
	if err != nil {
		return nil, fmt.Errorf("role repo: list platform: %w", err)
	}
	return entities(recs)
}

// ListForProject implements roledom.Repository.
func (r *RoleRepository) ListForProject(ctx context.Context, projectID uuid.UUID) ([]*roledom.Role, error) {
	var recs []roleRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+roleCols+`,
		       (SELECT COUNT(*) FROM role_attachments ra WHERE ra.role_id = r.id AND ra.project_id = $1::uuid) AS attachment_count
		FROM roles r
		WHERE r.project_id = $1::uuid
		   OR EXISTS (SELECT 1 FROM role_attachments ra WHERE ra.role_id = r.id AND ra.project_id = $1::uuid)
		ORDER BY (r.project_id IS NULL), r.name`, projectID.String())
	if err != nil {
		return nil, fmt.Errorf("role repo: list for project: %w", err)
	}
	return entities(recs)
}

// FindByID implements roledom.Repository.
func (r *RoleRepository) FindByID(ctx context.Context, id uuid.UUID, countProject *uuid.UUID) (*roledom.Role, error) {
	var rec roleRecord
	err := r.db.GetContext(ctx, &rec, `
		SELECT `+roleCols+`,
		       (SELECT COUNT(*) FROM role_attachments ra
		         WHERE ra.role_id = r.id AND ($2::uuid IS NULL OR ra.project_id = $2::uuid)) AS attachment_count
		FROM roles r WHERE r.id = $1::uuid`, id.String(), nullableUUID(countProject))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, roledom.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("role repo: find by id: %w", err)
	}
	return rec.entity()
}

// FindByIDs implements roledom.Repository.
func (r *RoleRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]*roledom.Role, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var recs []roleRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+roleCols+`, 0 AS attachment_count
		FROM roles r WHERE r.id = ANY($1::uuid[]) ORDER BY r.name`, uuidStrings(ids))
	if err != nil {
		return nil, fmt.Errorf("role repo: find by ids: %w", err)
	}
	return entities(recs)
}

func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return id.String()
}

func isRoleNameViolation(err error) bool {
	c, ok := uniqueViolationConstraint(err)
	return ok && (c == "uq_roles_platform_name" || c == "uq_roles_project_name")
}

// Create implements roledom.Repository.
func (r *RoleRepository) Create(ctx context.Context, role *roledom.Role) error {
	var rec struct {
		ID        string    `db:"id"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	err := r.db.GetContext(ctx, &rec, `
		INSERT INTO roles (name, description, policy, project_id)
		VALUES ($1, $2, $3::jsonb, $4::uuid)
		RETURNING id, created_at, updated_at`,
		role.Name, role.Description, string(role.Policy), nullableUUID(role.ProjectID))
	if err != nil {
		if isRoleNameViolation(err) {
			return roledom.ErrNameTaken
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return roledom.ErrProjectNotFound
		}
		return fmt.Errorf("role repo: create: %w", err)
	}
	id, err := uuid.Parse(rec.ID)
	if err != nil {
		return fmt.Errorf("role repo: create: bad id: %w", err)
	}
	role.ID, role.CreatedAt, role.UpdatedAt = id, rec.CreatedAt, rec.UpdatedAt
	return nil
}

// Update implements roledom.Repository.
func (r *RoleRepository) Update(ctx context.Context, role *roledom.Role) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if err := lockKey(ctx, tx, lockWildcardHolders); err != nil {
			return err
		}
		var locked string
		err := tx.GetContext(ctx, &locked, `SELECT id FROM roles WHERE id = $1::uuid FOR UPDATE`, role.ID.String())
		if errors.Is(err, sql.ErrNoRows) {
			return roledom.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("role repo: update (lock): %w", err)
		}
		before, err := wildcardHolders(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE roles SET name = $2, description = $3, policy = $4::jsonb, updated_at = NOW()
			WHERE id = $1::uuid`, role.ID.String(), role.Name, role.Description, string(role.Policy)); err != nil {
			if isRoleNameViolation(err) {
				return roledom.ErrNameTaken
			}
			return fmt.Errorf("role repo: update: %w", err)
		}
		return checkWildcardHolders(ctx, tx, before)
	})
}

// Delete implements roledom.Repository.
func (r *RoleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if err := lockKey(ctx, tx, lockWildcardHolders); err != nil {
			return err
		}
		var cur struct {
			IsSystem  bool `db:"is_system"`
			IsDefault bool `db:"is_default"`
		}
		err := tx.GetContext(ctx, &cur, `SELECT is_system, is_default FROM roles WHERE id = $1::uuid FOR UPDATE`, id.String())
		if errors.Is(err, sql.ErrNoRows) {
			return roledom.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("role repo: delete (lock): %w", err)
		}
		if cur.IsSystem {
			return roledom.ErrSystemRole
		}
		if cur.IsDefault {
			return roledom.ErrIsDefault
		}
		before, err := wildcardHolders(ctx, tx)
		if err != nil {
			return err
		}
		// role_attachments rows go with the role (ON DELETE CASCADE).
		if _, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE id = $1::uuid`, id.String()); err != nil {
			return fmt.Errorf("role repo: delete: %w", err)
		}
		return checkWildcardHolders(ctx, tx, before)
	})
}

// SetDefault implements roledom.Repository.
func (r *RoleRepository) SetDefault(ctx context.Context, id uuid.UUID) error {
	return WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		if err := lockKey(ctx, tx, lockDefaultRole); err != nil {
			return err
		}
		var cur struct {
			ProjectID *string `db:"project_id"`
		}
		err := tx.GetContext(ctx, &cur, `SELECT project_id FROM roles WHERE id = $1::uuid FOR UPDATE`, id.String())
		if errors.Is(err, sql.ErrNoRows) || (err == nil && cur.ProjectID != nil) {
			return roledom.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("role repo: set default (lock): %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE roles SET is_default = false, updated_at = NOW() WHERE is_default AND id <> $1::uuid`, id.String()); err != nil {
			return fmt.Errorf("role repo: set default (clear): %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE roles SET is_default = true, updated_at = NOW() WHERE id = $1::uuid AND NOT is_default`, id.String()); err != nil {
			return fmt.Errorf("role repo: set default (set): %w", err)
		}
		return nil
	})
}

// UserExists implements roledom.Repository.
func (r *RoleRepository) UserExists(ctx context.Context, id uuid.UUID) (bool, error) {
	return r.exists(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1::uuid AND deleted_at IS NULL)`, id)
}

// GlobalAgentExists implements roledom.Repository.
func (r *RoleRepository) GlobalAgentExists(ctx context.Context, id uuid.UUID) (bool, error) {
	return r.exists(ctx, `SELECT EXISTS (SELECT 1 FROM agents WHERE id = $1::uuid AND deleted_at IS NULL AND agent_scope = 'global')`, id)
}

// ProjectExists implements roledom.Repository.
func (r *RoleRepository) ProjectExists(ctx context.Context, id uuid.UUID) (bool, error) {
	return r.exists(ctx, `SELECT EXISTS (SELECT 1 FROM projects WHERE id = $1::uuid AND deleted_at IS NULL)`, id)
}

func (r *RoleRepository) exists(ctx context.Context, q string, id uuid.UUID) (bool, error) {
	var ok bool
	if err := r.db.GetContext(ctx, &ok, q, id.String()); err != nil {
		return false, fmt.Errorf("role repo: exists: %w", err)
	}
	return ok, nil
}

// FindMember implements roledom.Repository.
func (r *RoleRepository) FindMember(ctx context.Context, projectID, memberID uuid.UUID) (*roledom.Member, error) {
	var rec struct {
		ID          string `db:"id"`
		ProjectID   string `db:"project_id"`
		MemberType  string `db:"member_type"`
		PrincipalID string `db:"principal_id"`
	}
	err := r.db.GetContext(ctx, &rec, `
		SELECT pm.id, pm.project_id, pm.member_type, COALESCE(pm.user_id, pm.agent_id) AS principal_id
		FROM project_members pm
		WHERE pm.id = $1::uuid AND pm.project_id = $2::uuid AND pm.deleted_at IS NULL
		  AND CASE pm.member_type
		        WHEN 'agent' THEN EXISTS (SELECT 1 FROM agents a WHERE a.id = pm.agent_id AND a.deleted_at IS NULL)
		        ELSE EXISTS (SELECT 1 FROM users u WHERE u.id = pm.user_id AND u.deleted_at IS NULL)
		      END`, memberID.String(), projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, roledom.ErrMemberNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("role repo: find member: %w", err)
	}
	m := &roledom.Member{ID: memberID, ProjectID: projectID, PrincipalType: roledom.PrincipalUser}
	if rec.MemberType == "agent" {
		m.PrincipalType = roledom.PrincipalAgent
	}
	if m.PrincipalID, err = uuid.Parse(rec.PrincipalID); err != nil {
		return nil, fmt.Errorf("role repo: find member: bad principal id: %w", err)
	}
	return m, nil
}

// ListAttached implements roledom.Repository.
func (r *RoleRepository) ListAttached(ctx context.Context, principalType string, principalID uuid.UUID, projectID *uuid.UUID) ([]*roledom.Role, error) {
	var recs []roleRecord
	err := r.db.SelectContext(ctx, &recs, `
		SELECT `+roleCols+`,
		       (SELECT COUNT(*) FROM role_attachments x
		         WHERE x.role_id = r.id AND ($4::uuid IS NULL OR x.project_id = $4::uuid)) AS attachment_count
		FROM roles r
		WHERE r.id IN (SELECT ra.role_id FROM role_attachments ra
		                WHERE ra.principal_type = $1 AND ra.principal_id = $2::uuid
		                  AND ra.project_id IS NOT DISTINCT FROM $3::uuid)
		ORDER BY r.name`, principalType, principalID.String(), nullableUUID(projectID), nullableUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("role repo: list attached: %w", err)
	}
	return entities(recs)
}

// ReplaceAttachments implements roledom.Repository.
func (r *RoleRepository) ReplaceAttachments(ctx context.Context, in roledom.ReplaceAttachmentsInput) ([]uuid.UUID, error) {
	var changed []uuid.UUID
	err := WithTx(ctx, r.db, func(tx *sqlx.Tx) error {
		changed = nil
		if in.ProjectID == nil {
			if err := lockKey(ctx, tx, lockWildcardHolders); err != nil {
				return err
			}
		}
		// The requested roles must exist and be attachable in this scope. FOR
		// SHARE keeps them from being deleted while the attachments are written.
		var found []struct {
			ID        string  `db:"id"`
			ProjectID *string `db:"project_id"`
			Policy    string  `db:"policy"`
		}
		if len(in.RoleIDs) > 0 {
			if err := tx.SelectContext(ctx, &found, `SELECT id, project_id, policy FROM roles WHERE id = ANY($1::uuid[]) FOR SHARE`, uuidStrings(in.RoleIDs)); err != nil {
				return fmt.Errorf("role repo: replace (roles): %w", err)
			}
		}
		byID := make(map[string]*string, len(found))
		template := make(map[string]bool, len(found))
		for _, f := range found {
			byID[f.ID] = f.ProjectID
			template[f.ID] = f.ProjectID == nil && roledom.PolicyIsProjectTemplate(json.RawMessage(f.Policy))
		}
		for _, id := range in.RoleIDs {
			owner, ok := byID[id.String()]
			switch {
			case !ok:
				return fmt.Errorf("%w: unknown role %s", roledom.ErrNotAttachable, id)
			case owner != nil && (in.ProjectID == nil || *owner != in.ProjectID.String()):
				return fmt.Errorf("%w: role %s belongs to another scope", roledom.ErrNotAttachable, id)
			case in.ProjectID == nil && template[id.String()]:
				return fmt.Errorf("%w: role %s is a project template", roledom.ErrNotAttachable, id)
			}
		}

		var before int
		if in.ProjectID == nil {
			var err error
			if before, err = wildcardHolders(ctx, tx); err != nil {
				return err
			}
		}

		scope := nullableUUID(in.ProjectID)
		var existing []string
		if err := tx.SelectContext(ctx, &existing, `
			SELECT role_id::text FROM role_attachments
			WHERE principal_type = $1 AND principal_id = $2::uuid AND project_id IS NOT DISTINCT FROM $3::uuid`,
			in.PrincipalType, in.PrincipalID.String(), scope); err != nil {
			return fmt.Errorf("role repo: replace (existing): %w", err)
		}
		want := uuidStrings(in.RoleIDs)
		var stale []string
		for _, e := range existing {
			if !slices.Contains(want, e) {
				stale = append(stale, e)
			}
		}
		if len(stale) > 0 {
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM role_attachments
				WHERE principal_type = $1 AND principal_id = $2::uuid AND project_id IS NOT DISTINCT FROM $3::uuid
				  AND role_id = ANY($4::uuid[])`,
				in.PrincipalType, in.PrincipalID.String(), scope, stale); err != nil {
				return fmt.Errorf("role repo: replace (delete): %w", err)
			}
		}
		for _, w := range want {
			if slices.Contains(existing, w) {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id, created_by)
				VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5::uuid)`,
				w, in.PrincipalType, in.PrincipalID.String(), scope, nullableUUID(in.CreatedBy)); err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "23503" {
					// created_by or project vanished, or the role was deleted
					// between the check and the insert.
					return fmt.Errorf("%w: %s", roledom.ErrNotAttachable, pgErr.ConstraintName)
				}
				return fmt.Errorf("role repo: replace (insert): %w", err)
			}
			if id, perr := uuid.Parse(w); perr == nil {
				changed = append(changed, id)
			}
		}
		for _, s := range stale {
			if id, perr := uuid.Parse(s); perr == nil {
				changed = append(changed, id)
			}
		}
		if in.ProjectID == nil {
			return checkWildcardHolders(ctx, tx, before)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}

// RolePolicies implements roledom.Repository.
func (r *RoleRepository) RolePolicies(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]byte, error) {
	out := make(map[uuid.UUID][]byte, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID     string `db:"id"`
		Policy []byte `db:"policy"`
	}
	if err := r.db.SelectContext(ctx, &rows, `SELECT id, policy FROM roles WHERE id = ANY($1::uuid[])`, uuidStrings(ids)); err != nil {
		return nil, fmt.Errorf("role repo: role policies: %w", err)
	}
	for _, row := range rows {
		id, err := uuid.Parse(row.ID)
		if err != nil {
			return nil, fmt.Errorf("role repo: role policies: bad id %q: %w", row.ID, err)
		}
		out[id] = row.Policy
	}
	return out, nil
}

func lockKey(ctx context.Context, tx *sqlx.Tx, key string) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key); err != nil {
		return fmt.Errorf("role repo: lock: %w", err)
	}
	return nil
}

// wildcardHoldersSQL counts the accounts that hold, platform-wide, a platform
// role with an unconditional Allow of every action on every resource:
// live users that can sign in (password_hash '!' marks the seeded agent-bot
// account, which cannot) — the people who can administer the workspace.
const wildcardHoldersSQL = `
SELECT COUNT(DISTINCT ra.principal_id)
FROM role_attachments ra
JOIN roles r ON r.id = ra.role_id
JOIN users u ON u.id = ra.principal_id
WHERE ra.principal_type = 'user'
  AND ra.project_id IS NULL
  AND r.project_id IS NULL
  AND u.deleted_at IS NULL
  AND u.password_hash <> '!'
  AND EXISTS (
        SELECT 1
        FROM jsonb_array_elements(CASE WHEN jsonb_typeof(r.policy -> 'statements') = 'array'
                                       THEN r.policy -> 'statements' ELSE '[]'::jsonb END) AS s(stmt)
        WHERE s.stmt ->> 'effect' = 'Allow'
          AND s.stmt -> 'actions' @> '"*"'::jsonb
          AND s.stmt -> 'resources' @> '"*"'::jsonb
          AND COALESCE(s.stmt -> 'conditions', '{}'::jsonb) = '{}'::jsonb)`

func wildcardHolders(ctx context.Context, tx *sqlx.Tx) (int, error) {
	var n int
	if err := tx.GetContext(ctx, &n, wildcardHoldersSQL); err != nil {
		return 0, fmt.Errorf("role repo: count full-access holders: %w", err)
	}
	return n, nil
}

// checkWildcardHolders refuses (by returning ErrLastWildcard, which rolls the
// transaction back) a change that took the number of full-access holders from
// at least one to none. A workspace that had no holder to begin with is not
// blocked.
func checkWildcardHolders(ctx context.Context, tx *sqlx.Tx, before int) error {
	if before == 0 {
		return nil
	}
	after, err := wildcardHolders(ctx, tx)
	if err != nil {
		return err
	}
	if after == 0 {
		return roledom.ErrLastWildcard
	}
	return nil
}
