package authz_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz"
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

// TestAuthorizer_ProjectSettingsWildcard confirms the nested
// "project.settings.*" namespace matches its per-area leaves through the
// same suffix-prefix wildcard logic ordinary two-level keys use — this is
// what lets a role grant every settings area at once with a single key.
func TestAuthorizer_ProjectSettingsWildcard(t *testing.T) {
	projectID := uuid.New()
	a := authz.NewAuthorizer(&stubPermissionStore{
		projectPerms: []authz.Permission{authz.PermissionProjectSettingsAll},
	})

	for _, leaf := range []authz.Permission{
		authz.PermissionProjectSettingsTaskTypesWrite,
		authz.PermissionProjectSettingsTaskStatusesRead,
		authz.PermissionProjectSettingsCustomFieldsWrite,
	} {
		ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, "USER", leaf)
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", leaf, err)
		}
		if !ok {
			t.Errorf("expected project.settings.* to authorize %s", leaf)
		}
	}
}

// TestAuthorizer_TasksWriteNoLongerImpliesProjectSettings is the actual
// point of splitting these permissions: someone who can edit a task's
// content must not automatically be able to redefine the project's task
// schema (task types/statuses, custom fields) just because tasks.write and
// project.settings.* used to be the same bit.
func TestAuthorizer_TasksWriteNoLongerImpliesProjectSettings(t *testing.T) {
	projectID := uuid.New()
	a := authz.NewAuthorizer(&stubPermissionStore{
		projectPerms: []authz.Permission{authz.PermissionTasksWrite},
	})

	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, "USER", authz.PermissionProjectSettingsTaskStatusesWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("tasks.write must not authorize project.settings.task_statuses.write")
	}
}

// TestAuthorizer_ViewsNoLongerBorrowsSprintsPermission confirms views.* has
// its own key and sprints.* alone no longer authorizes it.
func TestAuthorizer_ViewsNoLongerBorrowsSprintsPermission(t *testing.T) {
	projectID := uuid.New()
	a := authz.NewAuthorizer(&stubPermissionStore{
		projectPerms: []authz.Permission{authz.PermissionSprintsAll},
	})

	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, "USER", authz.PermissionViewsWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("sprints.* must not authorize views.write")
	}
}
