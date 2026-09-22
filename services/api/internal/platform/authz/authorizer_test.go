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

// defaultRolePermissions returns the permissions a built-in global role is
// seeded with (authz.DefaultGlobalRoles) — what its role row stores right
// after startup, before anyone edits it.
func defaultRolePermissions(t *testing.T, name string) []authz.Permission {
	t.Helper()
	for _, def := range authz.DefaultGlobalRoles() {
		if def.Name == name {
			return def.Permissions
		}
	}
	t.Fatalf("no built-in global role named %q", name)
	return nil
}

// TestAuthorizer_NoStoreGrantsNothing pins fail-closed behavior: with no
// permission store there is nothing to grant from, so every permission is
// denied in both scopes. Nothing keyed off a role name may stand in for it.
func TestAuthorizer_NoStoreGrantsNothing(t *testing.T) {
	a := authz.NewAuthorizer(nil)
	projectID := uuid.New()
	for _, scope := range []*uuid.UUID{nil, &projectID} {
		for _, p := range []authz.Permission{
			authz.PermissionUsersDelete,
			authz.PermissionGlobalRolesAssign,
			authz.PermissionEnvironmentsConnect,
			authz.PermissionAll,
		} {
			ok, err := a.HasPermissions(context.Background(), uuid.New(), scope, p)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", p, err)
			}
			if ok {
				t.Errorf("authorizer with no store granted %q (project scope: %v)", p, scope != nil)
			}
		}
	}
}

// TestAuthorizer_StrippedAdminRoleGrantsNothingFromItsDefaults is the
// regression test for the bug where a user holding the built-in ADMIN role
// could still change users' roles and edit global roles after those
// permissions had been removed from the ADMIN role. Authorization used to
// merge in the default permission set for the role's *name* on top of what
// the role row stores, so stripping a permission from the role changed
// nothing. The permissions ADMIN is *seeded* with are only a starting point:
// once the row no longer stores them, they must not authorize anything.
func TestAuthorizer_StrippedAdminRoleGrantsNothingFromItsDefaults(t *testing.T) {
	a := authz.NewAuthorizer(&stubPermissionStore{}) // ADMIN row stripped bare

	for _, p := range defaultRolePermissions(t, "ADMIN") {
		ok, err := a.HasPermissions(context.Background(), uuid.New(), nil, p)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", p, err)
		}
		if ok {
			t.Errorf("ADMIN's default permission %q authorized a caller whose role no longer stores it", p)
		}
	}
}

// TestAuthorizer_AdminRoleGrantsExactlyWhatItStores checks the same fix from
// the other side: a partially-stripped role grants what it still stores and
// nothing beyond it — including the role-management permissions the report
// was about.
func TestAuthorizer_AdminRoleGrantsExactlyWhatItStores(t *testing.T) {
	a := authz.NewAuthorizer(&stubPermissionStore{
		globalPerms: []authz.Permission{authz.PermissionUsersRead, authz.PermissionProjectsAll},
	})

	tests := []struct {
		perm authz.Permission
		want bool
	}{
		{authz.PermissionUsersRead, true},
		{authz.PermissionProjectsCreate, true}, // via the stored projects.* wildcard
		{authz.PermissionUsersWrite, false},
		{authz.PermissionUsersDelete, false},
		{authz.PermissionGlobalRolesRead, false},
		{authz.PermissionGlobalRolesWrite, false},
		{authz.PermissionGlobalRolesAssign, false},
		{authz.PermissionSettingsWrite, false},
		{authz.PermissionAgentsWrite, false},
		{authz.PermissionPluginsWrite, false},
	}
	for _, tc := range tests {
		ok, err := a.HasPermissions(context.Background(), uuid.New(), nil, tc.perm)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.perm, err)
		}
		if ok != tc.want {
			t.Errorf("HasPermissions(%q) = %v, want %v", tc.perm, ok, tc.want)
		}
	}
}

// TestAuthorizer_AdminDefaultsFromStoreAuthorizeIntendedGlobalPermissions
// guards against overcorrecting: a role row that stores ADMIN's seeded
// defaults must still authorize ADMIN's real, intended global capabilities.
func TestAuthorizer_AdminDefaultsFromStoreAuthorizeIntendedGlobalPermissions(t *testing.T) {
	a := authz.NewAuthorizer(&stubPermissionStore{globalPerms: defaultRolePermissions(t, "ADMIN")})
	for _, p := range []authz.Permission{
		authz.PermissionUsersAll,
		authz.PermissionGlobalRolesRead, // may see the roles, not change or hand them out
		authz.PermissionProjectsAll,
		authz.PermissionSettingsWrite,
		authz.PermissionAgentsAll,
		authz.PermissionPluginsAll,
	} {
		ok, err := a.HasPermissions(context.Background(), uuid.New(), nil, p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Errorf("expected a role storing ADMIN's defaults to authorize global permission %q", p)
		}
	}
}

