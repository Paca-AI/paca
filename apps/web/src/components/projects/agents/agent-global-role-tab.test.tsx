import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/agent-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/agent-api")>("@/lib/agent-api");
	return {
		...actual,
		setGlobalAgentRole: vi.fn(),
		clearGlobalAgentRole: vi.fn(),
	};
});

import {
	type Agent,
	clearGlobalAgentRole,
	globalAgentQueryOptions,
	setGlobalAgentRole,
} from "@/lib/agent-api";
import { makeRole, renderWithQueries } from "@/test/render-with-queries";
import { AgentGlobalRoleTab } from "./agent-global-role-tab";

const ROLES = [
	makeRole("role-user", "USER", { "tasks.read": true }),
	makeRole("role-admin", "ADMIN", { "users.read": true }),
	makeRole("role-root", "SUPER_ADMIN", { "*": true }),
];
const CAN_ASSIGN = ["global_roles.assign", "global_roles.read"];

const agentWith = (globalRoleId: string | null): Agent =>
	({
		id: "agent-1",
		name: "Bot",
		handle: "bot",
		global_role_id: globalRoleId,
	}) as Agent;

function renderTab({
	roleId = null,
	canWrite = true,
	permissions = CAN_ASSIGN,
	roles = ROLES,
}: {
	roleId?: string | null;
	canWrite?: boolean;
	permissions?: string[];
	roles?: typeof ROLES | null;
} = {}) {
	const agent = agentWith(roleId);
	const view = renderWithQueries(
		<AgentGlobalRoleTab agent={agent} canWrite={canWrite} />,
		{ permissions, roles },
	);
	return { agent, ...view };
}

const button = (name: RegExp | string) => screen.getByRole("button", { name });
const noButton = (name: RegExp | string) =>
	expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();

beforeEach(() => {
	vi.clearAllMocks();
	vi.mocked(setGlobalAgentRole).mockImplementation(async (_id, roleId) =>
		agentWith(roleId),
	);
	vi.mocked(clearGlobalAgentRole).mockResolvedValue(agentWith(null));
});

describe("AgentGlobalRoleTab — showing the role", () => {
	it("says an agent without a role has no global permissions and offers to assign one", () => {
		renderTab();

		expect(screen.getByText("No global role")).toBeInTheDocument();
		expect(
			screen.getByText("This agent has no global permissions."),
		).toBeInTheDocument();
		expect(button("Assign role")).toBeInTheDocument();
		noButton("Remove role");
		noButton("Change role");
	});

	it("shows the role an agent holds with what it grants, and offers to change or remove it", () => {
		renderTab({ roleId: "role-admin" });

		expect(screen.getByText("ADMIN")).toBeInTheDocument();
		expect(screen.getByText("users.read")).toBeInTheDocument();
		expect(screen.queryByText("No global role")).not.toBeInTheDocument();
		expect(button("Change role")).toBeInTheDocument();
		expect(button("Remove role")).toBeInTheDocument();
		noButton("Assign role");
	});

	it("shows a full-access role as such", () => {
		renderTab({ roleId: "role-root" });

		expect(screen.getByText("Full access")).toBeInTheDocument();
	});

	it("explains the scope: global chat, not what it may do inside a project", () => {
		renderTab();

		expect(
			screen.getByText(
				/at global scope.*inside a project comes from its role in that project/i,
			),
		).toBeInTheDocument();
	});
});

