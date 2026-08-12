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
		toolName: "read_task_attachment",
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

/** Parses a permissions object/array response body into a flat boolean map. */
function parsePermissionData(data: any): Record<string, boolean> {
	const out: Record<string, boolean> = {};
	if (!data) return out;
	if (Array.isArray(data)) {
		for (const perm of data) {
			out[perm] = true;
		}
	} else if (typeof data === "object") {
		for (const [key, value] of Object.entries(data)) {
			out[key] = value === true || value === "true";
		}
	}
	return out;
}

/** Extracts a `permissions` payload from either an unwrapped body or one
 * still wrapped in the API's `{success, data}` envelope — defensively
 * checking both shapes, since callers here use a raw fetch() rather than
 * PacaAPIClient's envelope-unwrapping request() helper. */
function extractPermissions(json: any): any {
	if (json && typeof json === "object") {
		if (
			"data" in json &&
			json.data &&
			typeof json.data === "object" &&
			"permissions" in json.data
		) {
			return json.data.permissions;
		}
		if ("permissions" in json) {
			return json.permissions;
		}
	}
	return json;
}

/**
 * Fetches this project's permission map for a single project and merges it
 * into `projects`. Shared by the pinned single-project fetch and the
 * unpinned global-agent multi-project fetch below — same endpoint
 * (GET .../members/me/permissions), same agent-or-human auth headers.
 */
async function fetchProjectPermissions(
	config: PacaConfig,
	headers: Record<string, string>,
	projectId: string,
	projects: Record<string, Record<string, boolean>>,
): Promise<void> {
	try {
		const permUrl = `${config.baseURL}/api/v1/projects/${projectId}/members/me/permissions`;
		const permResponse = await fetch(permUrl, { headers });

		if (!permResponse.ok) {
			console.error(
				`Failed to fetch permissions for project ${projectId}: ${permResponse.status} ${permResponse.statusText}`,
			);
			return;
		}
		const permJson = await permResponse.json();
		const permData = extractPermissions(permJson);
		if (permData && typeof permData === "object") {
			projects[projectId] = parsePermissionData(permData);
			console.error(
				`[permissions] Project ${projectId} permissions:`,
				Object.keys(projects[projectId]),
			);
		}
	} catch (err) {
		console.error(`Failed to fetch permissions for project ${projectId}:`, err);
	}
}

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

	// A global-scope agent: agentId set, no fixed projectId — running
	// "unpinned" (see index.ts). Unlike a project-scoped agent, it has its
	// own global role (agents.global_role_id) and may be invited into
	// several projects at once, so both the global-permissions fetch below
	// and the project-permissions fetch need a global-agent-specific path.
	const isGlobalAgent = Boolean(config.agentId) && !config.projectId;

	try {
		const headers: Record<string, string> = {
			"Content-Type": "application/json",
			"X-API-Key": config.apiKey,
		};
		if (config.agentId) {
			headers["X-Agent-ID"] = config.agentId;
		}

		// Global permissions: a human (personal key) reads their own via
		// /users/me/global-permissions; a global agent reads its own via the
		// analogous agent self-service endpoint (agent-API-key authenticated,
		// resolves via the agent's own global_role_id — see
		// authz.HasGlobalPermissionsForAgent on the backend). A
		// project-scoped agent has no global role of its own and skips this,
		// same as before.
		if (!config.agentId || isGlobalAgent) {
			try {
				const globalUrl = isGlobalAgent
					? `${config.baseURL}/api/v1/agents/me/global-permissions`
					: `${config.baseURL}/api/v1/users/me/global-permissions`;
				const globalResponse = await fetch(globalUrl, { headers });

				if (globalResponse.ok) {
					const globalJson = await globalResponse.json();
					const globalData = extractPermissions(globalJson);
					Object.assign(global, parsePermissionData(globalData));
				}
			} catch (err) {
				console.error("[permissions] Failed to fetch global permissions:", err);
			}
		}

		if (config.projectId) {
			await fetchProjectPermissions(config, headers, config.projectId, projects);
			const entityType = config.agentId ? "agent" : "user";
			console.error(
				`[permissions] Loaded permissions for ${entityType} in project ${config.projectId}`,
			);
		} else if (isGlobalAgent) {
			// Discover every project this global agent has been invited into
			// (GET /agents/me/projects), then fetch its own permissions in
			// each — the multi-project counterpart of the single fetch above.
			// This is what lets the tool-listing filter's requiresProject
			// branch (server.ts) and per-call projectId validation (never
			// pinned when config.projectId is unset) resolve to real
			// per-project permissions instead of an empty map.
			try {
				const projectsUrl = `${config.baseURL}/api/v1/agents/me/projects`;
				const projectsResponse = await fetch(projectsUrl, { headers });
				if (projectsResponse.ok) {
					const projectsJson = await projectsResponse.json();
					const body =
						projectsJson && typeof projectsJson === "object" && "data" in projectsJson
							? projectsJson.data
							: projectsJson;
					const projectIds: string[] = Array.isArray(body?.project_ids)
						? body.project_ids
						: [];
					await Promise.all(
						projectIds.map((projectId) =>
							fetchProjectPermissions(config, headers, projectId, projects),
						),
					);
					console.error(
						`[permissions] Global agent loaded permissions for ${projectIds.length} invited project(s)`,
					);
				} else {
					console.error(
						`[permissions] Failed to fetch invited projects: ${projectsResponse.status} ${projectsResponse.statusText}`,
					);
				}
			} catch (err) {
				console.error("[permissions] Failed to fetch invited projects:", err);
			}
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
