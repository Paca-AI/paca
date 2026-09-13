import { describe, expect, it } from "vitest";
import type { PermissionMap } from "../permissions.js";
import type { PluginContextSection } from "../plugin-loader.js";
import { isToolVisible, mergePluginContext } from "../server.js";

// ---------------------------------------------------------------------------
// isToolVisible
// ---------------------------------------------------------------------------
//
// Regression coverage for https://github.com/Paca-AI/paca/issues/461: an
// unpinned human user (no PACA_PROJECT_ID) whose global role grants
// everything via "*" only saw 10 of 76 tools, because the requiresProject
// branch scanned permissionMap.projects (empty for this mode) and never
// consulted the global map that was already fetched successfully.

describe("isToolVisible", () => {
	it("allows a tool with no permission mapping by default", () => {
		const empty: PermissionMap = { global: {}, projects: {} };
		expect(isToolVisible("nonexistent_tool", empty, undefined)).toBe(true);
	});

	describe("unpinned mode (projectId undefined)", () => {
		it("grants a requiresProject tool via a global wildcard *", () => {
			const map: PermissionMap = { global: { "*": true }, projects: {} };
			expect(isToolVisible("get_task", map, undefined)).toBe(true);
			expect(isToolVisible("create_task", map, undefined)).toBe(true);
		});

		it("grants a requiresProject tool via a global domain wildcard", () => {
			const map: PermissionMap = { global: { "tasks.*": true }, projects: {} };
			expect(isToolVisible("get_task", map, undefined)).toBe(true);
		});

		it("grants a requiresProject tool via a global exact match", () => {
			const map: PermissionMap = {
				global: { "tasks.read": true },
				projects: {},
			};
			expect(isToolVisible("get_task", map, undefined)).toBe(true);
		});

		it("still denies a requiresProject tool when the global grant doesn't cover it", () => {
			const map: PermissionMap = {
				global: { "projects.read": true },
				projects: {},
			};
			expect(isToolVisible("get_task", map, undefined)).toBe(false);
		});

		it("still falls back to scanning per-project permissions when there's no global grant", () => {
			const map: PermissionMap = {
				global: {},
				projects: { "proj-1": { "tasks.read": true } },
			};
			expect(isToolVisible("get_task", map, undefined)).toBe(true);
		});

		it("denies a requiresProject tool with no global grant and no matching project", () => {
			const map: PermissionMap = {
				global: {},
				projects: { "proj-1": { "tasks.write": true } },
			};
			expect(isToolVisible("get_task", map, undefined)).toBe(false);
		});

		it("gates a non-project tool on the global permission alone", () => {
			const map: PermissionMap = {
				global: { "projects.read": true },
				projects: {},
			};
			expect(isToolVisible("list_projects", map, undefined)).toBe(true);
			expect(isToolVisible("create_project", map, undefined)).toBe(false);
		});
	});

	describe("pinned mode (projectId set)", () => {
		it("gates a requiresProject tool on that project's permissions only", () => {
			const map: PermissionMap = {
				global: {},
				projects: { "proj-1": { "tasks.read": true } },
			};
			expect(isToolVisible("get_task", map, "proj-1")).toBe(true);
			expect(isToolVisible("get_task", map, "proj-2")).toBe(false);
		});

		it("still grants via a global wildcard even when pinned", () => {
			const map: PermissionMap = { global: { "*": true }, projects: {} };
			expect(isToolVisible("get_task", map, "proj-1")).toBe(true);
		});
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
