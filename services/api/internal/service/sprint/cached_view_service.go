package sprintsvc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/platform/cache"
)

// ErrViewNotFound is re-exported from the domain package so callers can
// reference the sentinel without importing sprintdom directly.
var ErrViewNotFound = sprintdom.ErrViewNotFound

// CachedViewService decorates a sprintdom.ViewService with a
// Valkey/Redis-backed cache.
//
// # What is cached
//
//   - ListProjectViews – views for a project+context (backlog/timeline);
//     keyed by project ID and view context.
//   - GetView          – single view; keyed by view ID.
//
// Sprint-context views (ListViews by sprintID) are NOT cached because their
// invalidation would require knowing the project ID from the sprint ID, which
// would add an extra DB round-trip that negates the benefit.
//
// Task positions (ListTaskPositions) are NOT cached because they are updated
// frequently during drag-and-drop interactions.
//
// Cache errors are non-fatal: read errors fall through to the real service;
// write/delete errors are logged so mutations always succeed.
type CachedViewService struct {
	svc sprintdom.ViewService
	st  *cache.Store
	ttl time.Duration
	log *slog.Logger
}

// NewCachedViewService wraps svc with a caching layer backed by st.
// ttl controls how long cached entries live; zero disables caching.
// log receives non-fatal cache warnings.
func NewCachedViewService(svc sprintdom.ViewService, st *cache.Store, ttl time.Duration, log *slog.Logger) *CachedViewService {
	return &CachedViewService{svc: svc, st: st, ttl: ttl, log: log}
}

// --- cache key helpers -------------------------------------------------------

func projectViewsKey(projectID uuid.UUID, viewCtx sprintdom.ViewContext) string {
	return fmt.Sprintf("project:%s:views:%s", projectID, viewCtx)
}

func viewItemKey(id uuid.UUID) string {
	return fmt.Sprintf("view:%s", id)
}

// --- ViewService -------------------------------------------------------------

// ListViews returns all views for a sprint (sprint context). Not cached; see
// type-level documentation for rationale.
func (c *CachedViewService) ListViews(ctx context.Context, projectID, sprintID uuid.UUID) ([]*sprintdom.SprintView, error) {
	return c.svc.ListViews(ctx, projectID, sprintID)
}

// ListProjectViews returns all views for a project filtered by viewCtx,
// reading from cache when available and populating it on a miss.
func (c *CachedViewService) ListProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext) ([]*sprintdom.SprintView, error) {
	if c.ttl == 0 {
		return c.svc.ListProjectViews(ctx, projectID, viewCtx)
	}
	key := projectViewsKey(projectID, viewCtx)
	var result []*sprintdom.SprintView
	if ok, err := c.st.Get(ctx, key, &result); ok {
		return result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: ListProjectViews get", "err", err)
	}

	result, err := c.svc.ListProjectViews(ctx, projectID, viewCtx)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, result, c.ttl); err != nil {
		c.log.WarnContext(ctx, "cache: ListProjectViews set", "err", err)
	}
	return result, nil
}

// GetView returns a single view, reading from cache when available and
// populating it on a miss. On a cache hit the cached view's ProjectID is
// compared with the requested projectID; a mismatch returns ErrViewNotFound
// without delegating to the underlying service, preventing cross-project leaks.
func (c *CachedViewService) GetView(ctx context.Context, projectID, id uuid.UUID) (*sprintdom.SprintView, error) {
	if c.ttl == 0 {
		return c.svc.GetView(ctx, projectID, id)
	}
	key := viewItemKey(id)
	var result sprintdom.SprintView
	if ok, err := c.st.Get(ctx, key, &result); ok {
		if result.ProjectID != projectID {
			return nil, ErrViewNotFound
		}
		return &result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: GetView get", "err", err)
	}

	v, err := c.svc.GetView(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, v, c.ttl); err != nil {
		c.log.WarnContext(ctx, "cache: GetView set", "err", err)
	}
	return v, nil
}

// CreateView delegates to the underlying service and invalidates the project view list cache.
func (c *CachedViewService) CreateView(ctx context.Context, in sprintdom.CreateViewInput) (*sprintdom.SprintView, error) {
	v, err := c.svc.CreateView(ctx, in)
	if err != nil {
		return nil, err
	}
	// Invalidate the project-level view list for the relevant context.
	if err := c.st.Delete(ctx, projectViewsKey(in.ProjectID, in.ViewContext)); err != nil {
		c.log.WarnContext(ctx, "cache: CreateView delete", "err", err)
	}
	return v, nil
}

