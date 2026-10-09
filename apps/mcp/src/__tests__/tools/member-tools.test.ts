import { describe, expect, it, vi } from "vitest";

vi.mock("../../utils/index.js", () => ({
	formatList: vi.fn((items: any[], fn: any) => items.map(fn).join("---")),
}));

import {
	getProjectMemberTools,
	getProjectRoleTools,
	handleProjectMemberTool,
} from "../../tools/member-tools.js";

const member = {
	id: "m1",
	project_id: "p1",
	user_id: "u1",
	roles: [{ id: "r1", name: "Developer" }],
	username: "alice",
	full_name: "Alice Smith",
	joined_at: "2024-01-01T00:00:00Z",
};

const role = {
	id: "r1",
	project_id: "p1",
	name: "Developer",
	description: "Dev role",
	policy: {
		version: "2026-10-01",
		statements: [
			{
				effect: "Allow",
				actions: ["tasks:write"],
				resources: ["project/p1/*"],
			},
		],
	},
	is_system: false,
	created_at: "2024-01-01T00:00:00Z",
	updated_at: "2024-01-01T00:00:00Z",
};

function makeClient(overrides: Record<string, any> = {}) {
	return {
		listProjectMembers: vi.fn().mockResolvedValue([member]),
		addProjectMember: vi.fn().mockResolvedValue(member),
		getMyProjectPermissions: vi.fn().mockResolvedValue(["tasks:read"]),
		updateProjectMemberRole: vi.fn().mockResolvedValue([role]),
		removeProjectMember: vi.fn().mockResolvedValue(undefined),
		listProjectRoles: vi.fn().mockResolvedValue([role]),
		createProjectRole: vi.fn().mockResolvedValue(role),
		getProjectRole: vi.fn().mockResolvedValue(role),
		updateProjectRole: vi.fn().mockResolvedValue(role),
		deleteProjectRole: vi.fn().mockResolvedValue(undefined),
		...overrides,
	} as any;
}

// ---------------------------------------------------------------------------
// getProjectMemberTools / getProjectRoleTools
// ---------------------------------------------------------------------------

describe("getProjectMemberTools", () => {
	it("returns 5 member tools", () => {
		expect(getProjectMemberTools()).toHaveLength(5);
	});
});

describe("getProjectRoleTools", () => {
	it("returns 4 role tools", () => {
		expect(getProjectRoleTools()).toHaveLength(4);
	});
});

// ---------------------------------------------------------------------------
// list_project_members
// ---------------------------------------------------------------------------

