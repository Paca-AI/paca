// Package postgres — GORM implementation of sprintdom.SprintRepository.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	sprintdom "github.com/paca/api/internal/domain/sprint"
	"gorm.io/gorm"
)

// --- GORM model -------------------------------------------------------------

type sprintRecord struct {
	ID        string     `gorm:"primarykey;type:uuid"`
	ProjectID string     `gorm:"type:uuid;not null;column:project_id"`
	Name      string     `gorm:"not null"`
	StartDate *time.Time `gorm:"type:date;column:start_date"`
	EndDate   *time.Time `gorm:"type:date;column:end_date"`
	Goal      *string    `gorm:"type:text"`
	Status    string     `gorm:"not null;default:planned"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (sprintRecord) TableName() string { return "sprints" }

// --- Repository struct -------------------------------------------------------

// SprintRepository is the GORM implementation of sprintdom.SprintRepository.
type SprintRepository struct {
	db *gorm.DB
}

// NewSprintRepository returns a new SprintRepository.
func NewSprintRepository(db *gorm.DB) *SprintRepository {
	return &SprintRepository{db: db}
}

// --- Methods ----------------------------------------------------------------

// ListSprints returns all sprints for a project ordered by creation time.
func (r *SprintRepository) ListSprints(ctx context.Context, projectID uuid.UUID) ([]*sprintdom.Sprint, error) {
	var records []sprintRecord
	if err := r.db.WithContext(ctx).
		Where("project_id = ?", projectID.String()).
		Order("created_at ASC").
		Find(&records).Error; err != nil {
		return nil, fmt.Errorf("sprint repo: list: %w", err)
	}
	out := make([]*sprintdom.Sprint, 0, len(records))
	for i := range records {
		out = append(out, toSprintEntity(&records[i]))
	}
	return out, nil
}

// FindSprintByID returns the sprint with the given ID.
func (r *SprintRepository) FindSprintByID(ctx context.Context, id uuid.UUID) (*sprintdom.Sprint, error) {
	var rec sprintRecord
	err := r.db.WithContext(ctx).Where("id = ?", id.String()).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, sprintdom.ErrSprintNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("sprint repo: find by id: %w", err)
	}
	return toSprintEntity(&rec), nil
}

// CreateSprint persists a new sprint.
func (r *SprintRepository) CreateSprint(ctx context.Context, s *sprintdom.Sprint) error {
	rec := &sprintRecord{
		ID:        s.ID.String(),
		ProjectID: s.ProjectID.String(),
		Name:      s.Name,
		StartDate: s.StartDate,
		EndDate:   s.EndDate,
		Goal:      s.Goal,
		Status:    string(s.Status),
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(rec).Error; err != nil {
		return fmt.Errorf("sprint repo: create: %w", err)
	}
	return nil
}

// UpdateSprint persists changes to an existing sprint.
func (r *SprintRepository) UpdateSprint(ctx context.Context, s *sprintdom.Sprint) error {
	updates := map[string]any{
		"name":       s.Name,
		"start_date": s.StartDate,
		"end_date":   s.EndDate,
		"goal":       s.Goal,
		"status":     string(s.Status),
		"updated_at": s.UpdatedAt,
	}
	res := r.db.WithContext(ctx).Model(&sprintRecord{}).Where("id = ?", s.ID.String()).Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("sprint repo: update: %w", res.Error)
	}
	return nil
}

// DeleteSprint removes a sprint by ID.
func (r *SprintRepository) DeleteSprint(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Delete(&sprintRecord{}, "id = ?", id.String())
	if res.Error != nil {
		return fmt.Errorf("sprint repo: delete: %w", res.Error)
	}
	return nil
}

// --- Entity converter -------------------------------------------------------

func toSprintEntity(r *sprintRecord) *sprintdom.Sprint {
	id, _ := uuid.Parse(r.ID)
	pid, _ := uuid.Parse(r.ProjectID)
	return &sprintdom.Sprint{
		ID:        id,
		ProjectID: pid,
		Name:      r.Name,
		StartDate: r.StartDate,
		EndDate:   r.EndDate,
		Goal:      r.Goal,
		Status:    sprintdom.SprintStatus(r.Status),
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}
