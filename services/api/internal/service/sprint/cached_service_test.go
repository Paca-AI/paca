// Package sprintsvc_test contains unit tests for the sprint service layer,
// including the CachedSprintService and CachedViewService decorators.
package sprintsvc_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/platform/cache"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newCacheStore(t *testing.T) *cache.Store {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return cache.NewStore(client, "paca:")
}

// ---------------------------------------------------------------------------
// Stub SprintService
// ---------------------------------------------------------------------------

type stubSprintSvc struct {
	listSprints    func(ctx context.Context, projectID uuid.UUID) ([]*sprintdom.Sprint, error)
	getSprint      func(ctx context.Context, projectID, id uuid.UUID) (*sprintdom.Sprint, error)
	createSprint   func(ctx context.Context, in sprintdom.CreateSprintInput) (*sprintdom.Sprint, error)
	updateSprint   func(ctx context.Context, projectID, id uuid.UUID, in sprintdom.UpdateSprintInput) (*sprintdom.Sprint, error)
	deleteSprint   func(ctx context.Context, projectID, id uuid.UUID) error
	completeSprint func(ctx context.Context, projectID, id uuid.UUID, in sprintdom.CompleteSprintInput) (*sprintdom.Sprint, error)

	listCalls int
	getCalls  int
}

func (s *stubSprintSvc) ListSprints(ctx context.Context, projectID uuid.UUID) ([]*sprintdom.Sprint, error) {
	s.listCalls++
	if s.listSprints != nil {
		return s.listSprints(ctx, projectID)
	}
	return []*sprintdom.Sprint{{ID: uuid.New(), ProjectID: projectID, Name: "Sprint 1"}}, nil
}

func (s *stubSprintSvc) GetSprint(ctx context.Context, projectID, id uuid.UUID) (*sprintdom.Sprint, error) {
	s.getCalls++
	if s.getSprint != nil {
		return s.getSprint(ctx, projectID, id)
	}
	return &sprintdom.Sprint{ID: id, ProjectID: projectID, Name: "Sprint 1"}, nil
}

func (s *stubSprintSvc) CreateSprint(ctx context.Context, in sprintdom.CreateSprintInput) (*sprintdom.Sprint, error) {
	if s.createSprint != nil {
		return s.createSprint(ctx, in)
	}
	return &sprintdom.Sprint{ID: uuid.New(), ProjectID: in.ProjectID, Name: in.Name}, nil
}

func (s *stubSprintSvc) UpdateSprint(ctx context.Context, projectID, id uuid.UUID, in sprintdom.UpdateSprintInput) (*sprintdom.Sprint, error) {
	if s.updateSprint != nil {
		return s.updateSprint(ctx, projectID, id, in)
	}
	return &sprintdom.Sprint{ID: id, ProjectID: projectID, Name: in.Name}, nil
}

func (s *stubSprintSvc) DeleteSprint(ctx context.Context, projectID, id uuid.UUID) error {
	if s.deleteSprint != nil {
		return s.deleteSprint(ctx, projectID, id)
	}
	return nil
}

func (s *stubSprintSvc) CompleteSprint(ctx context.Context, projectID, id uuid.UUID, in sprintdom.CompleteSprintInput) (*sprintdom.Sprint, error) {
	if s.completeSprint != nil {
		return s.completeSprint(ctx, projectID, id, in)
	}
	return &sprintdom.Sprint{ID: id, ProjectID: projectID, Status: sprintdom.SprintStatusCompleted}, nil
}

// ---------------------------------------------------------------------------
// CachedSprintService – ListSprints
// ---------------------------------------------------------------------------

