import { type LucideIcon, FolderKanban, Shield, Users } from "lucide-react";

export interface KnownPermission {
	key: string;
	label: string;
	description: string;
	domain: string;
}

export const KNOWN_PERMISSIONS: KnownPermission[] = [
	{
		key: "global_roles.read",
		label: "Read Global Roles",
		description: "View global role definitions",
		domain: "global_roles",
	},
	{
		key: "global_roles.write",
		label: "Write Global Roles",
		description: "Create and update global role definitions",
		domain: "global_roles",
	},
	{
		key: "global_roles.assign",
		label: "Assign Global Roles",
		description: "Assign global roles to users",
		domain: "global_roles",
	},
	{
		key: "users.read",
		label: "Read Users",
		description: "View user profiles and list",
		domain: "users",
	},
	{
		key: "users.write",
		label: "Write Users",
		description: "Create and update user accounts",
		domain: "users",
	},
	{
		key: "users.delete",
		label: "Delete Users",
		description: "Remove user accounts",
		domain: "users",
	},
	{
		key: "projects.read",
		label: "Read All Projects",
		description: "View all projects in the workspace",
		domain: "projects",
	},
	{
		key: "projects.create",
		label: "Create Projects",
		description: "Create new projects",
		domain: "projects",
	},
	{
		key: "projects.write",
		label: "Write Projects",
		description: "Update project details",
		domain: "projects",
	},
	{
		key: "projects.delete",
		label: "Delete Projects",
		description: "Permanently delete projects",
		domain: "projects",
	},
	{
		key: "project.members.read",
		label: "Read Project Members",
		description: "View members of any project",
		domain: "projects",
	},
	{
		key: "project.members.write",
		label: "Write Project Members",
		description: "Add, remove, and update members in any project",
		domain: "projects",
	},
	{
		key: "project.roles.read",
		label: "Read Project Roles",
		description: "View roles defined in any project",
		domain: "projects",
	},
	{
		key: "project.roles.write",
		label: "Write Project Roles",
		description: "Create and update roles in any project",
		domain: "projects",
	},
];

export interface PermissionGroup {
	domain: string;
	label: string;
	Icon: LucideIcon;
}

export const PERMISSION_GROUPS: PermissionGroup[] = [
	{ domain: "global_roles", label: "Global Roles", Icon: Shield },
	{ domain: "users", label: "Users", Icon: Users },
	{ domain: "projects", label: "Projects", Icon: FolderKanban },
];
