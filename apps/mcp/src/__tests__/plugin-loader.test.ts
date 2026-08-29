import { afterEach, describe, expect, it, vi } from "vitest";
import { PluginRegistry, resolveImportUrl } from "../plugin-loader.js";
import type { PacaConfig } from "../types/index.js";

const config: PacaConfig = {
	apiKey: "test-key",
	baseURL: "http://localhost:8080",
};

// ---------------------------------------------------------------------------
// PluginRegistry.getToolContext
// ---------------------------------------------------------------------------

describe("PluginRegistry.getToolContext", () => {
	it("returns an empty array when no plugin is loaded", async () => {
		const registry = new PluginRegistry([]);
		const sections = await registry.getToolContext(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			config,
		);
		expect(sections).toEqual([]);
	});

	it("never calls a plugin that didn't declare a hook for this toolId", async () => {
		const getToolContext = vi.fn().mockResolvedValue("should not be called");
		const registry = new PluginRegistry([
			{
				pluginId: "com.paca.no-hook",
				entry: { tools: [], handleToolCall: vi.fn(), getToolContext },
				toolContextHooks: [], // implements the method but never declared it
			},
		]);
		const sections = await registry.getToolContext(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			config,
		);
		expect(sections).toEqual([]);
		expect(getToolContext).not.toHaveBeenCalled();
	});

	it("only calls plugins that declared a hook for the requested toolId", async () => {
		const taskHook = vi.fn().mockResolvedValue("## GitHub\nBranch: feat/t1");
		const sprintOnlyHook = vi.fn().mockResolvedValue("should not run for get_task");
		const registry = new PluginRegistry([
			{
				pluginId: "com.paca.github",
				entry: { tools: [], handleToolCall: vi.fn(), getToolContext: taskHook },
				toolContextHooks: ["get_task"],
			},
			{
				pluginId: "com.paca.other",
				entry: {
					tools: [],
					handleToolCall: vi.fn(),
					getToolContext: sprintOnlyHook,
				},
				toolContextHooks: ["list_sprints"],
			},
		]);

		const sections = await registry.getToolContext(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			config,
		);
		expect(sections).toEqual([
			{ pluginId: "com.paca.github", text: "## GitHub\nBranch: feat/t1" },
		]);
		expect(taskHook).toHaveBeenCalledTimes(1);
		expect(sprintOnlyHook).not.toHaveBeenCalled();
	});

	it("collects text from every declared plugin that returns a non-empty section", async () => {
		const registry = new PluginRegistry([
			{
				pluginId: "com.paca.github",
				entry: {
					tools: [],
					handleToolCall: vi.fn(),
					getToolContext: vi.fn().mockResolvedValue("## GitHub\nBranch: feat/t1"),
				},
				toolContextHooks: ["get_task"],
			},
			{
				pluginId: "com.paca.checklist",
				entry: {
					tools: [],
					handleToolCall: vi.fn(),
					getToolContext: vi.fn().mockResolvedValue("## Checklist\n- [ ] Item"),
				},
				toolContextHooks: ["get_task"],
			},
		]);
		const sections = await registry.getToolContext(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			config,
		);
		expect(sections).toEqual([
			{ pluginId: "com.paca.github", text: "## GitHub\nBranch: feat/t1" },
			{ pluginId: "com.paca.checklist", text: "## Checklist\n- [ ] Item" },
		]);
	});

	it("omits plugins that resolve to null (nothing to contribute)", async () => {
		const registry = new PluginRegistry([
			{
				pluginId: "com.paca.github",
				entry: {
					tools: [],
					handleToolCall: vi.fn(),
					getToolContext: vi.fn().mockResolvedValue(null),
				},
				toolContextHooks: ["get_task"],
			},
		]);
		const sections = await registry.getToolContext(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			config,
		);
		expect(sections).toEqual([]);
	});

	it("logs and contributes nothing when a declared plugin's module has no getToolContext", async () => {
		const registry = new PluginRegistry([
			{
				pluginId: "com.paca.mismatched",
				entry: { tools: [], handleToolCall: vi.fn() }, // no getToolContext impl
				toolContextHooks: ["get_task"], // but manifest declares it
			},
		]);
		const sections = await registry.getToolContext(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			config,
		);
		expect(sections).toEqual([]);
	});

	it("swallows a throwing plugin without affecting the others", async () => {
		const registry = new PluginRegistry([
			{
				pluginId: "com.paca.broken",
				entry: {
					tools: [],
					handleToolCall: vi.fn(),
					getToolContext: vi.fn().mockRejectedValue(new Error("boom")),
				},
				toolContextHooks: ["get_task"],
			},
			{
				pluginId: "com.paca.bdd",
				entry: {
					tools: [],
					handleToolCall: vi.fn(),
					getToolContext: vi.fn().mockResolvedValue("## BDD\nScenario: X"),
				},
				toolContextHooks: ["get_task"],
			},
		]);
		const sections = await registry.getToolContext(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			config,
		);
		expect(sections).toEqual([
			{ pluginId: "com.paca.bdd", text: "## BDD\nScenario: X" },
		]);
	});

	it("passes toolId, args, and per-plugin context to getToolContext", async () => {
		const getToolContext = vi.fn().mockResolvedValue("## GitHub\n...");
		const registry = new PluginRegistry([
			{
				pluginId: "com.paca.github",
				entry: { tools: [], handleToolCall: vi.fn(), getToolContext },
				toolContextHooks: ["get_task"],
			},
		]);
		await registry.getToolContext(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			config,
		);
		expect(getToolContext).toHaveBeenCalledWith(
			"get_task",
			{ projectId: "p1", taskId: "t1" },
			{
				pluginId: "com.paca.github",
				baseURL: config.baseURL,
				apiKey: config.apiKey,
			},
		);
	});
});