func TestCachedSprint_ListSprints_CacheMissPopulatesCache(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubSprintSvc{}
	svc := sprintsvc.NewCachedSprintService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	// First call: miss.
	sprints, err := svc.ListSprints(ctx, projectID)
	if err != nil {
		t.Fatalf("ListSprints (miss): %v", err)
	}
	if len(sprints) == 0 {
		t.Fatal("expected at least one sprint")
	}
	if stub.listCalls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.listCalls)
	}

	// Second call: hit.
	if _, err := svc.ListSprints(ctx, projectID); err != nil {
		t.Fatalf("ListSprints (hit): %v", err)
	}
	if stub.listCalls != 1 {
		t.Fatalf("cache hit: stub called again; got %d calls", stub.listCalls)
	}
}

func TestCachedSprint_ListSprints_ZeroTTLBypassesCache(t *testing.T) {
	ctx := context.Background()
	stub := &stubSprintSvc{}
	svc := sprintsvc.NewCachedSprintService(stub, newCacheStore(t), 0, discardLogger())

	for i := 0; i < 3; i++ {
		if _, err := svc.ListSprints(ctx, uuid.New()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if stub.listCalls != 3 {
		t.Fatalf("TTL=0 should bypass cache; want 3 calls, got %d", stub.listCalls)
	}
}

func TestCachedSprint_ListSprints_ServiceErrorPropagated(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("db failure")
	stub := &stubSprintSvc{
		listSprints: func(_ context.Context, _ uuid.UUID) ([]*sprintdom.Sprint, error) {
			return nil, sentinel
		},
	}
	svc := sprintsvc.NewCachedSprintService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	_, err := svc.ListSprints(ctx, uuid.New())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// CachedSprintService – GetSprint
// ---------------------------------------------------------------------------

func TestCachedSprint_GetSprint_CacheHit(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	sprintID := uuid.New()
	stub := &stubSprintSvc{}
	svc := sprintsvc.NewCachedSprintService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.GetSprint(ctx, projectID, sprintID); err != nil {
		t.Fatalf("GetSprint (miss): %v", err)
	}
	if _, err := svc.GetSprint(ctx, projectID, sprintID); err != nil {
		t.Fatalf("GetSprint (hit): %v", err)
	}
	if stub.getCalls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.getCalls)
	}
}

func TestCachedSprint_GetSprint_CacheHit_ProjectMismatchReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	projectA := uuid.New()
	projectB := uuid.New()
	sprintID := uuid.New()
	stub := &stubSprintSvc{}
	svc := sprintsvc.NewCachedSprintService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.GetSprint(ctx, projectA, sprintID); err != nil {
		t.Fatalf("GetSprint (seed cache): %v", err)
	}

	got, err := svc.GetSprint(ctx, projectB, sprintID)
	if !errors.Is(err, sprintdom.ErrSprintNotFound) {
		t.Fatalf("expected ErrSprintNotFound for cached sprint requested under wrong project, got sprint=%v err=%v", got, err)
	}
	if got != nil {
		t.Fatalf("expected no sprint on project mismatch, got %#v", got)
	}
}

// ---------------------------------------------------------------------------
// CachedSprintService – write invalidation
// ---------------------------------------------------------------------------

func TestCachedSprint_CreateSprint_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubSprintSvc{}
	svc := sprintsvc.NewCachedSprintService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.ListSprints(ctx, projectID); err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	if _, err := svc.CreateSprint(ctx, sprintdom.CreateSprintInput{ProjectID: projectID, Name: "S2"}); err != nil {
		t.Fatalf("CreateSprint: %v", err)
	}
	if _, err := svc.ListSprints(ctx, projectID); err != nil {
		t.Fatalf("ListSprints after Create: %v", err)
	}
	if stub.listCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listCalls)
	}
}

