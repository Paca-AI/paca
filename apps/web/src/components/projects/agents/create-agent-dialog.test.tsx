import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { makeRole, renderWithQueries } from "@/test/render-with-queries";
import { CreateAgentDialog } from "./create-agent-dialog";

// ---------------------------------------------------------------------------
// Agent type selector — global vs project scope
// ---------------------------------------------------------------------------
//
// Regression coverage: the "Provider CLI" agent type used to be omitted
// entirely from the grid when creating a global agent (no projectId), since
// it requires a project's own static environment (see
// agentdom.ErrCLIProviderNotSupportedForGlobalAgents server-side). It's now
// always shown, but disabled with a tooltip at global scope, so the option
// is discoverable rather than silently missing.

const agentApi = vi.hoisted(() => ({
	createAgent: vi.fn(),
	createGlobalAgent: vi.fn(),
	setGlobalAgentRole: vi.fn(),
	generateAcpBridgeToken: vi.fn(),
	generateAgentMCPKey: vi.fn(),
	generateGlobalAcpBridgeToken: vi.fn(),
	generateGlobalAgentMCPKey: vi.fn(),
}));

// What the project's roles query returns; a test can make it fail.
const projectRoles = vi.hoisted(() => ({
	fail: false,
	roles: [
		{
			id: "pr-owner",
			role_name: "Owner",
			permissions: { "*": true },
			created_at: "2026-01-01T00:00:00.000Z",
			updated_at: "2026-01-01T00:00:00.000Z",
		},
		{
			id: "pr-editor",
			role_name: "Editor",
			permissions: { "tasks.write": true, "docs.write": true },
			created_at: "2026-01-01T00:00:00.000Z",
			updated_at: "2026-01-01T00:00:00.000Z",
		},
		{
			id: "pr-viewer",
			role_name: "Viewer",
			permissions: { "tasks.read": true },
			created_at: "2026-01-01T00:00:00.000Z",
			updated_at: "2026-01-01T00:00:00.000Z",
		},
	],
}));

vi.mock("@/lib/agent-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/agent-api")>("@/lib/agent-api");
	return {
		...actual,
		...agentApi,
		llmModelsQueryOptions: {
			queryKey: ["agents", "llm-models"],
			queryFn: async () => ({}),
		},
	};
});

// projectRolesQueryOptions is only read (by the project role step) for the
// project-scoped test cases below, but mocking it unconditionally keeps every
// case hermetic rather than letting them hit a real fetch() that jsdom can't
// resolve.
vi.mock("@/lib/project-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/project-api")>(
			"@/lib/project-api",
		);
	return {
		...actual,
		projectRolesQueryOptions: (projectId: string) => ({
			queryKey: ["projects", projectId, "roles"],
			queryFn: async () => {
				if (projectRoles.fail) throw new Error("roles unavailable");
				return projectRoles.roles;
			},
		}),
	};
});

const ROLES = [
	makeRole("role-user", "USER", { "tasks.read": true }),
	makeRole("role-admin", "ADMIN", { "users.read": true }),
	makeRole("role-root", "SUPER_ADMIN", { "*": true }),
];
const CAN_ASSIGN = ["global_roles.assign", "global_roles.read"];

function renderDialog({
	projectId,
	permissions = [],
	onAcpAgentCreated = () => {},
	onOpenChange = () => {},
}: {
	projectId?: string;
	permissions?: string[];
	onAcpAgentCreated?: (...args: unknown[]) => void;
	onOpenChange?: (open: boolean) => void;
} = {}) {
	return renderWithQueries(
		<CreateAgentDialog
			projectId={projectId}
			open
			onOpenChange={onOpenChange}
			onAcpAgentCreated={onAcpAgentCreated}
		/>,
		{ permissions, roles: ROLES },
	);
}

