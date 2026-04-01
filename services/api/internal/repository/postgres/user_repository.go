// Package postgres provides GORM-backed repository implementations.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
	"gorm.io/gorm"
)

// userRecord is the GORM write model for the users table. It mirrors the
// columns defined in 000001_init.sql.
type userRecord struct {
	ID                 string `gorm:"primarykey;type:uuid"`
	Username           string `gorm:"uniqueIndex;not null"`
	PasswordHash       string `gorm:"not null"`
	FullName           string `gorm:"column:full_name"`
	RoleID             string `gorm:"column:role_id;type:uuid;not null"`
	MustChangePassword bool   `gorm:"column:must_change_password;not null;default:false"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          gorm.DeletedAt `gorm:"index"`
}

func (userRecord) TableName() string { return "users" }

// userReadRow is the result of a SELECT … JOIN global_roles used for all read
// operations so that the role name is always available alongside the FK.
type userReadRow struct {
	ID                 string
	Username           string
	PasswordHash       string
	FullName           string
	RoleID             string
	RoleName           string
	MustChangePassword bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
	DeletedAt          gorm.DeletedAt
}

// userReadCols and userReadJoin are shared by all read queries.
const (
	userReadCols = "users.id, users.username, users.password_hash, users.full_name, users.role_id, users.must_change_password, users.created_at, users.updated_at, users.deleted_at, gr.name AS role_name"
	userReadJoin = "JOIN global_roles gr ON gr.id = users.role_id"
)

// UserRepository is the GORM implementation of userdom.Repository.
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository returns a new UserRepository.
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// List returns a page of non-deleted users ordered by creation date plus the
// total count across all pages.
func (r *UserRepository) List(ctx context.Context, offset, limit int) ([]*userdom.User, int64, error) {
	var total int64
	if err := r.db.WithContext(ctx).
		Table("users").
		Where("deleted_at IS NULL").
		Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("user repo: list count: %w", err)
	}

	var rows []userReadRow
	if err := r.db.WithContext(ctx).
		Select(userReadCols).
		Table("users").
		Joins(userReadJoin).
		Where("users.deleted_at IS NULL").
		Order("users.created_at ASC").
		Offset(offset).
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("user repo: list: %w", err)
	}

	users := make([]*userdom.User, 0, len(rows))
	for i := range rows {
		users = append(users, rowToEntity(&rows[i]))
	}
	return users, total, nil
}

// FindByID returns the user with the given primary key, or userdom.ErrNotFound.
func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*userdom.User, error) {
	var row userReadRow
	result := r.db.WithContext(ctx).
		Select(userReadCols).
		Table("users").
		Joins(userReadJoin).
		Where("users.id = ? AND users.deleted_at IS NULL", id.String()).
		Scan(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("user repo: find by id: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, userdom.ErrNotFound
	}
	return rowToEntity(&row), nil
}

// FindByUsername returns the user with the given username, or userdom.ErrNotFound.
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*userdom.User, error) {
	var row userReadRow
	result := r.db.WithContext(ctx).
		Select(userReadCols).
		Table("users").
		Joins(userReadJoin).
		Where("users.username = ? AND users.deleted_at IS NULL", username).
		Scan(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("user repo: find by username: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, userdom.ErrNotFound
	}
	return rowToEntity(&row), nil
}

// FindByUsernameIncludingDeleted returns the user with the given username,
// including rows that were soft-deleted.
func (r *UserRepository) FindByUsernameIncludingDeleted(ctx context.Context, username string) (*userdom.User, error) {
	var row userReadRow
	result := r.db.WithContext(ctx).
		Unscoped().
		Select(userReadCols).
		Table("users").
		Joins(userReadJoin).
		Where("users.username = ?", username).
		Scan(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("user repo: find by username including deleted: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, userdom.ErrNotFound
	}
	return rowToEntity(&row), nil
}

// Create persists a new user record.
func (r *UserRepository) Create(ctx context.Context, u *userdom.User) error {
	rec := entityToRecord(u)
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
		return fmt.Errorf("user repo: create: %w", err)
	}
	return nil
}

// Update saves changes to an existing user record.
func (r *UserRepository) Update(ctx context.Context, u *userdom.User) error {
	rec := entityToRecord(u)
	if err := r.db.WithContext(ctx).Save(rec).Error; err != nil {
		return fmt.Errorf("user repo: update: %w", err)
	}
	return nil
}

// Delete soft-deletes the user by setting deleted_at via GORM's built-in mechanism.
func (r *UserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Where("id = ?", id.String()).
		Delete(&userRecord{})
	if result.Error != nil {
		return fmt.Errorf("user repo: delete: %w", result.Error)
	}
	return nil
}

// -- mapping helpers ---------------------------------------------------------

func rowToEntity(row *userReadRow) *userdom.User {
	id, _ := uuid.Parse(row.ID)
	roleID, _ := uuid.Parse(row.RoleID)
	var deletedAt *time.Time
	if row.DeletedAt.Valid {
		deletedAt = &row.DeletedAt.Time
	}
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
		DeletedAt:          deletedAt,
	}
}

func entityToRecord(u *userdom.User) *userRecord {
	var deletedAt gorm.DeletedAt
	if u.DeletedAt != nil {
		deletedAt = gorm.DeletedAt{Time: *u.DeletedAt, Valid: true}
	}
	return &userRecord{
		ID:                 u.ID.String(),
		Username:           u.Username,
		PasswordHash:       u.PasswordHash,
		FullName:           u.FullName,
		RoleID:             u.RoleID.String(),
		MustChangePassword: u.MustChangePassword,
		CreatedAt:          u.CreatedAt,
		UpdatedAt:          u.UpdatedAt,
		DeletedAt:          deletedAt,
	}
}
