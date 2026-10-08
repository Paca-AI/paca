package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"

	roledom "github.com/Paca-AI/api/internal/domain/role"
)

// The helpers below let the repositories that create a principal (a user, a
// project member, an agent) write that principal's role attachments in the
// same transaction, so no account or membership ever exists half set up.
//
// They check data integrity only — the roles exist and may be attached in the
// scope. Whether the caller may hand those roles out is decided by the
// roles:assign gate (middleware.RequireAssignRoles) before the request gets here.

// dedupeUUIDs returns ids without duplicates, in first-seen order.
func dedupeUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// attachRolesTx attaches the roles to the principal in one scope
// (projectID nil = platform-wide) and returns the distinct role ids it
// attached. A role that is already attached is left alone. It returns
// roledom.ErrNotAttachable when a role does not exist or cannot be attached
// in the scope: platform-wide only platform roles; inside a project the
// platform roles and that project's own roles.
func attachRolesTx(ctx context.Context, tx *sqlx.Tx, principalType string, principalID uuid.UUID,
	projectID *uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID,
) ([]uuid.UUID, error) {
	ids := dedupeUUIDs(roleIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	// FOR SHARE keeps a role from being deleted between the check and the insert.
	var found []struct {
		ID        string  `db:"id"`
		ProjectID *string `db:"project_id"`
		Policy    string  `db:"policy"`
	}
	if err := tx.SelectContext(ctx, &found, `SELECT id, project_id, policy FROM roles WHERE id = ANY($1::uuid[]) FOR SHARE`, uuidStrings(ids)); err != nil {
		return nil, fmt.Errorf("attach roles: load roles: %w", err)
	}
	owner := make(map[string]*string, len(found))
	template := make(map[string]bool, len(found))
	for _, f := range found {
		owner[f.ID] = f.ProjectID
		template[f.ID] = f.ProjectID == nil && roledom.PolicyIsProjectTemplate(json.RawMessage(f.Policy))
	}
	for _, id := range ids {
		o, ok := owner[id.String()]
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: unknown role %s", roledom.ErrNotAttachable, id)
		case o != nil && (projectID == nil || *o != projectID.String()):
			return nil, fmt.Errorf("%w: role %s belongs to another scope", roledom.ErrNotAttachable, id)
		case projectID == nil && template[id.String()]:
			return nil, fmt.Errorf("%w: role %s is a project template", roledom.ErrNotAttachable, id)
		}
	}

	insert := `
		INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id, created_by)
		VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5::uuid)
		ON CONFLICT (role_id, principal_type, principal_id) WHERE project_id IS NULL DO NOTHING`
	if projectID != nil {
		insert = `
		INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id, created_by)
		VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5::uuid)
		ON CONFLICT (role_id, principal_type, principal_id, project_id) WHERE project_id IS NOT NULL DO NOTHING`
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, insert, id.String(), principalType, principalID.String(),
			nullableUUID(projectID), nullableUUID(createdBy)); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				// created_by, the project or the role vanished meanwhile.
				return nil, fmt.Errorf("%w: %s", roledom.ErrNotAttachable, pgErr.ConstraintName)
			}
			return nil, fmt.Errorf("attach roles: insert: %w", err)
		}
	}
	return ids, nil
}

// attachDefaultRoleTx attaches the default platform role to the principal,
// platform-wide. With none set it returns roledom.ErrNoDefault when required,
// and attaches nothing otherwise. It returns the role id it attached (nil
// when it attached none).
func attachDefaultRoleTx(ctx context.Context, tx *sqlx.Tx, principalType string, principalID uuid.UUID, required bool) (*uuid.UUID, error) {
	var id string
	err := tx.GetContext(ctx, &id, `
		INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id)
		SELECT r.id, $1, $2::uuid, NULL FROM roles r WHERE r.is_default AND r.project_id IS NULL
		ON CONFLICT (role_id, principal_type, principal_id) WHERE project_id IS NULL DO NOTHING
		RETURNING role_id::text`, principalType, principalID.String())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Either no default role exists, or it was already attached.
			var exists bool
			if qerr := tx.GetContext(ctx, &exists, `SELECT EXISTS (SELECT 1 FROM roles WHERE is_default AND project_id IS NULL)`); qerr != nil {
				return nil, fmt.Errorf("attach default role: %w", qerr)
			}
			if !exists && required {
				return nil, roledom.ErrNoDefault
			}
			return nil, nil
		}
		return nil, fmt.Errorf("attach default role: %w", err)
	}
	parsed, perr := uuid.Parse(id)
	if perr != nil {
		return nil, fmt.Errorf("attach default role: bad id: %w", perr)
	}
	return &parsed, nil
}

// summariesTx returns the roles attached to the principal in one scope,
// sorted by name.
func summariesTx(ctx context.Context, q sqlx.QueryerContext, principalType string, principalID uuid.UUID, projectID *uuid.UUID) ([]roledom.Summary, error) {
	var rows []struct {
		ID   string `db:"id"`
		Name string `db:"name"`
	}
	if err := sqlx.SelectContext(ctx, q, &rows, `
		SELECT r.id, r.name FROM role_attachments ra JOIN roles r ON r.id = ra.role_id
		WHERE ra.principal_type = $1 AND ra.principal_id = $2::uuid AND ra.project_id IS NOT DISTINCT FROM $3::uuid
		ORDER BY r.name, r.id`, principalType, principalID.String(), nullableUUID(projectID)); err != nil {
		return nil, fmt.Errorf("role summaries: %w", err)
	}
	out := make([]roledom.Summary, 0, len(rows))
	for _, row := range rows {
		id, err := uuid.Parse(row.ID)
		if err != nil {
			return nil, fmt.Errorf("role summaries: bad id %q: %w", row.ID, err)
		}
		out = append(out, roledom.Summary{ID: id, Name: row.Name})
	}
	return out, nil
}