describe("CreateAgentDialog — agent type selector", () => {
	it("shows Provider CLI enabled and selectable when creating a project agent", async () => {
		const user = userEvent.setup();
		renderDialog({ projectId: "proj-1" });

		const providerCliCard = screen.getByText("Provider CLI").closest("button");
		expect(providerCliCard).not.toBeNull();
		expect(providerCliCard).not.toHaveAttribute("aria-disabled", "true");

		await user.click(providerCliCard as HTMLElement);
		// Selecting it reveals the CLI provider sub-select from step 2's
		// provider_cli branch's step-1-only preset-grid absence; the clearest
		// step-1 signal that selection took effect is the preset grid (LLM-only)
		// disappearing.
		expect(screen.queryByText("Start from a preset")).not.toBeInTheDocument();
	});

	it("shows Provider CLI disabled when creating a global agent, and clicking it doesn't select it", async () => {
		const user = userEvent.setup();
		renderDialog();

		const providerCliCard = screen.getByText("Provider CLI").closest("button");
		expect(providerCliCard).not.toBeNull();
		expect(providerCliCard).toHaveAttribute("aria-disabled", "true");

		// Clicking it must not select it — the LLM preset grid (only shown for
		// the still-selected "llm" type) should remain visible.
		await user.click(providerCliCard as HTMLElement);
		expect(screen.getByText("Start from a preset")).toBeInTheDocument();
	});

	it("still shows Provider CLI enabled and selectable for a project agent alongside the other two types", () => {
		renderDialog({ projectId: "proj-1" });
		expect(screen.getByText("LLM (API key)")).toBeInTheDocument();
		expect(screen.getByText("Provider CLI")).toBeInTheDocument();
		expect(screen.getByText("ACP (local CLI)")).toBeInTheDocument();
	});
});

// ---------------------------------------------------------------------------
// Steps and roles
// ---------------------------------------------------------------------------
//
// The wizard is the same for every agent: 1 Identity, 2 AI configuration,
// 3 Role. A project agent's role (its project role) is required and part of
// the create request. A global agent's role is optional, a separate privilege
// (global_roles.assign) and a separate request, so its third step is offered
// only to someone who may assign roles. Nothing is created until the last step.

const button = (name: RegExp | string) => screen.getByRole("button", { name });

async function chooseAcpAndName(user: ReturnType<typeof userEvent.setup>) {
	const acpCard = screen.getByText("ACP (local CLI)").closest("button");
	expect(acpCard).not.toBeNull();
	await user.click(acpCard as HTMLElement);
	await user.type(screen.getByLabelText(/^Name/), "Bot");
}

/** Identity → Configuration → Role, for an ACP agent (the simplest to fill in). */
async function goToRoleStep(
	user: ReturnType<typeof userEvent.setup>,
	group = "Global role",
) {
	await chooseAcpAndName(user);
	await user.click(button(/continue/i));
	await user.click(button(/continue/i));
	await screen.findByRole("radiogroup", { name: group });
}

beforeEach(() => {
	vi.resetAllMocks();
	projectRoles.fail = false;
	agentApi.createAgent.mockResolvedValue({
		id: "agent-1",
		name: "Bot",
		handle: "bot",
		agent_type: "acp",
	});
	agentApi.generateAcpBridgeToken.mockResolvedValue({ token: "t" });
	agentApi.generateAgentMCPKey.mockResolvedValue({ token: "k" });
	agentApi.createGlobalAgent.mockResolvedValue({
		id: "agent-1",
		name: "Bot",
		handle: "bot",
		agent_type: "acp",
	});
	agentApi.setGlobalAgentRole.mockResolvedValue({});
	agentApi.generateGlobalAcpBridgeToken.mockResolvedValue({ token: "t" });
	agentApi.generateGlobalAgentMCPKey.mockResolvedValue({ token: "k" });
});

describe("CreateAgentDialog — steps by scope", () => {
	it("gives a project agent the same three steps, with no role in the first", () => {
		renderDialog({ projectId: "proj-1" });

		expect(screen.getByText("1 / 3")).toBeInTheDocument();
		expect(
			screen.getByText("Set up your agent's identity"),
		).toBeInTheDocument();
		// The project role is its own step now, not a field of the identity step.
		expect(screen.queryByText("Project Role")).not.toBeInTheDocument();
		expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
	});

	it.each([
		["no role permissions", []],
		["the global role permissions", CAN_ASSIGN],
	])("keeps a project agent at three steps with %s", (_label, permissions) => {
		renderDialog({ projectId: "proj-1", permissions });

		// The project role is required, so it is always there; the global role
		// permissions have nothing to do with it.
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
	});

	it("gives a global agent no role in step 1, and the same subtitle", () => {
		renderDialog();

		expect(screen.queryByText("Project Role")).not.toBeInTheDocument();
		expect(screen.queryByText("Global Role")).not.toBeInTheDocument();
		expect(
			screen.getByText("Set up your agent's identity"),
		).toBeInTheDocument();
		expect(
			screen.queryByText("Set up your agent's identity and role"),
		).not.toBeInTheDocument();
	});

	it.each([
		["no role permissions", []],
		["global_roles.read but not global_roles.assign", ["global_roles.read"]],
		["global_roles.assign but not global_roles.read", ["global_roles.assign"]],
	])("gives a global agent two steps, ending in Create Agent, with %s", async (_label, permissions) => {
		const user = userEvent.setup();
		renderDialog({ permissions });
		expect(screen.getByText("1 / 2")).toBeInTheDocument();

		await chooseAcpAndName(user);
		await user.click(button(/continue/i));

		expect(screen.getByText("2 / 2")).toBeInTheDocument();
		expect(button(/create agent/i)).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /continue/i }),
		).not.toBeInTheDocument();
	});

	it("gives a global agent a third step for someone who may assign roles", async () => {
		const user = userEvent.setup();
		renderDialog({ permissions: CAN_ASSIGN });
		expect(screen.getByText("1 / 3")).toBeInTheDocument();

		await chooseAcpAndName(user);
		await user.click(button(/continue/i));
		// Step 2 does not create the agent any more: it leads on to the role.
		expect(screen.getByText("2 / 3")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /create agent/i }),
		).not.toBeInTheDocument();

		await user.click(button(/continue/i));

		expect(screen.getByText("3 / 3")).toBeInTheDocument();
		expect(
			screen.getByText("Choose a global role for the agent (optional)"),
		).toBeInTheDocument();
		expect(
			screen.getByText(/at global scope.*comes from its role in that project/i),
		).toBeInTheDocument();
		// "No global role" is chosen until another is picked, and is an option
		// beside the real roles.
		expect(screen.getByRole("radio", { name: "No global role" })).toBeChecked();
		expect(screen.getAllByRole("radio")).toHaveLength(4);
		expect(button(/create agent/i)).toBeInTheDocument();
	});
});

