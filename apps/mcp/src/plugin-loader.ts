/**
 * Plugin MCP loader.
 *
 * At MCP server startup this module:
 * 1. Fetches the list of enabled plugins from `GET /api/v1/plugins`.
 * 2. For each plugin that declares `manifest.mcp.remoteEntryUrl`, dynamically
 *    imports the remote entry module.
 * 3. Validates the module's default export against the PluginMCPEntry contract.
 * 4. Collects all tool definitions and registers dispatch mappings.
 *
 * The resulting {@link PluginRegistry} is consumed by `server.ts` to merge
 * plugin tools with core tools and route tool calls.
 */

import { resolve4, resolve6 } from "node:dns/promises";
import { isIPv4, isIPv6 } from "node:net";
import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import type { PacaConfig } from "./types/index.js";

// ── Types ─────────────────────────────────────────────────────────────────────

interface InstalledPlugin {
	id: string;
	name: string;
	version: string;
	enabled: boolean;
	manifest: {
		mcp?: {
			remoteEntryUrl: string;
			/**
			 * Core tool IDs (e.g. "get_task") this plugin's `getToolContext`
			 * can contribute to. Declared up front so the host only calls
			 * into plugins that actually registered interest in a given
			 * tool, instead of invoking every loaded plugin on every call.
			 */
			toolContextHooks?: string[];
		};
	};
}

interface PluginListResponse {
	plugins: InstalledPlugin[];
}

/** The contract a plugin's default export must satisfy. */
interface PluginMCPEntry {
	tools: Tool[];
	handleToolCall(
		name: string,
		args: Record<string, unknown>,
		context: PluginMCPContext,
	): Promise<PluginToolResult>;
	/** Optional: contribute additional text to the response of any core tool call. */
	getToolContext?(
		toolId: string,
		args: Record<string, unknown>,
		context: PluginMCPContext,
	): Promise<string | null | undefined>;
}

/** A plugin-contributed section attached to a core tool's response. */
export interface PluginContextSection {
	pluginId: string;
	text: string;
}

interface PluginMCPContext {
	pluginId: string;
	baseURL: string;
	apiKey: string;
}

interface PluginToolResult {
	content: Array<{ type: string; [key: string]: unknown }>;
	isError?: boolean;
}

/** Loaded plugin: entry module + metadata. */
interface LoadedPlugin {
	pluginId: string;
	entry: PluginMCPEntry;
	/** Core tool IDs this plugin declared (in its manifest) it can add context to. */
	toolContextHooks: string[];
}

// ── Registry ──────────────────────────────────────────────────────────────────

/**
 * Holds all successfully loaded plugin MCP entries and their merged tool list.
 * Constructed by {@link loadPlugins}.
 */
export class PluginRegistry {
	private readonly loaded: LoadedPlugin[];
	/** Map from tool name → plugin ID (for fast dispatch). */
	private readonly toolOwner: Map<string, string>;
	/** Deduplicated tool definitions contributed by loaded plugins. */
	private readonly tools: Tool[];
	/**
	 * Map from core tool ID → plugins that declared (in their manifest) a
	 * `getToolContext` hook for it. Built once at load time so a call for a
	 * tool no plugin cares about costs a single Map lookup, not N plugin
	 * invocations.
	 */
	private readonly toolContextOwners: Map<string, LoadedPlugin[]>;

	constructor(loaded: LoadedPlugin[]) {
		this.loaded = loaded;
		this.toolOwner = new Map();
		this.tools = [];
		this.toolContextOwners = new Map();
		for (const p of loaded) {
			for (const tool of p.entry.tools) {
				if (this.toolOwner.has(tool.name)) {
					const existingPluginId = this.toolOwner.get(tool.name);
					console.warn(
						`Duplicate plugin MCP tool name "${tool.name}" declared by plugin "${p.pluginId}"; already registered by plugin "${existingPluginId}". Skipping duplicate.`,
					);
					continue;
				}

				this.toolOwner.set(tool.name, p.pluginId);
				this.tools.push(tool);
			}

			for (const toolId of p.toolContextHooks) {
				const owners = this.toolContextOwners.get(toolId) ?? [];
				owners.push(p);
				this.toolContextOwners.set(toolId, owners);
			}
		}
	}

