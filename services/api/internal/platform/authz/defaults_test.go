package authz_test

import (
	"testing"

	"github.com/Paca-AI/api/internal/platform/authz"
)

// TestDefaultProjectRoles_ProjectMemberHasNoSettingsWritePermissions is a
// regression test: PROJECT_MEMBER (the project_id-IS-NULL template that
// mirrors the real per-project "Editor" role — see projectsvc.Service.Create
// and 000054_add_project_settings_permissions.sql's comment on the two)
// previously granted the PermissionProjectSettingsAll wildcard, which
// includes project.settings.*.write — letting a project member redefine the
// project's task schema, an Admin-level action the real Editor role never
// actually granted.
func TestDefaultProjectRoles_ProjectMemberHasNoSettingsWritePermissions(t *testing.T) {
	var member *authz.RoleDefinition
	for _, def := range authz.DefaultProjectRoles() {
		if def.Name == "PROJECT_MEMBER" {
			d := def
			member = &d
			break
		}
	}
	if member == nil {
		t.Fatal("expected a PROJECT_MEMBER role definition, found none")
	}

	disallowed := map[authz.Permission]bool{
		authz.PermissionProjectSettingsTaskTypesWrite:    true,
		authz.PermissionProjectSettingsTaskStatusesWrite: true,
		authz.PermissionProjectSettingsCustomFieldsWrite: true,
		authz.PermissionProjectSettingsAll:               true,
	}
	for _, p := range member.Permissions {
		if disallowed[p] {
			t.Errorf("PROJECT_MEMBER must not grant %q", p)
		}
	}

	// There's no dedicated read permission for the schema at all (see
	// authz.PermissionProjectSettingsTaskTypesWrite's doc comment) — viewing
	// task types/statuses/custom fields is implied by tasks.read, which
	// PROJECT_MEMBER must still hold.
	hasTasksRead := false
	for _, p := range member.Permissions {
		if p == authz.PermissionTasksRead {
			hasTasksRead = true
			break
		}
	}
	if !hasTasksRead {
		t.Error("PROJECT_MEMBER should still grant tasks.read")
	}
}

// TestLegacyPermissionsForRole_MatchesDefaultGlobalRoles is a regression test
// for GHSA-hjcj-373w-vq8m: LegacyPermissionsForRole used to hand-maintain its
// own permission list per role name, and its ADMIN case had drifted to the
// bare PermissionAll wildcard while DefaultGlobalRoles' ADMIN entry was
// correctly scoped to global-only permissions — letting any caller keyed off
// the legacy role claim (the authz middleware included) bypass
// project-membership checks entirely. Asserting exact set-equality against
// DefaultGlobalRoles for every defined role, rather than re-asserting ADMIN's
// list by hand, also catches the same class of drift for any future role.
func TestLegacyPermissionsForRole_MatchesDefaultGlobalRoles(t *testing.T) {
	for _, def := range authz.DefaultGlobalRoles() {
		got := authz.LegacyPermissionsForRole(def.Name)
		if !samePermissionSet(got, def.Permissions) {
			t.Errorf("LegacyPermissionsForRole(%q) = %v, want %v (DefaultGlobalRoles)", def.Name, got, def.Permissions)
		}
	}
}

// TestLegacyPermissionsForRole_AdminNoLongerGrantsWildcard directly pins the
// GHSA-hjcj-373w-vq8m fix: the global ADMIN legacy role must never resolve to
// PermissionAll, no matter how DefaultGlobalRoles evolves.
func TestLegacyPermissionsForRole_AdminNoLongerGrantsWildcard(t *testing.T) {
	for _, p := range authz.LegacyPermissionsForRole("ADMIN") {
		if p == authz.PermissionAll {
			t.Fatal(`LegacyPermissionsForRole("ADMIN") must not include the PermissionAll wildcard — see GHSA-hjcj-373w-vq8m`)
		}
	}
}

// samePermissionSet compares a and b as sets (order- and duplicate-
// insensitive) — builds both sides into sets first so a duplicate on one
// side can't paper over a genuinely missing element on the other, the way
// comparing len(a) == len(b) against one-directional membership could.
func samePermissionSet(a, b []authz.Permission) bool {
	setA := make(map[authz.Permission]struct{}, len(a))
	for _, p := range a {
		setA[p] = struct{}{}
	}
	setB := make(map[authz.Permission]struct{}, len(b))
	for _, p := range b {
		setB[p] = struct{}{}
	}
	if len(setA) != len(setB) {
		return false
	}
	for p := range setA {
		if _, ok := setB[p]; !ok {
			return false
		}
	}
	return true
}
