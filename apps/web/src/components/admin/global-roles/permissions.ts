import { type LucideIcon, Shield, Users } from "lucide-react";

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
		key: "users.delete",
		label: "Delete Users",
		description: "Remove user accounts",
		domain: "users",
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
];