	/** All unique tool definitions contributed by loaded plugins. */
	getAllTools(): Tool[] {
		return this.tools;
	}

	/**
	 * Dispatch a tool call to the owning plugin.
	 * Returns `null` if no loaded plugin owns the given tool name.
	 */
	async handleToolCall(
		toolName: string,
		args: Record<string, unknown>,
		config: PacaConfig,
	): Promise<PluginToolResult | null> {
		const pluginId = this.toolOwner.get(toolName);
		if (!pluginId) return null;

		const plugin = this.loaded.find((p) => p.pluginId === pluginId);
		if (!plugin) return null;

		const context: PluginMCPContext = {
			pluginId,
			baseURL: config.baseURL,
			apiKey: config.apiKey,
		};

		try {
			return await plugin.entry.handleToolCall(toolName, args, context);
		} catch (error) {
			const message =
				error instanceof Error ? error.message : "Unknown plugin tool error";

			return {
				content: [
					{
						type: "text",
						text: `Plugin tool "${toolName}" failed: ${message}`,
					},
				],
				isError: true,
			};
		}
	}

	/**
	 * Collect context sections for a given core tool call, but only from
	 * plugins that declared a `getToolContext` hook for that exact tool ID
	 * in their manifest (`mcp.toolContextHooks`). Plugins that didn't
	 * register interest in `toolId` are never invoked — this is a Map
	 * lookup, not a fan-out over every loaded plugin.
	 *
	 * A registered plugin that throws, or that turns out not to actually
	 * implement `getToolContext` (manifest/module mismatch), contributes
	 * nothing — one broken plugin cannot blank out the rest of the response.
	 *
	 * Results are returned in `candidates` (i.e. plugin load) order
	 * regardless of which plugin's call resolves first — the requests
	 * themselves still run concurrently via `Promise.all`, only the
	 * ordering of the resolved output is fixed, so a slow plugin can't
	 * shuffle the sections a caller sees on one call vs. the next.
	 */
	async getToolContext(
		toolId: string,
		args: Record<string, unknown>,
		config: PacaConfig,
	): Promise<PluginContextSection[]> {
		const candidates = this.toolContextOwners.get(toolId);
		if (!candidates || candidates.length === 0) return [];

		const results = await Promise.all(
			candidates.map(async (p): Promise<PluginContextSection | null> => {
				if (!p.entry.getToolContext) {
					console.error(
						`[plugin-loader] Plugin "${p.pluginId}" declared toolContextHooks for "${toolId}" but its module has no getToolContext method`,
					);
					return null;
				}

				const context: PluginMCPContext = {
					pluginId: p.pluginId,
					baseURL: config.baseURL,
					apiKey: config.apiKey,
				};

				try {
					const text = await p.entry.getToolContext(toolId, args, context);
					return text ? { pluginId: p.pluginId, text } : null;
				} catch (error) {
					console.error(
						`[plugin-loader] Plugin "${p.pluginId}" getToolContext("${toolId}") failed:`,
						error,
					);
					return null;
				}
			}),
		);

		return results.filter((s): s is PluginContextSection => s !== null);
	}
}

// ── Loader ────────────────────────────────────────────────────────────────────

/**
 * Fetch all enabled plugins from the Paca API and load any that declare an
 * `mcp.remoteEntryUrl` in their manifest.
 *
 * Failures for individual plugins are logged and skipped so that a broken
 * third-party plugin does not prevent the server from starting.
 */
