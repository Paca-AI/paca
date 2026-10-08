import {
	Bot,
	FolderKanban,
	type LucideIcon,
	Puzzle,
	Settings,
	Shield,
	Users,
} from "lucide-react";

import type { PluginKnownPermission } from "@/lib/plugin-api";

export { toPluginKnownPermissions } from "@/lib/plugin-api";
export type KnownPermission = PluginKnownPermission;

export const KNOWN_PERMISSIONS = [
	{
		key: "roles:read",
		labelKey: "globalRoles.permissions.globalRolesRead.label",
		descriptionKey: "globalRoles.permissions.globalRolesRead.description",
		domain: "roles",
	},
	{
		key: "roles:write",
		labelKey: "globalRoles.permissions.globalRolesWrite.label",
		descriptionKey: "globalRoles.permissions.globalRolesWrite.description",
		domain: "roles",
	},
	{
		key: "roles:assign",
		labelKey: "globalRoles.permissions.globalRolesAssign.label",
		descriptionKey: "globalRoles.permissions.globalRolesAssign.description",
		domain: "roles",
	},
	{
		key: "users:read",
		labelKey: "globalRoles.permissions.usersRead.label",
		descriptionKey: "globalRoles.permissions.usersRead.description",
		domain: "users",
	},
	{
		key: "users:write",
		labelKey: "globalRoles.permissions.usersWrite.label",
		descriptionKey: "globalRoles.permissions.usersWrite.description",
		domain: "users",
	},
	{
		key: "users:delete",
		labelKey: "globalRoles.permissions.usersDelete.label",
		descriptionKey: "globalRoles.permissions.usersDelete.description",
		domain: "users",
	},
	{
		key: "projects:read",
		labelKey: "globalRoles.permissions.projectsRead.label",
		descriptionKey: "globalRoles.permissions.projectsRead.description",
		domain: "projects",
	},
	{
		key: "projects:create",
		labelKey: "globalRoles.permissions.projectsCreate.label",
		descriptionKey: "globalRoles.permissions.projectsCreate.description",
		domain: "projects",
	},
	{
		key: "projects:write",
		labelKey: "globalRoles.permissions.projectsWrite.label",
		descriptionKey: "globalRoles.permissions.projectsWrite.description",
		domain: "projects",
	},
	{
		key: "projects:delete",
		labelKey: "globalRoles.permissions.projectsDelete.label",
		descriptionKey: "globalRoles.permissions.projectsDelete.description",
		domain: "projects",
	},
	{
		key: "agents:read",
		labelKey: "globalRoles.permissions.agentsRead.label",
		descriptionKey: "globalRoles.permissions.agentsRead.description",
		domain: "agents",
	},
	{
		key: "agents:write",
		labelKey: "globalRoles.permissions.agentsWrite.label",
		descriptionKey: "globalRoles.permissions.agentsWrite.description",
		domain: "agents",
	},
	{
		key: "plugins:read",
		labelKey: "globalRoles.permissions.pluginsRead.label",
		descriptionKey: "globalRoles.permissions.pluginsRead.description",
		domain: "plugins",
	},
	{
		key: "plugins:write",
		labelKey: "globalRoles.permissions.pluginsWrite.label",
		descriptionKey: "globalRoles.permissions.pluginsWrite.description",
		domain: "plugins",
	},
	{
		key: "settings:write",
		labelKey: "globalRoles.permissions.settingsWrite.label",
		descriptionKey: "globalRoles.permissions.settingsWrite.description",
		domain: "settings",
	},
	{
		key: "settings.sso:write",
		labelKey: "globalRoles.permissions.settingsSsoWrite.label",
		descriptionKey: "globalRoles.permissions.settingsSsoWrite.description",
		domain: "settings",
	},
] as const satisfies KnownPermission[];

export interface PermissionGroup {
	domain: string;
	labelKey: string;
	Icon: LucideIcon;
}

export const PERMISSION_GROUPS = [
	{
		domain: "roles",
		labelKey: "globalRoles.permissionGroups.globalRoles",
		Icon: Shield,
	},
	{
		domain: "users",
		labelKey: "globalRoles.permissionGroups.users",
		Icon: Users,
	},
	{
		domain: "agents",
		labelKey: "globalRoles.permissionGroups.agents",
		Icon: Bot,
	},
	{
		domain: "projects",
		labelKey: "globalRoles.permissionGroups.projects",
		Icon: FolderKanban,
	},
	{
		domain: "plugins",
		labelKey: "globalRoles.permissionGroups.plugins",
		Icon: Puzzle,
	},
	{
		domain: "settings",
		labelKey: "globalRoles.permissionGroups.settings",
		Icon: Settings,
	},
] as const satisfies PermissionGroup[];
