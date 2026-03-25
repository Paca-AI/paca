// Package postgres provides GORM-backed repository implementations.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
	"gorm.io/gorm"
)

// userRecord is the GORM model; it stays inside this package and is mapped to/
// from the domain entity at the boundary.
type userRecord struct {
	ID           string `gorm:"primarykey;type:uuid"`
	Username     string `gorm:"uniqueIndex;not null"`
	PasswordHash string `gorm:"not null"`
	FullName     string `gorm:"column:full_name"`
	Role         string `gorm:"not null;default:'USER'"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

func (userRecord) TableName() string { return "users" }

// UserRepository is the GORM implementation of user.Repository.
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository returns a new UserRepository.
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// FindByID returns the user with the given primary key, or userdom.ErrNotFound.
func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*userdom.User, error) {
	var rec userRecord
	result := r.db.WithContext(ctx).First(&rec, "id = ?", id.String())
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, userdom.ErrNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("user repo: find by id: %w", result.Error)
	}
	return toEntity(&rec), nil
}

// FindByUsername returns the user with the given username, or userdom.ErrNotFound.
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*userdom.User, error) {
	var rec userRecord
	result := r.db.WithContext(ctx).First(&rec, "username = ?", username)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, userdom.ErrNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("user repo: find by username: %w", result.Error)
	}
	return toEntity(&rec), nil
}

// Create persists a new user record.
func (r *UserRepository) Create(ctx context.Context, u *userdom.User) error {
	rec := fromEntity(u)
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
		return fmt.Errorf("user repo: create: %w", err)
	}
	return nil
}

// Update saves changes to an existing user record.
func (r *UserRepository) Update(ctx context.Context, u *userdom.User) error {
	rec := fromEntity(u)
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

func toEntity(r *userRecord) *userdom.User {
	id, _ := uuid.Parse(r.ID)
	var deletedAt *time.Time
	if r.DeletedAt.Valid {
		deletedAt = &r.DeletedAt.Time
	}
	return &userdom.User{
		ID:           id,
		Username:     r.Username,
		PasswordHash: r.PasswordHash,
		FullName:     r.FullName,
		Role:         r.Role,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		DeletedAt:    deletedAt,
	}
}

func fromEntity(u *userdom.User) *userRecord {
	var deletedAt gorm.DeletedAt
	if u.DeletedAt != nil {
		deletedAt = gorm.DeletedAt{Time: *u.DeletedAt, Valid: true}
	}
	return &userRecord{
		ID:           u.ID.String(),
		Username:     u.Username,
		PasswordHash: u.PasswordHash,
		FullName:     u.FullName,
		Role:         u.Role,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
		DeletedAt:    deletedAt,
	}
}
