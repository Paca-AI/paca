package authz

// Permission is a stable machine-readable permission key.
type Permission string

// Stable permission keys used by the authorization system.
const (
	PermissionAll Permission = "*"

	PermissionUsersRead   Permission = "users.read"
	PermissionUsersWrite  Permission = "users.write"
	PermissionUsersDelete Permission = "users.delete"
	PermissionUsersAll    Permission = "users.*"

	PermissionGlobalRolesRead   Permission = "global_roles.read"
	PermissionGlobalRolesWrite  Permission = "global_roles.write"
	PermissionGlobalRolesAssign Permission = "global_roles.assign"
	PermissionGlobalRolesAll    Permission = "global_roles.*"

	PermissionProjectsRead   Permission = "projects.read"
	PermissionProjectsWrite  Permission = "projects.write"
	PermissionProjectsCreate Permission = "projects.create"
	PermissionProjectsDelete Permission = "projects.delete"
	PermissionProjectsAll    Permission = "projects.*"

	PermissionProjectMembersRead  Permission = "project.members.read"
	PermissionProjectMembersWrite Permission = "project.members.write"
	PermissionProjectMembersAll   Permission = "project.members.*"

	PermissionProjectRolesRead  Permission = "project.roles.read"
	PermissionProjectRolesWrite Permission = "project.roles.write"
	PermissionProjectRolesAll   Permission = "project.roles.*"

	PermissionTasksRead  Permission = "tasks.read"
	PermissionTasksWrite Permission = "tasks.write"
	PermissionTasksAll   Permission = "tasks.*"

	// PermissionProjectSettings{TaskTypes,TaskStatuses,CustomFields} gate
	// redefining the project's task *schema* — the task-type list, the
	// task-status list (including reorder/set-default), and custom field
	// definitions — split out from PermissionTasksWrite, which now governs
	// only task *content* (create/edit/delete a task, comments, links,
	// attachments, moving a task between existing statuses). Someone who can
	// drag a card to "Done" is no longer necessarily someone who can delete
	// the "Done" status or add a project-wide custom field. Three separate
	// keys, not one, so a role can grant just one area (e.g. custom fields
	// only) without the others. Prefixed with "project." like
	// project.members.*/project.roles.* — deliberately not bare
	// "settings.*", which already exists as a *global* permission
	// (workspace branding) and would otherwise be merged into the same
	// granted-set for any project-scoped check.
	//
	// No paired *Read variants: viewing what task types/statuses/custom
	// fields exist is implied by PermissionTasksRead (anyone who can see the
	// project's tasks needs to see what's used to render them — the status
	// badge, the type icon, a task's custom field values), so a separate
	// read gate on the schema itself was redundant friction, not a real
	// security boundary. Only the ability to *redefine* the schema is its
	// own, narrower, independently grantable capability.
	PermissionProjectSettingsTaskTypesWrite    Permission = "project.settings.task_types.write"
	PermissionProjectSettingsTaskStatusesWrite Permission = "project.settings.task_statuses.write"
	PermissionProjectSettingsCustomFieldsWrite Permission = "project.settings.custom_fields.write"
	PermissionProjectSettingsAll               Permission = "project.settings.*"

	PermissionSprintsRead  Permission = "sprints.read"
	PermissionSprintsWrite Permission = "sprints.write"
	PermissionSprintsAll   Permission = "sprints.*"

	// PermissionViews{Read,Write} gate a saved view's own definition
	// (create/update/delete/reorder a board or list layout) — split out from
	// PermissionSprintsWrite, which it used to borrow with no dedicated key
	// of its own. Moving a *task* within a view (drag-and-drop) stays on
	// PermissionTasksWrite; that's editing a task, not the view.
	PermissionViewsRead  Permission = "views.read"
	PermissionViewsWrite Permission = "views.write"
	PermissionViewsAll   Permission = "views.*"

	PermissionDocsRead  Permission = "docs.read"
	PermissionDocsWrite Permission = "docs.write"
	PermissionDocsAll   Permission = "docs.*"

	PermissionAgentsRead  Permission = "agents.read"
	PermissionAgentsWrite Permission = "agents.write"
	PermissionAgentsAll   Permission = "agents.*"

	// PermissionConversationsRead/Write gate a conversation itself (viewing
	// its transcript, or creating/driving one — starting a chat session,
	// sending a message, stopping/pausing a run) — split from Agents, which
	// gates the agent *entity's configuration* (its MCP servers, skills, env
	// vars, avatar, etc). A role can therefore let someone chat with an
	// already-configured agent without also letting them reconfigure it, or
	// the reverse. Covers both the human-facing routes
	// (/conversations/*, /agents/{id}/chat-sessions*) and the agent-
	// authenticated read_conversation MCP tool (see
	// Service.authorizeConversationsReadForConversation).
	PermissionConversationsRead  Permission = "conversations.read"
	PermissionConversationsWrite Permission = "conversations.write"
	PermissionConversationsAll   Permission = "conversations.*"

	// PermissionEnvironmentsConnect gates gaining a live, interactive
	// session inside an already-running environment — today that's
	// minting a terminal ticket (EnvironmentHandler.TerminalTicket), which
	// hands the browser a real shell. Deliberately a separate tier from
	// Write: managing an environment's configuration (folders, SSH keys,
	// port forwards, lifecycle) doesn't imply the ability to open a shell
	// inside it, and vice versa.
	PermissionEnvironmentsRead    Permission = "environments.read"
	PermissionEnvironmentsWrite   Permission = "environments.write"
	PermissionEnvironmentsConnect Permission = "environments.connect"
	PermissionEnvironmentsAll     Permission = "environments.*"

	PermissionWorkflowsRead  Permission = "workflows.read"
	PermissionWorkflowsWrite Permission = "workflows.write"
	PermissionWorkflowsAll   Permission = "workflows.*"

	// PermissionAnnotationsResolve gates resolving/reopening a page
	// annotation (a comment pinned via the Paca browser extension) without
	// granting the ability to create or delete one — split from Write the
	// same way PermissionEnvironmentsConnect is split from
	// PermissionEnvironmentsWrite, so a role can triage/dismiss comments
	// without being able to author or remove them.
	PermissionAnnotationsRead    Permission = "annotations.read"
	PermissionAnnotationsWrite   Permission = "annotations.write"
	PermissionAnnotationsResolve Permission = "annotations.resolve"
	PermissionAnnotationsAll     Permission = "annotations.*"

	// PermissionSettingsWrite gates changes to instance-wide workspace
	// branding (logo/favicon/primary color). There is no paired
	// settings.read: the branding itself is served by an unauthenticated
	// public endpoint, so the only thing to gate is writing to it.
	PermissionSettingsWrite Permission = "settings.write"

	// PermissionPlugins{Read,Write} gate global plugin installation/
	// marketplace management — previously reused PermissionUsersWrite as a
	// rough "is this someone important" proxy, with no permission of its
	// own.
	PermissionPluginsRead  Permission = "plugins.read"
	PermissionPluginsWrite Permission = "plugins.write"
	PermissionPluginsAll   Permission = "plugins.*"
)
