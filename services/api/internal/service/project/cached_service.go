package projectsvc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/platform/cache"
	"github.com/google/uuid"
)

// CachedService decorates a projectdom.Service with a Valkey/Redis-backed
// cache.
//
// # What is cached
//
//   - GetByID        – project detail; keyed by project ID.
//   - ListMembers    – project member list; keyed by project ID.
//   - ListRoles      – project role list; keyed by project ID.
//
// List/ListAccessible are NOT cached because they are paginated, potentially
// user-scoped, and have high result-set cardinality.
// GetMyProjectPermissions is NOT cached because it is user-specific and
// depends on the member's current role.
// IsProjectPublic is NOT cached; it is a single-column read used in hot-path
// middleware and is already handled by the database's plan cache.
//
// Cache errors are non-fatal: read errors fall through to the real service;
// write/delete errors are logged so mutations always succeed.
type CachedService struct {
	svc        projectdom.Service
	st         *cache.Store
	projectTTL time.Duration
	configTTL  time.Duration
	log        *slog.Logger
}

// NewCachedService wraps svc with a caching layer backed by st.
//
//   - projectTTL governs project detail and member data.
//   - configTTL governs project role data.
//
// Pass zero for either TTL to disable caching for that category.
// log receives non-fatal cache warnings.
func NewCachedService(svc projectdom.Service, st *cache.Store, projectTTL, configTTL time.Duration, log *slog.Logger) *CachedService {
	return &CachedService{
		svc:        svc,
		st:         st,
		projectTTL: projectTTL,
		configTTL:  configTTL,
		log:        log,
	}
}

// --- cache key helpers -------------------------------------------------------

func projectKey(id uuid.UUID) string {
	return fmt.Sprintf("project:%s", id)
}

func membersKey(projectID uuid.UUID) string {
	return fmt.Sprintf("project:%s:members", projectID)
}

func rolesKey(projectID uuid.UUID) string {
	return fmt.Sprintf("project:%s:roles", projectID)
}

// --- Project -----------------------------------------------------------------

// List delegates directly to the underlying service (not cached).
func (c *CachedService) List(ctx context.Context, page, pageSize int) ([]*projectdom.Project, int64, error) {
	return c.svc.List(ctx, page, pageSize)
}

// ListAccessible delegates directly to the underlying service (not cached).
func (c *CachedService) ListAccessible(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]*projectdom.Project, int64, error) {
	return c.svc.ListAccessible(ctx, userID, page, pageSize)
}

// GetByID returns a project by its ID, reading from cache when available and
// populating it on a miss.
func (c *CachedService) GetByID(ctx context.Context, id uuid.UUID) (*projectdom.Project, error) {
	if c.projectTTL == 0 {
		return c.svc.GetByID(ctx, id)
	}
	key := projectKey(id)
	var result projectdom.Project
	if ok, err := c.st.Get(ctx, key, &result); ok {
		return &result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: GetProject get", "err", err)
	}

	p, err := c.svc.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, p, c.projectTTL); err != nil {
		c.log.WarnContext(ctx, "cache: GetProject set", "err", err)
	}
	return p, nil
}

// IsProjectPublic delegates directly to the underlying service (not cached).
func (c *CachedService) IsProjectPublic(ctx context.Context, id uuid.UUID) (bool, error) {
	return c.svc.IsProjectPublic(ctx, id)
}

// Create delegates directly to the underlying service (not cached).
func (c *CachedService) Create(ctx context.Context, in projectdom.CreateProjectInput) (*projectdom.Project, error) {
	return c.svc.Create(ctx, in)
}

// Update delegates to the underlying service and invalidates the project cache entry.
func (c *CachedService) Update(ctx context.Context, id uuid.UUID, in projectdom.UpdateProjectInput) (*projectdom.Project, error) {
	p, err := c.svc.Update(ctx, id, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, projectKey(id)); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateProject delete", "err", err)
	}
	return p, nil
}

// Delete delegates to the underlying service and invalidates all related cache entries.
func (c *CachedService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := c.svc.Delete(ctx, id); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, projectKey(id), membersKey(id), rolesKey(id)); err != nil {
		c.log.WarnContext(ctx, "cache: DeleteProject delete", "err", err)
	}
	return nil
}

// --- Members -----------------------------------------------------------------

// ListMembers returns all members of a project, reading from cache when
// available and populating it on a miss.
func (c *CachedService) ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	if c.projectTTL == 0 {
		return c.svc.ListMembers(ctx, projectID)
	}
	key := membersKey(projectID)
	var result []*projectdom.ProjectMember
	if ok, err := c.st.Get(ctx, key, &result); ok {
		return result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: ListMembers get", "err", err)
	}

	result, err := c.svc.ListMembers(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, result, c.projectTTL); err != nil {
		c.log.WarnContext(ctx, "cache: ListMembers set", "err", err)
	}
	return result, nil
}

// AddMember delegates to the underlying service and invalidates the members cache.
func (c *CachedService) AddMember(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput, caller authz.PermissionSet) (*projectdom.ProjectMember, error) {
	m, err := c.svc.AddMember(ctx, projectID, in, caller)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, membersKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: AddMember delete", "err", err)
	}
	return m, nil
}

