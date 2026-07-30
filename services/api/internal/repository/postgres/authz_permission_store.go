package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
// the provided project.
func (s *AuthzPermissionStore) ListProjectPermissions(ctx context.Context, userID, projectID uuid.UUID) ([]authz.Permission, error) {
	var rows []struct {
		Permissions []byte `db:"permissions"`
	}
	err := s.db.SelectContext(ctx, &rows, `
		SELECT pr.permissions
		FROM project_roles pr
		JOIN project_members pm ON pm.project_role_id = pr.id
		WHERE pm.user_id = $1 AND pm.project_id = $2`, userID.String(), projectID.String())
	if err != nil {
		return nil, fmt.Errorf("authz store: list project permissions: %w", err)
	}

	return collectPermissions(rows), nil
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

func permissionsFromJSON(raw []byte) []authz.Permission {
	if len(raw) == 0 {
		return nil
	}

	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}

	seen := map[authz.Permission]struct{}{}
	out := make([]authz.Permission, 0)

	add := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" {
			return
		}
		p := authz.Permission(k)
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	switch v := payload.(type) {
	case map[string]any:
		for key, enabled := range v {
			switch e := enabled.(type) {
			case bool:
				if e {
					add(key)
				}
			case float64:
				if e != 0 {
					add(key)
				}
			case string:
				if strings.EqualFold(e, "true") {
					add(key)
				}
			}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				add(s)
			}
		}
	}

	return out
}