// UpdateView delegates to the underlying service and invalidates the view item and project list caches.
func (c *CachedViewService) UpdateView(ctx context.Context, projectID, id uuid.UUID, in sprintdom.UpdateViewInput) (*sprintdom.SprintView, error) {
	v, err := c.svc.UpdateView(ctx, projectID, id, in)
	if err != nil {
		return nil, err
	}
	// Invalidate both the project-level list and the individual item cache.
	toDelete := []string{viewItemKey(id)}
	toDelete = append(toDelete,
		projectViewsKey(projectID, sprintdom.ViewContextBacklog),
		projectViewsKey(projectID, sprintdom.ViewContextTimeline),
	)
	if err := c.st.Delete(ctx, toDelete...); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateView delete", "err", err)
	}
	return v, nil
}

// DeleteView delegates to the underlying service and invalidates the view item and project list caches.
func (c *CachedViewService) DeleteView(ctx context.Context, projectID, id uuid.UUID) error {
	if err := c.svc.DeleteView(ctx, projectID, id); err != nil {
		return err
	}
	toDelete := []string{viewItemKey(id),
		projectViewsKey(projectID, sprintdom.ViewContextBacklog),
		projectViewsKey(projectID, sprintdom.ViewContextTimeline),
	}
	if err := c.st.Delete(ctx, toDelete...); err != nil {
		c.log.WarnContext(ctx, "cache: DeleteView delete", "err", err)
	}
	return nil
}

// MoveTask delegates directly to the underlying service (not cached).
func (c *CachedViewService) MoveTask(ctx context.Context, projectID, viewID uuid.UUID, in sprintdom.MoveTaskInput) error {
	return c.svc.MoveTask(ctx, projectID, viewID, in)
}

// BulkMoveTasks delegates directly to the underlying service (not cached).
func (c *CachedViewService) BulkMoveTasks(ctx context.Context, projectID, viewID uuid.UUID, items []sprintdom.MoveTaskInput) error {
	return c.svc.BulkMoveTasks(ctx, projectID, viewID, items)
}

// ListTaskPositions delegates directly to the underlying service (not cached).
func (c *CachedViewService) ListTaskPositions(ctx context.Context, projectID, viewID uuid.UUID) ([]*sprintdom.ViewTaskPosition, error) {
	return c.svc.ListTaskPositions(ctx, projectID, viewID)
}

// ReorderViews delegates directly to the underlying service (not cached).
func (c *CachedViewService) ReorderViews(ctx context.Context, projectID, sprintID uuid.UUID, viewIDs []uuid.UUID) error {
	return c.svc.ReorderViews(ctx, projectID, sprintID, viewIDs)
}

// ReorderProjectViews reorders project-level views (backlog or timeline) and
// invalidates the corresponding cached list.
func (c *CachedViewService) ReorderProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext, viewIDs []uuid.UUID) error {
	if err := c.svc.ReorderProjectViews(ctx, projectID, viewCtx, viewIDs); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, projectViewsKey(projectID, viewCtx)); err != nil {
		c.log.WarnContext(ctx, "cache: ReorderProjectViews delete", "err", err)
	}
	return nil
}

// SetUserViewConfig delegates directly to the underlying service. Personal
// configs are never stored in the shared cache, so no invalidation is needed:
// the cached ListProjectViews/GetView entries hold the shared view definition
// and are overlaid with the caller's personal config after the cache is read.
func (c *CachedViewService) SetUserViewConfig(ctx context.Context, projectID, viewID, userID uuid.UUID, cfg sprintdom.ViewConfig) (*sprintdom.SprintView, error) {
	return c.svc.SetUserViewConfig(ctx, projectID, viewID, userID, cfg)
}

// OverlayUserConfigs delegates directly to the underlying service. It must run
// after the cache is read so the shared cached entries are never polluted with
// one user's personal config.
func (c *CachedViewService) OverlayUserConfigs(ctx context.Context, userID uuid.UUID, views []*sprintdom.SprintView) error {
	return c.svc.OverlayUserConfigs(ctx, userID, views)
}
