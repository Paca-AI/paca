import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/role-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/role-api")>("@/lib/role-api");
	return { ...actual, replaceAgentRoles: vi.fn() };
});

import { type Agent, globalAgentQueryOptions } from "@/lib/agent-api";
import { replaceAgentRoles } from "@/lib/role-api";
import { makeRole, renderWithQueries } from "@/test/render-with-queries";
import { roleOption, toggleRole } from "@/test/role-select";
import { AgentGlobalRoleTab } from "./agent-global-role-tab";

const ROLES = [
	makeRole("role-user", "USER", { "tasks:read": true }),
	makeRole("role-admin", "ADMIN", { "users:read": true }),
	makeRole("role-root", "SUPER_ADMIN", { "*": true }),
];
const CAN_ASSIGN = ["roles:assign", "roles:read"];

const agentWith = (roleIds: string[]): Agent =>
	({
		id: "agent-1",
		name: "Bot",
		handle: "bot",
		roles: ROLES.filter((r) => roleIds.includes(r.id)).map((r) => ({
			id: r.id,
			name: r.name,
		})),
	}) as Agent;

function renderTab({
	roleId = null as string | null,
	canWrite = true,
	permissions = CAN_ASSIGN,
	roles = ROLES,
}: {
	roleId?: string | null;
	canWrite?: boolean;
	permissions?: string[];
	roles?: typeof ROLES | null;
} = {}) {
	const agent = agentWith(roleId ? [roleId] : []);
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
	vi.mocked(replaceAgentRoles).mockImplementation(async (_id, ids) =>
		ROLES.filter((r) => ids.includes(r.id)),
	);
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
		expect(screen.getByText("users:read")).toBeInTheDocument();
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

	it("is read-only without roles:assign, even with agents.write", () => {
		renderTab({ roleId: "role-admin", permissions: ["roles:read"] });

		expect(screen.getByText("ADMIN")).toBeInTheDocument();
		expect(
			screen.getByText(/don't have permission to change it/i),
		).toBeInTheDocument();
		noButton("Change role");
		noButton("Remove role");
	});

	it("offers no controls, and says the name is hidden, without roles:read", () => {
		renderTab({
			roleId: "role-admin",
			permissions: ["roles:assign"],
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
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await userEvent.click(button("Assign role"));
		// Nothing chosen yet: nothing to assign.
		expect(button("Assign role")).toBeDisabled();
		await toggleRole("ADMIN");
		await userEvent.click(button("Assign role"));

		await waitFor(() =>
			expect(replaceAgentRoles).toHaveBeenCalledWith("agent-1", ["role-admin"]),
		);
		// The agent is refetched so it shows the new role, and the picker closes.
		await waitFor(() =>
			expect(invalidate).toHaveBeenCalledWith({
				queryKey: globalAgentQueryOptions("agent-1").queryKey,
			}),
		);
		expect(
			screen.queryByRole("combobox", { name: "Global role" }),
		).not.toBeInTheDocument();
	});

	it("changes the set of roles, marking the ones held", async () => {
		renderTab({ roleId: "role-user" });

		await userEvent.click(button("Change role"));

		const current = await roleOption("USER");
		expect(current).toHaveAttribute("aria-selected", "true");
		expect(current).toHaveAccessibleDescription(/Current/);
		expect(button("Assign role")).toBeDisabled();

		await toggleRole("ADMIN");
		await userEvent.click(button("Assign role"));

		await waitFor(() =>
			expect(replaceAgentRoles).toHaveBeenCalledWith("agent-1", [
				"role-user",
				"role-admin",
			]),
		);
	});

	it("warns before giving an agent a full-access role", async () => {
		renderTab();
		await userEvent.click(button("Assign role"));

		await toggleRole("SUPER_ADMIN");

		expect(
			screen.getByText(/the agent will be able to do everything/i),
		).toBeInTheDocument();
	});

	it("leaves the picker without changing anything on Cancel", async () => {
		renderTab({ roleId: "role-user" });
		await userEvent.click(button("Change role"));
		await toggleRole("ADMIN");

		await userEvent.click(button("Cancel"));

		expect(
			screen.queryByRole("combobox", { name: "Global role" }),
		).not.toBeInTheDocument();
		expect(replaceAgentRoles).not.toHaveBeenCalled();
		expect(button("Change role")).toBeInTheDocument();
	});

	it("disables the choices and the button while the request is in flight", async () => {
		vi.mocked(replaceAgentRoles).mockReturnValue(new Promise(() => {}));
		renderTab();
		await userEvent.click(button("Assign role"));
		await toggleRole("ADMIN");

		await userEvent.click(button("Assign role"));

		expect(
			await screen.findByRole("button", { name: /assigning/i }),
		).toBeDisabled();
		expect(
			screen.getByRole("combobox", { name: "Global role" }),
		).toBeDisabled();
	});

	it.each([
		["FORBIDDEN", "You don't have permission to do this."],
		["ROLE_NOT_ATTACHABLE", "One of the roles can't be assigned here."],
		["INTERNAL_ERROR", "Something went wrong on the server. Try again."],
	])("explains a %s failure and lets the person retry", async (code, message) => {
		vi.mocked(replaceAgentRoles).mockRejectedValue({
			response: { data: { error_code: code } },
		});
		renderTab();
		await userEvent.click(button("Assign role"));
		await toggleRole("ADMIN");
		await userEvent.click(button("Assign role"));

		expect(await screen.findByRole("alert")).toHaveTextContent(message);
		expect(
			screen.getByRole("combobox", { name: "Global role" }),
		).toBeInTheDocument();
		expect(button("Assign role")).toBeEnabled();
	});
});

describe("AgentGlobalRoleTab — removing", () => {
	it("asks first, then clears the role through its own endpoint", async () => {
		const { client } = renderTab({ roleId: "role-admin" });
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await userEvent.click(button("Remove role"));
		expect(
			screen.getByText(/it will lose the permissions the role grants/i),
		).toBeInTheDocument();
		expect(replaceAgentRoles).not.toHaveBeenCalled();

		await userEvent.click(button("Remove role"));

		await waitFor(() =>
			expect(replaceAgentRoles).toHaveBeenCalledWith("agent-1", []),
		);
		await waitFor(() =>
			expect(invalidate).toHaveBeenCalledWith({
				queryKey: globalAgentQueryOptions("agent-1").queryKey,
			}),
		);
	});

	it("keeps the role when the person backs out", async () => {
		renderTab({ roleId: "role-admin" });
		await userEvent.click(button("Remove role"));

		await userEvent.click(button("Cancel"));

		expect(replaceAgentRoles).not.toHaveBeenCalled();
		expect(screen.queryByText(/lose the permissions/i)).not.toBeInTheDocument();
		expect(button("Remove role")).toBeInTheDocument();
	});

	it("reports a failed removal and stays on the confirmation", async () => {
		vi.mocked(replaceAgentRoles).mockRejectedValue({
			response: { data: { error_code: "FORBIDDEN" } },
		});
		renderTab({ roleId: "role-admin" });
		await userEvent.click(button("Remove role"));
		await userEvent.click(button("Remove role"));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"You don't have permission to do this.",
		);
		expect(screen.getByText(/lose the permissions/i)).toBeInTheDocument();
	});
});
