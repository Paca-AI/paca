package authz

import "testing"

func TestDefaultAdminHasIndependentAuthenticationPermission(t *testing.T) {
	var admin *RoleDefinition
	for i := range DefaultGlobalRoles() {
		role := DefaultGlobalRoles()[i]
		if role.Name == "ADMIN" {
			admin = &role
			break
		}
	}
	if admin == nil {
		t.Fatal("ADMIN role not found")
	}

	granted := make(map[Permission]bool, len(admin.Permissions))
	for _, permission := range admin.Permissions {
		granted[permission] = true
	}
	if !granted[PermissionAuthenticationWrite] {
		t.Fatal("ADMIN must receive authentication.write")
	}
	if !granted[PermissionSettingsWrite] {
		t.Fatal("ADMIN must retain settings.write")
	}
	if PermissionAuthenticationWrite == PermissionSettingsWrite {
		t.Fatal("authentication and branding writes must remain independent permissions")
	}
}