export async function loadPlugins(config: PacaConfig): Promise<PluginRegistry> {
	let plugins: InstalledPlugin[] = [];

	try {
		plugins = await fetchInstalledPlugins(config);
	} catch (err) {
		console.error(
			"[plugin-loader] Failed to fetch plugin list — starting without plugins:",
			err,
		);
		return new PluginRegistry([]);
	}

	const mcpPlugins = plugins.filter(
		(p) => p.enabled && p.manifest?.mcp?.remoteEntryUrl,
	);

	if (mcpPlugins.length === 0) {
		return new PluginRegistry([]);
	}

	console.error(
		`[plugin-loader] Loading ${mcpPlugins.length} plugin(s) with MCP tools...`,
	);

	const loaded: LoadedPlugin[] = [];

	for (const plugin of mcpPlugins) {
		// biome-ignore lint/style/noNonNullAssertion: filtered above
		const url = plugin.manifest.mcp!.remoteEntryUrl;
		// Use gatewayURL when set — MCP bundles are served by the gateway (Caddy),
		// not the API service, so relative URLs must be resolved against the gateway.
		const pluginBaseURL = config.gatewayURL ?? config.baseURL;
		try {
			const entry = await loadPluginEntry(plugin.name, url, pluginBaseURL);
			const toolContextHooks = plugin.manifest.mcp?.toolContextHooks ?? [];
			loaded.push({ pluginId: plugin.name, entry, toolContextHooks });
			console.error(
				`[plugin-loader] Loaded "${plugin.name}" (${entry.tools.length} tool(s)` +
					(toolContextHooks.length > 0
						? `, context hooks: ${toolContextHooks.join(", ")})`
						: ")"),
			);
		} catch (err) {
			console.error(
				`[plugin-loader] Failed to load plugin "${plugin.name}" from ${url}:`,
				err,
			);
		}
	}

	return new PluginRegistry(loaded);
}

// ── Helpers ───────────────────────────────────────────────────────────────────

async function fetchInstalledPlugins(
	config: PacaConfig,
): Promise<InstalledPlugin[]> {
	const url = `${config.baseURL}/api/v1/plugins`;
	const response = await fetch(url, {
		headers: {
			"Content-Type": "application/json",
			"X-API-Key": config.apiKey,
		},
	});

	if (!response.ok) {
		const text = await response.text();
		throw new Error(
			`GET /api/v1/plugins failed: ${response.status} ${response.statusText} — ${text}`,
		);
	}

	const body = await response.json();

	// Handle SuccessEnvelope wrapper
	if (body && typeof body === "object" && "success" in body && body.success) {
		const data = body.data as PluginListResponse;
		return data.plugins ?? [];
	}

	// Direct array fallback
	if (Array.isArray(body)) return body;

	return [];
}

async function loadPluginEntry(
	pluginId: string,
	url: string,
	baseURL: string,
): Promise<PluginMCPEntry> {
	// Dynamic import works for both file:// and https:// URLs in Node 18+.
	// For http:// URLs (common in local dev), we fetch the source first and
	// evaluate it via a data: URL import.
	const importUrl = await resolveImportUrl(url, baseURL);
	const mod = await import(importUrl);

	const entry: unknown = mod.default ?? mod;

	assertPluginMCPEntry(pluginId, entry);
	return entry;
}

/**
 * Resolve a plugin entry URL to a form that Node.js `import()` can consume,
 * while enforcing URL safety rules:
 *
 * - Relative / path-only URLs (e.g. `/plugins-mcp/<id>/mcp.js`) are resolved
 *   against `baseURL` so that Node's `import()` receives an absolute URL.
 * - Only `https://`, `file://`, and `http://` (localhost-only or baseURL host)
 *   URLs are accepted.  Any other scheme is rejected.
 * - For `https://` URLs the hostname is resolved via DNS and the function
 *   throws if any resolved address falls inside a private / internal IP range
 *   (SSRF protection, similar to the API marketplace URL validator).
 * - For `http://` URLs the hostname must be localhost, a loopback address, or
 *   the same hostname as `baseURL` (trusted operator-configured gateway).
 *   The source is fetched and re-exposed as a `data:` URL because Node.js
 *   cannot `import()` plain `http://` URLs.
 */
