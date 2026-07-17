import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	fetchAgentPermissions,
	filterToolsByPermissions,
	getToolPermission,
	hasPermission,
	PermissionFetchError,
	type PermissionMap,
	TOOL_PERMISSIONS,
} from "../permissions.js";

// ---------------------------------------------------------------------------
// hasPermission
// ---------------------------------------------------------------------------

describe("hasPermission", () => {
	const empty: PermissionMap = { global: {}, projects: {} };

	it("returns true when permissionKey is empty", () => {
		expect(hasPermission(empty, "")).toBe(true);
	});

	it("returns false when no permissions are configured", () => {
		expect(hasPermission(empty, "tasks.read", "proj-1")).toBe(false);
	});

	it("returns false without projectId when only global permissions are empty", () => {
		expect(hasPermission(empty, "tasks.read")).toBe(false);
	});

	describe("global permissions", () => {
		it("grants via global wildcard *", () => {
			const map: PermissionMap = { global: { "*": true }, projects: {} };
			expect(hasPermission(map, "tasks.read")).toBe(true);
			expect(hasPermission(map, "projects.delete")).toBe(true);
		});

		it("grants via global exact match", () => {
			const map: PermissionMap = {
				global: { "tasks.read": true },
				projects: {},
			};
			expect(hasPermission(map, "tasks.read")).toBe(true);
		});

		it("denies when exact key does not match", () => {
			const map: PermissionMap = {
				global: { "tasks.write": true },
				projects: {},
			};
			expect(hasPermission(map, "tasks.read", "proj-1")).toBe(false);
		});

		it("grants via global domain wildcard tasks.*", () => {
			const map: PermissionMap = { global: { "tasks.*": true }, projects: {} };
			expect(hasPermission(map, "tasks.read")).toBe(true);
			expect(hasPermission(map, "tasks.write")).toBe(true);
		});

		it("denies via global domain wildcard when domain differs", () => {
			const map: PermissionMap = {
				global: { "projects.*": true },
				projects: {},
			};
			expect(hasPermission(map, "tasks.read", "proj-1")).toBe(false);
		});
	});

	describe("project-scoped permissions", () => {
		it("grants via project wildcard *", () => {
			const map: PermissionMap = {
				global: {},
				projects: { "proj-1": { "*": true } },
			};
			expect(hasPermission(map, "tasks.read", "proj-1")).toBe(true);
		});

		it("grants via project exact match", () => {
			const map: PermissionMap = {
				global: {},
				projects: { "proj-1": { "tasks.read": true } },
			};
			expect(hasPermission(map, "tasks.read", "proj-1")).toBe(true);
		});

		it("grants via project domain wildcard", () => {
			const map: PermissionMap = {
				global: {},
				projects: { "proj-1": { "tasks.*": true } },
			};
			expect(hasPermission(map, "tasks.write", "proj-1")).toBe(true);
		});

		it("denies when permission is for a different project", () => {
			const map: PermissionMap = {
				global: {},
				projects: { "proj-2": { "tasks.read": true } },
			};
			expect(hasPermission(map, "tasks.read", "proj-1")).toBe(false);
		});

		it("denies when no projectId is provided but permission only exists per-project", () => {
			const map: PermissionMap = {
				global: {},
				projects: { "proj-1": { "tasks.read": true } },
			};
			expect(hasPermission(map, "tasks.read")).toBe(false);
		});
	});

	describe("precedence", () => {
		it("global wildcard takes precedence over missing project permission", () => {
			const map: PermissionMap = {
				global: { "*": true },
				projects: { "proj-1": {} },
			};
			expect(hasPermission(map, "tasks.read", "proj-1")).toBe(true);
		});

		it("global exact match takes precedence over missing project permission", () => {
			const map: PermissionMap = {
				global: { "tasks.read": true },
				projects: {},
			};
			expect(hasPermission(map, "tasks.read", "proj-1")).toBe(true);
		});
	});
});

// ---------------------------------------------------------------------------
// getToolPermission
// ---------------------------------------------------------------------------