func TestCachedSprint_UpdateSprint_InvalidatesListAndItem(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	sprintID := uuid.New()
	stub := &stubSprintSvc{}
	svc := sprintsvc.NewCachedSprintService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	// Populate both caches.
	if _, err := svc.ListSprints(ctx, projectID); err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	if _, err := svc.GetSprint(ctx, projectID, sprintID); err != nil {
		t.Fatalf("GetSprint: %v", err)
	}

	// Update evicts both keys.
	name := "Updated"
	if _, err := svc.UpdateSprint(ctx, projectID, sprintID, sprintdom.UpdateSprintInput{Name: name}); err != nil {
		t.Fatalf("UpdateSprint: %v", err)
	}

	if _, err := svc.ListSprints(ctx, projectID); err != nil {
		t.Fatalf("ListSprints after Update: %v", err)
	}
	if _, err := svc.GetSprint(ctx, projectID, sprintID); err != nil {
		t.Fatalf("GetSprint after Update: %v", err)
	}

	if stub.listCalls != 2 {
		t.Fatalf("ListSprints: expected 2 stub calls, got %d", stub.listCalls)
	}
	if stub.getCalls != 2 {
		t.Fatalf("GetSprint: expected 2 stub calls, got %d", stub.getCalls)
	}
}

func TestCachedSprint_DeleteSprint_InvalidatesListAndItem(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	sprintID := uuid.New()
	stub := &stubSprintSvc{}
	svc := sprintsvc.NewCachedSprintService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.ListSprints(ctx, projectID); err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	if _, err := svc.GetSprint(ctx, projectID, sprintID); err != nil {
		t.Fatalf("GetSprint: %v", err)
	}

	if err := svc.DeleteSprint(ctx, projectID, sprintID); err != nil {
		t.Fatalf("DeleteSprint: %v", err)
	}

	if _, err := svc.ListSprints(ctx, projectID); err != nil {
		t.Fatalf("ListSprints after Delete: %v", err)
	}
	if _, err := svc.GetSprint(ctx, projectID, sprintID); err != nil {
		t.Fatalf("GetSprint after Delete: %v", err)
	}

	if stub.listCalls != 2 {
		t.Fatalf("ListSprints: expected 2 stub calls, got %d", stub.listCalls)
	}
	if stub.getCalls != 2 {
		t.Fatalf("GetSprint: expected 2 stub calls, got %d", stub.getCalls)
	}
}

func TestCachedSprint_CompleteSprint_InvalidatesListAndItem(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	sprintID := uuid.New()
	stub := &stubSprintSvc{}
	svc := sprintsvc.NewCachedSprintService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.ListSprints(ctx, projectID); err != nil {
		t.Fatalf("ListSprints: %v", err)
	}
	if _, err := svc.GetSprint(ctx, projectID, sprintID); err != nil {
		t.Fatalf("GetSprint: %v", err)
	}

	if _, err := svc.CompleteSprint(ctx, projectID, sprintID, sprintdom.CompleteSprintInput{}); err != nil {
		t.Fatalf("CompleteSprint: %v", err)
	}

	if _, err := svc.ListSprints(ctx, projectID); err != nil {
		t.Fatalf("ListSprints after Complete: %v", err)
	}
	if _, err := svc.GetSprint(ctx, projectID, sprintID); err != nil {
		t.Fatalf("GetSprint after Complete: %v", err)
	}

	if stub.listCalls != 2 {
		t.Fatalf("ListSprints: expected 2 stub calls, got %d", stub.listCalls)
	}
	if stub.getCalls != 2 {
		t.Fatalf("GetSprint: expected 2 stub calls, got %d", stub.getCalls)
	}
}

// ---------------------------------------------------------------------------
// Stub ViewService
// ---------------------------------------------------------------------------

type stubViewSvc struct {
	listViews           func(ctx context.Context, sprintID uuid.UUID) ([]*sprintdom.SprintView, error)
	listProjectViews    func(ctx context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext) ([]*sprintdom.SprintView, error)
	getView             func(ctx context.Context, projectID, id uuid.UUID) (*sprintdom.SprintView, error)
	createView          func(ctx context.Context, in sprintdom.CreateViewInput) (*sprintdom.SprintView, error)
	updateView          func(ctx context.Context, projectID, id uuid.UUID, in sprintdom.UpdateViewInput) (*sprintdom.SprintView, error)
	deleteView          func(ctx context.Context, projectID, id uuid.UUID) error
	moveTask            func(ctx context.Context, projectID, viewID uuid.UUID, in sprintdom.MoveTaskInput) error
	bulkMoveTasks       func(ctx context.Context, projectID, viewID uuid.UUID, items []sprintdom.MoveTaskInput) error
	listTaskPositions   func(ctx context.Context, projectID, viewID uuid.UUID) ([]*sprintdom.ViewTaskPosition, error)
	reorderViews        func(ctx context.Context, sprintID uuid.UUID, viewIDs []uuid.UUID) error
	reorderProjectViews func(ctx context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext, viewIDs []uuid.UUID) error

	listProjectViewsCalls int
	getViewCalls          int
}

