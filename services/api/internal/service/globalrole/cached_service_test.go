// Package globalrolesvc_test contains unit tests for the globalrole service
// layer, including the CachedService decorator.
package globalrolesvc_test

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

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/platform/cache"
	globalrolesvc "github.com/Paca-AI/api/internal/service/globalrole"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newCacheStore spins up an embedded Redis server and returns a Store wired
// to it. The server is stopped and the client closed when t ends.
func newCacheStore(t *testing.T) *cache.Store {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return cache.NewStore(client, "paca:")
}

// ---------------------------------------------------------------------------
// Stub global-role service
// ---------------------------------------------------------------------------

type stubGlobalRoleSvc struct {
	list             func(ctx context.Context) ([]*globalroledom.GlobalRole, error)
	create           func(ctx context.Context, in globalroledom.CreateInput) (*globalroledom.GlobalRole, error)
	update           func(ctx context.Context, id uuid.UUID, in globalroledom.UpdateInput) (*globalroledom.GlobalRole, error)
	delete           func(ctx context.Context, id uuid.UUID) error
	replaceUserRoles func(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) ([]*globalroledom.GlobalRole, error)

	listCalls int
}

// ensure stubGlobalRoleSvc satisfies the domain service interface.
var _ globalroledom.Service = (*stubGlobalRoleSvc)(nil)

func (s *stubGlobalRoleSvc) List(ctx context.Context) ([]*globalroledom.GlobalRole, error) {
	s.listCalls++
	if s.list != nil {
		return s.list(ctx)
	}
	return nil, nil
}

func (s *stubGlobalRoleSvc) Create(ctx context.Context, in globalroledom.CreateInput, _ authz.PermissionSet) (*globalroledom.GlobalRole, error) {
	if s.create != nil {
		return s.create(ctx, in)
	}
	return &globalroledom.GlobalRole{ID: uuid.New(), Name: in.Name}, nil
}

func (s *stubGlobalRoleSvc) Update(ctx context.Context, id uuid.UUID, in globalroledom.UpdateInput, _ authz.PermissionSet) (*globalroledom.GlobalRole, error) {
	if s.update != nil {
		return s.update(ctx, id, in)
	}
	return &globalroledom.GlobalRole{ID: id, Name: in.Name}, nil
}

func (s *stubGlobalRoleSvc) Delete(ctx context.Context, id uuid.UUID) error {
	if s.delete != nil {
		return s.delete(ctx, id)
	}
	return nil
}

func (s *stubGlobalRoleSvc) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID, _ authz.PermissionSet) ([]*globalroledom.GlobalRole, error) {
	if s.replaceUserRoles != nil {
		return s.replaceUserRoles(ctx, userID, roleIDs)
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// CachedService – List
// ---------------------------------------------------------------------------

func TestCachedGlobalRole_List_CacheMissPopulatesCache(t *testing.T) {
	ctx := context.Background()
	roles := []*globalroledom.GlobalRole{{ID: uuid.New(), Name: "ADMIN"}}
	stub := &stubGlobalRoleSvc{
		list: func(_ context.Context) ([]*globalroledom.GlobalRole, error) { return roles, nil },
	}
	svc := globalrolesvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	// First call: cache miss → delegates to stub.
	got, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List (miss): %v", err)
	}
	if len(got) != 1 || got[0].Name != "ADMIN" {
		t.Fatalf("List (miss): unexpected result %v", got)
	}
	if stub.listCalls != 1 {
		t.Fatalf("List (miss): expected 1 stub call, got %d", stub.listCalls)
	}

	// Second call: cache hit → stub is NOT called again.
	got2, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List (hit): %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("List (hit): unexpected result %v", got2)
	}
	if stub.listCalls != 1 {
		t.Fatalf("List (hit): expected stub to not be called again, got %d calls", stub.listCalls)
	}
}

