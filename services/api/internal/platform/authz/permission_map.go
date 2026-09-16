package authz

import "strings"

// PermissionsFromValue converts a decoded permissions value — the JSONB shape
// stored in global_roles.permissions / project_roles.permissions — into the
// permissions it grants. Both persisted shapes are accepted: an object mapping
// permission keys to truthy values ({"users.read": true}), and an array of
// permission strings (["users.read"]).
//
// This is the single source of truth for reading a persisted permission blob.
// postgres.permissionsFromJSON (the runtime resolver) delegates here, so an
// authorization guard and the permission store can never disagree about what a
// role grants — the duplicate-permission-logic drift class that produced
// GHSA-hjcj-373w-vq8m. Keys are trimmed and values are read truthily exactly as
// the store does: a key is granted when its value is boolean true, a nonzero
// number, or the string "true" (any case).
func PermissionsFromValue(payload any) []Permission {
	seen := map[Permission]struct{}{}
	out := make([]Permission, 0)

	add := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" {
			return
		}
		p := Permission(k)
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	switch v := payload.(type) {
	case map[string]any:
		for key, enabled := range v {
			switch e := enabled.(type) {
			case bool:
				if e {
					add(key)
				}
			case float64:
				if e != 0 {
					add(key)
				}
			case string:
				if strings.EqualFold(e, "true") {
					add(key)
				}
			}
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				add(s)
			}
		}
	}

	return out
}

// PermissionsGrantAll reports whether a decoded permissions value grants the
// universal PermissionAll wildcard. It is the escalation check the role
// handlers rely on: a caller may only create/edit a role carrying "*", assign
// such a role to a user, or bind one to a global agent if they themselves hold
// PermissionAll. Without it, any ADMIN (who legitimately holds global_roles.*)
// could grant themselves SUPER_ADMIN.
//
// Deriving the check from PermissionsFromValue — the same parser the
// permission store uses — is deliberate: a hand-rolled key comparison here
// once missed whitespace-padded keys (" *", "*\t"), which the store trims
// before granting, letting a padded "*" pass the guard and still resolve to
// PermissionAll at request time.
func PermissionsGrantAll(payload any) bool {
	for _, p := range PermissionsFromValue(payload) {
		if p == PermissionAll {
			return true
		}
	}
	return false
}