async function resolveImportUrl(url: string, baseURL: string): Promise<string> {
	// Always resolve against baseURL so the URL constructor handles absolute,
	// relative, and protocol-relative URLs correctly without fragile heuristics:
	//   "/plugins-mcp/id/mcp.js" → "<baseURL>/plugins-mcp/id/mcp.js"
	//   "//cdn.example.com/mcp.js" → inherits baseURL's scheme
	//   "https://cdn.example.com/mcp.js" → unchanged (base ignored for absolute URLs)
	let resolved: URL;
	try {
		resolved = new URL(url, baseURL);
	} catch {
		throw new Error(`Plugin entry URL is invalid: "${url}"`);
	}

	const scheme = resolved.protocol; // includes trailing ':', e.g. "https:"

	if (scheme === "file:") {
		return resolved.href;
	}

	if (scheme === "https:") {
		// Guard against SSRF: reject hostnames that resolve to private IPs.
		await assertNotPrivateHost(resolved.hostname);
		return resolved.href;
	}

	if (scheme === "http:") {
		// http:// is allowed for localhost/loopback OR the operator-configured
		// baseURL hostname (e.g. an internal Docker gateway service).
		let baseURLHostname: string | undefined;
		try {
			baseURLHostname = new URL(baseURL).hostname;
		} catch {
			// ignore — baseURL may be invalid; fall through to localhost-only check
		}
		const isTrustedHost =
			isLocalhostHostname(resolved.hostname) ||
			(baseURLHostname !== undefined && resolved.hostname === baseURLHostname);
		if (!isTrustedHost) {
			throw new Error(
				`http:// plugin URLs are only allowed for localhost or the configured gateway host. ` +
					`Got "${resolved.hostname}" — use https:// for remote plugins.`,
			);
		}

		// Node.js cannot import() http:// URLs — fetch source and wrap in a
		// data: URL so import() can evaluate it without network restrictions.
		const response = await fetch(resolved.href);
		if (!response.ok) {
			throw new Error(
				`Failed to fetch plugin module from ${resolved.href}: ${response.status} ${response.statusText}`,
			);
		}
		const source = await response.text();
		// Use base64 to avoid issues with special characters in the source
		const b64 = Buffer.from(source, "utf8").toString("base64");
		return `data:text/javascript;base64,${b64}`;
	}

	throw new Error(
		`Plugin entry URL scheme "${scheme.replace(":", "")}" is not allowed. ` +
			`Only https://, http:// (localhost only), and file:// are permitted.`,
	);
}

/** Returns true if the hostname is a loopback / localhost address. */
function isLocalhostHostname(hostname: string): boolean {
	const lower = hostname.toLowerCase();
	// Plain loopback names and addresses
	if (lower === "localhost" || lower === "127.0.0.1" || lower === "::1") {
		return true;
	}
	// IPv4-mapped IPv6 loopback (e.g. ::ffff:127.0.0.1)
	if (lower.startsWith("::ffff:")) {
		const ipv4Part = lower.slice(7);
		return ipv4Part.startsWith("127.");
	}
	return false;
}

/**
 * Throws if `hostname` is an IP in a private / internal range, or if it
 * resolves via DNS to such an IP.  Mirrors the Go `isPrivateOrInternalIP`
 * helper used by the API marketplace URL validator.
 *
 * Note: like the Go implementation this is susceptible to DNS rebinding.
 * For production deployments consider an egress proxy with allowlist filtering.
 */
async function assertNotPrivateHost(hostname: string): Promise<void> {
	// If the hostname is already a bare IP, check it directly.
	if (isIPv4(hostname) || isIPv6(hostname)) {
		if (isPrivateIP(hostname)) {
			throw new Error(
				`Plugin entry URL hostname "${hostname}" is a private/internal IP address`,
			);
		}
		return;
	}

	// Resolve the hostname and check every resulting IP.
	const ips: string[] = [];
	const [v4Result, v6Result] = await Promise.allSettled([
		resolve4(hostname),
		resolve6(hostname),
	]);
	if (v4Result.status === "fulfilled") ips.push(...v4Result.value);
	if (v6Result.status === "fulfilled") ips.push(...v6Result.value);

	if (ips.length === 0) {
		throw new Error(
			`Failed to resolve plugin entry URL hostname "${hostname}"`,
		);
	}

	for (const ip of ips) {
		if (isPrivateIP(ip)) {
			throw new Error(
				`Plugin entry URL hostname "${hostname}" resolves to private/internal IP "${ip}"`,
			);
		}
	}
}