describe("AgentGlobalRoleTab — who may change it", () => {
	it("is read-only without agents.write, even with the role permissions", () => {
		renderTab({ roleId: "role-admin", canWrite: false });

		expect(screen.getByText("ADMIN")).toBeInTheDocument();
		expect(
			screen.getByText(/don't have permission to change it/i),
		).toBeInTheDocument();
		noButton("Change role");
		noButton("Remove role");
	});

	it("is read-only without global_roles.assign, even with agents.write", () => {
		renderTab({ roleId: "role-admin", permissions: ["global_roles.read"] });

		expect(screen.getByText("ADMIN")).toBeInTheDocument();
		expect(
			screen.getByText(/don't have permission to change it/i),
		).toBeInTheDocument();
		noButton("Change role");
		noButton("Remove role");
	});

	it("offers no controls, and says the name is hidden, without global_roles.read", () => {
		renderTab({
			roleId: "role-admin",
			permissions: ["global_roles.assign"],
			roles: null,
		});

		expect(
			screen.getByText(/don't have permission to view roles/i),
		).toBeInTheDocument();
		expect(screen.queryByText("ADMIN")).not.toBeInTheDocument();
		noButton("Change role");
	});

	it("does not offer the read-only note to someone who can change the role", () => {
		renderTab();

		expect(
			screen.queryByText(/don't have permission to change it/i),
		).not.toBeInTheDocument();
	});
});

describe("AgentGlobalRoleTab — assigning and changing", () => {
	it("assigns the chosen role through its own endpoint and shows it", async () => {
		const { client } = renderTab();

		await userEvent.click(button("Assign role"));
		// Nothing chosen yet: nothing to assign.
		expect(button("Assign role")).toBeDisabled();
		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(button("Assign role"));

		await waitFor(() =>
			expect(setGlobalAgentRole).toHaveBeenCalledWith("agent-1", "role-admin"),
		);
		// The agent's cache carries the new role straight away, and the picker closes.
		await waitFor(() =>
			expect(
				client.getQueryData<Agent>(globalAgentQueryOptions("agent-1").queryKey)
					?.global_role_id,
			).toBe("role-admin"),
		);
		expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
	});

	it("changes an existing role, marking the current one and not offering it again", async () => {
		renderTab({ roleId: "role-user" });

		await userEvent.click(button("Change role"));

		const current = screen.getByRole("radio", { name: "USER" });
		expect(current).toBeChecked();
		expect(current).toHaveAccessibleDescription(/Current/);
		expect(button("Assign role")).toBeDisabled();

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(button("Assign role"));

		await waitFor(() =>
			expect(setGlobalAgentRole).toHaveBeenCalledWith("agent-1", "role-admin"),
		);
	});

	it("warns before giving an agent a full-access role", async () => {
		renderTab();
		await userEvent.click(button("Assign role"));

		await userEvent.click(screen.getByRole("radio", { name: "SUPER_ADMIN" }));

		expect(
			screen.getByText(/the agent will be able to do everything/i),
		).toBeInTheDocument();
	});

	it("leaves the picker without changing anything on Cancel", async () => {
		renderTab({ roleId: "role-user" });
		await userEvent.click(button("Change role"));
		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));

		await userEvent.click(button("Cancel"));

		expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
		expect(setGlobalAgentRole).not.toHaveBeenCalled();
		expect(button("Change role")).toBeInTheDocument();
	});

	it("disables the choices and the button while the request is in flight", async () => {
		vi.mocked(setGlobalAgentRole).mockReturnValue(new Promise(() => {}));
		renderTab();
		await userEvent.click(button("Assign role"));
		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));

		await userEvent.click(button("Assign role"));

		expect(
			await screen.findByRole("button", { name: /assigning/i }),
		).toBeDisabled();
		expect(screen.getByRole("radio", { name: "USER" })).toBeDisabled();
	});

	it.each([
		["FORBIDDEN", "You don't have permission to change global roles."],
		[
			"GLOBAL_ROLE_NOT_FOUND",
			"That role no longer exists. Reload the page and try again.",
		],
		["INTERNAL_ERROR", "Couldn't update the role. Please try again."],
	])("explains a %s failure and lets the person retry", async (code, message) => {
		vi.mocked(setGlobalAgentRole).mockRejectedValue({
			response: { data: { error_code: code } },
		});
		renderTab();
		await userEvent.click(button("Assign role"));
		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(button("Assign role"));

		expect(await screen.findByRole("alert")).toHaveTextContent(message);
		expect(screen.getByRole("radiogroup")).toBeInTheDocument();
		expect(button("Assign role")).toBeEnabled();
	});
});

describe("AgentGlobalRoleTab — removing", () => {
	it("asks first, then clears the role through its own endpoint", async () => {
		const { client } = renderTab({ roleId: "role-admin" });

		await userEvent.click(button("Remove role"));
		expect(
			screen.getByText(/it will lose the permissions the role grants/i),
		).toBeInTheDocument();
		expect(clearGlobalAgentRole).not.toHaveBeenCalled();

		await userEvent.click(button("Remove role"));

		await waitFor(() =>
			expect(clearGlobalAgentRole).toHaveBeenCalledWith("agent-1"),
		);
		await waitFor(() =>
			expect(
				client.getQueryData<Agent>(globalAgentQueryOptions("agent-1").queryKey)
					?.global_role_id,
			).toBeNull(),
		);
	});

	it("keeps the role when the person backs out", async () => {
		renderTab({ roleId: "role-admin" });
		await userEvent.click(button("Remove role"));

		await userEvent.click(button("Cancel"));

		expect(clearGlobalAgentRole).not.toHaveBeenCalled();
		expect(screen.queryByText(/lose the permissions/i)).not.toBeInTheDocument();
		expect(button("Remove role")).toBeInTheDocument();
	});

	it("reports a failed removal and stays on the confirmation", async () => {
		vi.mocked(clearGlobalAgentRole).mockRejectedValue({
			response: { data: { error_code: "FORBIDDEN" } },
		});
		renderTab({ roleId: "role-admin" });
		await userEvent.click(button("Remove role"));
		await userEvent.click(button("Remove role"));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"You don't have permission to change global roles.",
		);
		expect(screen.getByText(/lose the permissions/i)).toBeInTheDocument();
	});
});
