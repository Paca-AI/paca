// Package postgres provides GORM-backed repository implementations.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/paca/api/internal/domain/user"
	"gorm.io/gorm"
)

// userRecord is the GORM model; it stays inside this package and is mapped to/
// from the domain entity at the boundary.
type userRecord struct {
	ID           string `gorm:"primarykey;type:uuid"`
	Email        string `gorm:"uniqueIndex;not null"`
	PasswordHash string `gorm:"not null"`
	Name         string
	Role         string `gorm:"not null;default:'USER'"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time `gorm:"index"`
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

func (r *UserRepository) FindByID(ctx context.Context, id uuid.UUID) (*user.User, error) {
	var rec userRecord
	result := r.db.WithContext(ctx).First(&rec, "id = ?", id.String())
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, user.ErrNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("user repo: find by id: %w", result.Error)
	}
	return toEntity(&rec), nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*user.User, error) {
	var rec userRecord
	result := r.db.WithContext(ctx).First(&rec, "email = ?", email)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, user.ErrNotFound
	}
	if result.Error != nil {
		return nil, fmt.Errorf("user repo: find by email: %w", result.Error)
	}
	return toEntity(&rec), nil
}

func (r *UserRepository) Create(ctx context.Context, u *user.User) error {
	rec := fromEntity(u)
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
		return fmt.Errorf("user repo: create: %w", err)
	}
	return nil
}

func (r *UserRepository) Update(ctx context.Context, u *user.User) error {
	rec := fromEntity(u)
	if err := r.db.WithContext(ctx).Save(rec).Error; err != nil {
		return fmt.Errorf("user repo: update: %w", err)
	}
	return nil
}

func (r *UserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&userRecord{}).
		Where("id = ?", id.String()).
		Update("deleted_at", &now)
	if result.Error != nil {
		return fmt.Errorf("user repo: delete: %w", result.Error)
	}
	return nil
}

// -- mapping helpers ---------------------------------------------------------

func toEntity(r *userRecord) *user.User {
	id, _ := uuid.Parse(r.ID)
	return &user.User{
		ID:           id,
		Email:        r.Email,
		PasswordHash: r.PasswordHash,
		Name:         r.Name,
		Role:         r.Role,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
		DeletedAt:    r.DeletedAt,
	}
}

func fromEntity(u *user.User) *userRecord {
	return &userRecord{
		ID:           u.ID.String(),
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		Name:         u.Name,
		Role:         u.Role,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
		DeletedAt:    u.DeletedAt,
	}
}
