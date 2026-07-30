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

// CachedSprintService decorates a sprintdom.SprintService with a
// Valkey/Redis-backed cache.
//
// # What is cached
//
//   - ListSprints  – all sprints for a project; keyed by project ID.
//   - GetSprint    – single sprint; keyed by sprint ID.
//
// Write operations (Create/Update/Delete/Complete) invalidate the relevant
// cached entries after a successful database write.
//
// Cache errors are non-fatal: read errors fall through to the real service;
// write/delete errors are logged so mutations always succeed.
type CachedSprintService struct {
	svc sprintdom.SprintService
	st  *cache.Store
	ttl time.Duration
	log *slog.Logger
}

// NewCachedSprintService wraps svc with a caching layer backed by st.
// ttl controls how long cached entries live; zero disables caching.
// log receives non-fatal cache warnings.
func NewCachedSprintService(svc sprintdom.SprintService, st *cache.Store, ttl time.Duration, log *slog.Logger) *CachedSprintService {
	return &CachedSprintService{svc: svc, st: st, ttl: ttl, log: log}
}

// --- cache key helpers -------------------------------------------------------

func sprintListKey(projectID uuid.UUID) string {
	return fmt.Sprintf("project:%s:sprints", projectID)
}

func sprintItemKey(id uuid.UUID) string {
	return fmt.Sprintf("sprint:%s", id)
}

// --- SprintService -----------------------------------------------------------

// ListSprints returns all sprints for a project, reading from cache when
// available and populating it on a miss.
func (c *CachedSprintService) ListSprints(ctx context.Context, projectID uuid.UUID) ([]*sprintdom.Sprint, error) {
	if c.ttl == 0 {
		return c.svc.ListSprints(ctx, projectID)
	}
	key := sprintListKey(projectID)
	var result []*sprintdom.Sprint
	if ok, err := c.st.Get(ctx, key, &result); ok {
		return result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: ListSprints get", "err", err)
	}

	result, err := c.svc.ListSprints(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, result, c.ttl); err != nil {
		c.log.WarnContext(ctx, "cache: ListSprints set", "err", err)
	}
	return result, nil
}

// GetSprint returns a single sprint, reading from cache when available and
// populating it on a miss. On a cache hit the cached sprint's ProjectID is
// compared with the requested projectID; a mismatch returns ErrSprintNotFound
// without delegating to the underlying service, preventing cross-project leaks.
func (c *CachedSprintService) GetSprint(ctx context.Context, projectID, id uuid.UUID) (*sprintdom.Sprint, error) {
	if c.ttl == 0 {
		return c.svc.GetSprint(ctx, projectID, id)
	}
	key := sprintItemKey(id)
	var result sprintdom.Sprint
	if ok, err := c.st.Get(ctx, key, &result); ok {
		if result.ProjectID != projectID {
			return nil, sprintdom.ErrSprintNotFound
		}
		return &result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: GetSprint get", "err", err)
	}

	sp, err := c.svc.GetSprint(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, sp, c.ttl); err != nil {
		c.log.WarnContext(ctx, "cache: GetSprint set", "err", err)
	}
	return sp, nil
}

// CreateSprint delegates to the underlying service and invalidates the sprint list cache.
func (c *CachedSprintService) CreateSprint(ctx context.Context, in sprintdom.CreateSprintInput) (*sprintdom.Sprint, error) {
	sp, err := c.svc.CreateSprint(ctx, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, sprintListKey(in.ProjectID)); err != nil {
		c.log.WarnContext(ctx, "cache: CreateSprint delete", "err", err)
	}
	return sp, nil
}

// UpdateSprint delegates to the underlying service and invalidates the sprint list and item caches.
func (c *CachedSprintService) UpdateSprint(ctx context.Context, projectID, id uuid.UUID, in sprintdom.UpdateSprintInput) (*sprintdom.Sprint, error) {
	sp, err := c.svc.UpdateSprint(ctx, projectID, id, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, sprintListKey(projectID), sprintItemKey(id)); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateSprint delete", "err", err)
	}
	return sp, nil
}

// DeleteSprint delegates to the underlying service and invalidates the sprint list and item caches.
func (c *CachedSprintService) DeleteSprint(ctx context.Context, projectID, id uuid.UUID) error {
	if err := c.svc.DeleteSprint(ctx, projectID, id); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, sprintListKey(projectID), sprintItemKey(id)); err != nil {
		c.log.WarnContext(ctx, "cache: DeleteSprint delete", "err", err)
	}
	return nil
}

// CompleteSprint delegates to the underlying service and invalidates the sprint list and item caches.
func (c *CachedSprintService) CompleteSprint(ctx context.Context, projectID, id uuid.UUID, in sprintdom.CompleteSprintInput) (*sprintdom.Sprint, error) {
	sp, err := c.svc.CompleteSprint(ctx, projectID, id, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, sprintListKey(projectID), sprintItemKey(id)); err != nil {
		c.log.WarnContext(ctx, "cache: CompleteSprint delete", "err", err)
	}
	return sp, nil
}
