package authz

import "testing"

func TestPermissionsFromValue(t *testing.T) {
	tests := []struct {
		name    string
		payload any
		want    []Permission
	}{
		{
			name:    "nil payload grants nothing",
			payload: nil,
			want:    []Permission{},
		},
		{
			name:    "empty map grants nothing",
			payload: map[string]any{},
			want:    []Permission{},
		},
		{
			name:    "boolean true is granted",
			payload: map[string]any{"users.read": true},
			want:    []Permission{PermissionUsersRead},
		},
		{
			name:    "boolean false is not granted",
			payload: map[string]any{"users.read": false},
			want:    []Permission{},
		},
		{
			name:    "nonzero number is granted",
			payload: map[string]any{"users.read": float64(1)},
			want:    []Permission{PermissionUsersRead},
		},
		{
			name:    "zero number is not granted",
			payload: map[string]any{"users.read": float64(0)},
			want:    []Permission{},
		},
		{
			name:    "true string is granted",
			payload: map[string]any{"users.read": "true"},
			want:    []Permission{PermissionUsersRead},
		},
		{
			name:    "mixed-case true string is granted",
			payload: map[string]any{"users.read": "True"},
			want:    []Permission{PermissionUsersRead},
		},
		{
			name:    "false string is not granted",
			payload: map[string]any{"users.read": "false"},
			want:    []Permission{},
		},
		{
			name:    "keys are trimmed",
			payload: map[string]any{" users.read ": true},
			want:    []Permission{PermissionUsersRead},
		},
		{
			name:    "array shape is accepted",
			payload: []any{"users.read", "  global_roles.write  "},
			want:    []Permission{PermissionUsersRead, PermissionGlobalRolesWrite},
		},
		{
			name:    "unsupported shapes grant nothing",
			payload: "users.read",
			want:    []Permission{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PermissionsFromValue(tt.payload)
			if len(got) != len(tt.want) {
				t.Fatalf("PermissionsFromValue(%v) = %v, want %v", tt.payload, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("PermissionsFromValue(%v) = %v, want %v", tt.payload, got, tt.want)
				}
			}
		})
	}
}

func TestPermissionsGrantAll(t *testing.T) {
	tests := []struct {
		name    string
		payload any
		want    bool
	}{
		{
			name:    "nil payload does not grant all",
			payload: nil,
			want:    false,
		},
		{
			name:    "empty map does not grant all",
			payload: map[string]any{},
			want:    false,
		},
		{
			name:    "named permissions without wildcard",
			payload: map[string]any{"users.read": true, "global_roles.write": true},
			want:    false,
		},
		{
			name:    "wildcard with boolean true",
			payload: map[string]any{"*": true, "users.read": true},
			want:    true,
		},
		{
			name:    "wildcard with boolean false",
			payload: map[string]any{"*": false},
			want:    false,
		},
		{
			name:    "wildcard with true string",
			payload: map[string]any{"*": "true"},
			want:    true,
		},
		{
			name:    "wildcard with mixed-case true string",
			payload: map[string]any{"*": "True"},
			want:    true,
		},
		{
			name:    "wildcard with false string",
			payload: map[string]any{"*": "false"},
			want:    false,
		},
		{
			name:    "wildcard with nonzero number",
			payload: map[string]any{"*": float64(1)},
			want:    true,
		},
		{
			name:    "wildcard with zero number",
			payload: map[string]any{"*": float64(0)},
			want:    false,
		},
		{
			// Regression: the store trims keys before granting, so a padded
			// wildcard resolves to PermissionAll at request time — the guard
			// must agree, or an ADMIN can bypass it with " *": true.
			name:    "leading-space wildcard is granted",
			payload: map[string]any{" *": true},
			want:    true,
		},
		{
			name:    "trailing-space wildcard is granted",
			payload: map[string]any{"* ": true},
			want:    true,
		},
		{
			name:    "tab-padded wildcard is granted",
			payload: map[string]any{"\t*": true},
			want:    true,
		},
		{
			name:    "non-breaking-space-padded wildcard is granted",
			payload: map[string]any{"\u00a0*": true},
			want:    true,
		},
		{
			name:    "padded wildcard with false value is not granted",
			payload: map[string]any{" *": false},
			want:    false,
		},
		{
			name:    "wildcard in array shape is granted",
			payload: []any{" * ", "users.read"},
			want:    true,
		},
		{
			name:    "literal asterisk prefix is not the wildcard",
			payload: map[string]any{"global_roles.*": true},
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PermissionsGrantAll(tt.payload); got != tt.want {
				t.Fatalf("PermissionsGrantAll(%v) = %v, want %v", tt.payload, got, tt.want)
			}
		})
	}
}