describe("CreateAgentDialog — the project role step", () => {
	it("asks for the project role in the third step, with what each role grants", async () => {
		const user = userEvent.setup();
		renderDialog({ projectId: "proj-1" });
		await goToRoleStep(user, "Project Role");

		expect(screen.getByText("3 / 3")).toBeInTheDocument();
		expect(
			screen.getByText("Choose the agent's role in this project"),
		).toBeInTheDocument();
		expect(
			screen.getByText(
				"Controls what the agent can read and modify in this project.",
			),
		).toBeInTheDocument();
		for (const name of ["Owner", "Editor", "Viewer"]) {
			expect(screen.getByRole("radio", { name })).toBeInTheDocument();
		}
		expect(screen.getByText("Full access")).toBeInTheDocument();
		expect(screen.getByText("tasks.write")).toBeInTheDocument();
		// A project agent always has a role, so there is no "no role" choice.
		expect(screen.queryByText("No global role")).not.toBeInTheDocument();
		expect(screen.getAllByRole("radio")).toHaveLength(3);
	});

	it("needs a role to be chosen before the agent can be created", async () => {
		const user = userEvent.setup();
		renderDialog({ projectId: "proj-1" });
		await goToRoleStep(user, "Project Role");

		// Nothing is preselected: the role is a permission decision.
		for (const radio of screen.getAllByRole("radio")) {
			expect(radio).not.toBeChecked();
		}
		expect(button(/create agent/i)).toBeDisabled();

		await user.click(screen.getByRole("radio", { name: "Editor" }));

		expect(button(/create agent/i)).toBeEnabled();
	});

	it("creates the agent with the chosen project role in one request", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		renderDialog({ projectId: "proj-1", onAcpAgentCreated });
		await goToRoleStep(user, "Project Role");
		await user.click(screen.getByRole("radio", { name: "Editor" }));

		await user.click(button(/create agent/i));

		await waitFor(() => expect(onAcpAgentCreated).toHaveBeenCalled());
		expect(agentApi.createAgent).toHaveBeenCalledTimes(1);
		expect(agentApi.createAgent).toHaveBeenCalledWith(
			"proj-1",
			expect.objectContaining({
				name: "Bot",
				handle: "bot",
				agent_type: "acp",
				project_role_id: "pr-editor",
			}),
		);
		// Nothing about a global role, and no second request.
		expect(agentApi.createGlobalAgent).not.toHaveBeenCalled();
		expect(agentApi.setGlobalAgentRole).not.toHaveBeenCalled();
	});

	it("goes back to the earlier steps and keeps the chosen role", async () => {
		const user = userEvent.setup();
		renderDialog({ projectId: "proj-1" });
		await goToRoleStep(user, "Project Role");
		await user.click(screen.getByRole("radio", { name: "Viewer" }));

		await user.click(button(/back/i));
		expect(screen.getByText("2 / 3")).toBeInTheDocument();
		await user.click(button(/back/i));
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
		expect(screen.getByLabelText(/^Name/)).toHaveValue("Bot");
		await user.click(button(/continue/i));
		await user.click(button(/continue/i));

		expect(await screen.findByRole("radio", { name: "Viewer" })).toBeChecked();
		expect(agentApi.createAgent).not.toHaveBeenCalled();
	});

	it("says so when the roles cannot be loaded, and creates nothing", async () => {
		const user = userEvent.setup();
		projectRoles.fail = true;
		renderDialog({ projectId: "proj-1" });

		await chooseAcpAndName(user);
		await user.click(button(/continue/i));
		await user.click(button(/continue/i));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Couldn't load the roles.",
		);
		expect(button(/create agent/i)).toBeDisabled();
		expect(agentApi.createAgent).not.toHaveBeenCalled();
	});

	it("shows a failed creation on this step and lets the person try again", async () => {
		const user = userEvent.setup();
		agentApi.createAgent.mockRejectedValueOnce(new Error("handle taken"));
		renderDialog({ projectId: "proj-1" });
		await goToRoleStep(user, "Project Role");
		await user.click(screen.getByRole("radio", { name: "Editor" }));

		await user.click(button(/create agent/i));

		expect(
			await screen.findByText("Failed to create agent. Please try again."),
		).toBeInTheDocument();
		expect(button(/back/i)).not.toHaveClass("invisible");
		expect(button(/create agent/i)).toBeEnabled();
	});
});