// TestAuthorizer_AdminDefaultNamedPermissionsDoNotCrossIntoProjectScope is the
// GHSA-hjcj-373w-vq8m guard for ADMIN's seeded permission set. Two of ADMIN's
// real, intended global permissions — agents.* and projects.* — are *also*
// used to gate project-scoped routes (a project's own agent config/secrets,
// and a project's own entity — see router.go's ProjectScopeFromParam
// ("projectId") routes for both). A global role holding them must not be able
// to reach into a project it was never added to. Every permission ADMIN is
// seeded with is checked, not just the two that collide today, so a future
// addition to ADMIN's set is covered automatically.
func TestAuthorizer_AdminDefaultNamedPermissionsDoNotCrossIntoProjectScope(t *testing.T) {
	a := authz.NewAuthorizer(&stubPermissionStore{globalPerms: defaultRolePermissions(t, "ADMIN")})
	projectID := uuid.New()
	for _, p := range defaultRolePermissions(t, "ADMIN") {
		ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, p)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", p, err)
		}
		if ok {
			t.Errorf("a global role storing ADMIN's defaults must not satisfy project-scoped %q absent a project-membership grant (GHSA-hjcj-373w-vq8m)", p)
		}
	}
}

// TestAuthorizer_SuperAdminWildcardStillAppliesInProjectScope guards against
// overcorrecting: SUPER_ADMIN's PermissionAll is the one global grant that is
// *supposed* to reach every project regardless of membership (confirmed
// intentional in the PR for GHSA-hjcj-373w-vq8m, unlike ADMIN's narrower set).
// addGlobalGrants must keep letting it through.
func TestAuthorizer_SuperAdminWildcardStillAppliesInProjectScope(t *testing.T) {
	a := authz.NewAuthorizer(&stubPermissionStore{globalPerms: defaultRolePermissions(t, "SUPER_ADMIN")})
	projectID := uuid.New()
	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, authz.PermissionEnvironmentsConnect)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("SUPER_ADMIN's stored wildcard must still satisfy project-scoped permissions with no membership grant")
	}
}

// TestAuthorizer_GlobalRoleNamedPermissionDoesNotCrossIntoProjectScope
// covers a global role read via PermissionStore.ListGlobalPermissions (backed
// by the global_roles DB table). The seed migration (000001_init.sql) has
// granted the DB-backed "ADMIN" global role projects.* since before
// GHSA-hjcj-373w-vq8m, so a user whose users.role_id points at that seeded
// row must not be able to use it to rename or delete a project they were
// never added to.
func TestAuthorizer_GlobalRoleNamedPermissionDoesNotCrossIntoProjectScope(t *testing.T) {
	projectID := uuid.New()
	a := authz.NewAuthorizer(&stubPermissionStore{
		globalPerms: []authz.Permission{authz.PermissionProjectsAll},
	})

	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, authz.PermissionProjectsDelete)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("an explicitly-assigned global role's projects.* must not satisfy project-scoped projects.delete absent a project-membership grant")
	}
}

// TestAuthorizer_GlobalRoleWildcardStillAppliesInProjectScope is the
// DB-backed-role parity check for
// TestAuthorizer_LegacySuperAdminWildcardStillAppliesInProjectScope: an
// explicitly-assigned global role holding the literal PermissionAll wildcard
// (the DB equivalent of SUPER_ADMIN) must still reach every project, same as
// the legacy claim does.
func TestAuthorizer_GlobalRoleWildcardStillAppliesInProjectScope(t *testing.T) {
	projectID := uuid.New()
	a := authz.NewAuthorizer(&stubPermissionStore{
		globalPerms: []authz.Permission{authz.PermissionAll},
	})

	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, authz.PermissionProjectsDelete)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("an explicitly-assigned global role's PermissionAll wildcard must still satisfy project-scoped permissions with no membership grant")
	}
}

func TestAuthorizer_GlobalAndProjectPermissions(t *testing.T) {
	projectID := uuid.New()
	a := authz.NewAuthorizer(&stubPermissionStore{
		globalPerms:  []authz.Permission{authz.PermissionGlobalRolesRead},
		projectPerms: []authz.Permission{authz.PermissionTasksWrite},
	})

	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, authz.PermissionTasksWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected project permission to authorize")
	}
}

func TestAuthorizer_WildcardMatch(t *testing.T) {
	a := authz.NewAuthorizer(&stubPermissionStore{globalPerms: []authz.Permission{authz.PermissionTasksAll}})
	ok, err := a.HasPermissions(context.Background(), uuid.New(), nil, authz.PermissionTasksWrite)
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
		authz.PermissionProjectSettingsTaskStatusesWrite,
		authz.PermissionProjectSettingsCustomFieldsWrite,
	} {
		ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, leaf)
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

	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, authz.PermissionProjectSettingsTaskStatusesWrite)
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

	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, authz.PermissionViewsWrite)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("sprints.* must not authorize views.write")
	}
}