func (s *stubViewSvc) ListViews(ctx context.Context, sprintID uuid.UUID) ([]*sprintdom.SprintView, error) {
	if s.listViews != nil {
		return s.listViews(ctx, sprintID)
	}
	return nil, nil
}

func (s *stubViewSvc) ListProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext) ([]*sprintdom.SprintView, error) {
	s.listProjectViewsCalls++
	if s.listProjectViews != nil {
		return s.listProjectViews(ctx, projectID, viewCtx)
	}
	return []*sprintdom.SprintView{{ID: uuid.New(), ProjectID: projectID, ViewContext: viewCtx}}, nil
}

func (s *stubViewSvc) GetView(ctx context.Context, projectID, id uuid.UUID) (*sprintdom.SprintView, error) {
	s.getViewCalls++
	if s.getView != nil {
		return s.getView(ctx, projectID, id)
	}
	return &sprintdom.SprintView{ID: id, ProjectID: projectID}, nil
}

func (s *stubViewSvc) CreateView(ctx context.Context, in sprintdom.CreateViewInput) (*sprintdom.SprintView, error) {
	if s.createView != nil {
		return s.createView(ctx, in)
	}
	return &sprintdom.SprintView{ID: uuid.New(), ProjectID: in.ProjectID, ViewContext: in.ViewContext}, nil
}

func (s *stubViewSvc) UpdateView(ctx context.Context, projectID, id uuid.UUID, in sprintdom.UpdateViewInput) (*sprintdom.SprintView, error) {
	if s.updateView != nil {
		return s.updateView(ctx, projectID, id, in)
	}
	return &sprintdom.SprintView{ID: id, ProjectID: projectID}, nil
}

func (s *stubViewSvc) DeleteView(ctx context.Context, projectID, id uuid.UUID) error {
	if s.deleteView != nil {
		return s.deleteView(ctx, projectID, id)
	}
	return nil
}

func (s *stubViewSvc) MoveTask(ctx context.Context, projectID, viewID uuid.UUID, in sprintdom.MoveTaskInput) error {
	if s.moveTask != nil {
		return s.moveTask(ctx, projectID, viewID, in)
	}
	return nil
}

func (s *stubViewSvc) BulkMoveTasks(ctx context.Context, projectID, viewID uuid.UUID, items []sprintdom.MoveTaskInput) error {
	if s.bulkMoveTasks != nil {
		return s.bulkMoveTasks(ctx, projectID, viewID, items)
	}
	return nil
}

func (s *stubViewSvc) ListTaskPositions(ctx context.Context, projectID, viewID uuid.UUID) ([]*sprintdom.ViewTaskPosition, error) {
	if s.listTaskPositions != nil {
		return s.listTaskPositions(ctx, projectID, viewID)
	}
	return nil, nil
}

func (s *stubViewSvc) ReorderViews(ctx context.Context, sprintID uuid.UUID, viewIDs []uuid.UUID) error {
	if s.reorderViews != nil {
		return s.reorderViews(ctx, sprintID, viewIDs)
	}
	return nil
}

func (s *stubViewSvc) ReorderProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext, viewIDs []uuid.UUID) error {
	if s.reorderProjectViews != nil {
		return s.reorderProjectViews(ctx, projectID, viewCtx, viewIDs)
	}
	return nil
}