func TestCachedGlobalRole_List_ZeroTTLBypassesCache(t *testing.T) {
	ctx := context.Background()
	roles := []*globalroledom.GlobalRole{{ID: uuid.New(), Name: "USER"}}
	stub := &stubGlobalRoleSvc{
		list: func(_ context.Context) ([]*globalroledom.GlobalRole, error) { return roles, nil },
	}
	svc := globalrolesvc.NewCachedService(stub, newCacheStore(t), 0, discardLogger())

	for i := 0; i < 3; i++ {
		if _, err := svc.List(ctx); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if stub.listCalls != 3 {
		t.Fatalf("TTL=0 should bypass cache; expected 3 stub calls, got %d", stub.listCalls)
	}
}

func TestCachedGlobalRole_List_ServiceErrorPropagated(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("db error")
	stub := &stubGlobalRoleSvc{
		list: func(_ context.Context) ([]*globalroledom.GlobalRole, error) { return nil, sentinel },
	}
	svc := globalrolesvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	_, err := svc.List(ctx)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// CachedService – Create invalidates list cache
// ---------------------------------------------------------------------------

func TestCachedGlobalRole_Create_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	stub := &stubGlobalRoleSvc{
		list: func(_ context.Context) ([]*globalroledom.GlobalRole, error) {
			return []*globalroledom.GlobalRole{{ID: uuid.New(), Name: "CACHED"}}, nil
		},
	}
	svc := globalrolesvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	// Populate the cache.
	if _, err := svc.List(ctx); err != nil {
		t.Fatalf("initial List: %v", err)
	}
	if stub.listCalls != 1 {
		t.Fatalf("expected 1 stub call after initial List, got %d", stub.listCalls)
	}

	// Create a role → should evict the list key.
	if _, err := svc.Create(ctx, globalroledom.CreateInput{Name: "NEW"}, authz.PermissionSet{authz.PermissionAll: {}}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Next List call must hit the stub again (cache was evicted).
	if _, err := svc.List(ctx); err != nil {
		t.Fatalf("List after Create: %v", err)
	}
	if stub.listCalls != 2 {
		t.Fatalf("expected 2 stub calls after Create+List, got %d", stub.listCalls)
	}
}

func TestCachedGlobalRole_Update_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	stub := &stubGlobalRoleSvc{
		list: func(_ context.Context) ([]*globalroledom.GlobalRole, error) {
			return []*globalroledom.GlobalRole{{ID: uuid.New()}}, nil
		},
	}
	svc := globalrolesvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.List(ctx); err != nil {
		t.Fatalf("List: %v", err)
	}

	if _, err := svc.Update(ctx, uuid.New(), globalroledom.UpdateInput{Name: "X"}, authz.PermissionSet{authz.PermissionAll: {}}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if _, err := svc.List(ctx); err != nil {
		t.Fatalf("List after Update: %v", err)
	}
	if stub.listCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listCalls)
	}
}

func TestCachedGlobalRole_Delete_InvalidatesList(t *testing.T) {
	ctx := context.Background()
	stub := &stubGlobalRoleSvc{
		list: func(_ context.Context) ([]*globalroledom.GlobalRole, error) {
			return []*globalroledom.GlobalRole{{ID: uuid.New()}}, nil
		},
	}
	svc := globalrolesvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.List(ctx); err != nil {
		t.Fatalf("List: %v", err)
	}

	if err := svc.Delete(ctx, uuid.New()); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := svc.List(ctx); err != nil {
		t.Fatalf("List after Delete: %v", err)
	}
	if stub.listCalls != 2 {
		t.Fatalf("expected 2 stub calls, got %d", stub.listCalls)
	}
}

// ---------------------------------------------------------------------------
// CachedService – write errors propagate; cache invalidation errors are non-fatal
// ---------------------------------------------------------------------------

func TestCachedGlobalRole_Create_ServiceErrorPropagated(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("repo error")
	stub := &stubGlobalRoleSvc{
		create: func(_ context.Context, _ globalroledom.CreateInput) (*globalroledom.GlobalRole, error) {
			return nil, sentinel
		},
	}
	svc := globalrolesvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	_, err := svc.Create(ctx, globalroledom.CreateInput{Name: "X"}, authz.PermissionSet{authz.PermissionAll: {}})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// CachedService – ReplaceUserRoles passes through (no cache involvement)
// ---------------------------------------------------------------------------

func TestCachedGlobalRole_ReplaceUserRoles_Passthrough(t *testing.T) {
	ctx := context.Background()
	called := false
	stub := &stubGlobalRoleSvc{
		replaceUserRoles: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID) ([]*globalroledom.GlobalRole, error) {
			called = true
			return nil, nil
		},
	}
	svc := globalrolesvc.NewCachedService(stub, newCacheStore(t), 5*time.Minute, discardLogger())

	if _, err := svc.ReplaceUserRoles(ctx, uuid.New(), nil, authz.PermissionSet{authz.PermissionAll: {}}); err != nil {
		t.Fatalf("ReplaceUserRoles: %v", err)
	}
	if !called {
		t.Fatal("ReplaceUserRoles should delegate to underlying service")
	}
}