// ---------------------------------------------------------------------------
// resolveImportUrl — importable-form resolution + SSRF guard
// ---------------------------------------------------------------------------

describe("resolveImportUrl", () => {
	afterEach(() => {
		vi.unstubAllGlobals();
	});

	function decodeDataUrl(dataUrl: string): string {
		const m = /^data:text\/javascript;base64,(.*)$/.exec(dataUrl);
		if (!m) throw new Error(`not a js data: URL: ${dataUrl.slice(0, 40)}`);
		return Buffer.from(m[1], "base64").toString("utf8");
	}

	it("fetches an https:// entry and returns it as a data: URL (Node can't import() https)", async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			text: async () => "export default { hello: 1 }",
		});
		vi.stubGlobal("fetch", fetchMock);

		// Bare *public* IP host: passes the SSRF guard without any DNS lookup.
		const out = await resolveImportUrl(
			"https://8.8.8.8/plugins-mcp/x/mcp.js",
			"https://paca.example.com",
		);

		expect(fetchMock).toHaveBeenCalledWith("https://8.8.8.8/plugins-mcp/x/mcp.js");
		expect(decodeDataUrl(out)).toBe("export default { hello: 1 }");
	});

	it("resolves a path-only entry against baseURL, then fetches it", async () => {
		const fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			text: async () => "export default {}",
		});
		vi.stubGlobal("fetch", fetchMock);

		const out = await resolveImportUrl(
			"/plugins-mcp/x/mcp.js",
			"https://8.8.8.8",
		);

		expect(fetchMock).toHaveBeenCalledWith("https://8.8.8.8/plugins-mcp/x/mcp.js");
		expect(out.startsWith("data:text/javascript;base64,")).toBe(true);
	});

	it("rejects an https:// host that is a private IP (SSRF) without fetching", async () => {
		const fetchMock = vi.fn();
		vi.stubGlobal("fetch", fetchMock);

		await expect(
			resolveImportUrl("https://10.0.0.5/mcp.js", "https://paca.example.com"),
		).rejects.toThrow(/private\/internal IP/);
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it("passes a file:// entry through unchanged (import()-able as-is)", async () => {
		const out = await resolveImportUrl(
			"file:///tmp/mcp.js",
			"https://paca.example.com",
		);
		expect(out).toBe("file:///tmp/mcp.js");
	});

	it("throws when the fetch fails", async () => {
		vi.stubGlobal(
			"fetch",
			vi.fn().mockResolvedValue({ ok: false, status: 404, statusText: "Not Found" }),
		);
		await expect(
			resolveImportUrl("https://8.8.8.8/mcp.js", "https://paca.example.com"),
		).rejects.toThrow(/Failed to fetch plugin module.*404/);
	});
});
