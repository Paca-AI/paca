package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/platform/authz"
)

// AuthzPermissionStore resolves effective permissions from persisted roles.
type AuthzPermissionStore struct {
	db *sqlx.DB
}

// NewAuthzPermissionStore returns a new permission store.
func NewAuthzPermissionStore(db *sqlx.DB) *AuthzPermissionStore {
	return &AuthzPermissionStore{db: db}
}

// ListGlobalPermissions returns permissions granted by the user's global role (via users.role_id).
func (s *AuthzPermissionStore) ListGlobalPermissions(ctx context.Context, userID uuid.UUID) ([]authz.Permission, error) {
	var rows []struct {
		Permissions []byte `db:"permissions"`
	}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT gr.permissions
		FROM global_roles gr
		JOIN users u ON u.role_id = gr.id
		WHERE u.id = $1 AND u.deleted_at IS NULL`, userID.String())
	if err != nil {
		return nil, fmt.Errorf("authz store: list global permissions: %w", err)
	}

	return collectPermissions(rows), nil
}

// ListProjectPermissions returns permissions from project role memberships for
// the provided project. Any active membership implies projects.read
// regardless of what the role's own stored permissions say — a project
// member who can't fetch the project they belong to (e.g. a hand-edited
// custom role that never included projects.read) is a contradiction the
// role editor shouldn't be able to produce, so membership itself is the
// authorization signal for that one permission. Every other capability
// still comes strictly from the role's permissions, unchanged.
func (s *AuthzPermissionStore) ListProjectPermissions(ctx context.Context, userID, projectID uuid.UUID) ([]authz.Permission, error) {
	var rows []struct {
		Permissions []byte `db:"permissions"`
	}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT pr.permissions
		FROM project_roles pr
		JOIN project_members pm ON pm.project_role_id = pr.id
		WHERE pm.user_id = $1 AND pm.project_id = $2 AND pm.deleted_at IS NULL`,
		userID.String(), projectID.String())
	if err != nil {
		return nil, fmt.Errorf("authz store: list project permissions: %w", err)
	}

	perms := collectPermissions(rows)
	if len(rows) > 0 {
		perms = append(perms, authz.PermissionProjectsRead)
	}
	return perms, nil
}

func collectPermissions(rows []struct {
	Permissions []byte `db:"permissions"`
}) []authz.Permission {
	seen := map[authz.Permission]struct{}{}
	out := make([]authz.Permission, 0)
	for _, row := range rows {
		for _, p := range permissionsFromJSON(row.Permissions) {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

// permissionsFromJSON decodes a persisted permissions blob and returns the
// permissions it grants. The key/value rules themselves live in
// authz.PermissionsFromValue — the same parser the authorization guards use
// (authz.PermissionsGrantAll) — so this resolver and those guards can never
// drift on what a role grants. That drift is exactly what let a
// whitespace-padded "*" resolve to PermissionAll here while a hand-rolled
// guard elsewhere failed to recognize it.
func permissionsFromJSON(raw []byte) []authz.Permission {
	if len(raw) == 0 {
		return nil
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	return authz.PermissionsFromValue(payload)
}
