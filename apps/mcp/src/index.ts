#!/usr/bin/env node

import { createRequire } from "node:module";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { resolveAuthEnv } from "./auth.js";
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

	// Auth mode (Galaxy fork, ADR-038): fail fast on invalid PACA_AUTH_MODE
	// or bearer mode without a PACA_MCP_TOKEN — a misconfigured server that
	// limps along could only ever produce confusing downstream 401s.
	let authMode: PacaConfig["authMode"];
	let bearerToken: string | undefined;
	try {
		({ authMode, bearerToken } = resolveAuthEnv(process.env));
	} catch (err) {
		console.error(err instanceof Error ? err.message : String(err));
		process.exit(1);
	}

	// Validate required configuration (apikey mode only — bearer mode
	// authenticates exclusively with the platform token).
	if (authMode !== "bearer" && !apiKey) {
		console.error(
			"PACA_API_KEY environment variable is required. Please set it to your Paca API key.",
		);
		console.error("\nExample:");
		console.error("  export PACA_API_KEY='your-api-key-here'");
		console.error("  export PACA_API_URL='http://localhost:8080'");
		process.exit(1);
	}

	// If agent ID is provided, project ID is required
	if (agentId && !projectId) {
		console.error(
			"PACA_PROJECT_ID environment variable is required when using PACA_AGENT_ID.",
		);
		console.error("\nExample:");
		console.error("  export PACA_AGENT_ID='your-agent-id-here'");
		console.error("  export PACA_PROJECT_ID='your-project-id-here'");
		console.error("  export PACA_API_KEY='your-api-key-here'");
		console.error("  export PACA_API_URL='http://localhost:8080'");
		process.exit(1);
	}

	if (authMode === "bearer" && agentId) {
		// Identity comes from the token's signed act_as claim — never from an
		// X-Agent-ID header (the Galaxy API rejects header impersonation by
		// design). Drop the agent id so no code path can ever send it.
		console.error(
			"[auth] PACA_AUTH_MODE=bearer: ignoring PACA_AGENT_ID — the principal " +
				"comes from the platform token's signed act_as claim, and agent " +
				"attribution from its act_as_agent claim.",
		);
	}

	// Create configuration object
	const config: PacaConfig = {
		apiKey: authMode === "bearer" ? "" : (apiKey as string),
		baseURL,
		gatewayURL,
		agentId: authMode === "bearer" ? undefined : agentId,
		projectId,
		authMode,
		bearerToken,
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
