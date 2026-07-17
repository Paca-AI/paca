import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import {
	CallToolRequestSchema,
	ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import {
	PacaAPIClient,
	PacaAPIDocClient,
	PacaAPIExtendedClient,
	PacaAPITaskExtendedClient,
	PacaAPIViewsClient,
	PacaAPIWorkflowClient,
} from "./api/index.js";
import {
	fetchAgentPermissions,
	filterToolsByPermissions,
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
	const workflowClient = new PacaAPIWorkflowClient(config);

	const clients = {
		apiClient,
		extendedClient,
		viewsClient,
		taskExtendedClient,
		docClient,
		workflowClient,
	};

	// Load plugin MCP modules from the Paca API.
	// Failures for individual plugins are logged and skipped.
	const pluginRegistry = await loadPlugins(config);

	// Fetch agent permissions at startup. If this fails the server still
	// starts, but it exposes ZERO tools until a fetch succeeds — never "all
	// tools" (fail closed, ADR-038).
	let permissionMap: PermissionMap | null = null;
	try {
		permissionMap = await fetchAgentPermissions(config);
	} catch (err) {
		console.error(
			"[server] Permission fetch failed at startup — exposing ZERO tools until it succeeds (fail closed, ADR-038):",
			err,
		);
	}

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
		// Retry a failed startup fetch; if permissions still cannot be
		// determined, surface the error and expose ZERO tools (ADR-038).
		if (permissionMap === null) {
			try {
				permissionMap = await fetchAgentPermissions(config);
			} catch (err) {
				const reason = err instanceof Error ? err.message : String(err);
				console.error(
					"[server] Permission fetch failed — exposing ZERO tools (fail closed, ADR-038):",
					err,
				);
				throw new Error(
					`Permission fetch failed; refusing to expose any tools (fail closed, ADR-038): ${reason}`,
				);
			}
		}

		const allCoreTools = getAllTools();
		const allPluginTools = pluginRegistry.getAllTools();

		// Filter core tools based on permissions
		const filteredCoreTools = filterToolsByPermissions(
			allCoreTools,
			permissionMap,
			config,
		);

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