describe("getToolPermission", () => {
	it("returns null for an unknown tool name", () => {
		expect(getToolPermission("nonexistent_tool")).toBeNull();
	});

	it("returns the correct entry for list_projects", () => {
		const perm = getToolPermission("list_projects");
		expect(perm).not.toBeNull();
		expect(perm?.toolName).toBe("list_projects");
		expect(perm?.permissionKey).toBe("projects.read");
		expect(perm?.requiresProject).toBeUndefined();
	});

	it("returns requiresProject=true for get_project", () => {
		const perm = getToolPermission("get_project");
		expect(perm?.requiresProject).toBe(true);
	});

	it("returns the correct permission for create_task", () => {
		const perm = getToolPermission("create_task");
		expect(perm?.permissionKey).toBe("tasks.write");
		expect(perm?.requiresProject).toBe(true);
	});

	it("returns the correct permission for delete_sprint", () => {
		const perm = getToolPermission("delete_sprint");
		expect(perm?.permissionKey).toBe("sprints.write");
		expect(perm?.requiresProject).toBe(true);
	});

	it("returns the correct permission for list_views", () => {
		const perm = getToolPermission("list_views");
		expect(perm?.permissionKey).toBe("tasks.read");
		expect(perm?.requiresProject).toBe(true);
	});
});

// ---------------------------------------------------------------------------
// TOOL_PERMISSIONS constant
// ---------------------------------------------------------------------------

describe("TOOL_PERMISSIONS", () => {
	it("contains entries covering all main tool categories", () => {
		const names = TOOL_PERMISSIONS.map((p) => p.toolName);
		const expected = [
			"list_projects",
			"create_task",
			"delete_task",
			"list_sprints",
			"complete_sprint",
			"read_doc",
			"write_doc",
			"list_project_members",
			"list_task_types",
			"list_task_statuses",
			"list_views",
			"create_custom_field",
			"list_task_attachments",
			"add_task_comment",
		];
		for (const name of expected) {
			expect(names, `missing tool: ${name}`).toContain(name);
		}
	});

	it("has no duplicate tool names", () => {
		const names = TOOL_PERMISSIONS.map((p) => p.toolName);
		expect(new Set(names).size).toBe(names.length);
	});

	it("every entry has a non-empty permissionKey", () => {
		for (const entry of TOOL_PERMISSIONS) {
			expect(
				entry.permissionKey,
				`empty key for ${entry.toolName}`,
			).toBeTruthy();
		}
	});
});

// ---------------------------------------------------------------------------
// fetchAgentPermissions
// ---------------------------------------------------------------------------