describe("CreateAgentDialog — the global role step", () => {
	it("creates the agent without a role and never binds one when 'No global role' is kept", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		renderDialog({ permissions: CAN_ASSIGN, onAcpAgentCreated });
		await goToRoleStep(user);

		await user.click(button(/create agent/i));

		await waitFor(() => expect(onAcpAgentCreated).toHaveBeenCalled());
		expect(agentApi.createGlobalAgent).toHaveBeenCalledTimes(1);
		const payload = agentApi.createGlobalAgent.mock.calls[0][0];
		expect(payload).toMatchObject({ name: "Bot", handle: "bot" });
		expect(payload).not.toHaveProperty("global_role_id");
		expect(agentApi.setGlobalAgentRole).not.toHaveBeenCalled();
	});

	it("creates the agent and then binds the picked role through its own request", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		renderDialog({ permissions: CAN_ASSIGN, onAcpAgentCreated });
		await goToRoleStep(user);

		await user.click(screen.getByRole("radio", { name: "ADMIN" }));
		await user.click(button(/create agent/i));

		await waitFor(() => expect(onAcpAgentCreated).toHaveBeenCalled());
		expect(agentApi.createGlobalAgent).toHaveBeenCalledTimes(1);
		expect(agentApi.createGlobalAgent.mock.calls[0][0]).not.toHaveProperty(
			"global_role_id",
		);
		expect(agentApi.setGlobalAgentRole).toHaveBeenCalledWith(
			"agent-1",
			"role-admin",
		);
		// Created first, role second: the role needs the agent's id.
		expect(agentApi.createGlobalAgent.mock.invocationCallOrder[0]).toBeLessThan(
			agentApi.setGlobalAgentRole.mock.invocationCallOrder[0],
		);
	});

	it("lets the person go back to a role-less choice after picking one", async () => {
		const user = userEvent.setup();
		renderDialog({ permissions: CAN_ASSIGN });
		await goToRoleStep(user);
		await user.click(screen.getByRole("radio", { name: "ADMIN" }));

		await user.click(screen.getByRole("radio", { name: "No global role" }));

		expect(screen.getByRole("radio", { name: "No global role" })).toBeChecked();
		expect(screen.getByRole("radio", { name: "ADMIN" })).not.toBeChecked();
		await user.click(button(/create agent/i));
		await waitFor(() =>
			expect(agentApi.createGlobalAgent).toHaveBeenCalledTimes(1),
		);
		expect(agentApi.setGlobalAgentRole).not.toHaveBeenCalled();
	});

	it("warns before giving an agent a full-access role", async () => {
		const user = userEvent.setup();
		renderDialog({ permissions: CAN_ASSIGN });
		await goToRoleStep(user);
		expect(
			screen.queryByText(/the agent will be able to do everything/i),
		).toBeNull();

		await user.click(screen.getByRole("radio", { name: "SUPER_ADMIN" }));

		expect(
			screen.getByText(/the agent will be able to do everything/i),
		).toBeInTheDocument();
	});

	it("creates nothing while the person is only choosing, and Back returns through the steps", async () => {
		const user = userEvent.setup();
		renderDialog({ permissions: CAN_ASSIGN });
		await goToRoleStep(user);
		await user.click(screen.getByRole("radio", { name: "ADMIN" }));

		await user.click(button(/back/i));
		expect(screen.getByText("2 / 3")).toBeInTheDocument();
		await user.click(button(/back/i));
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
		expect(screen.getByLabelText(/^Name/)).toHaveValue("Bot");

		expect(agentApi.createGlobalAgent).not.toHaveBeenCalled();
		expect(agentApi.setGlobalAgentRole).not.toHaveBeenCalled();
	});

	it("keeps the role picked when the person steps back and forward again", async () => {
		const user = userEvent.setup();
		renderDialog({ permissions: CAN_ASSIGN });
		await goToRoleStep(user);
		await user.click(screen.getByRole("radio", { name: "ADMIN" }));

		await user.click(button(/back/i));
		await user.click(button(/continue/i));

		expect(await screen.findByRole("radio", { name: "ADMIN" })).toBeChecked();
	});

	it("shows a failed creation on the role step, where the button was, and allows going back", async () => {
		const user = userEvent.setup();
		agentApi.createGlobalAgent.mockRejectedValue(new Error("handle taken"));
		renderDialog({ permissions: CAN_ASSIGN });
		await goToRoleStep(user);
		await user.click(screen.getByRole("radio", { name: "ADMIN" }));

		await user.click(button(/create agent/i));

		expect(
			await screen.findByText("Failed to create agent. Please try again."),
		).toBeInTheDocument();
		expect(agentApi.setGlobalAgentRole).not.toHaveBeenCalled();
		// Nothing exists yet, so the earlier steps can still be fixed.
		expect(button(/back/i)).not.toHaveClass("invisible");
		expect(button(/create agent/i)).toBeEnabled();
	});
});

