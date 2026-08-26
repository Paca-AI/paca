import {
	BookOpen,
	Bot,
	Layers,
	ListTodo,
	type LucideIcon,
	Puzzle,
	Server,
	Settings,
	Shield,
	Users,
	Workflow,
} from "lucide-react";

import type { PluginKnownPermission } from "@/lib/plugin-api";

export { toPluginKnownPermissions } from "@/lib/plugin-api";
export type KnownPermission = PluginKnownPermission;

export const PROJECT_KNOWN_PERMISSIONS = [
	// projects
	{
		key: "projects.read",
		labelKey: "roles.permissions.projectsRead.label",
		descriptionKey: "roles.permissions.projectsRead.description",
		domain: "projects",
	},
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
	// project roles
	{
		key: "project.roles.read",
		labelKey: "roles.permissions.rolesRead.label",
		descriptionKey: "roles.permissions.rolesRead.description",
		domain: "project.roles",
	},
	{
		key: "project.roles.write",
		labelKey: "roles.permissions.rolesWrite.label",
		descriptionKey: "roles.permissions.rolesWrite.description",
		domain: "project.roles",
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
		domain: "project.roles",
		labelKey: "roles.permissionGroups.roles",
		Icon: Shield,
	},
	{ domain: "tasks", labelKey: "roles.permissionGroups.tasks", Icon: ListTodo },
	{
		domain: "sprints",
		labelKey: "roles.permissionGroups.sprints",
		Icon: Layers,
	},
	{
		domain: "docs",
		labelKey: "roles.permissionGroups.documents",
		Icon: BookOpen,
	},
	{ domain: "agents", labelKey: "roles.permissionGroups.aiAgents", Icon: Bot },
	{
		domain: "environments",
		labelKey: "roles.permissionGroups.environments",
		Icon: Server,
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
