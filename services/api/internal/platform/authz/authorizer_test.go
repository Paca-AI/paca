package authz_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/paca/api/internal/platform/authz"
)

type stubPermissionStore struct {
	globalPerms  []authz.Permission
	projectPerms []authz.Permission
}

func (s *stubPermissionStore) ListGlobalPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return s.globalPerms, nil
}

func (s *stubPermissionStore) ListProjectPermissions(context.Context, uuid.UUID, uuid.UUID) ([]authz.Permission, error) {
	return s.projectPerms, nil
}

func TestAuthorizer_LegacyAdminFallback(t *testing.T) {
	a := authz.NewAuthorizer(nil)
	ok, err := a.HasPermissions(context.Background(), uuid.New(), nil, "ADMIN", authz.PermissionUsersDelete)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ADMIN legacy role to authorize users.delete")
	}
}

func TestAuthorizer_GlobalAndProjectPermissions(t *testing.T) {
	projectID := uuid.New()
	a := authz.NewAuthorizer(&stubPermissionStore{
		globalPerms:  []authz.Permission{authz.PermissionGlobalRolesRead},
		projectPerms: []authz.Permission{authz.PermissionTasksWrite},
	})

	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, "USER", authz.PermissionTasksWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected project permission to authorize")
	}
}

func TestAuthorizer_WildcardMatch(t *testing.T) {
	a := authz.NewAuthorizer(&stubPermissionStore{globalPerms: []authz.Permission{authz.PermissionTasksAll}})
	ok, err := a.HasPermissions(context.Background(), uuid.New(), nil, "USER", authz.PermissionTasksWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected tasks.* to authorize tasks.write")
	}
}
