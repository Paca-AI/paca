import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
	CallToolRequestSchema,
	ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import {
	PacaAPIAutomationClient,
	PacaAPIClient,
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
import { loadPlugins } from "./plugin-loader.js";
import { getAllTools, handleToolCall } from "./tools/index.js";
import type { PacaConfig } from "./types/index.js";

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

	const clients = {
		apiClient,
		extendedClient,
		viewsClient,
		taskExtendedClient,
		docClient,
		automationClient,
	};

	// Load plugin MCP modules from the Paca API.
	// Failures for individual plugins are logged and skipped.
	const pluginRegistry = await loadPlugins(config);

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
		const filteredCoreTools = allCoreTools.filter((tool) => {
			const toolPerm = getToolPermission(tool.name);
			if (!toolPerm) {
				console.error(
					`[server] Tool ${tool.name} has no permission mapping, allowing by default`,
				);
				return true;
			}

			// For personal API key without project ID, show all tools (backward compatibility)
			if (!config.agentId && !config.projectId) {
				console.error(
					`[server] Personal API key mode, allowing tool ${tool.name}`,
				);
				return true;
			}

			if (config.projectId) {
				const hasPerm = hasPermission(
					permissionMap,
					toolPerm.permissionKey,
					config.projectId,
				);
				console.error(
					`[server] Tool ${tool.name} requires ${toolPerm.permissionKey}, granted: ${hasPerm}`,
				);
				return hasPerm;
			}

			if (toolPerm.requiresProject) {
				const hasPerm = Object.keys(permissionMap.projects).some((projectId) =>
					hasPermission(permissionMap, toolPerm.permissionKey, projectId),
				);
				console.error(
					`[server] Tool ${tool.name} requires project permission ${toolPerm.permissionKey}, granted: ${hasPerm}`,
				);
				return hasPerm;
			}
			const hasPerm = hasPermission(permissionMap, toolPerm.permissionKey);
			console.error(
				`[server] Tool ${tool.name} requires global permission ${toolPerm.permissionKey}, granted: ${hasPerm}`,
			);
			return hasPerm;
		});

		console.error(
			`[server] Filtered ${filteredCoreTools.length} tools from ${allCoreTools.length} total tools`,
		);

		// Note: Plugin tools are not filtered by permissions at this level
		// Permissions are enforced at the API level
		return {
			tools: [...filteredCoreTools, ...allPluginTools],
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
		return handleToolCall(request, clients);
	});

	return server;
}