func (s *stubViewSvc) SetUserViewConfig(_ context.Context, _, _, _ uuid.UUID, _ sprintdom.ViewConfig) (*sprintdom.SprintView, error) {
	return nil, nil
}

func (s *stubViewSvc) OverlayUserConfigs(_ context.Context, _ uuid.UUID, _ []*sprintdom.SprintView) error {
	return nil
}

// ---------------------------------------------------------------------------
// CachedViewService – ListProjectViews
// ---------------------------------------------------------------------------

func TestCachedView_ListProjectViews_CacheHit(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubViewSvc{}
	svc := sprintsvc.NewCachedViewService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("ListProjectViews (miss): %v", err)
	}
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("ListProjectViews (hit): %v", err)
	}
	if stub.listProjectViewsCalls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.listProjectViewsCalls)
	}
}

func TestCachedView_ListProjectViews_DifferentContextsAreSeparateCacheEntries(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubViewSvc{}
	svc := sprintsvc.NewCachedViewService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("backlog: %v", err)
	}
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextTimeline); err != nil {
		t.Fatalf("timeline: %v", err)
	}
	// Both were misses, so stub called twice.
	if stub.listProjectViewsCalls != 2 {
		t.Fatalf("expected 2 stub calls (one per context), got %d", stub.listProjectViewsCalls)
	}
	// Re-request both: both should hit cache.
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("backlog (2nd): %v", err)
	}
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextTimeline); err != nil {
		t.Fatalf("timeline (2nd): %v", err)
	}
	if stub.listProjectViewsCalls != 2 {
		t.Fatalf("expected no additional stub calls on cache hits; got %d", stub.listProjectViewsCalls)
	}
}

// ---------------------------------------------------------------------------
// CachedViewService – GetView
// ---------------------------------------------------------------------------

func TestCachedView_GetView_CacheHit(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	viewID := uuid.New()
	stub := &stubViewSvc{}
	svc := sprintsvc.NewCachedViewService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.GetView(ctx, projectID, viewID); err != nil {
		t.Fatalf("GetView (miss): %v", err)
	}
	if _, err := svc.GetView(ctx, projectID, viewID); err != nil {
		t.Fatalf("GetView (hit): %v", err)
	}
	if stub.getViewCalls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.getViewCalls)
	}
}

func TestCachedView_GetView_CacheHit_EnforcesProjectOwnership(t *testing.T) {
	ctx := context.Background()
	projectA := uuid.New()
	projectB := uuid.New()
	viewID := uuid.New()
	stub := &stubViewSvc{}
	svc := sprintsvc.NewCachedViewService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.GetView(ctx, projectA, viewID); err != nil {
		t.Fatalf("GetView (project A miss): %v", err)
	}

	got, err := svc.GetView(ctx, projectB, viewID)
	if !errors.Is(err, sprintsvc.ErrViewNotFound) {
		t.Fatalf("GetView (project B cached lookup): expected ErrViewNotFound, got view=%v err=%v", got, err)
	}
	if got != nil {
		t.Fatalf("expected nil view on project mismatch, got %#v", got)
	}
	if stub.getViewCalls != 1 {
		t.Fatalf("expected project mismatch to be rejected from cache without an additional stub call; got %d calls", stub.getViewCalls)
	}
}

func TestCachedView_GetView_ZeroTTLBypassesCache(t *testing.T) {
	ctx := context.Background()
	stub := &stubViewSvc{}
	svc := sprintsvc.NewCachedViewService(stub, newCacheStore(t), 0, discardLogger())

	for i := 0; i < 3; i++ {
		if _, err := svc.GetView(ctx, uuid.New(), uuid.New()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if stub.getViewCalls != 3 {
		t.Fatalf("TTL=0 should bypass cache; want 3 calls, got %d", stub.getViewCalls)
	}
}

// ---------------------------------------------------------------------------
// CachedViewService – write invalidation
// ---------------------------------------------------------------------------

func TestCachedView_CreateView_InvalidatesProjectViewList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubViewSvc{}
	svc := sprintsvc.NewCachedViewService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("ListProjectViews: %v", err)
	}
	in := sprintdom.CreateViewInput{
		ProjectID:   projectID,
		Name:        "Board",
		ViewType:    sprintdom.ViewTypeBoard,
		ViewContext: sprintdom.ViewContextBacklog,
	}
	if _, err := svc.CreateView(ctx, in); err != nil {
		t.Fatalf("CreateView: %v", err)
	}
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("ListProjectViews after Create: %v", err)
	}
	if stub.listProjectViewsCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listProjectViewsCalls)
	}
}

