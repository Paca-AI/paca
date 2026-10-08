package projectsvc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/cache"
)

// CachedService decorates a projectdom.Service with a Valkey/Redis-backed
// cache.
//
// # What is cached
//
//   - GetByID        – project detail; keyed by project ID.
//   - ListMembers    – project member list; keyed by project ID.
//
// List/ListAccessible are NOT cached because they are paginated, potentially
// user-scoped, and have high result-set cardinality.
// IsProjectPublic is NOT cached; it is a single-column read used in hot-path
// middleware and is already handled by the database's plan cache.
//
// Cache errors are non-fatal: read errors fall through to the real service;
// write/delete errors are logged so mutations always succeed.
type CachedService struct {
	svc        projectdom.Service
	jevConfig  projectJevConfigWriter
	st         *cache.Store
	projectTTL time.Duration
	log        *slog.Logger
}

// projectJevConfigWriter is the one method this decorator adds on top of
// projectdom.Service. UpdateJevConfig is deliberately absent from that
// interface (see the handler's WithProjectJevConfigService doc comment), so
// it's picked up here by assertion against the concrete service rather than
// by widening projectdom.Service and every mock implementing it.
type projectJevConfigWriter interface {
	UpdateJevConfig(ctx context.Context, projectID uuid.UUID, apiKey, baseURL, model *string) (*projectdom.Project, error)
}

// ErrJevConfigUnsupported is returned by UpdateJevConfig when the service
// this decorator wraps doesn't implement projectJevConfigWriter. Only the
// concrete production service does; test doubles that don't care about Jev
// credentials can leave it unimplemented.
var ErrJevConfigUnsupported = errors.New("project svc: jev config updates are not supported by the underlying service")

// NewCachedService wraps svc with a caching layer backed by st.
//
//   - projectTTL governs project detail and member data.
//
// Pass zero to disable caching.
// log receives non-fatal cache warnings.
func NewCachedService(svc projectdom.Service, st *cache.Store, projectTTL time.Duration, log *slog.Logger) *CachedService {
	// UpdateJevConfig isn't part of projectdom.Service, so it's picked up by
	// assertion — production passes the concrete service, which has it; test
	// doubles that don't care simply leave it unset and UpdateJevConfig then
	// reports ErrJevConfigUnsupported.
	config, _ := svc.(projectJevConfigWriter)
	return &CachedService{
		svc:        svc,
		jevConfig:  config,
		st:         st,
		projectTTL: projectTTL,
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
	if err := c.st.Delete(ctx, projectKey(id), membersKey(id)); err != nil {
		c.log.WarnContext(ctx, "cache: DeleteProject delete", "err", err)
	}
	return nil
}

// InitiateAvatarUpload delegates directly to the underlying service (nothing to cache).
func (c *CachedService) InitiateAvatarUpload(ctx context.Context, projectID uuid.UUID, fileName, contentType string, fileSize int64, uploadedBy uuid.UUID) (*attachmentdom.UploadSession, error) {
	return c.svc.InitiateAvatarUpload(ctx, projectID, fileName, contentType, fileSize, uploadedBy)
}

// CompleteAvatarUpload delegates to the underlying service and invalidates the project cache entry.
func (c *CachedService) CompleteAvatarUpload(ctx context.Context, projectID, fileID uuid.UUID) (*projectdom.Project, error) {
	p, err := c.svc.CompleteAvatarUpload(ctx, projectID, fileID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, projectKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: CompleteAvatarUpload delete", "err", err)
	}
	return p, nil
}

// RemoveAvatar delegates to the underlying service and invalidates the project cache entry.
func (c *CachedService) RemoveAvatar(ctx context.Context, projectID uuid.UUID) (*projectdom.Project, error) {
	p, err := c.svc.RemoveAvatar(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, projectKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: RemoveAvatar delete", "err", err)
	}
	return p, nil
}

// UpdateJevConfig delegates to the underlying service and invalidates the
// project cache entry.
//
// The invalidation is the whole reason this wrapper exists. Jev credentials
// are read back off a cached *projectdom.Project (GetByID caches the entity
// whole, JevAPIKeySecret/JevBaseURL/JevModel included) by this handler's own
// TestJevConfig/GetProject and by every Jev-dependent worker, so a write that
// didn't drop the entry would leave the save-then-test flow — the two
// endpoints are adjacent in the router — testing the credentials the caller
// just replaced, for up to projectTTL.
func (c *CachedService) UpdateJevConfig(ctx context.Context, projectID uuid.UUID, apiKey, baseURL, model *string) (*projectdom.Project, error) {
	if c.jevConfig == nil {
		return nil, ErrJevConfigUnsupported
	}
	p, err := c.jevConfig.UpdateJevConfig(ctx, projectID, apiKey, baseURL, model)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, projectKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateJevConfig delete", "err", err)
	}
	return p, nil
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

// CountDistinctAgentsByProjects delegates to the underlying service without
// caching — an arbitrary-project-ID-set
// aggregate isn't a good fit for this cache's per-project key scheme.
func (c *CachedService) CountDistinctAgentsByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error) {
	return c.svc.CountDistinctAgentsByProjects(ctx, projectIDs)
}

// AddMember delegates to the underlying service and invalidates the members cache.
func (c *CachedService) AddMember(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput) (*projectdom.ProjectMember, error) {
	m, err := c.svc.AddMember(ctx, projectID, in)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, membersKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: AddMember delete", "err", err)
	}
	return m, nil
}

// UpdateMemberDescription delegates to the underlying service and invalidates the members cache.
func (c *CachedService) UpdateMemberDescription(ctx context.Context, projectID, memberID uuid.UUID, description string) (*projectdom.ProjectMember, error) {
	m, err := c.svc.UpdateMemberDescription(ctx, projectID, memberID, description)
	if err != nil {
		return nil, err
	}
	if err := c.st.Delete(ctx, membersKey(projectID)); err != nil {
		c.log.WarnContext(ctx, "cache: UpdateMemberDescription delete", "err", err)
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
func (c *CachedService) AddAgentMember(ctx context.Context, memberID, projectID, agentID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) error {
	if err := c.svc.AddAgentMember(ctx, memberID, projectID, agentID, roleIDs, createdBy); err != nil {
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
