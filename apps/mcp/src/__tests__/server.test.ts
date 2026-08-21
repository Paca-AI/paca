import { afterEach, describe, expect, it } from "vitest";
import { type PluginContextSection, PluginRegistry } from "../plugin-loader.js";
import {
	createServer,
	mergePluginContext,
	turnPolicyAllowsTool,
} from "../server.js";

const originalFetch = globalThis.fetch;
afterEach(() => {
	globalThis.fetch = originalFetch;
});

describe("turnPolicyAllowsTool", () => {
	it("does not fetch or load plugin modules for an authoritative turn", async () => {
		globalThis.fetch = async () =>
			new Response(
				JSON.stringify({ success: true, data: { permissions: {} } }),
				{
					status: 200,
					headers: { "Content-Type": "application/json" },
				},
			);
		let loaderCalls = 0;
		const loader = async () => {
			loaderCalls++;
			return new PluginRegistry([]);
		};

		await createServer(
			{
				apiKey: "turn-scoped-only",
				baseURL: "https://paca.invalid",
				agentId: "agent-1",
				projectId: "project-1",
				agentTurnId: "turn-1",
				turnAllowedCapabilities: ["tasks.read"],
			},
			loader,
		);

		expect(loaderCalls).toBe(0);
	});

	it("allows mapped read tools in an authoritative private turn", () => {
		expect(turnPolicyAllowsTool("get_task", "turn-1", ["tasks.read"])).toBe(
			true,
		);
	});

	it("rejects task mutation tools even when the agent role is broader", () => {
		expect(turnPolicyAllowsTool("update_task", "turn-1", ["tasks.read"])).toBe(
			false,
		);
	});

	it("fails closed for unknown and plugin tools", () => {
		expect(
			turnPolicyAllowsTool("plugin_mutate_task", "turn-1", ["tasks.read"]),
		).toBe(false);
	});

	it("preserves legacy tool discovery outside an authoritative turn", () => {
		expect(turnPolicyAllowsTool("plugin_mutate_task", undefined, [])).toBe(
			true,
		);
	});
});

// ---------------------------------------------------------------------------
// mergePluginContext
// ---------------------------------------------------------------------------
//
// Regression coverage for the bug described in the getToolContext PR: a
// plugin's contributed text was originally appended as a separate trailing
// content block, which agents were observed treating as unrelated and
// ignoring (e.g. calling github_list_task_branches right after get_task
// despite the branch already being in a second block). The fix merges into
// the last existing text block instead.

describe("mergePluginContext", () => {
	const section = (text: string, pluginId = "com.paca.github") =>
		[{ pluginId, text }] satisfies PluginContextSection[];

	it("merges a single section into the last text block", () => {
		const result = {
			content: [{ type: "text", text: "# Task: Fix login bug" }],
		};
		const merged = mergePluginContext(
			result,
			section("## GitHub\nBranch: feat/t1"),
		);
		expect(merged.content).toHaveLength(1);
		expect(merged.content[0]).toEqual({
			type: "text",
			text: "# Task: Fix login bug\n\n## GitHub\nBranch: feat/t1",
		});
	});

	it("joins multiple sections in the given order before merging", () => {
		const result = { content: [{ type: "text", text: "# Task" }] };
		const merged = mergePluginContext(result, [
			{ pluginId: "com.paca.github", text: "## GitHub" },
			{ pluginId: "com.paca.checklist", text: "## Checklist" },
		]);
		expect(merged.content[0].text).toBe("# Task\n\n## GitHub\n\n## Checklist");
	});

	it("appends a new text block when content is empty", () => {
		const result = { content: [] };
		const merged = mergePluginContext(result, section("## GitHub"));
		expect(merged.content).toEqual([{ type: "text", text: "## GitHub" }]);
	});

	it("appends a new text block when the last block isn't type text", () => {
		const result = {
			content: [{ type: "image", data: "base64...", mimeType: "image/png" }],
		};
		const merged = mergePluginContext(result, section("## GitHub"));
		expect(merged.content).toHaveLength(2);
		expect(merged.content[1]).toEqual({ type: "text", text: "## GitHub" });
	});

	it("does not mutate the original result's content array", () => {
		const originalContent = [{ type: "text", text: "# Task" }];
		const result = { content: originalContent };
		mergePluginContext(result, section("## GitHub"));
		expect(originalContent).toEqual([{ type: "text", text: "# Task" }]);
	});

	it("preserves other fields on the result (e.g. isError: false)", () => {
		const result = {
			content: [{ type: "text", text: "# Task" }],
			isError: false,
		};
		const merged = mergePluginContext(result, section("## GitHub"));
		expect(merged.isError).toBe(false);
	});
});
