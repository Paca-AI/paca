package globalrolesvc

import (
	"context"
	"log/slog"
	"time"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/platform/cache"
	"github.com/google/uuid"
)

const globalRolesKey = "global-roles"

// CachedService decorates a globalroledom.Service with a Valkey/Redis-backed
// cache.
//
// # What is cached
//
//   - List – the full list of global roles. Global roles change infrequently
//     (admin-level operations only) so a relatively long TTL is appropriate.
//
// All write operations (Create/Update/Delete/ReplaceUserRoles) invalidate the
// cached list after a successful database write.
//
// Cache errors are non-fatal: read errors fall through to the real service;
// write/delete errors are logged so mutations always succeed.
type CachedService struct {
	svc globalroledom.Service
	st  *cache.Store
	ttl time.Duration
	log *slog.Logger
}

// NewCachedService wraps svc with a caching layer backed by st.
// ttl controls how long the global roles list is cached; zero disables caching.
// log receives non-fatal cache warnings.
func NewCachedService(svc globalroledom.Service, st *cache.Store, ttl time.Duration, log *slog.Logger) *CachedService {
	return &CachedService{svc: svc, st: st, ttl: ttl, log: log}
}

// List returns all global roles, reading from cache when available and
// populating it on a miss.
func (c *CachedService) List(ctx context.Context) ([]*globalroledom.GlobalRole, error) {
	if c.ttl == 0 {
		return c.svc.List(ctx)
	}
	var result []*globalroledom.GlobalRole
	if ok, err := c.st.Get(ctx, globalRolesKey, &result); ok {
		return result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: ListGlobalRoles get", "err", err)
	}

	result, err := c.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, globalRolesKey, result, c.ttl); err != nil {
		c.log.WarnContext(ctx, "cache: ListGlobalRoles set", "err", err)
	}
	return result, nil
}

// Create delegates to the underlying service and invalidates the list cache.
func (c *CachedService) Create(ctx context.Context, in globalroledom.CreateInput, caller authz.PermissionSet) (*globalroledom.GlobalRole, error) {
	r, err := c.svc.Create(ctx, in, caller)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, globalRolesKey); err != nil {
		c.log.WarnContext(ctx, "cache: CreateGlobalRole delete", "err", err)
	}
	return r, nil
}

// Update delegates to the underlying service and invalidates the list cache.
func (c *CachedService) Update(ctx context.Context, id uuid.UUID, in globalroledom.UpdateInput, caller authz.PermissionSet) (*globalroledom.GlobalRole, error) {
	r, err := c.svc.Update(ctx, id, in, caller)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, globalRolesKey); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateGlobalRole delete", "err", err)
	}
	return r, nil
}

// Delete delegates to the underlying service and invalidates the list cache.
func (c *CachedService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := c.svc.Delete(ctx, id); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, globalRolesKey); err != nil {
		c.log.WarnContext(ctx, "cache: DeleteGlobalRole delete", "err", err)
	}
	return nil
}

// ReplaceUserRoles delegates directly to the underlying service and invalidates the list cache.
func (c *CachedService) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID, caller authz.PermissionSet) ([]*globalroledom.GlobalRole, error) {
	rs, err := c.svc.ReplaceUserRoles(ctx, userID, roleIDs, caller)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, globalRolesKey); err != nil {
		c.log.WarnContext(ctx, "cache: ReplaceUserRoles delete", "err", err)
	}
	return rs, nil
}
