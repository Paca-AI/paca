// Package projectsvc_test contains unit tests for the project service layer,
// including the CachedService decorator.
package projectsvc_test

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

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/platform/cache"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
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

const (
	projectTTL = 5 * time.Minute
	configTTL  = 10 * time.Minute
)

// ---------------------------------------------------------------------------
// Stub project service
// ---------------------------------------------------------------------------

type stubProjectSvc struct {
	list                    func(ctx context.Context, page, pageSize int) ([]*projectdom.Project, int64, error)
	listAccessible          func(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]*projectdom.Project, int64, error)
	getByID                 func(ctx context.Context, id uuid.UUID) (*projectdom.Project, error)
	isProjectPublic         func(ctx context.Context, id uuid.UUID) (bool, error)
	create                  func(ctx context.Context, in projectdom.CreateProjectInput) (*projectdom.Project, error)
	update                  func(ctx context.Context, id uuid.UUID, in projectdom.UpdateProjectInput) (*projectdom.Project, error)
	delete                  func(ctx context.Context, id uuid.UUID) error
	listMembers             func(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error)
	addMember               func(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput) (*projectdom.ProjectMember, error)
	updateMemberRole        func(ctx context.Context, projectID, userID uuid.UUID, in projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error)
	removeMember            func(ctx context.Context, projectID, userID uuid.UUID) error
	getMyProjectPermissions func(ctx context.Context, projectID, userID uuid.UUID, agentID *uuid.UUID) (map[string]any, error)
	listRoles               func(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectRole, error)
	createRole              func(ctx context.Context, projectID uuid.UUID, in projectdom.CreateRoleInput) (*projectdom.ProjectRole, error)
	updateRole              func(ctx context.Context, projectID, roleID uuid.UUID, in projectdom.UpdateRoleInput) (*projectdom.ProjectRole, error)
	deleteRole              func(ctx context.Context, projectID, roleID uuid.UUID) error

	getByIDCalls     int
	listMembersCalls int
	listRolesCalls   int
}

func (s *stubProjectSvc) List(ctx context.Context, page, pageSize int) ([]*projectdom.Project, int64, error) {
	if s.list != nil {
		return s.list(ctx, page, pageSize)
	}
	return nil, 0, nil
}

func (s *stubProjectSvc) ListAccessible(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]*projectdom.Project, int64, error) {
	if s.listAccessible != nil {
		return s.listAccessible(ctx, userID, page, pageSize)
	}
	return nil, 0, nil
}

func (s *stubProjectSvc) GetByID(ctx context.Context, id uuid.UUID) (*projectdom.Project, error) {
	s.getByIDCalls++
	if s.getByID != nil {
		return s.getByID(ctx, id)
	}
	return &projectdom.Project{ID: id, Name: "stub-project"}, nil
}

func (s *stubProjectSvc) IsProjectPublic(ctx context.Context, id uuid.UUID) (bool, error) {
	if s.isProjectPublic != nil {
		return s.isProjectPublic(ctx, id)
	}
	return false, nil
}

func (s *stubProjectSvc) Create(ctx context.Context, in projectdom.CreateProjectInput) (*projectdom.Project, error) {
	if s.create != nil {
		return s.create(ctx, in)
	}
	return &projectdom.Project{ID: uuid.New(), Name: in.Name}, nil
}

func (s *stubProjectSvc) Update(ctx context.Context, id uuid.UUID, in projectdom.UpdateProjectInput) (*projectdom.Project, error) {
	if s.update != nil {
		return s.update(ctx, id, in)
	}
	return &projectdom.Project{ID: id, Name: in.Name}, nil
}

func (s *stubProjectSvc) Delete(ctx context.Context, id uuid.UUID) error {
	if s.delete != nil {
		return s.delete(ctx, id)
	}
	return nil
}

func (s *stubProjectSvc) ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	s.listMembersCalls++
	if s.listMembers != nil {
		return s.listMembers(ctx, projectID)
	}
	return []*projectdom.ProjectMember{{ID: uuid.New(), ProjectID: projectID}}, nil
}

func (s *stubProjectSvc) AddMember(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput, _ authz.PermissionSet) (*projectdom.ProjectMember, error) {
	if s.addMember != nil {
		return s.addMember(ctx, projectID, in)
	}
	return &projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, UserID: in.UserID}, nil
}

func (s *stubProjectSvc) UpdateMemberRole(ctx context.Context, projectID, userID uuid.UUID, in projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error) {
	if s.updateMemberRole != nil {
		return s.updateMemberRole(ctx, projectID, userID, in)
	}
	return &projectdom.ProjectMember{ID: uuid.New(), ProjectID: projectID, UserID: userID}, nil
}

func (s *stubProjectSvc) RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error {
	if s.removeMember != nil {
		return s.removeMember(ctx, projectID, userID)
	}
	return nil
}