describe("handleProjectMemberTool – list_project_members", () => {
	it("calls client.listProjectMembers with projectId", async () => {
		const client = makeClient();
		await handleProjectMemberTool(
			"list_project_members",
			{ projectId: "p1" },
			client,
		);
		expect(client.listProjectMembers).toHaveBeenCalledWith("p1");
	});

	it("includes 'Project Members:' header and member username in response", async () => {
		const result = await handleProjectMemberTool(
			"list_project_members",
			{ projectId: "p1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("Project Members:");
		expect(result.content[0].text).toContain("alice");
	});

	it("surfaces the member ID distinctly from the user ID", async () => {
		// Regression test: assigneeId (task assignment) and trigger_ai_agent's
		// member_id both expect member.id, not member.user_id — the two differ
		// per member, and this formatted text is the only place an agent can
		// look them up. Losing this line silently breaks every "use
		// list_project_members to get the member ID" tool description.
		const result = await handleProjectMemberTool(
			"list_project_members",
			{ projectId: "p1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("Member ID: m1");
		expect(result.content[0].text).toContain("User ID: u1");
	});
});

// ---------------------------------------------------------------------------
// add_project_member
// ---------------------------------------------------------------------------

describe("handleProjectMemberTool – add_project_member", () => {
	it("calls client.addProjectMember with mapped input", async () => {
		const client = makeClient();
		await handleProjectMemberTool(
			"add_project_member",
			{ projectId: "p1", userId: "u1", roleIds: ["r1"] },
			client,
		);
		expect(client.addProjectMember).toHaveBeenCalledWith("p1", {
			user_id: "u1",
			role_ids: ["r1"],
		});
	});

	it("includes 'added successfully' in response", async () => {
		const result = await handleProjectMemberTool(
			"add_project_member",
			{ projectId: "p1", userId: "u1", roleIds: ["r1"] },
			makeClient(),
		);
		expect(result.content[0].text).toContain("added successfully");
	});
});

// ---------------------------------------------------------------------------
// get_my_project_permissions
// ---------------------------------------------------------------------------

describe("handleProjectMemberTool – get_my_project_permissions", () => {
	it("calls client.getMyProjectPermissions with projectId", async () => {
		const client = makeClient();
		await handleProjectMemberTool(
			"get_my_project_permissions",
			{ projectId: "p1" },
			client,
		);
		expect(client.getMyProjectPermissions).toHaveBeenCalledWith("p1");
	});

	it("returns JSON-serialized actions", async () => {
		const result = await handleProjectMemberTool(
			"get_my_project_permissions",
			{ projectId: "p1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("tasks:read");
		expect(result.content[0].text).toContain("My Actions:");
	});
});

// ---------------------------------------------------------------------------
// update_project_member_role
// ---------------------------------------------------------------------------

describe("handleProjectMemberTool – update_project_member_role", () => {
	it("calls client.updateProjectMemberRole with userId and mapped input", async () => {
		const client = makeClient();
		await handleProjectMemberTool(
			"update_project_member_role",
			{ projectId: "p1", userId: "u1", roleIds: ["r2"] },
			client,
		);
		expect(client.updateProjectMemberRole).toHaveBeenCalledWith("p1", "u1", {
			role_ids: ["r2"],
		});
	});

	it("includes 'updated successfully' in response", async () => {
		const result = await handleProjectMemberTool(
			"update_project_member_role",
			{ projectId: "p1", userId: "u1", roleIds: ["r2"] },
			makeClient(),
		);
		expect(result.content[0].text).toContain("updated successfully");
	});
});

// ---------------------------------------------------------------------------
// remove_project_member
// ---------------------------------------------------------------------------

describe("handleProjectMemberTool – remove_project_member", () => {
	it("calls client.removeProjectMember with projectId and userId", async () => {
		const client = makeClient();
		await handleProjectMemberTool(
			"remove_project_member",
			{ projectId: "p1", userId: "u1" },
			client,
		);
		expect(client.removeProjectMember).toHaveBeenCalledWith("p1", "u1");
	});

	it("includes 'removed successfully' in response", async () => {
		const result = await handleProjectMemberTool(
			"remove_project_member",
			{ projectId: "p1", userId: "u1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("removed successfully");
		expect(result.content[0].text).toContain("u1");
	});
});

// ---------------------------------------------------------------------------
// list_project_roles
// ---------------------------------------------------------------------------

describe("handleProjectMemberTool – list_project_roles", () => {
	it("calls client.listProjectRoles with projectId", async () => {
		const client = makeClient();
		await handleProjectMemberTool(
			"list_project_roles",
			{ projectId: "p1" },
			client,
		);
		expect(client.listProjectRoles).toHaveBeenCalledWith("p1");
	});

	it("includes 'Project Roles:' header and role name in response", async () => {
		const result = await handleProjectMemberTool(
			"list_project_roles",
			{ projectId: "p1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("Project Roles:");
		expect(result.content[0].text).toContain("Developer");
	});
});

// ---------------------------------------------------------------------------
// create_project_role
// ---------------------------------------------------------------------------

describe("handleProjectMemberTool – create_project_role", () => {
	const policy = {
		statements: [
			{
				effect: "Allow",
				actions: ["tasks:write", "tasks:read"],
				resources: ["project/p1/*"],
			},
		],
	};

	it("calls client.createProjectRole with the policy document", async () => {
		const client = makeClient();
		await handleProjectMemberTool(
			"create_project_role",
			{ projectId: "p1", name: "Dev", policy },
			client,
		);
		expect(client.createProjectRole).toHaveBeenCalledWith("p1", {
			name: "Dev",
			description: undefined,
			policy: { version: "2026-10-01", ...policy },
		});
	});

	it("includes 'created successfully' in response", async () => {
		const result = await handleProjectMemberTool(
			"create_project_role",
			{ projectId: "p1", name: "Dev", policy },
			makeClient(),
		);
		expect(result.content[0].text).toContain("created successfully");
	});

	it("rejects a legacy permission list", async () => {
		await expect(
			handleProjectMemberTool(
				"create_project_role",
				{ projectId: "p1", name: "Dev", permissions: ["tasks.write"] },
				makeClient(),
			),
		).rejects.toThrow();
	});
});

// ---------------------------------------------------------------------------
// update_project_role
// ---------------------------------------------------------------------------

describe("handleProjectMemberTool – update_project_role", () => {
	it("calls client.updateProjectRole with the new name and policy", async () => {
		const client = makeClient();
		const policy = {
			statements: [
				{ effect: "Allow", actions: ["tasks:*"], resources: ["project/p1/*"] },
			],
		};
		await handleProjectMemberTool(
			"update_project_role",
			{ projectId: "p1", roleId: "r1", name: "Senior Dev", policy },
			client,
		);
		expect(client.updateProjectRole).toHaveBeenCalledWith("p1", "r1", {
			name: "Senior Dev",
			description: "Dev role",
			policy: { version: "2026-10-01", ...policy },
		});
	});

	it("keeps the current name, description and policy when omitted", async () => {
		const client = makeClient();
		await handleProjectMemberTool(
			"update_project_role",
			{ projectId: "p1", roleId: "r1" },
			client,
		);
		expect(client.getProjectRole).toHaveBeenCalledWith("p1", "r1");
		expect(client.updateProjectRole).toHaveBeenCalledWith("p1", "r1", {
			name: role.name,
			description: role.description,
			policy: role.policy,
		});
	});
});

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------
// delete_project_role
// ---------------------------------------------------------------------------

describe("handleProjectMemberTool – delete_project_role", () => {
	it("calls client.deleteProjectRole with projectId and roleId", async () => {
		const client = makeClient();
		await handleProjectMemberTool(
			"delete_project_role",
			{ projectId: "p1", roleId: "r1" },
			client,
		);
		expect(client.deleteProjectRole).toHaveBeenCalledWith("p1", "r1");
	});

	it("includes 'deleted successfully' in response", async () => {
		const result = await handleProjectMemberTool(
			"delete_project_role",
			{ projectId: "p1", roleId: "r1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("deleted successfully");
		expect(result.content[0].text).toContain("r1");
	});
});

// ---------------------------------------------------------------------------
// unknown tool
// ---------------------------------------------------------------------------

describe("handleProjectMemberTool – unknown tool", () => {
	it("throws for an unknown tool name", async () => {
		await expect(
			handleProjectMemberTool("unknown", {}, makeClient()),
		).rejects.toThrow();
	});
});
