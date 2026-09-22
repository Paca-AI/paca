package authz

import "sync"

// RoleDefinition binds a role name to the permissions it grants.
type RoleDefinition struct {
	Name        string
	Permissions []Permission
}

// DefaultGlobalRoles returns the built-in global role set. It is the source
// the built-in global_roles rows are seeded/synced from at startup — it is
// never consulted when authorizing a request: the permissions a request is
// checked against are always the ones the caller's role row actually stores
// (see PermissionStore), so a role edited after seeding is enforced as
// edited, not as defined here.
//
// Computed once and cached; the data is static for the process lifetime and
// callers only ever range over the result, so a shared cached slice is safe
// to hand out.
var DefaultGlobalRoles = sync.OnceValue(func() []RoleDefinition {
	return []RoleDefinition{
		{
			Name:        "SUPER_ADMIN",
			Permissions: []Permission{PermissionAll},
		},
		{
			// ADMIN runs the workspace — users, projects, agents, plugins and
			// settings — and may *see* the global roles. It deliberately does
			// not hold global_roles.write or global_roles.assign: between them
			// they let their holder define any role, one storing "*" included,
			// and give it to any account, their own included. Both are
			// root-equivalent, and the router can only ask whether a caller
			// holds a permission — not whether the role being written or
			// assigned is above the caller's own — so the line is drawn here,
			// in what each role holds: they belong to SUPER_ADMIN alone unless
			// an operator knowingly grants them to a custom role.
			Name: "ADMIN",
			Permissions: []Permission{
				PermissionUsersAll,
				PermissionGlobalRolesRead,
				PermissionProjectsAll,
				PermissionSettingsWrite,
				// Global agents and plugins were previously left off this
				// list, so an ADMIN got 403s managing either. That was
				// masked for accounts whose role-name claim happened to
				// read "ADMIN", because authorization used to grant by
				// role *name* on top of what the role row stored. That
				// fallback is gone: this list, as synced into the ADMIN
				// role row at startup, is exactly what ADMIN can do.
				PermissionAgentsAll,
				PermissionPluginsAll,
			},
		},
		{
			Name: "USER",
			Permissions: []Permission{
				PermissionUsersRead,
			},
		},
	}
})

// DefaultProjectRoles returns built-in project role templates.
func DefaultProjectRoles() []RoleDefinition {
	return []RoleDefinition{
		{
			Name: "PROJECT_OWNER",
			Permissions: []Permission{
				PermissionProjectsAll,
				PermissionProjectMembersAll,
				PermissionProjectRolesAll,
				PermissionTasksAll,
				PermissionProjectSettingsAll,
				PermissionSprintsAll,
				PermissionViewsAll,
				PermissionDocsAll,
				PermissionAgentsAll,
				PermissionConversationsAll,
				PermissionWorkflowsAll,
				PermissionEnvironmentsAll,
				PermissionAnnotationsAll,
			},
		},
		{
			Name: "PROJECT_MANAGER",
			// PermissionProjectsRead is deliberately omitted here and below —
			// AuthzPermissionStore.ListProjectPermissions grants it to any
			// active project member unconditionally, so listing it per-role
			// would be redundant (see that method's doc comment). Global-
			// scope projects.read (an admin browsing projects they aren't a
			// member of) is unaffected — this only covers the project-scoped
			// resolution.
			Permissions: []Permission{
				PermissionProjectsWrite,
				PermissionProjectMembersRead,
				PermissionProjectMembersWrite,
				PermissionTasksAll,
				PermissionProjectSettingsAll,
				PermissionSprintsAll,
				PermissionViewsAll,
				PermissionDocsAll,
				PermissionAgentsAll,
				PermissionConversationsAll,
				PermissionWorkflowsAll,
				PermissionEnvironmentsAll,
				PermissionAnnotationsAll,
			},
		},
		{
			Name: "PROJECT_MEMBER",
			Permissions: []Permission{
				PermissionProjectMembersRead,
				PermissionProjectRolesRead,
				PermissionTasksRead,
				PermissionTasksWrite,
				// No ProjectSettings*Write here: redefining task
				// types/statuses/custom fields is an Admin-level (project
				// schema) action, not a content-editing one — mirrors the
				// real per-project "Editor" role's grants (see
				// projectsvc.Service.Create). No dedicated read permission
				// exists for the schema either; PermissionTasksRead above
				// already covers viewing it. A project owner can grant write
				// access per-project via the role editor.
				PermissionSprintsRead,
				// PermissionViewsWrite is a new capability here, not a
				// preservation: view CRUD previously required
				// PermissionSprintsWrite, which PROJECT_MEMBER never held
				// (only SprintsRead). Confirmed intentional — see
				// TestDefaultProjectRoles_ProjectMemberHasNoSettingsWritePermissions'
				// sibling assertions and 000054's own comment on this same
				// grant — and called out explicitly here since it's a real
				// behavior change for every existing project's members on
				// deploy, not implied by the settings-permission split above.
				PermissionViewsRead,
				PermissionViewsWrite,
				PermissionDocsRead,
				PermissionDocsWrite,
				PermissionAgentsRead,
				PermissionAgentsWrite,
				PermissionConversationsRead,
				PermissionConversationsWrite,
				PermissionWorkflowsRead,
				PermissionWorkflowsWrite,
				PermissionEnvironmentsRead,
				PermissionEnvironmentsWrite,
				PermissionEnvironmentsConnect,
				PermissionAnnotationsRead,
				PermissionAnnotationsWrite,
				PermissionAnnotationsResolve,
			},
		},
		{
			Name: "PROJECT_VIEWER",
			Permissions: []Permission{
				PermissionProjectMembersRead,
				PermissionProjectRolesRead,
				PermissionTasksRead,
				PermissionSprintsRead,
				PermissionViewsRead,
				PermissionDocsRead,
				PermissionAgentsRead,
				PermissionConversationsRead,
				PermissionWorkflowsRead,
				PermissionEnvironmentsRead,
				PermissionAnnotationsRead,
			},
		},
	}
}
