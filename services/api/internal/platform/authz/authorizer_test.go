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

// TestAuthorizer_LegacyAdminCannotSatisfyProjectScopedPermission is a
// regression test for GHSA-hjcj-373w-vq8m. LegacyPermissionsForRole("ADMIN")
// used to return PermissionAll, which short-circuits hasPermission for any
// required permission — including project-scoped ones such as
// environments.connect (the highest-impact reachable route: minting a
// terminal ticket for shell access) — regardless of whether the caller is a
// member of the requested project. The authorizer is given a nil store here
// so the only thing granting permissions is the legacy role claim itself,
// isolating the bug from any project-membership lookup: in production this
// scope would also consult AuthzPermissionStore.ListProjectPermissions, which
// correctly returns nothing for a non-member, but the wildcard grant used to
// make that check unreachable (hasPermission returns true on granted["*"]
// before ever looking at the required permission).
func TestAuthorizer_LegacyAdminCannotSatisfyProjectScopedPermission(t *testing.T) {
	a := authz.NewAuthorizer(nil)
	projectID := uuid.New()
	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, "ADMIN", authz.PermissionEnvironmentsConnect)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("global ADMIN role claim must not satisfy a project-scoped permission absent a project-membership grant")
	}
}

// TestAuthorizer_LegacyAdminStillHasIntendedGlobalPermissions guards against
// overcorrecting the GHSA-hjcj-373w-vq8m fix into denying ADMIN's real,
// intended global-scope capabilities (defined in DefaultGlobalRoles).
func TestAuthorizer_LegacyAdminStillHasIntendedGlobalPermissions(t *testing.T) {
	a := authz.NewAuthorizer(nil)
	for _, p := range []authz.Permission{
		authz.PermissionUsersAll,
		authz.PermissionGlobalRolesAll,
		authz.PermissionProjectsAll,
		authz.PermissionSettingsWrite,
		authz.PermissionAgentsAll,
		authz.PermissionPluginsAll,
	} {
		ok, err := a.HasPermissions(context.Background(), uuid.New(), nil, "ADMIN", p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Errorf("expected ADMIN legacy role to still authorize global permission %q", p)
		}
	}
}

// TestAuthorizer_LegacyAdminNamedPermissionsDoNotCrossIntoProjectScope closes
// the gap TestAuthorizer_LegacyAdminCannotSatisfyProjectScopedPermission left
// open: that test only checked environments.connect, which ADMIN's new
// permission set never included. Two of ADMIN's real, intended global
// permissions — agents.* and projects.* — are *also* used to gate
// project-scoped routes (a project's own agent config/secrets, and a
// project's own entity — see router.go's ProjectScopeFromParam("projectId")
// routes for both). Because hasPermissionsForActor used to merge every
// global grant in regardless of scope, ADMIN could still reach any project's
// agent env vars/MCP servers or rename/delete any project without ever being
// added to it — the exact bug this advisory reports, just narrower than the
// bare wildcard. Every permission ADMIN holds must be checked here, not just
// the two that happen to collide today, so a future addition to ADMIN's set
// is automatically covered.
func TestAuthorizer_LegacyAdminNamedPermissionsDoNotCrossIntoProjectScope(t *testing.T) {
	a := authz.NewAuthorizer(nil)
	projectID := uuid.New()
	for _, p := range authz.LegacyPermissionsForRole("ADMIN") {
		ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, "ADMIN", p)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", p, err)
		}
		if ok {
			t.Errorf("global ADMIN role claim must not satisfy project-scoped %q absent a project-membership grant (GHSA-hjcj-373w-vq8m)", p)
		}
	}
}

// TestAuthorizer_LegacySuperAdminWildcardStillAppliesInProjectScope guards
// against overcorrecting: SUPER_ADMIN's PermissionAll is the one legacy grant
// that is *supposed* to reach every project regardless of membership (see the
// PR description for GHSA-hjcj-373w-vq8m — this is confirmed intentional,
// unlike ADMIN's narrower set). addGlobalGrants must keep letting it through.
func TestAuthorizer_LegacySuperAdminWildcardStillAppliesInProjectScope(t *testing.T) {
	a := authz.NewAuthorizer(nil)
	projectID := uuid.New()
	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, "SUPER_ADMIN", authz.PermissionEnvironmentsConnect)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("legacy SUPER_ADMIN's wildcard must still satisfy project-scoped permissions with no membership grant")
	}
}

// TestAuthorizer_GlobalRoleNamedPermissionDoesNotCrossIntoProjectScope is
// TestAuthorizer_LegacyAdminNamedPermissionsDoNotCrossIntoProjectScope's
// sibling for the *other* source of global permissions: an explicitly
// assigned global role read via PermissionStore.ListGlobalPermissions
// (backed by the global_roles DB table, not the legacy role claim). This
// path has the identical scope-blind merge, and the seed migration
// (000001_init.sql) has granted the DB-backed "ADMIN" global role
// projects.* since before this advisory — independently of
// LegacyPermissionsForRole, and independently of this fix's change to
// DefaultGlobalRoles. A user whose users.role_id points at that seeded row
// (rather than relying on the legacy claims.Role string) must not be able to
// use it to rename or delete a project they were never added to.
func TestAuthorizer_GlobalRoleNamedPermissionDoesNotCrossIntoProjectScope(t *testing.T) {
	projectID := uuid.New()
	a := authz.NewAuthorizer(&stubPermissionStore{
		globalPerms: []authz.Permission{authz.PermissionProjectsAll},
	})

	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, "USER", authz.PermissionProjectsDelete)
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

	ok, err := a.HasPermissions(context.Background(), uuid.New(), &projectID, "USER", authz.PermissionProjectsDelete)
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
		authz.PermissionProjectSettingsTaskStatusesWrite,
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