func (s *stubProjectSvc) GetMyProjectPermissions(ctx context.Context, projectID, userID uuid.UUID, agentID *uuid.UUID) (map[string]any, error) {
	if s.getMyProjectPermissions != nil {
		return s.getMyProjectPermissions(ctx, projectID, userID, agentID)
	}
	return nil, nil
}

func (s *stubProjectSvc) ListRoles(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectRole, error) {
	s.listRolesCalls++
	if s.listRoles != nil {
		return s.listRoles(ctx, projectID)
	}
	return []*projectdom.ProjectRole{{ID: uuid.New()}}, nil
}

func (s *stubProjectSvc) CreateRole(ctx context.Context, projectID uuid.UUID, in projectdom.CreateRoleInput) (*projectdom.ProjectRole, error) {
	if s.createRole != nil {
		return s.createRole(ctx, projectID, in)
	}
	return &projectdom.ProjectRole{ID: uuid.New()}, nil
}

func (s *stubProjectSvc) UpdateRole(ctx context.Context, projectID, roleID uuid.UUID, in projectdom.UpdateRoleInput) (*projectdom.ProjectRole, error) {
	if s.updateRole != nil {
		return s.updateRole(ctx, projectID, roleID, in)
	}
	return &projectdom.ProjectRole{ID: roleID}, nil
}

func (s *stubProjectSvc) DeleteRole(ctx context.Context, projectID, roleID uuid.UUID) error {
	if s.deleteRole != nil {
		return s.deleteRole(ctx, projectID, roleID)
	}
	return nil
}

func (s *stubProjectSvc) AddAgentMember(_ context.Context, _, _, _, _ uuid.UUID) error { return nil }
func (s *stubProjectSvc) RemoveAgentMember(_ context.Context, _, _ uuid.UUID) error    { return nil }
func (s *stubProjectSvc) UpdateMemberRoleByMemberID(_ context.Context, _, _ uuid.UUID, _ projectdom.UpdateMemberRoleInput, _ authz.PermissionSet) (*projectdom.ProjectMember, error) {
	return nil, errors.New("not implemented in stub")
}
func (s *stubProjectSvc) RemoveMemberByMemberID(_ context.Context, _, _ uuid.UUID) error {
	return errors.New("not implemented in stub")
}

// ---------------------------------------------------------------------------
// GetByID
// ---------------------------------------------------------------------------

func TestCachedProject_GetByID_CacheMissPopulatesCache(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubProjectSvc{}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())

	// First call: cache miss.
	p, err := svc.GetByID(ctx, projectID)
	if err != nil {
		t.Fatalf("GetByID (miss): %v", err)
	}
	if p.ID != projectID {
		t.Fatalf("GetByID (miss): got ID %s, want %s", p.ID, projectID)
	}
	if stub.getByIDCalls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.getByIDCalls)
	}

	// Second call: cache hit.
	p2, err := svc.GetByID(ctx, projectID)
	if err != nil {
		t.Fatalf("GetByID (hit): %v", err)
	}
	if p2.ID != projectID {
		t.Fatalf("GetByID (hit): got ID %s, want %s", p2.ID, projectID)
	}
	if stub.getByIDCalls != 1 {
		t.Fatalf("cache hit: stub should not be called again; got %d calls", stub.getByIDCalls)
	}
}

func TestCachedProject_GetByID_ZeroTTLBypassesCache(t *testing.T) {
	ctx := context.Background()
	stub := &stubProjectSvc{}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), 0, configTTL, discardLogger())

	for i := 0; i < 3; i++ {
		if _, err := svc.GetByID(ctx, uuid.New()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if stub.getByIDCalls != 3 {
		t.Fatalf("TTL=0 should bypass cache; want 3 calls, got %d", stub.getByIDCalls)
	}
}

func TestCachedProject_Update_InvalidatesGetByID(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubProjectSvc{}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())

	// Populate cache.
	if _, err := svc.GetByID(ctx, projectID); err != nil {
		t.Fatalf("initial GetByID: %v", err)
	}

	// Update should evict the project key.
	if _, err := svc.Update(ctx, projectID, projectdom.UpdateProjectInput{Name: "new"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// Must call stub again (cache evicted).
	if _, err := svc.GetByID(ctx, projectID); err != nil {
		t.Fatalf("GetByID after Update: %v", err)
	}
	if stub.getByIDCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.getByIDCalls)
	}
}

func TestCachedProject_Delete_InvalidatesAllProjectKeys(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubProjectSvc{}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())

	// Populate caches.
	if _, err := svc.GetByID(ctx, projectID); err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if _, err := svc.ListMembers(ctx, projectID); err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if _, err := svc.ListRoles(ctx, projectID); err != nil {
		t.Fatalf("ListRoles: %v", err)
	}

	// Delete should evict all three.
	if err := svc.Delete(ctx, projectID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Each should hit stub again.
	if _, err := svc.GetByID(ctx, projectID); err != nil {
		t.Fatalf("GetByID after Delete: %v", err)
	}
	if _, err := svc.ListMembers(ctx, projectID); err != nil {
		t.Fatalf("ListMembers after Delete: %v", err)
	}
	if _, err := svc.ListRoles(ctx, projectID); err != nil {
		t.Fatalf("ListRoles after Delete: %v", err)
	}

	if stub.getByIDCalls != 2 {
		t.Fatalf("GetByID: expected 2 stub calls, got %d", stub.getByIDCalls)
	}
	if stub.listMembersCalls != 2 {
		t.Fatalf("ListMembers: expected 2 stub calls, got %d", stub.listMembersCalls)
	}
	if stub.listRolesCalls != 2 {
		t.Fatalf("ListRoles: expected 2 stub calls, got %d", stub.listRolesCalls)
	}
}

