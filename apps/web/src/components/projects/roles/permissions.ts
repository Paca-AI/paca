import {
	BookOpen,
	Bot,
	Layers,
	LayoutGrid,
	ListTodo,
	type LucideIcon,
	MessageSquare,
	Pin,
	Puzzle,
	Server,
	Settings,
	SlidersHorizontal,
	Users,
	Workflow,
} from "lucide-react";

import type { PluginKnownPermission } from "@/lib/plugin-api";

export { toPluginKnownPermissions } from "@/lib/plugin-api";
export type KnownPermission = PluginKnownPermission;

export const PROJECT_KNOWN_PERMISSIONS = [
	// projects — projects.read is deliberately not offered here: the backend
	// grants it to any active project member unconditionally now (see
	// AuthzPermissionStore.ListProjectPermissions's doc comment), so a
	// per-role toggle for it would do nothing. Global projects.read (an
	// admin browsing projects they aren't a member of) is unaffected and
	// still has its own toggle in the global role editor.
	{
		key: "projects.write",
		labelKey: "roles.permissions.projectsWrite.label",
		descriptionKey: "roles.permissions.projectsWrite.description",
		domain: "projects",
	},
	{
		key: "projects.delete",
		labelKey: "roles.permissions.projectsDelete.label",
		descriptionKey: "roles.permissions.projectsDelete.description",
		domain: "projects",
	},
	{
		key: "project.activities.read",
		labelKey: "roles.permissions.activitiesRead.label",
		descriptionKey: "roles.permissions.activitiesRead.description",
		domain: "projects",
	},
	// project members
	{
		key: "project.members.read",
		labelKey: "roles.permissions.membersRead.label",
		descriptionKey: "roles.permissions.membersRead.description",
		domain: "project.members",
	},
	{
		key: "project.members.write",
		labelKey: "roles.permissions.membersWrite.label",
		descriptionKey: "roles.permissions.membersWrite.description",
		domain: "project.members",
	},
	// project roles — grouped under the "project.settings" UI domain
	// alongside task types/statuses/custom fields below (a unified
	// "Settings" section), even though the permission key itself is
	// unchanged (project.roles.*, not renamed) to avoid migrating every
	// existing role's stored permission JSONB.
	{
		key: "project.roles.read",
		labelKey: "roles.permissions.rolesRead.label",
		descriptionKey: "roles.permissions.rolesRead.description",
		domain: "project.settings",
	},
	{
		key: "project.roles.write",
		labelKey: "roles.permissions.rolesWrite.label",
		descriptionKey: "roles.permissions.rolesWrite.description",
		domain: "project.settings",
	},
	// project settings — task types, task statuses, and custom field
	// definitions. Split out from tasks.write: redefining the project's
	// task *schema* is a different capability from editing a task's own
	// content, so each area is independently grantable. No .read variants:
	// viewing what exists is implied by tasks.read below, same as viewing
	// the tasks that reference it — only redefining the schema is its own,
	// narrower capability.
	{
		key: "project.settings.task_types.write",
		labelKey: "roles.permissions.settingsTaskTypesWrite.label",
		descriptionKey: "roles.permissions.settingsTaskTypesWrite.description",
		domain: "project.settings",
	},
	{
		key: "project.settings.task_statuses.write",
		labelKey: "roles.permissions.settingsTaskStatusesWrite.label",
		descriptionKey: "roles.permissions.settingsTaskStatusesWrite.description",
		domain: "project.settings",
	},
	{
		key: "project.settings.custom_fields.write",
		labelKey: "roles.permissions.settingsCustomFieldsWrite.label",
		descriptionKey: "roles.permissions.settingsCustomFieldsWrite.description",
		domain: "project.settings",
	},
	// tasks
	{
		key: "tasks.read",
		labelKey: "roles.permissions.tasksRead.label",
		descriptionKey: "roles.permissions.tasksRead.description",
		domain: "tasks",
	},
	{
		key: "tasks.write",
		labelKey: "roles.permissions.tasksWrite.label",
		descriptionKey: "roles.permissions.tasksWrite.description",
		domain: "tasks",
	},
	// sprints
	{
		key: "sprints.read",
		labelKey: "roles.permissions.sprintsRead.label",
		descriptionKey: "roles.permissions.sprintsRead.description",
		domain: "sprints",
	},
	{
		key: "sprints.write",
		labelKey: "roles.permissions.sprintsWrite.label",
		descriptionKey: "roles.permissions.sprintsWrite.description",
		domain: "sprints",
	},
	// views
	{
		key: "views.read",
		labelKey: "roles.permissions.viewsRead.label",
		descriptionKey: "roles.permissions.viewsRead.description",
		domain: "views",
	},
	{
		key: "views.write",
		labelKey: "roles.permissions.viewsWrite.label",
		descriptionKey: "roles.permissions.viewsWrite.description",
		domain: "views",
	},
	// docs
	{
		key: "docs.read",
		labelKey: "roles.permissions.docsRead.label",
		descriptionKey: "roles.permissions.docsRead.description",
		domain: "docs",
	},
	{
		key: "docs.write",
		labelKey: "roles.permissions.docsWrite.label",
		descriptionKey: "roles.permissions.docsWrite.description",
		domain: "docs",
	},
	// agents
	{
		key: "agents.read",
		labelKey: "roles.permissions.agentsRead.label",
		descriptionKey: "roles.permissions.agentsRead.description",
		domain: "agents",
	},
	{
		key: "agents.write",
		labelKey: "roles.permissions.agentsWrite.label",
		descriptionKey: "roles.permissions.agentsWrite.description",
		domain: "agents",
	},
	// conversations
	{
		key: "conversations.read",
		labelKey: "roles.permissions.conversationsRead.label",
		descriptionKey: "roles.permissions.conversationsRead.description",
		domain: "conversations",
	},
	{
		key: "conversations.write",
		labelKey: "roles.permissions.conversationsWrite.label",
		descriptionKey: "roles.permissions.conversationsWrite.description",
		domain: "conversations",
	},
	// environments
	{
		key: "environments.read",
		labelKey: "roles.permissions.environmentsRead.label",
		descriptionKey: "roles.permissions.environmentsRead.description",
		domain: "environments",
	},
	{
		key: "environments.write",
		labelKey: "roles.permissions.environmentsWrite.label",
		descriptionKey: "roles.permissions.environmentsWrite.description",
		domain: "environments",
	},
	{
		key: "environments.connect",
		labelKey: "roles.permissions.environmentsConnect.label",
		descriptionKey: "roles.permissions.environmentsConnect.description",
		domain: "environments",
	},
	// annotations — page comments pinned via the Paca browser extension.
	// Already enforced server-side; added here so a custom role can
	// actually configure it instead of only ever getting it from defaults.
	{
		key: "annotations.read",
		labelKey: "roles.permissions.annotationsRead.label",
		descriptionKey: "roles.permissions.annotationsRead.description",
		domain: "annotations",
	},
	{
		key: "annotations.write",
		labelKey: "roles.permissions.annotationsWrite.label",
		descriptionKey: "roles.permissions.annotationsWrite.description",
		domain: "annotations",
	},
	{
		key: "annotations.resolve",
		labelKey: "roles.permissions.annotationsResolve.label",
		descriptionKey: "roles.permissions.annotationsResolve.description",
		domain: "annotations",
	},
	// automation workflows
	{
		key: "workflows.read",
		labelKey: "roles.permissions.workflowsRead.label",
		descriptionKey: "roles.permissions.workflowsRead.description",
		domain: "workflows",
	},
	{
		key: "workflows.write",
		labelKey: "roles.permissions.workflowsWrite.label",
		descriptionKey: "roles.permissions.workflowsWrite.description",
		domain: "workflows",
	},
] as const satisfies KnownPermission[];