// If binding the role fails the agent already exists. The dialog must not
// create it a second time on retry, and must not trap the person either.
describe("CreateAgentDialog — when the role cannot be bound", () => {
	async function createWithFailingRole(
		user: ReturnType<typeof userEvent.setup>,
		props: Parameters<typeof renderDialog>[0] = {},
	) {
		agentApi.setGlobalAgentRole.mockRejectedValueOnce(new Error("forbidden"));
		const view = renderDialog({ permissions: CAN_ASSIGN, ...props });
		await goToRoleStep(user);
		await user.click(screen.getByRole("radio", { name: "ADMIN" }));
		await user.click(button(/create agent/i));
		await screen.findByText(/the agent was created, but its role couldn't/i);
		return view;
	}

	it("says the agent was created, offers Finish, and locks the earlier steps", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		await createWithFailingRole(user, { onAcpAgentCreated });

		expect(
			screen.getByText(/choose “No global role” to finish/i),
		).toBeInTheDocument();
		expect(button(/finish/i)).toBeEnabled();
		expect(
			screen.queryByRole("button", { name: /create agent/i }),
		).not.toBeInTheDocument();
		// Editing the name or type now would change nothing, so Back is gone.
		expect(button(/back/i)).toHaveClass("invisible");
		// Not finished yet: the ACP setup dialog has not been opened.
		expect(onAcpAgentCreated).not.toHaveBeenCalled();
	});

	it("retries only the role, never creating the agent again", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		await createWithFailingRole(user, { onAcpAgentCreated });

		await user.click(button(/finish/i));

		await waitFor(() => expect(onAcpAgentCreated).toHaveBeenCalled());
		expect(agentApi.createGlobalAgent).toHaveBeenCalledTimes(1);
		expect(agentApi.setGlobalAgentRole).toHaveBeenCalledTimes(2);
		expect(agentApi.setGlobalAgentRole).toHaveBeenLastCalledWith(
			"agent-1",
			"role-admin",
		);
	});

	it("can finish without a role instead, and still opens the ACP setup", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		await createWithFailingRole(user, { onAcpAgentCreated });

		await user.click(screen.getByRole("radio", { name: "No global role" }));
		await user.click(button(/finish/i));

		await waitFor(() => expect(onAcpAgentCreated).toHaveBeenCalled());
		expect(agentApi.createGlobalAgent).toHaveBeenCalledTimes(1);
		expect(agentApi.setGlobalAgentRole).toHaveBeenCalledTimes(1);
	});

	it("refreshes the agent lists if the person closes the dialog instead", async () => {
		const user = userEvent.setup();
		const onOpenChange = vi.fn();
		const { client } = await createWithFailingRole(user, { onOpenChange });
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await user.click(screen.getByRole("button", { name: "Close" }));

		// The agent exists, so the list behind the dialog has to show it.
		expect(onOpenChange).toHaveBeenCalledWith(false);
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ["global-agents"] });
	});
});