// UpdateMemberRole delegates to the underlying service and invalidates the members cache.
func (c *CachedService) UpdateMemberRole(ctx context.Context, projectID, userID uuid.UUID, in projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error) {
	m, err := c.svc.UpdateMemberRole(ctx, projectID, userID, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, membersKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateMemberRole delete", "err", err)
	}
	return m, nil
}

// RemoveMember delegates to the underlying service and invalidates the members cache.
func (c *CachedService) RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error {
	if err := c.svc.RemoveMember(ctx, projectID, userID); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, membersKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: RemoveMember delete", "err", err)
	}
	return nil
}

// UpdateMemberRoleByMemberID delegates to the underlying service and invalidates the members cache.
func (c *CachedService) UpdateMemberRoleByMemberID(ctx context.Context, projectID, memberID uuid.UUID, in projectdom.UpdateMemberRoleInput, caller authz.PermissionSet) (*projectdom.ProjectMember, error) {
	m, err := c.svc.UpdateMemberRoleByMemberID(ctx, projectID, memberID, in, caller)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, membersKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateMemberRoleByMemberID delete", "err", err)
	}
	return m, nil
}

// RemoveMemberByMemberID delegates to the underlying service and invalidates the members cache.
func (c *CachedService) RemoveMemberByMemberID(ctx context.Context, projectID, memberID uuid.UUID) error {
	if err := c.svc.RemoveMemberByMemberID(ctx, projectID, memberID); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, membersKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: RemoveMemberByMemberID delete", "err", err)
	}
	return nil
}

// AddAgentMember delegates to the underlying service and invalidates the members cache.
func (c *CachedService) AddAgentMember(ctx context.Context, memberID, projectID, agentID, roleID uuid.UUID) error {
	if err := c.svc.AddAgentMember(ctx, memberID, projectID, agentID, roleID); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, membersKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: AddAgentMember delete", "err", err)
	}
	return nil
}

// RemoveAgentMember delegates to the underlying service and invalidates the members cache.
func (c *CachedService) RemoveAgentMember(ctx context.Context, projectID, agentID uuid.UUID) error {
	if err := c.svc.RemoveAgentMember(ctx, projectID, agentID); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, membersKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: RemoveAgentMember delete", "err", err)
	}
	return nil
}

// InvalidateMembersCache removes the cached member list for projectID.
// It is called after transactional agent mutations to ensure the next read
// fetches fresh data from the database.
func (c *CachedService) InvalidateMembersCache(ctx context.Context, projectID uuid.UUID) error {
	return c.st.Delete(ctx, membersKey(projectID))
}

// GetMyProjectPermissions delegates directly to the underlying service (not cached).
func (c *CachedService) GetMyProjectPermissions(ctx context.Context, projectID, userID uuid.UUID, agentID *uuid.UUID) (map[string]any, error) {
	return c.svc.GetMyProjectPermissions(ctx, projectID, userID, agentID)
}

// --- Roles -------------------------------------------------------------------

// ListRoles returns all roles for a project, reading from cache when available
// and populating it on a miss.
func (c *CachedService) ListRoles(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectRole, error) {
	if c.configTTL == 0 {
		return c.svc.ListRoles(ctx, projectID)
	}
	key := rolesKey(projectID)
	var result []*projectdom.ProjectRole
	if ok, err := c.st.Get(ctx, key, &result); ok {
		return result, nil
	} else if err != nil {
		c.log.WarnContext(ctx, "cache: ListRoles get", "err", err)
	}

	result, err := c.svc.ListRoles(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Set(ctx, key, result, c.configTTL); err != nil {
		c.log.WarnContext(ctx, "cache: ListRoles set", "err", err)
	}
	return result, nil
}

// CreateRole delegates to the underlying service and invalidates the roles cache.
func (c *CachedService) CreateRole(ctx context.Context, projectID uuid.UUID, in projectdom.CreateRoleInput) (*projectdom.ProjectRole, error) {
	r, err := c.svc.CreateRole(ctx, projectID, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, rolesKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: CreateRole delete", "err", err)
	}
	return r, nil
}

// UpdateRole delegates to the underlying service and invalidates the roles cache.
func (c *CachedService) UpdateRole(ctx context.Context, projectID, roleID uuid.UUID, in projectdom.UpdateRoleInput) (*projectdom.ProjectRole, error) {
	r, err := c.svc.UpdateRole(ctx, projectID, roleID, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, rolesKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateRole delete", "err", err)
	}
	return r, nil
}

// DeleteRole delegates to the underlying service and invalidates the roles cache.
func (c *CachedService) DeleteRole(ctx context.Context, projectID, roleID uuid.UUID) error {
	if err := c.svc.DeleteRole(ctx, projectID, roleID); err != nil {
		return err
	}
	if err := c.st.Delete(ctx, rolesKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: DeleteRole delete", "err", err)
	}
	return nil
}