describe("fetchAgentPermissions", () => {
	beforeEach(() => {
		vi.stubGlobal("fetch", vi.fn());
	});

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	it("returns empty maps when no agentId and no projectId (personal key mode)", async () => {
		const result = await fetchAgentPermissions({
			apiKey: "key-123",
			baseURL: "http://localhost:8080",
		});
		expect(result.global).toEqual({});
		expect(result.projects).toEqual({});
		// fetch should NOT have been called
		expect(vi.mocked(fetch)).not.toHaveBeenCalled();
	});

	it("fetches project permissions when projectId is provided", async () => {
		const mockPermissions = { "tasks.read": true, "tasks.write": false };

		// Without agentId the code fetches global permissions first, then project permissions.
		vi.mocked(fetch)
			.mockResolvedValueOnce({
				ok: true,
				json: async () => ({ permissions: {} }),
			} as Response)
			.mockResolvedValueOnce({
				ok: true,
				json: async () => ({ data: { permissions: mockPermissions } }),
			} as Response);

		const result = await fetchAgentPermissions({
			apiKey: "key-123",
			baseURL: "http://localhost:8080",
			projectId: "proj-abc",
		});

		const projectPermCall = vi
			.mocked(fetch)
			.mock.calls.find(([url]) =>
				(url as string).includes("members/me/permissions"),
			);
		expect(projectPermCall).toBeDefined();
		expect(projectPermCall?.[1]).toMatchObject({
			headers: expect.objectContaining({ "X-API-Key": "key-123" }),
		});
		expect(result.projects["proj-abc"]).toEqual({
			"tasks.read": true,
			"tasks.write": false,
		});
	});

	it("fetches global permissions when no agentId but projectId is set", async () => {
		// First call: global permissions; second call: project permissions
		vi.mocked(fetch)
			.mockResolvedValueOnce({
				ok: true,
				json: async () => ({ permissions: { "projects.read": true } }),
			} as Response)
			.mockResolvedValueOnce({
				ok: true,
				json: async () => ({ data: { permissions: {} } }),
			} as Response);

		const result = await fetchAgentPermissions({
			apiKey: "key-123",
			baseURL: "http://localhost:8080",
			projectId: "proj-abc",
		});

		expect(result.global).toEqual({ "projects.read": true });
	});

	it("throws when fetch fails so callers can fail closed (ADR-038)", async () => {
		vi.mocked(fetch).mockRejectedValue(new Error("network error"));

		await expect(
			fetchAgentPermissions({
				apiKey: "key-123",
				baseURL: "http://localhost:8080",
				projectId: "proj-abc",
			}),
		).rejects.toThrow(PermissionFetchError);
	});

	it("throws when the project permissions request returns non-OK", async () => {
		vi.mocked(fetch)
			.mockResolvedValueOnce({
				ok: true,
				json: async () => ({ permissions: {} }),
			} as Response)
			.mockResolvedValueOnce({
				ok: false,
				status: 500,
				statusText: "Internal Server Error",
			} as Response);

		await expect(
			fetchAgentPermissions({
				apiKey: "key-123",
				baseURL: "http://localhost:8080",
				projectId: "proj-abc",
			}),
		).rejects.toThrow(PermissionFetchError);
	});

	it("throws when the global permissions request returns non-OK", async () => {
		vi.mocked(fetch).mockResolvedValueOnce({
			ok: false,
			status: 502,
			statusText: "Bad Gateway",
		} as Response);

		await expect(
			fetchAgentPermissions({
				apiKey: "key-123",
				baseURL: "http://localhost:8080",
				projectId: "proj-abc",
			}),
		).rejects.toThrow(PermissionFetchError);
	});
});

// ---------------------------------------------------------------------------
// filterToolsByPermissions
// ---------------------------------------------------------------------------

describe("filterToolsByPermissions", () => {
	const tools = [
		{ name: "list_tasks" }, // mapped: tasks.read (requiresProject)
		{ name: "create_task" }, // mapped: tasks.write (requiresProject)
		{ name: "totally_unmapped_tool" },
	];

	it("keeps only tools whose permission is granted in project scope", () => {
		const map: PermissionMap = {
			global: {},
			projects: { "proj-1": { "tasks.read": true } },
		};

		const filtered = filterToolsByPermissions(tools, map, {
			apiKey: "k",
			baseURL: "http://localhost:8080",
			projectId: "proj-1",
		});

		const names = filtered.map((t) => t.name);
		expect(names).toContain("list_tasks");
		expect(names).not.toContain("create_task");
		// Unmapped tools are allowed by default (unchanged behavior).
		expect(names).toContain("totally_unmapped_tool");
	});

	it("hides all mapped tools when the permission map is empty in scoped mode", () => {
		const map: PermissionMap = { global: {}, projects: {} };

		const filtered = filterToolsByPermissions(tools, map, {
			apiKey: "k",
			baseURL: "http://localhost:8080",
			projectId: "proj-1",
		});

		expect(filtered.map((t) => t.name)).toEqual(["totally_unmapped_tool"]);
	});

	it("shows all tools in unscoped personal key mode", () => {
		const map: PermissionMap = { global: {}, projects: {} };

		const filtered = filterToolsByPermissions(tools, map, {
			apiKey: "k",
			baseURL: "http://localhost:8080",
		});

		expect(filtered).toHaveLength(tools.length);
	});
});
