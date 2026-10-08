package iam

// Action is an IAM action name of the form "domain:verb". Statements may also
// use the wildcards "*" and "domain:*", which are not Actions themselves.
type Action string

// Built-in actions. Route gates, services and the plugin runtime refer to
// these; role policies name them as strings.
const (
	ActionUsersRead   Action = "users:read"
	ActionUsersWrite  Action = "users:write"
	ActionUsersDelete Action = "users:delete"

	// Roles: defining, reading and attaching roles (platform-wide and
	// project roles alike).
	ActionRolesRead   Action = "roles:read"
	ActionRolesWrite  Action = "roles:write"
	ActionRolesAssign Action = "roles:assign"

	ActionProjectsRead   Action = "projects:read"
	ActionProjectsWrite  Action = "projects:write"
	ActionProjectsCreate Action = "projects:create"
	ActionProjectsDelete Action = "projects:delete"

	ActionProjectMembersRead  Action = "project.members:read"
	ActionProjectMembersWrite Action = "project.members:write"

	// ActionProjectActivitiesRead gates the project-wide activity log.
	ActionProjectActivitiesRead Action = "project.activities:read"
	// ActionProjectExport gates exporting a project's data.
	ActionProjectExport Action = "project:export"

	ActionTasksRead  Action = "tasks:read"
	ActionTasksWrite Action = "tasks:write"

	// Project task schema: redefining task types, statuses and custom fields
	// (distinct from editing task content, which is tasks:write).
	ActionProjectSettingsTaskTypesWrite    Action = "project.settings.task_types:write"
	ActionProjectSettingsTaskStatusesWrite Action = "project.settings.task_statuses:write"
	ActionProjectSettingsCustomFieldsWrite Action = "project.settings.custom_fields:write"

	ActionSprintsRead  Action = "sprints:read"
	ActionSprintsWrite Action = "sprints:write"
	ActionViewsRead    Action = "views:read"
	ActionViewsWrite   Action = "views:write"
	ActionDocsRead     Action = "docs:read"
	ActionDocsWrite    Action = "docs:write"

	ActionAgentsRead         Action = "agents:read"
	ActionAgentsWrite        Action = "agents:write"
	ActionConversationsRead  Action = "conversations:read"
	ActionConversationsWrite Action = "conversations:write"

	// Environments: Connect (an interactive shell / SSH key) is a separate
	// capability from configuring the environment (Write).
	ActionEnvironmentsRead    Action = "environments:read"
	ActionEnvironmentsWrite   Action = "environments:write"
	ActionEnvironmentsConnect Action = "environments:connect"

	ActionWorkflowsRead  Action = "workflows:read"
	ActionWorkflowsWrite Action = "workflows:write"

	ActionAnnotationsRead    Action = "annotations:read"
	ActionAnnotationsWrite   Action = "annotations:write"
	ActionAnnotationsResolve Action = "annotations:resolve"

	ActionSettingsWrite Action = "settings:write"
	// ActionSettingsSSOWrite is root-equivalent: a provider that links
	// accounts by email signs in as any account with a matching email.
	ActionSettingsSSOWrite Action = "settings.sso:write"

	ActionPluginsRead  Action = "plugins:read"
	ActionPluginsWrite Action = "plugins:write"
)

// BuiltinActions returns every built-in action, in declaration order.
func BuiltinActions() []Action {
	return []Action{
		ActionUsersRead, ActionUsersWrite, ActionUsersDelete,
		ActionRolesRead, ActionRolesWrite, ActionRolesAssign,
		ActionProjectsRead, ActionProjectsWrite, ActionProjectsCreate, ActionProjectsDelete,
		ActionProjectMembersRead, ActionProjectMembersWrite,
		ActionProjectActivitiesRead, ActionProjectExport,
		ActionTasksRead, ActionTasksWrite,
		ActionProjectSettingsTaskTypesWrite, ActionProjectSettingsTaskStatusesWrite, ActionProjectSettingsCustomFieldsWrite,
		ActionSprintsRead, ActionSprintsWrite, ActionViewsRead, ActionViewsWrite, ActionDocsRead, ActionDocsWrite,
		ActionAgentsRead, ActionAgentsWrite, ActionConversationsRead, ActionConversationsWrite,
		ActionEnvironmentsRead, ActionEnvironmentsWrite, ActionEnvironmentsConnect,
		ActionWorkflowsRead, ActionWorkflowsWrite,
		ActionAnnotationsRead, ActionAnnotationsWrite, ActionAnnotationsResolve,
		ActionSettingsWrite, ActionSettingsSSOWrite, ActionPluginsRead, ActionPluginsWrite,
	}
}

var builtinSet = func() map[string]struct{} {
	m := map[string]struct{}{}
	for _, a := range BuiltinActions() {
		m[string(a)] = struct{}{}
	}
	return m
}()

// IsBuiltinAction reports whether action is one of the host's own actions. Any
// other action belongs to a plugin (it declares it in its manifest).
func IsBuiltinAction(action string) bool {
	_, ok := builtinSet[action]
	return ok
}

// PluginProjectResource names a plugin inside a project:
// "project/<projectID>/plugin/<pluginID>". A plugin's own actions are checked
// on it, so a role can grant a plugin's permissions in one project only.
func PluginProjectResource(projectID, pluginID string) string {
	return "project/" + projectID + "/plugin/" + pluginID
}