func TestCachedView_UpdateView_InvalidatesItemAndBothProjectLists(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	viewID := uuid.New()
	stub := &stubViewSvc{}
	svc := sprintsvc.NewCachedViewService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	// Populate all three cache entries.
	if _, err := svc.GetView(ctx, projectID, viewID); err != nil {
		t.Fatalf("GetView: %v", err)
	}
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("ListProjectViews(backlog): %v", err)
	}
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextTimeline); err != nil {
		t.Fatalf("ListProjectViews(timeline): %v", err)
	}

	// Update should evict all three.
	name := "Renamed"
	if _, err := svc.UpdateView(ctx, projectID, viewID, sprintdom.UpdateViewInput{Name: &name}); err != nil {
		t.Fatalf("UpdateView: %v", err)
	}

	if _, err := svc.GetView(ctx, projectID, viewID); err != nil {
		t.Fatalf("GetView after Update: %v", err)
	}
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("ListProjectViews(backlog) after Update: %v", err)
	}
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextTimeline); err != nil {
		t.Fatalf("ListProjectViews(timeline) after Update: %v", err)
	}

	if stub.getViewCalls != 2 {
		t.Fatalf("GetView: expected 2 stub calls, got %d", stub.getViewCalls)
	}
	// listProjectViewsCalls: 2 (initial) + 2 (after update) = 4
	if stub.listProjectViewsCalls != 4 {
		t.Fatalf("ListProjectViews: expected 4 stub calls, got %d", stub.listProjectViewsCalls)
	}
}

func TestCachedView_DeleteView_InvalidatesItemAndBothProjectLists(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	viewID := uuid.New()
	stub := &stubViewSvc{}
	svc := sprintsvc.NewCachedViewService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.GetView(ctx, projectID, viewID); err != nil {
		t.Fatalf("GetView: %v", err)
	}
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("ListProjectViews(backlog): %v", err)
	}

	if err := svc.DeleteView(ctx, projectID, viewID); err != nil {
		t.Fatalf("DeleteView: %v", err)
	}

	if _, err := svc.GetView(ctx, projectID, viewID); err != nil {
		t.Fatalf("GetView after Delete: %v", err)
	}
	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("ListProjectViews(backlog) after Delete: %v", err)
	}

	if stub.getViewCalls != 2 {
		t.Fatalf("GetView: expected 2 stub calls, got %d", stub.getViewCalls)
	}
	if stub.listProjectViewsCalls != 2 {
		t.Fatalf("ListProjectViews: expected 2 stub calls, got %d", stub.listProjectViewsCalls)
	}
}

func TestCachedView_ReorderProjectViews_InvalidatesProjectList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubViewSvc{}
	svc := sprintsvc.NewCachedViewService(stub, newCacheStore(t), 2*time.Minute, discardLogger())

	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("ListProjectViews: %v", err)
	}

	if err := svc.ReorderProjectViews(ctx, projectID, sprintdom.ViewContextBacklog, []uuid.UUID{uuid.New()}); err != nil {
		t.Fatalf("ReorderProjectViews: %v", err)
	}

	if _, err := svc.ListProjectViews(ctx, projectID, sprintdom.ViewContextBacklog); err != nil {
		t.Fatalf("ListProjectViews after Reorder: %v", err)
	}
	if stub.listProjectViewsCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listProjectViewsCalls)
	}
}
