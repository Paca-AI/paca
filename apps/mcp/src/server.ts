import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
	CallToolRequestSchema,
	ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import {
	PacaAPIAnnotationClient,
	PacaAPIAutomationClient,
	PacaAPIClient,
	PacaAPIConversationClient,
	PacaAPIDocClient,
	PacaAPIExtendedClient,
	PacaAPITaskExtendedClient,
	PacaAPIViewsClient,
} from "./api/index.js";
import {
	fetchAgentPermissions,
	getToolPermission,
	hasPermission,
	type PermissionMap,
} from "./permissions.js";
import { loadPlugins, type PluginContextSection } from "./plugin-loader.js";
import { getAllTools, handleToolCall } from "./tools/index.js";
import type { PacaConfig } from "./types/index.js";

const REPO_TOOL_NAMES = new Set([
	"list_repositories",
	"clone_repository",
	"push_branch",
]);

/**
 * Creates and configures the Paca MCP server.
 * Loads plugin MCP modules from the Paca API before returning.
 *
 * @param config - Paca configuration
 * @returns Configured MCP server
 */
export async function createServer(config: PacaConfig): Promise<Server> {
	// Initialize all API clients
	const apiClient = new PacaAPIClient(config);
	const extendedClient = new PacaAPIExtendedClient(config);
	const viewsClient = new PacaAPIViewsClient(config);
	const taskExtendedClient = new PacaAPITaskExtendedClient(config);
	const docClient = new PacaAPIDocClient(config);
	const automationClient = new PacaAPIAutomationClient(config);
	const conversationClient = new PacaAPIConversationClient(config);
	const annotationClient = new PacaAPIAnnotationClient(config);

	// Load plugin MCP modules from the Paca API.
	// Failures for individual plugins are logged and skipped.
	const pluginRegistry = await loadPlugins(config);

	const clients = {
		apiClient,
		extendedClient,
		viewsClient,
		taskExtendedClient,
		docClient,
		automationClient,
		conversationClient,
		annotationClient,
	};

	// Fetch agent permissions at startup
	const permissionMap: PermissionMap = await fetchAgentPermissions(config);

	const server = new Server(
		{
			name: "paca",
			version: "0.1.0",
		},
		{
			capabilities: {
				tools: {},
			},
		},
	);

	// Handler for listing available tools (core + plugins)
	server.setRequestHandler(ListToolsRequestSchema, async (_request) => {
		const allCoreTools = getAllTools();
		const allPluginTools = pluginRegistry.getAllTools();

		// Filter core tools based on permissions
		const filteredCoreTools = allCoreTools.filter((tool) =>
			isToolVisible(tool.name, permissionMap, config.projectId),
		);

		// Repo tools are hidden entirely when no repository plugin is
		// configured for this conversation — mirrors executor.py's has_repos
		// gate, which never attaches these tools to the agent at all rather
		// than attaching them to fail against an empty plugin list.
		const hasRepoPlugins = (config.repoPluginIds?.length ?? 0) > 0;
		const visibleCoreTools = hasRepoPlugins
			? filteredCoreTools
			: filteredCoreTools.filter((tool) => !REPO_TOOL_NAMES.has(tool.name));

		console.error(
			`[server] Filtered ${visibleCoreTools.length} tools from ${allCoreTools.length} total tools`,
		);

		// Note: Plugin tools are not filtered by permissions at this level
		// Permissions are enforced at the API level
		return {
			tools: [...visibleCoreTools, ...allPluginTools],
		};
	});

	// Handler for executing tool calls
	server.setRequestHandler(CallToolRequestSchema, async (request) => {
		const { name, arguments: args } = request.params;

		// Validate projectId in single-project mode
		if (
			config.projectId &&
			args &&
			typeof args === "object" &&
			"projectId" in args
		) {
			if (args.projectId !== config.projectId) {
				return {
					content: [
						{
							type: "text",
							text: `Error: projectId must be ${config.projectId} in single-project agent mode. Got ${args.projectId}`,
						},
					],
					isError: true,
				};
			}
		}

		// Try plugin registry first (plugin tool names are chosen by developers,
		// so we check plugins before falling through to core tools to make
		// routing explicit).
		const pluginResult = await pluginRegistry.handleToolCall(
			name,
			(args ?? {}) as Record<string, unknown>,
			config,
		);
		if (pluginResult !== null) {
			return pluginResult;
		}

		// Fall through to core tool handlers
		const result = await handleToolCall(
			request,
			clients,
			config.repoPluginIds ?? [],
		);

		// Let every loaded plugin optionally attach context to this core
		// tool's response (e.g. linked GitHub branches on get_task). Skipped
		// for error results so a failure isn't buried under unrelated data.
		if (!result?.isError) {
			const sections = await pluginRegistry.getToolContext(
				name,
				(args ?? {}) as Record<string, unknown>,
				config,
			);
			if (sections.length > 0) {
				return mergePluginContext(result, sections);
			}
		}

		return result;
	});

	return server;
}