/**
 * Returns true if `ip` falls within a private / internal IPv4 or IPv6 range.
 * Covers: loopback, link-local, RFC-1918, and IPv6 unique-local / link-local.
 */
function isPrivateIP(ip: string): boolean {
	if (isIPv4(ip)) {
		const parts = ip.split(".").map(Number);
		const [a, b] = parts;
		// 127.0.0.0/8 — loopback
		if (a === 127) return true;
		// 10.0.0.0/8 — RFC 1918
		if (a === 10) return true;
		// 172.16.0.0/12 — RFC 1918
		if (a === 172 && b >= 16 && b <= 31) return true;
		// 192.168.0.0/16 — RFC 1918
		if (a === 192 && b === 168) return true;
		// 169.254.0.0/16 — link-local
		if (a === 169 && b === 254) return true;
		return false;
	}

	if (isIPv6(ip)) {
		const bytes = parseIPv6Bytes(ip);
		if (!bytes) return false;

		const b0 = bytes[0];
		const b1 = bytes[1];

		// ::1 — loopback (128-bit all-zeros except last bit)
		if (bytes.slice(0, 15).every((b) => b === 0) && bytes[15] === 1)
			return true;
		// fc00::/7 — unique local: first 7 bits are 1111110x  (b0 & 0xfe === 0xfc)
		if ((b0 & 0xfe) === 0xfc) return true;
		// fe80::/10 — link-local: first 10 bits are 1111111010 (b0 === 0xfe, b1 high 2 bits === 10)
		if (b0 === 0xfe && (b1 & 0xc0) === 0x80) return true;
		// ::ffff:0:0/96 — IPv4-mapped: delegate to IPv4 check
		const isIPv4Mapped =
			bytes.slice(0, 10).every((b) => b === 0) &&
			bytes[10] === 0xff &&
			bytes[11] === 0xff;
		if (isIPv4Mapped) {
			const ipv4 = `${bytes[12]}.${bytes[13]}.${bytes[14]}.${bytes[15]}`;
			return isPrivateIP(ipv4);
		}
		return false;
	}

	return false;
}

/**
 * Parse an IPv6 address string into a 16-byte Uint8Array.
 * Handles compressed (::) notation by expanding it first via the URL API,
 * which gives us reliable normalization without manual parsing.
 * Returns null if parsing fails.
 */
function parseIPv6Bytes(ip: string): Uint8Array | null {
	try {
		// Wrap in a URL to let the browser/Node URL parser normalize the address.
		const u = new URL(`http://[${ip}]/`);
		// hostname strips the brackets; the URL spec normalises IPv6 to lowercase.
		const normalized = u.hostname;

		// Expand :: shorthand.
		const halves = normalized.split("::");
		let groups: string[];
		if (halves.length === 2) {
			const left = halves[0] ? halves[0].split(":") : [];
			const right = halves[1] ? halves[1].split(":") : [];
			const missing = 8 - left.length - right.length;
			const fill = Array<string>(missing).fill("0");
			groups = [...left, ...fill, ...right];
		} else {
			groups = normalized.split(":");
		}

		if (groups.length !== 8) return null;

		const bytes = new Uint8Array(16);
		for (let i = 0; i < 8; i++) {
			const val = Number.parseInt(groups[i], 16);
			if (Number.isNaN(val) || val < 0 || val > 0xffff) return null;
			bytes[i * 2] = (val >> 8) & 0xff;
			bytes[i * 2 + 1] = val & 0xff;
		}
		return bytes;
	} catch {
		return null;
	}
}

function assertPluginMCPEntry(
	pluginId: string,
	value: unknown,
): asserts value is PluginMCPEntry {
	if (!value || typeof value !== "object") {
		throw new Error(`Plugin "${pluginId}": default export is not an object`);
	}
	const entry = value as Record<string, unknown>;
	if (!Array.isArray(entry.tools)) {
		throw new Error(
			`Plugin "${pluginId}": default export must have a "tools" array`,
		);
	}
	if (typeof entry.handleToolCall !== "function") {
		throw new Error(
			`Plugin "${pluginId}": default export must have a "handleToolCall" function`,
		);
	}
}
