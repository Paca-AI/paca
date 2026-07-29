import type { PacaConfig } from "./types/index.js";

export interface PermissionMap {
	global: Record<string, boolean>;
	projects: Record<string, Record<string, boolean>>;
}

export interface ToolPermission {
	toolName: string;
	permissionKey: string;
	// Some tools require project-scoped permissions
	requiresProject?: boolean;
}

export const TOOL_PERMISSIONS: ToolPermission[] = [
	// Project tools
	{ toolName: "list_projects", permissionKey: "projects.read" },
	{
		toolName: "get_project",
		permissionKey: "projects.read",
		requiresProject: true,
	},
	{ toolName: "create_project", permissionKey: "projects.create" },
	{
		toolName: "update_project",
		permissionKey: "projects.write",
		requiresProject: true,
	},
	{
		toolName: "delete_project",
		permissionKey: "projects.delete",
		requiresProject: true,
	},

	// Task tools
	{
		toolName: "list_tasks",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{ toolName: "get_task", permissionKey: "tasks.read", requiresProject: true },
	{
		toolName: "get_task_by_number",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{
		toolName: "create_task",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "update_task",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "delete_task",
		permissionKey: "tasks.write",
		requiresProject: true,
	},

	// Sprint tools
	{
		toolName: "list_sprints",
		permissionKey: "sprints.read",
		requiresProject: true,
	},
	{
		toolName: "get_sprint",
		permissionKey: "sprints.read",
		requiresProject: true,
	},
	{
		toolName: "create_sprint",
		permissionKey: "sprints.write",
		requiresProject: true,
	},
	{
		toolName: "update_sprint",
		permissionKey: "sprints.write",
		requiresProject: true,
	},
	{
		toolName: "delete_sprint",
		permissionKey: "sprints.write",
		requiresProject: true,
	},
	{
		toolName: "complete_sprint",
		permissionKey: "sprints.write",
		requiresProject: true,
	},

	// Filesystem document tools
	{ toolName: "list_docs", permissionKey: "docs.read", requiresProject: true },
	{ toolName: "read_doc", permissionKey: "docs.read", requiresProject: true },
	{ toolName: "write_doc", permissionKey: "docs.write", requiresProject: true },
	{
		toolName: "delete_doc",
		permissionKey: "docs.write",
		requiresProject: true,
	},
	{ toolName: "move_doc", permissionKey: "docs.write", requiresProject: true },

	// Project member tools
	{
		toolName: "list_project_members",
		permissionKey: "project.members.read",
		requiresProject: true,
	},
	{
		toolName: "add_project_member",
		permissionKey: "project.members.write",
		requiresProject: true,
	},
	{
		toolName: "get_my_project_permissions",
		permissionKey: "project.members.read",
		requiresProject: true,
	},
	{
		toolName: "update_project_member_role",
		permissionKey: "project.members.write",
		requiresProject: true,
	},
	{
		toolName: "remove_project_member",
		permissionKey: "project.members.write",
		requiresProject: true,
	},

	// Project role tools
	{
		toolName: "list_project_roles",
		permissionKey: "project.roles.read",
		requiresProject: true,
	},
	{
		toolName: "create_project_role",
		permissionKey: "project.roles.write",
		requiresProject: true,
	},
	{
		toolName: "update_project_role",
		permissionKey: "project.roles.write",
		requiresProject: true,
	},
	{
		toolName: "delete_project_role",
		permissionKey: "project.roles.write",
		requiresProject: true,
	},

	// Task type tools
	{
		toolName: "list_task_types",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{
		toolName: "create_task_type",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "update_task_type",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "delete_task_type",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "set_default_task_type",
		permissionKey: "tasks.write",
		requiresProject: true,
	},

	// Task status tools
	{
		toolName: "list_task_statuses",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{
		toolName: "create_task_status",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "update_task_status",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "delete_task_status",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "set_default_task_status",
		permissionKey: "tasks.write",
		requiresProject: true,
	},

	// View tools
	{
		toolName: "list_views",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{
		toolName: "create_view",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "reorder_views",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{ toolName: "get_view", permissionKey: "tasks.read", requiresProject: true },
	{
		toolName: "update_view",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "delete_view",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "list_task_positions",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{
		toolName: "bulk_move_tasks",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "move_task",
		permissionKey: "tasks.write",
		requiresProject: true,
	},

	// Custom field tools
	{
		toolName: "list_custom_fields",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{
		toolName: "create_custom_field",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "get_custom_field",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{
		toolName: "update_custom_field",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "delete_custom_field",
		permissionKey: "tasks.write",
		requiresProject: true,
	},

	// Attachment tools
	{
		toolName: "list_task_attachments",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{
		toolName: "get_attachment_download_url",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{
		toolName: "delete_task_attachment",
		permissionKey: "tasks.write",
		requiresProject: true,
	},

	// Task activity and comment tools
	{
		toolName: "list_task_activities",
		permissionKey: "tasks.read",
		requiresProject: true,
	},
	{
		toolName: "add_task_comment",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "update_task_comment",
		permissionKey: "tasks.write",
		requiresProject: true,
	},
	{
		toolName: "delete_task_comment",
		permissionKey: "tasks.write",
		requiresProject: true,
	},

	// Automation tools (still gated by the workflows.* permission keys — see
	// the backend's automation router registration for why those key names
	// were kept rather than renamed to automations.*)
	{
		toolName: "get_automation",
		permissionKey: "workflows.read",
		requiresProject: true,
	},
	{
		toolName: "create_automation",
		permissionKey: "workflows.write",
		requiresProject: true,
	},
	{
		toolName: "update_automation",
		permissionKey: "workflows.write",
		requiresProject: true,
	},
	{
		toolName: "delete_automation",
		permissionKey: "workflows.write",
		requiresProject: true,
	},
];

export async function fetchAgentPermissions(
	config: PacaConfig,
): Promise<PermissionMap> {
	const global: Record<string, boolean> = {};
	const projects: Record<string, Record<string, boolean>> = {};

	// For personal API key without project ID, no permission filtering needed
	if (!config.agentId && !config.projectId) {
		console.error(
			"[permissions] Personal API key mode: permission filtering disabled",
		);
		return { global, projects };
	}

	try {
		const headers: Record<string, string> = {
			"Content-Type": "application/json",
			"X-API-Key": config.apiKey,
		};
		if (config.agentId) {
			headers["X-Agent-ID"] = config.agentId;
		}

		if (!config.agentId) {
			try {
				const globalUrl = `${config.baseURL}/api/v1/users/me/global-permissions`;
				const globalResponse = await fetch(globalUrl, { headers });

				if (globalResponse.ok) {
					const globalJson = await globalResponse.json();
					let globalData: any;
					if (
						globalJson &&
						typeof globalJson === "object" &&
						"permissions" in globalJson
					) {
						globalData = globalJson.permissions;
					} else if (Array.isArray(globalJson)) {
						globalData = {};
						for (const perm of globalJson) {
							globalData[perm] = true;
						}
					}

					if (globalData) {
						if (Array.isArray(globalData)) {
							for (const perm of globalData) {
								global[perm] = true;
							}
						} else if (typeof globalData === "object") {
							for (const [key, value] of Object.entries(globalData)) {
								global[key] = value === true || value === "true";
							}
						}
					}
				}
			} catch (err) {
				console.error("[permissions] Failed to fetch global permissions:", err);
			}
		}

		if (config.projectId) {
			const projectId = config.projectId;
			try {
				const permUrl = `${config.baseURL}/api/v1/projects/${projectId}/members/me/permissions`;
				const permResponse = await fetch(permUrl, { headers });

				if (permResponse.ok) {
					const permJson = await permResponse.json();
					let permData: any;
					// Check for nested data.permissions structure (API response format)
					if (
						permJson &&
						typeof permJson === "object" &&
						"data" in permJson &&
						permJson.data &&
						typeof permJson.data === "object" &&
						"permissions" in permJson.data
					) {
						permData = permJson.data.permissions;
					} else if (
						permJson &&
						typeof permJson === "object" &&
						"permissions" in permJson
					) {
						permData = permJson.permissions;
					} else {
						permData = permJson;
					}

					if (permData && typeof permData === "object") {
						projects[projectId] = {};
						for (const [key, value] of Object.entries(permData)) {
							projects[projectId][key] = value === true || value === "true";
						}
						console.error(
							`[permissions] Project ${projectId} permissions:`,
							Object.keys(projects[projectId]),
						);
					}
				} else {
					console.error(
						`Failed to fetch permissions for project ${projectId}: ${permResponse.status} ${permResponse.statusText}`,
					);
				}
			} catch (err) {
				console.error(
					`Failed to fetch permissions for project ${projectId}:`,
					err,
				);
			}

			const entityType = config.agentId ? "agent" : "user";
			console.error(
				`[permissions] Loaded permissions for ${entityType} in project ${projectId}`,
			);
		}
	} catch (error) {
		console.error("[permissions] Failed to fetch permissions:", error);
	}

	return { global, projects };
}

export function hasPermission(
	permissionMap: PermissionMap,
	permissionKey: string,
	projectId?: string,
): boolean {
	if (!permissionKey) return true;

	const { global, projects } = permissionMap;

	if (global["*"] === true) {
		console.error(`[permissions] Granting ${permissionKey} via global *`);
		return true;
	}

	if (global[permissionKey] === true) {
		console.error(
			`[permissions] Granting ${permissionKey} via global exact match`,
		);
		return true;
	}

	const parts = permissionKey.split(".");
	if (parts.length >= 2) {
		const wildcardKey = `${parts[0]}.*`;
		if (global[wildcardKey] === true) {
			console.error(
				`[permissions] Granting ${permissionKey} via global ${wildcardKey}`,
			);
			return true;
		}
	}

	if (projectId && projects[projectId]) {
		if (projects[projectId]["*"] === true) {
			console.error(
				`[permissions] Granting ${permissionKey} via project ${projectId} *`,
			);
			return true;
		}
		if (projects[projectId][permissionKey] === true) {
			console.error(
				`[permissions] Granting ${permissionKey} via project ${projectId} exact match`,
			);
			return true;
		}
		const parts = permissionKey.split(".");
		if (parts.length >= 2) {
			const wildcardKey = `${parts[0]}.*`;
			if (projects[projectId][wildcardKey] === true) {
				console.error(
					`[permissions] Granting ${permissionKey} via project ${projectId} ${wildcardKey}`,
				);
				return true;
			}
		}
		console.error(
			`[permissions] Denying ${permissionKey} for project ${projectId} - no matching permission`,
		);
	}

	return false;
}

export function getToolPermission(toolName: string): ToolPermission | null {
	return TOOL_PERMISSIONS.find((tp) => tp.toolName === toolName) || null;
}
