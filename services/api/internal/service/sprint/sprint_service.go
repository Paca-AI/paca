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

// publishSprintActivity durably records a sprint reaching eventType on
// StreamSprintActivities, alongside (not instead of) publish's pub/sub
// notification — see that stream's docstring for why a sprint trigger needs
// this rather than the pub/sub channel. eventType must match one of the
// TriggerSprint* constants in internal/domain/automation/entity.go exactly;
// kept as a plain string here rather than importing that package, the same
// loose coupling task activities already have from automation's trigger
// matching.
//
// Carries a full field snapshot rather than just sp.ID so the consumer never
// needs to re-fetch the sprint from Postgres to build the walk's context —
// which matters most for sprint_deleted: DeleteSprint hard-deletes the row
// (no deleted_at column) before this is called, so a re-fetch-by-ID would
// always 404 for that one event type specifically.
func (s *Service) publishSprintActivity(ctx context.Context, eventType string, sp *sprintdom.Sprint) {
	if s.publisher == nil {
		return
	}
	fields := map[string]any{
		"sprint_id":  sp.ID.String(),
		"project_id": sp.ProjectID.String(),
		"event_type": eventType,
		"name":       sp.Name,
		"status":     string(sp.Status),
	}
	if sp.Goal != nil {
		fields["goal"] = *sp.Goal
	}
	if sp.StartDate != nil {
		fields["start_date"] = sp.StartDate.Format(time.RFC3339)
	}
	if sp.EndDate != nil {
		fields["end_date"] = sp.EndDate.Format(time.RFC3339)
	}
	_ = s.publisher.AppendFlat(ctx, events.StreamSprintActivities, fields)
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
	s.publishSprintActivity(ctx, "sprint_created", sp)
	return sp, nil
}

// UpdateSprint updates the mutable fields of an existing sprint,
// verifying it belongs to projectID. The only distinguished transition is
// into Active status ("sprint_started") — every other field edit, including
// a status change to anything other than Active, only fires the generic
// pub/sub TopicSprintUpdated, same as before this method looked at what
// changed at all.
func (s *Service) UpdateSprint(ctx context.Context, projectID, id uuid.UUID, in sprintdom.UpdateSprintInput) (*sprintdom.Sprint, error) {
	sp, err := s.repo.FindSprintByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if sp.ProjectID != projectID {
		return nil, sprintdom.ErrSprintNotFound
	}
	wasActive := sp.Status == sprintdom.SprintStatusActive

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
	if sp.Status == sprintdom.SprintStatusActive && !wasActive {
		s.publishSprintActivity(ctx, "sprint_started", sp)
	}
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
	s.publishSprintActivity(ctx, "sprint_deleted", sp)
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
	s.publishSprintActivity(ctx, "sprint_completed", sp)
	return sp, nil
}