// ---------------------------------------------------------------------------
// ListMembers
// ---------------------------------------------------------------------------

func TestCachedProject_ListMembers_CacheHit(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubProjectSvc{}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())

	if _, err := svc.ListMembers(ctx, projectID); err != nil {
		t.Fatalf("ListMembers (miss): %v", err)
	}
	if _, err := svc.ListMembers(ctx, projectID); err != nil {
		t.Fatalf("ListMembers (hit): %v", err)
	}
	if stub.listMembersCalls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.listMembersCalls)
	}
}

func TestCachedProject_AddMember_InvalidatesMembersList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubProjectSvc{}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())

	if _, err := svc.ListMembers(ctx, projectID); err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if _, err := svc.AddMember(ctx, projectID, projectdom.AddMemberInput{UserID: uuid.New(), ProjectRoleID: uuid.New()}, authz.PermissionSet{authz.PermissionAll: {}}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if _, err := svc.ListMembers(ctx, projectID); err != nil {
		t.Fatalf("ListMembers after AddMember: %v", err)
	}
	if stub.listMembersCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listMembersCalls)
	}
}

func TestCachedProject_RemoveMember_InvalidatesMembersList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubProjectSvc{}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())

	if _, err := svc.ListMembers(ctx, projectID); err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if err := svc.RemoveMember(ctx, projectID, uuid.New()); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	if _, err := svc.ListMembers(ctx, projectID); err != nil {
		t.Fatalf("ListMembers after RemoveMember: %v", err)
	}
	if stub.listMembersCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listMembersCalls)
	}
}

// ---------------------------------------------------------------------------
// ListRoles
// ---------------------------------------------------------------------------

func TestCachedProject_ListRoles_CacheHit(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubProjectSvc{}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())

	if _, err := svc.ListRoles(ctx, projectID); err != nil {
		t.Fatalf("ListRoles (miss): %v", err)
	}
	if _, err := svc.ListRoles(ctx, projectID); err != nil {
		t.Fatalf("ListRoles (hit): %v", err)
	}
	if stub.listRolesCalls != 1 {
		t.Fatalf("expected 1 stub call, got %d", stub.listRolesCalls)
	}
}

func TestCachedProject_CreateRole_InvalidatesRolesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubProjectSvc{}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())

	if _, err := svc.ListRoles(ctx, projectID); err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if _, err := svc.CreateRole(ctx, projectID, projectdom.CreateRoleInput{RoleName: "DEV"}); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if _, err := svc.ListRoles(ctx, projectID); err != nil {
		t.Fatalf("ListRoles after CreateRole: %v", err)
	}
	if stub.listRolesCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listRolesCalls)
	}
}

func TestCachedProject_DeleteRole_InvalidatesRolesList(t *testing.T) {
	ctx := context.Background()
	projectID := uuid.New()
	stub := &stubProjectSvc{}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())

	if _, err := svc.ListRoles(ctx, projectID); err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if err := svc.DeleteRole(ctx, projectID, uuid.New()); err != nil {
		t.Fatalf("DeleteRole: %v", err)
	}
	if _, err := svc.ListRoles(ctx, projectID); err != nil {
		t.Fatalf("ListRoles after DeleteRole: %v", err)
	}
	if stub.listRolesCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listRolesCalls)
	}
}

// ---------------------------------------------------------------------------
// Pass-through methods are delegated unchanged
// ---------------------------------------------------------------------------

func TestCachedProject_List_Passthrough(t *testing.T) {
	ctx := context.Background()
	called := false
	stub := &stubProjectSvc{
		list: func(_ context.Context, _, _ int) ([]*projectdom.Project, int64, error) {
			called = true
			return nil, 0, nil
		},
	}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())
	if _, _, err := svc.List(ctx, 1, 20); err != nil {
		t.Fatalf("List: %v", err)
	}
	if !called {
		t.Fatal("List should delegate to underlying service")
	}
}

func TestCachedProject_GetByID_ServiceErrorPropagated(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("not found")
	stub := &stubProjectSvc{
		getByID: func(_ context.Context, _ uuid.UUID) (*projectdom.Project, error) { return nil, sentinel },
	}
	svc := projectsvc.NewCachedService(stub, newCacheStore(t), projectTTL, configTTL, discardLogger())

	_, err := svc.GetByID(ctx, uuid.New())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}
