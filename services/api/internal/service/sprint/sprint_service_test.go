// Package sprintsvc_test contains unit tests for the sprint service layer.
package sprintsvc_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	sprintdom "github.com/paca/api/internal/domain/sprint"
	sprintsvc "github.com/paca/api/internal/service/sprint"
)

// ---------------------------------------------------------------------------
// Fake repository
// ---------------------------------------------------------------------------

type fakeSprintRepo struct {
	mu      sync.RWMutex
	sprints map[uuid.UUID]*sprintdom.Sprint
}

func newFakeSprintRepo() *fakeSprintRepo {
	return &fakeSprintRepo{sprints: make(map[uuid.UUID]*sprintdom.Sprint)}
}

func (r *fakeSprintRepo) ListSprints(_ context.Context, projectID uuid.UUID) ([]*sprintdom.Sprint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*sprintdom.Sprint, 0)
	for _, s := range r.sprints {
		if s.ProjectID == projectID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeSprintRepo) FindSprintByID(_ context.Context, id uuid.UUID) (*sprintdom.Sprint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sprints[id]
	if !ok {
		return nil, sprintdom.ErrSprintNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *fakeSprintRepo) CreateSprint(_ context.Context, s *sprintdom.Sprint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *s
	r.sprints[s.ID] = &cp
	return nil
}

func (r *fakeSprintRepo) UpdateSprint(_ context.Context, s *sprintdom.Sprint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sprints[s.ID]; !ok {
		return sprintdom.ErrSprintNotFound
	}
	cp := *s
	r.sprints[s.ID] = &cp
	return nil
}

func (r *fakeSprintRepo) DeleteSprint(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sprints, id)
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCreateSprint_OK(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())
	projectID := uuid.New()

	start := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC)
	goal := "Ship v1"

	sp, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint 1",
		StartDate: &start,
		EndDate:   &end,
		Goal:      &goal,
		Status:    sprintdom.SprintStatusPlanned,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sp.Name != "Sprint 1" {
		t.Errorf("expected Name=Sprint 1, got %q", sp.Name)
	}
	if sp.Status != sprintdom.SprintStatusPlanned {
		t.Errorf("expected status planned, got %q", sp.Status)
	}
}

func TestCreateSprint_DefaultStatus(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())

	sp, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: uuid.New(),
		Name:      "Sprint X",
		// Status omitted — should default to planned
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sp.Status != sprintdom.SprintStatusPlanned {
		t.Errorf("expected default status planned, got %q", sp.Status)
	}
}

func TestCreateSprint_EmptyName(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())

	_, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: uuid.New(),
		Name:      "   ",
	})
	if err != sprintdom.ErrSprintNameInvalid {
		t.Errorf("expected ErrSprintNameInvalid, got %v", err)
	}
}

func TestCreateSprint_InvalidStatus(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())

	_, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: uuid.New(),
		Name:      "Bad Sprint",
		Status:    "unknown",
	})
	if err != sprintdom.ErrSprintStatusInvalid {
		t.Errorf("expected ErrSprintStatusInvalid, got %v", err)
	}
}

func TestUpdateSprint_ActivateSprint(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())
	projectID := uuid.New()

	sp, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint 2",
		Status:    sprintdom.SprintStatusPlanned,
	})

	active := sprintdom.SprintStatusActive
	updated, err := svc.UpdateSprint(ctx, sp.ID, sprintdom.UpdateSprintInput{
		Name:   "Sprint 2",
		Status: &active,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != sprintdom.SprintStatusActive {
		t.Errorf("expected status active, got %q", updated.Status)
	}
}

func TestUpdateSprint_InvalidStatus(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())

	sp, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: uuid.New(),
		Name:      "Sprint 3",
	})

	bad := sprintdom.SprintStatus("flying")
	_, err := svc.UpdateSprint(ctx, sp.ID, sprintdom.UpdateSprintInput{
		Status: &bad,
	})
	if err != sprintdom.ErrSprintStatusInvalid {
		t.Errorf("expected ErrSprintStatusInvalid, got %v", err)
	}
}

func TestDeleteSprint_OK(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())

	sp, _ := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: uuid.New(),
		Name:      "To Delete",
	})
	if err := svc.DeleteSprint(ctx, sp.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, err := svc.GetSprint(ctx, sp.ID)
	if err != sprintdom.ErrSprintNotFound {
		t.Errorf("expected ErrSprintNotFound after delete, got %v", err)
	}
}

func TestDeleteSprint_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())

	err := svc.DeleteSprint(ctx, uuid.New())
	if err != sprintdom.ErrSprintNotFound {
		t.Errorf("expected ErrSprintNotFound, got %v", err)
	}
}

func TestGetSprint_OK(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())
	projectID := uuid.New()

	sp, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      "Sprint Alpha",
		Status:    sprintdom.SprintStatusPlanned,
	})
	if err != nil {
		t.Fatalf("unexpected error creating sprint: %v", err)
	}

	got, err := svc.GetSprint(ctx, sp.ID)
	if err != nil {
		t.Fatalf("unexpected error getting sprint: %v", err)
	}
	if got.ID != sp.ID {
		t.Errorf("expected ID %v, got %v", sp.ID, got.ID)
	}
	if got.Name != "Sprint Alpha" {
		t.Errorf("expected Name=Sprint Alpha, got %q", got.Name)
	}
}

func TestGetSprint_NotFound(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())

	_, err := svc.GetSprint(ctx, uuid.New())
	if err != sprintdom.ErrSprintNotFound {
		t.Errorf("expected ErrSprintNotFound, got %v", err)
	}
}

func TestListSprints_ReturnsProjectSprints(t *testing.T) {
	ctx := context.Background()
	svc := sprintsvc.New(newFakeSprintRepo())
	projectID := uuid.New()
	otherProjectID := uuid.New()

	// Create sprints in the target project
	for i := range 3 {
		_, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
			ProjectID: projectID,
			Name:      fmt.Sprintf("Sprint %d", i+1),
		})
		if err != nil {
			t.Fatalf("create sprint: %v", err)
		}
	}
	// Create a sprint in another project — should not appear
	_, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{
		ProjectID: otherProjectID,
		Name:      "Other Sprint",
	})
	if err != nil {
		t.Fatalf("create other sprint: %v", err)
	}

	sprints, err := svc.ListSprints(ctx, projectID)
	if err != nil {
		t.Fatalf("list sprints: %v", err)
	}
	if len(sprints) != 3 {
		t.Errorf("expected 3 sprints for project, got %d", len(sprints))
	}
}
