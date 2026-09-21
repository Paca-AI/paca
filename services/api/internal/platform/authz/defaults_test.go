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

// TestDefaultGlobalRoles_OnlySuperAdminSeedsTheWildcard pins the
// GHSA-hjcj-373w-vq8m fix at its source. The ADMIN row is synced from this
// definition at startup, and a stored PermissionAll reaches every project
// regardless of membership — so it may be seeded onto SUPER_ADMIN only, no
// matter how DefaultGlobalRoles evolves.
func TestDefaultGlobalRoles_OnlySuperAdminSeedsTheWildcard(t *testing.T) {
	sawSuperAdmin := false
	for _, def := range authz.DefaultGlobalRoles() {
		hasWildcard := false
		for _, p := range def.Permissions {
			if p == authz.PermissionAll {
				hasWildcard = true
			}
		}
		if def.Name == "SUPER_ADMIN" {
			sawSuperAdmin = true
			if !hasWildcard {
				t.Error("SUPER_ADMIN must be seeded with the PermissionAll wildcard")
			}
			continue
		}
		if hasWildcard {
			t.Errorf("built-in role %q must not be seeded with the PermissionAll wildcard — see GHSA-hjcj-373w-vq8m", def.Name)
		}
	}
	if !sawSuperAdmin {
		t.Error("expected a SUPER_ADMIN role definition, found none")
	}
}
