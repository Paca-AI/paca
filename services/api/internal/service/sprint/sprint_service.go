// Package sprintsvc implements sprint application services.
package sprintsvc

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
)

// Service is the concrete implementation of sprintdom.SprintService.
type Service struct {
	repo      sprintdom.SprintRepository
	taskRepo  taskdom.TaskRepository
	publisher *messaging.Publisher
}

// New returns a configured sprint service. publisher may be nil; real-time
// events are then skipped silently.
func New(repo sprintdom.SprintRepository, taskRepo taskdom.TaskRepository, publisher *messaging.Publisher) *Service {
	return &Service{repo: repo, taskRepo: taskRepo, publisher: publisher}
}

// publish sends a real-time pub/sub notification for a sprint change.
// Errors are silently swallowed so a messaging failure never blocks the
// primary HTTP response — this is how the frontend now learns to refresh
// its sprint list/detail instead of polling on an interval.
func (s *Service) publish(ctx context.Context, topic string, payload map[string]any) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.Publish(ctx, events.ChannelRealtime, map[string]any{
		"type":    topic,
		"payload": payload,
	})
}

// ListSprints returns all sprints for a project.
func (s *Service) ListSprints(ctx context.Context, projectID uuid.UUID) ([]*sprintdom.Sprint, error) {
	return s.repo.ListSprints(ctx, projectID)
}

// GetSprint returns the sprint with the given ID, verifying it belongs to projectID.
func (s *Service) GetSprint(ctx context.Context, projectID, id uuid.UUID) (*sprintdom.Sprint, error) {
	sp, err := s.repo.FindSprintByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sp.ProjectID != projectID {
		return nil, sprintdom.ErrSprintNotFound
	}
	return sp, nil
}

// CreateSprint creates a new sprint for the given project.
func (s *Service) CreateSprint(ctx context.Context, in sprintdom.CreateSprintInput) (*sprintdom.Sprint, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, sprintdom.ErrSprintNameInvalid
	}

	status := in.Status
	if status == "" {
		status = sprintdom.SprintStatusPlanned
	}
	if !sprintdom.ValidSprintStatuses[status] {
		return nil, sprintdom.ErrSprintStatusInvalid
	}

	now := time.Now()
	sp := &sprintdom.Sprint{
		ID:        uuid.New(),
		ProjectID: in.ProjectID,
		Name:      name,
		StartDate: in.StartDate,
		EndDate:   in.EndDate,
		Goal:      in.Goal,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.CreateSprint(ctx, sp); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicSprintCreated, map[string]any{
		"project_id": sp.ProjectID.String(),
		"sprint_id":  sp.ID.String(),
	})
	return sp, nil
}

// UpdateSprint updates the mutable fields of an existing sprint,
// verifying it belongs to projectID.
func (s *Service) UpdateSprint(ctx context.Context, projectID, id uuid.UUID, in sprintdom.UpdateSprintInput) (*sprintdom.Sprint, error) {
	sp, err := s.repo.FindSprintByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sp.ProjectID != projectID {
		return nil, sprintdom.ErrSprintNotFound
	}

	if name := strings.TrimSpace(in.Name); name != "" {
		sp.Name = name
	}
	if in.StartDate != nil {
		sp.StartDate = *in.StartDate
	}
	if in.EndDate != nil {
		sp.EndDate = *in.EndDate
	}
	if in.Goal != nil {
		sp.Goal = *in.Goal
	}
	if in.Status != nil {
		if !sprintdom.ValidSprintStatuses[*in.Status] {
			return nil, sprintdom.ErrSprintStatusInvalid
		}
		sp.Status = *in.Status
	}
	sp.UpdatedAt = time.Now()

	if err := s.repo.UpdateSprint(ctx, sp); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicSprintUpdated, map[string]any{
		"project_id": sp.ProjectID.String(),
		"sprint_id":  sp.ID.String(),
	})
	return sp, nil
}

// DeleteSprint removes a sprint by ID, verifying it belongs to projectID.
func (s *Service) DeleteSprint(ctx context.Context, projectID, id uuid.UUID) error {
	sp, err := s.repo.FindSprintByID(ctx, id)
	if err != nil {
		return err
	}
	if sp.ProjectID != projectID {
		return sprintdom.ErrSprintNotFound
	}
	if err := s.repo.DeleteSprint(ctx, id); err != nil {
		return err
	}
	s.publish(ctx, events.TopicSprintDeleted, map[string]any{
		"project_id": sp.ProjectID.String(),
		"sprint_id":  sp.ID.String(),
	})
	return nil
}

// CompleteSprint bulk-moves all non-done tasks out of the sprint and marks
// the sprint as completed in two sequential writes.  Tasks whose status
// has category "done" are left in place.  Verifies the sprint belongs to projectID.
func (s *Service) CompleteSprint(ctx context.Context, projectID, id uuid.UUID, in sprintdom.CompleteSprintInput) (*sprintdom.Sprint, error) {
	sp, err := s.repo.FindSprintByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sp.ProjectID != projectID {
		return nil, sprintdom.ErrSprintNotFound
	}
	if sp.Status == sprintdom.SprintStatusCompleted {
		return nil, sprintdom.ErrSprintAlreadyComplete
	}

	// Move non-done tasks first so a subsequent failure leaves the sprint
	// in its original state (retrying the complete is then still possible).
	if err := s.taskRepo.BulkMoveSprintTasks(ctx, sp.ProjectID, id, in.MoveToSprintID); err != nil {
		return nil, err
	}

	sp.Status = sprintdom.SprintStatusCompleted
	sp.UpdatedAt = time.Now()
	if err := s.repo.UpdateSprint(ctx, sp); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicSprintCompleted, map[string]any{
		"project_id": sp.ProjectID.String(),
		"sprint_id":  sp.ID.String(),
	})
	return sp, nil
}