/**
 * Determines whether a core tool should be listed for the current caller.
 *
 * `projectId` is `config.projectId` — set only in single-project (pinned)
 * mode. When unset, a `requiresProject` tool used to be gated solely on a
 * scan of `permissionMap.projects`, which is populated only for a pinned
 * project or a global agent's discovered invited projects (see
 * `fetchAgentPermissions` in permissions.ts). That left it empty for an
 * unpinned *human* user, so a global role grant (e.g. `{"*": true}`) was
 * never consulted and every project-scoped tool stayed hidden even for a
 * super-admin. Consulting the global permission map first fixes that
 * without granting anything extra: it's the same `hasPermission` check
 * already used for non-project-scoped tools, just applied here too.
 *
 * Exported for testing.
 */
export function isToolVisible(
	toolName: string,
	permissionMap: PermissionMap,
	projectId: string | undefined,
): boolean {
	const toolPerm = getToolPermission(toolName);
	if (!toolPerm) {
		console.error(
			`[server] Tool ${toolName} has no permission mapping, allowing by default`,
		);
		return true;
	}

	if (projectId) {
		const hasPerm = hasPermission(
			permissionMap,
			toolPerm.permissionKey,
			projectId,
		);
		console.error(
			`[server] Tool ${toolName} requires ${toolPerm.permissionKey}, granted: ${hasPerm}`,
		);
		return hasPerm;
	}

	if (toolPerm.requiresProject) {
		const hasPerm =
			hasPermission(permissionMap, toolPerm.permissionKey) ||
			Object.keys(permissionMap.projects).some((pid) =>
				hasPermission(permissionMap, toolPerm.permissionKey, pid),
			);
		console.error(
			`[server] Tool ${toolName} requires project permission ${toolPerm.permissionKey}, granted: ${hasPerm}`,
		);
		return hasPerm;
	}

	const hasPerm = hasPermission(permissionMap, toolPerm.permissionKey);
	console.error(
		`[server] Tool ${toolName} requires global permission ${toolPerm.permissionKey}, granted: ${hasPerm}`,
	);
	return hasPerm;
}

/**
 * Merge plugin-contributed context sections into a core tool's result.
 *
 * Merges into the *last* text content block rather than appending a new
 * content entry: agents reliably read the one continuous text block a core
 * tool returns, but have been observed treating a separate trailing block
 * as unrelated/easy to miss — e.g. calling github_list_task_branches right
 * after get_task despite the branch already being in a second block. Falls
 * back to appending a new text block when the result has no existing text
 * block to merge into (e.g. empty content array).
 *
 * Exported for testing; `sections` is expected to be non-empty — callers
 * should skip invoking this when there's nothing to merge.
 */
export function mergePluginContext(
	result: any,
	sections: PluginContextSection[],
): any {
	const extra = sections.map((s) => s.text).join("\n\n");
	const content = [...result.content];
	const lastIdx = content.length - 1;
	if (lastIdx >= 0 && content[lastIdx]?.type === "text") {
		content[lastIdx] = {
			...content[lastIdx],
			text: `${content[lastIdx].text}\n\n${extra}`,
		};
	} else {
		content.push({ type: "text", text: extra });
	}
	return { ...result, content };
}