export interface PermissionGroup {
	domain: string;
	labelKey: string;
	Icon: LucideIcon;
}

export const PROJECT_PERMISSION_GROUPS = [
	{
		domain: "projects",
		labelKey: "roles.permissionGroups.project",
		Icon: Settings,
	},
	{
		domain: "project.members",
		labelKey: "roles.permissionGroups.members",
		Icon: Users,
	},
	{
		domain: "project.settings",
		labelKey: "roles.permissionGroups.settings",
		Icon: SlidersHorizontal,
	},
	{ domain: "tasks", labelKey: "roles.permissionGroups.tasks", Icon: ListTodo },
	{
		domain: "sprints",
		labelKey: "roles.permissionGroups.sprints",
		Icon: Layers,
	},
	{
		domain: "views",
		labelKey: "roles.permissionGroups.views",
		Icon: LayoutGrid,
	},
	{
		domain: "docs",
		labelKey: "roles.permissionGroups.documents",
		Icon: BookOpen,
	},
	{ domain: "agents", labelKey: "roles.permissionGroups.aiAgents", Icon: Bot },
	{
		domain: "conversations",
		labelKey: "roles.permissionGroups.conversations",
		Icon: MessageSquare,
	},
	{
		domain: "environments",
		labelKey: "roles.permissionGroups.environments",
		Icon: Server,
	},
	{
		domain: "annotations",
		labelKey: "roles.permissionGroups.annotations",
		Icon: Pin,
	},
	{
		domain: "workflows",
		labelKey: "roles.permissionGroups.workflows",
		Icon: Workflow,
	},
	{
		domain: "plugins",
		labelKey: "roles.permissionGroups.plugins",
		Icon: Puzzle,
	},
] as const satisfies PermissionGroup[];
