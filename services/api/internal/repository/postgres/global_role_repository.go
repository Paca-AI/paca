package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
	userdom "github.com/paca/api/internal/domain/user"
	"gorm.io/gorm"
)

type globalRoleRecord struct {
	ID          string `gorm:"primarykey;type:uuid"`
	Name        string `gorm:"uniqueIndex;not null"`
	Permissions []byte `gorm:"type:jsonb;not null"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (globalRoleRecord) TableName() string { return "global_roles" }

// GlobalRoleRepository is the GORM implementation of globalrole.Repository.
type GlobalRoleRepository struct {
	db *gorm.DB
}

// NewGlobalRoleRepository returns a new GlobalRoleRepository.
func NewGlobalRoleRepository(db *gorm.DB) *GlobalRoleRepository {
	return &GlobalRoleRepository{db: db}
}

// List returns all global roles sorted by name.
func (r *GlobalRoleRepository) List(ctx context.Context) ([]*globalroledom.GlobalRole, error) {
	var records []globalRoleRecord
	if err := r.db.WithContext(ctx).Order("name asc").Find(&records).Error; err != nil {
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
	result := r.db.WithContext(ctx).First(&record, "id = ?", id.String())
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, globalroledom.ErrNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("global role repo: find by id: %w", result.Error)
	}
	return toGlobalRoleEntity(&record)
}

// FindByName returns a role by exact name.
func (r *GlobalRoleRepository) FindByName(ctx context.Context, name string) (*globalroledom.GlobalRole, error) {
	var record globalRoleRecord
	result := r.db.WithContext(ctx).First(&record, "name = ?", strings.TrimSpace(name))
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, globalroledom.ErrNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("global role repo: find by name: %w", result.Error)
	}
	return toGlobalRoleEntity(&record)
}

// Create persists a new global role.
func (r *GlobalRoleRepository) Create(ctx context.Context, role *globalroledom.GlobalRole) error {
	rec, err := fromGlobalRoleEntity(role)
	if err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
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
	result := r.db.WithContext(ctx).
		Model(&globalRoleRecord{}).
		Where("id = ?", rec.ID).
		Updates(map[string]any{
			"name":        rec.Name,
			"permissions": rec.Permissions,
			"updated_at":  rec.UpdatedAt,
		})
	if result.Error != nil {
		if isUniqueViolation(result.Error) {
			return globalroledom.ErrNameTaken
		}
		return fmt.Errorf("global role repo: update: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return globalroledom.ErrNotFound
	}
	return nil
}

// Delete removes a role and all user-role assignments pointing to it.
func (r *GlobalRoleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Delete(&globalRoleRecord{}, "id = ?", id.String())
	if result.Error != nil {
		return fmt.Errorf("global role repo: delete: %w", result.Error)
	}
	if result.RowsAffected == 0 {
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

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var userCount int64
		if err := tx.Table("users").Where("id = ? AND deleted_at IS NULL", userID.String()).Count(&userCount).Error; err != nil {
			return fmt.Errorf("global role repo: check user exists: %w", err)
		}
		if userCount == 0 {
			return userdom.ErrNotFound
		}

		var roleCount int64
		if err := tx.Model(&globalRoleRecord{}).Where("id = ?", roleID).Count(&roleCount).Error; err != nil {
			return fmt.Errorf("global role repo: check role id: %w", err)
		}
		if roleCount == 0 {
			return globalroledom.ErrNotFound
		}

		result := tx.Table("users").Where("id = ?", userID.String()).Updates(map[string]any{
			"role_id":    roleID,
			"updated_at": time.Now(),
		})
		if result.Error != nil {
			return fmt.Errorf("global role repo: set user role: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return userdom.ErrNotFound
		}
		return nil
	})
}

// ListUserRoles returns the single global role assigned to the provided user via users.role_id.
func (r *GlobalRoleRepository) ListUserRoles(ctx context.Context, userID uuid.UUID) ([]*globalroledom.GlobalRole, error) {
	var record globalRoleRecord
	err := r.db.WithContext(ctx).
		Model(&globalRoleRecord{}).
		Select("global_roles.*").
		Joins("JOIN users u ON u.role_id = global_roles.id").
		Where("u.id = ? AND u.deleted_at IS NULL", userID.String()).
		First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
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
	if err := r.db.WithContext(ctx).
		Table("users").
		Where("role_id = ? AND deleted_at IS NULL", id.String()).
		Count(&count).Error; err != nil {
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
