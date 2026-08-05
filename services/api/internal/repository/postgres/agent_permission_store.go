package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz"
)

// GetAgentProjectRoleName returns the role name for an agent member in a project.
func (s *AuthzPermissionStore) GetAgentProjectRoleName(ctx context.Context, agentID, projectID uuid.UUID) (string, error) {
	var row struct {
		RoleName string `db:"role_name"`
	}
	err := s.db.GetContext(ctx, &row, `
		SELECT pr.role_name
		FROM project_roles pr
		JOIN project_members pm ON pm.project_role_id = pr.id
		WHERE pm.agent_id = $1 AND pm.project_id = $2 AND pm.deleted_at IS NULL`,
		agentID.String(), projectID.String())
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("agent not found in project")
	}
	if err != nil {
		return "", fmt.Errorf("authz store: get agent project role name: %w", err)
	}
	return row.RoleName, nil
}

// ListAgentProjectPermissions returns permissions from project role memberships for
// an agent in the given project.
func (s *AuthzPermissionStore) ListAgentProjectPermissions(ctx context.Context, agentID, projectID uuid.UUID) ([]authz.Permission, error) {
	var rows []struct {
		Permissions []byte `db:"permissions"`
	}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT pr.permissions
		FROM project_roles pr
		JOIN project_members pm ON pm.project_role_id = pr.id
		WHERE pm.agent_id = $1 AND pm.project_id = $2 AND pm.deleted_at IS NULL`,
		agentID.String(), projectID.String())
	if err != nil {
		return nil, fmt.Errorf("authz store: list agent project permissions: %w", err)
	}

	return collectPermissions(rows), nil
}

// ListAgentGlobalPermissions returns permissions granted by a global agent's
// own global role (via agents.global_role_id) — mirrors ListGlobalPermissions
// for users. Only global-scope agents ever have global_role_id set; a
// project-scoped agent (or a global agent with no role assigned) simply has
// no matching row and gets zero permissions here.
func (s *AuthzPermissionStore) ListAgentGlobalPermissions(ctx context.Context, agentID uuid.UUID) ([]authz.Permission, error) {
	var rows []struct {
		Permissions []byte `db:"permissions"`
	}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT gr.permissions
		FROM global_roles gr
		JOIN agents a ON a.global_role_id = gr.id
		WHERE a.id = $1 AND a.agent_scope = 'global' AND a.deleted_at IS NULL`,
		agentID.String())
	if err != nil {
		return nil, fmt.Errorf("authz store: list agent global permissions: %w", err)
	}

	return collectPermissions(rows), nil
}
