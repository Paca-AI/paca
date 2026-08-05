#!/usr/bin/env node

import { createRequire } from "node:module";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { createServer } from "./server.js";
import type { PacaConfig } from "./types/index.js";

const require = createRequire(import.meta.url);

/**
 * Main entry point for the Paca MCP server.
 * Initializes the API clients and starts the MCP server.
 */
async function main() {
	// Handle --version flag
	if (process.argv.includes("--version") || process.argv.includes("-v")) {
		const { version } = require("../package.json") as { version: string };
		console.log(version);
		process.exit(0);
	}

	// Get configuration from environment variables
	const apiKey = process.env.PACA_API_KEY;
	const baseURL = process.env.PACA_API_URL || "http://localhost:8080";
	const gatewayURL = process.env.PACA_GATEWAY_URL || undefined;
	const agentId = process.env.PACA_AGENT_ID || undefined;
	const projectId = process.env.PACA_PROJECT_ID || undefined;

	// Validate required configuration
	if (!apiKey) {
		console.error(
			"PACA_API_KEY environment variable is required. Please set it to your Paca API key.",
		);
		console.error("\nExample:");
		console.error("  export PACA_API_KEY='your-api-key-here'");
		console.error("  export PACA_API_URL='http://localhost:8080'");
		process.exit(1);
	}

	// PACA_PROJECT_ID pins every tool call to a single project ("single-project
	// agent mode" — see server.ts). It's optional when PACA_AGENT_ID is set:
	// a global-scope agent (services/ai-agent's builder.build_mcp_config omits
	// PACA_PROJECT_ID entirely for a global-chat conversation) runs "unpinned"
	// instead, the same mode a personal API key with no project already uses —
	// each tool call may target any project the agent has permission in
	// (e.g. list/create/update a project, or work within one it's been
	// invited into), passed explicitly per call rather than pinned once at
	// startup. See fetchAgentPermissions in permissions.ts.

	// Create configuration object
	const config: PacaConfig = {
		apiKey,
		baseURL,
		gatewayURL,
		agentId,
		projectId,
	};

	// Create and configure MCP server (loads plugin modules asynchronously)
	const server = await createServer(config);

	// Connect to stdio transport
	const transport = new StdioServerTransport();
	await server.connect(transport);
}

// Handle errors and exit gracefully
main().catch((error) => {
	console.error("Server error:", error);
	process.exit(1);
});
