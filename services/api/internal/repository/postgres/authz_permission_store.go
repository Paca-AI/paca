package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/paca/api/internal/platform/authz"
	"gorm.io/gorm"
)

// AuthzPermissionStore resolves effective permissions from persisted roles.
type AuthzPermissionStore struct {
	db *gorm.DB
}

// NewAuthzPermissionStore returns a new permission store.
func NewAuthzPermissionStore(db *gorm.DB) *AuthzPermissionStore {
	return &AuthzPermissionStore{db: db}
}

// ListGlobalPermissions returns permissions granted by the user's global role (via users.role_id).
func (s *AuthzPermissionStore) ListGlobalPermissions(ctx context.Context, userID uuid.UUID) ([]authz.Permission, error) {
	var rows []struct {
		Permissions []byte
	}
	err := s.db.WithContext(ctx).
		Table("global_roles gr").
		Select("gr.permissions").
		Joins("JOIN users u ON u.role_id = gr.id").
		Where("u.id = ? AND u.deleted_at IS NULL", userID.String()).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("authz store: list global permissions: %w", err)
	}

	return collectPermissions(rows), nil
}

// ListProjectPermissions returns permissions from project role memberships for
// the provided project.
func (s *AuthzPermissionStore) ListProjectPermissions(ctx context.Context, userID, projectID uuid.UUID) ([]authz.Permission, error) {
	var rows []struct {
		Permissions []byte
	}
	err := s.db.WithContext(ctx).
		Table("project_roles pr").
		Select("pr.permissions").
		Joins("JOIN project_members pm ON pm.project_role_id = pr.id").
		Where("pm.user_id = ? AND pm.project_id = ?", userID.String(), projectID.String()).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("authz store: list project permissions: %w", err)
	}

	return collectPermissions(rows), nil
}

func collectPermissions(rows []struct{ Permissions []byte }) []authz.Permission {
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
