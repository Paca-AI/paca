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
	clearGlobalAgentRole: vi.fn(),
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
	makeRole("role-user", "USER", { "tasks.read": true }, { isDefault: true }),
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
// the create request, so it is created by step 3's button. A global agent is
// created by step 2's button and starts with the default global role, which
// the server assigns; changing it is a separate privilege (global_roles.assign)
// and a separate request, made in step 3, which is offered only to someone who
// may assign roles.

const button = (name: RegExp | string) => screen.getByRole("button", { name });

async function chooseAcpAndName(user: ReturnType<typeof userEvent.setup>) {
	const acpCard = screen.getByText("ACP (local CLI)").closest("button");
	expect(acpCard).not.toBeNull();
	await user.click(acpCard as HTMLElement);
	await user.type(screen.getByLabelText(/^Name/), "Bot");
}

/** Identity → Configuration → Role, for an ACP agent (the simplest to fill in).
 * For a project agent, whose role is part of the create request. */
async function goToRoleStep(
	user: ReturnType<typeof userEvent.setup>,
	group = "Global role",
) {
	await chooseAcpAndName(user);
	await user.click(button(/continue/i));
	await user.click(button(/continue/i));
	await screen.findByRole("radiogroup", { name: group });
}

/** Identity → Configuration → Create Agent → Role, for a global agent whose
 * creator may assign roles: the agent exists by the time the role step shows. */
async function createGlobalAgentAndGoToRoleStep(
	user: ReturnType<typeof userEvent.setup>,
) {
	await chooseAcpAndName(user);
	await user.click(button(/continue/i));
	await user.click(button(/create agent/i));
	await screen.findByRole("radiogroup", { name: "Global role" });
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
	// The server gives a new global agent the default role.
	agentApi.createGlobalAgent.mockResolvedValue({
		id: "agent-1",
		name: "Bot",
		handle: "bot",
		agent_type: "acp",
		global_role_id: "role-user",
	});
	agentApi.setGlobalAgentRole.mockResolvedValue({});
	agentApi.clearGlobalAgentRole.mockResolvedValue({});
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

	it("gives a global agent a third step for someone who may assign roles, after creating the agent", async () => {
		const user = userEvent.setup();
		renderDialog({ permissions: CAN_ASSIGN });
		expect(screen.getByText("1 / 3")).toBeInTheDocument();

		await chooseAcpAndName(user);
		await user.click(button(/continue/i));
		// The agent is created here, before the role step: changing its role is a
		// request of its own.
		expect(screen.getByText("2 / 3")).toBeInTheDocument();
		expect(button(/create agent/i)).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /continue/i }),
		).not.toBeInTheDocument();
		expect(agentApi.createGlobalAgent).not.toHaveBeenCalled();

		await user.click(button(/create agent/i));

		expect(await screen.findByText("3 / 3")).toBeInTheDocument();
		expect(agentApi.createGlobalAgent).toHaveBeenCalledTimes(1);
		expect(
			screen.getByText(
				"Bot was created with the default global role. Keep it, choose another, or remove it.",
			),
		).toBeInTheDocument();
		expect(
			screen.getByText(/at global scope.*comes from its role in that project/i),
		).toBeInTheDocument();
		// The role it was created with is chosen, and marked as the current and
		// the default one; "No global role" is an option beside the real roles.
		const created = screen.getByRole("radio", { name: "USER" });
		expect(created).toBeChecked();
		expect(created).toHaveAccessibleDescription(/Current/);
		expect(created).toHaveAccessibleDescription(/Default/);
		expect(
			screen.getByRole("radio", { name: "No global role" }),
		).not.toBeChecked();
		expect(screen.getAllByRole("radio")).toHaveLength(4);
		// Nothing is different from what the agent has, so there is nothing to apply.
		expect(button(/finish/i)).toBeInTheDocument();
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
	async function reachRoleStep(
		user: ReturnType<typeof userEvent.setup>,
		props: Parameters<typeof renderDialog>[0] = {},
	) {
		const view = renderDialog({ permissions: CAN_ASSIGN, ...props });
		await createGlobalAgentAndGoToRoleStep(user);
		return view;
	}

	it("creates the agent without a role, and keeping the one it got makes no role request", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		const onOpenChange = vi.fn();
		await reachRoleStep(user, { onAcpAgentCreated, onOpenChange });

		// The create request asks for no role: the server picks the default one.
		const payload = agentApi.createGlobalAgent.mock.calls[0][0];
		expect(payload).toMatchObject({ name: "Bot", handle: "bot" });
		expect(payload).not.toHaveProperty("global_role_id");
		// Not finished yet: the ACP setup only opens once the wizard is done.
		expect(onAcpAgentCreated).not.toHaveBeenCalled();

		await user.click(button(/finish/i));

		expect(onOpenChange).toHaveBeenCalledWith(false);
		expect(onAcpAgentCreated).toHaveBeenCalledTimes(1);
		expect(onAcpAgentCreated).toHaveBeenCalledWith(
			expect.objectContaining({ id: "agent-1" }),
			{ token: "t" },
			"k",
		);
		expect(agentApi.setGlobalAgentRole).not.toHaveBeenCalled();
		expect(agentApi.clearGlobalAgentRole).not.toHaveBeenCalled();
	});

	it("assigns the picked role through its own request, once the agent exists, and then finishes", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		await reachRoleStep(user, { onAcpAgentCreated });

		await user.click(screen.getByRole("radio", { name: "ADMIN" }));
		await user.click(button(/assign role/i));

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
		expect(agentApi.clearGlobalAgentRole).not.toHaveBeenCalled();
	});

	it("removes the role through its own request when 'No global role' is picked", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		await reachRoleStep(user, { onAcpAgentCreated });

		await user.click(screen.getByRole("radio", { name: "No global role" }));
		await user.click(button(/remove role/i));

		await waitFor(() => expect(onAcpAgentCreated).toHaveBeenCalled());
		expect(agentApi.clearGlobalAgentRole).toHaveBeenCalledWith("agent-1");
		expect(agentApi.setGlobalAgentRole).not.toHaveBeenCalled();
		expect(agentApi.createGlobalAgent).toHaveBeenCalledTimes(1);
	});

	it("labels the button for what it will do, and goes back to Finish when the current role is picked again", async () => {
		const user = userEvent.setup();
		await reachRoleStep(user);
		expect(button(/finish/i)).toBeInTheDocument();

		await user.click(screen.getByRole("radio", { name: "ADMIN" }));
		expect(button(/assign role/i)).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: /finish/i }),
		).not.toBeInTheDocument();

		await user.click(screen.getByRole("radio", { name: "No global role" }));
		expect(button(/remove role/i)).toBeInTheDocument();

		await user.click(screen.getByRole("radio", { name: "USER" }));
		expect(button(/finish/i)).toBeInTheDocument();
		expect(agentApi.setGlobalAgentRole).not.toHaveBeenCalled();
		expect(agentApi.clearGlobalAgentRole).not.toHaveBeenCalled();
	});

	it("warns before giving an agent a full-access role", async () => {
		const user = userEvent.setup();
		await reachRoleStep(user);
		expect(
			screen.queryByText(/the agent will be able to do everything/i),
		).toBeNull();

		await user.click(screen.getByRole("radio", { name: "SUPER_ADMIN" }));
		expect(
			screen.getByText(/the agent will be able to do everything/i),
		).toBeInTheDocument();

		await user.click(screen.getByRole("radio", { name: "USER" }));
		expect(
			screen.queryByText(/the agent will be able to do everything/i),
		).toBeNull();
	});

	it("locks the earlier steps once the agent exists", async () => {
		const user = userEvent.setup();
		await reachRoleStep(user);

		// Editing the name or type now would change nothing, so Back is gone.
		expect(button(/back/i)).toHaveClass("invisible");
		expect(
			screen.queryByRole("button", { name: /create agent/i }),
		).not.toBeInTheDocument();
	});

	it("shows a failed creation on step 2, where its button is, and allows going back", async () => {
		const user = userEvent.setup();
		agentApi.createGlobalAgent.mockRejectedValue(new Error("handle taken"));
		renderDialog({ permissions: CAN_ASSIGN });
		await chooseAcpAndName(user);
		await user.click(button(/continue/i));

		await user.click(button(/create agent/i));

		expect(
			await screen.findByText("Failed to create agent. Please try again."),
		).toBeInTheDocument();
		expect(screen.getByText("2 / 3")).toBeInTheDocument();
		expect(agentApi.setGlobalAgentRole).not.toHaveBeenCalled();
		// Nothing exists yet, so the earlier steps can still be fixed.
		expect(button(/back/i)).not.toHaveClass("invisible");
		expect(button(/create agent/i)).toBeEnabled();
	});

	it("says so when there is no default role to give the agent", async () => {
		const user = userEvent.setup();
		agentApi.createGlobalAgent.mockRejectedValue(
			Object.assign(new Error("conflict"), {
				response: { data: { error_code: "GLOBAL_ROLE_NO_DEFAULT" } },
			}),
		);
		renderDialog({ permissions: CAN_ASSIGN });
		await chooseAcpAndName(user);
		await user.click(button(/continue/i));

		await user.click(button(/create agent/i));

		expect(
			await screen.findByText(
				/there is no default role to give the new agent/i,
			),
		).toBeInTheDocument();
	});

	it("does not close while the agent is being created", async () => {
		const user = userEvent.setup();
		agentApi.createGlobalAgent.mockReturnValue(new Promise(() => {}));
		const onOpenChange = vi.fn();
		renderDialog({ permissions: CAN_ASSIGN, onOpenChange });
		await chooseAcpAndName(user);
		await user.click(button(/continue/i));
		await user.click(button(/create agent/i));
		await screen.findByText("Creating…");

		await user.click(screen.getByRole("button", { name: "Close" }));

		expect(onOpenChange).not.toHaveBeenCalled();
	});

	it("finishes when the person closes the dialog on the role step: the agent keeps its role, the lists refresh, the ACP setup opens", async () => {
		const user = userEvent.setup();
		const onOpenChange = vi.fn();
		const onAcpAgentCreated = vi.fn();
		const { client } = await reachRoleStep(user, {
			onOpenChange,
			onAcpAgentCreated,
		});
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await user.click(screen.getByRole("button", { name: "Close" }));

		expect(onOpenChange).toHaveBeenCalledWith(false);
		expect(onAcpAgentCreated).toHaveBeenCalledTimes(1);
		// The agent exists, so the list behind the dialog has to show it.
		expect(invalidate).toHaveBeenCalledWith({ queryKey: ["global-agents"] });
		expect(agentApi.setGlobalAgentRole).not.toHaveBeenCalled();
		expect(agentApi.clearGlobalAgentRole).not.toHaveBeenCalled();
	});
});

// The agent exists before its role is changed, so a failed change must neither
// create it a second time on retry nor trap the person.
describe("CreateAgentDialog — when the role cannot be changed", () => {
	async function failToAssign(
		user: ReturnType<typeof userEvent.setup>,
		props: Parameters<typeof renderDialog>[0] = {},
	) {
		agentApi.setGlobalAgentRole.mockRejectedValueOnce(new Error("boom"));
		const view = renderDialog({ permissions: CAN_ASSIGN, ...props });
		await createGlobalAgentAndGoToRoleStep(user);
		await user.click(screen.getByRole("radio", { name: "ADMIN" }));
		await user.click(button(/assign role/i));
		await screen.findByText("Couldn't update the role. Please try again.");
		return view;
	}

	it("says so, keeps the wizard on the role step, and offers to try again", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		await failToAssign(user, { onAcpAgentCreated });

		expect(screen.getByText("3 / 3")).toBeInTheDocument();
		expect(button(/assign role/i)).toBeEnabled();
		expect(button(/back/i)).toHaveClass("invisible");
		// Not finished yet: the ACP setup dialog has not been opened.
		expect(onAcpAgentCreated).not.toHaveBeenCalled();
	});

	it("retries only the role, never creating the agent again", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		await failToAssign(user, { onAcpAgentCreated });

		await user.click(button(/assign role/i));

		await waitFor(() => expect(onAcpAgentCreated).toHaveBeenCalled());
		expect(agentApi.createGlobalAgent).toHaveBeenCalledTimes(1);
		expect(agentApi.setGlobalAgentRole).toHaveBeenCalledTimes(2);
		expect(agentApi.setGlobalAgentRole).toHaveBeenLastCalledWith(
			"agent-1",
			"role-admin",
		);
	});

	it("can finish with the role the agent has instead, and still opens the ACP setup", async () => {
		const user = userEvent.setup();
		const onAcpAgentCreated = vi.fn();
		await failToAssign(user, { onAcpAgentCreated });

		await user.click(screen.getByRole("radio", { name: "USER" }));
		expect(
			screen.queryByText("Couldn't update the role. Please try again."),
		).not.toBeInTheDocument();
		await user.click(button(/finish/i));

		await waitFor(() => expect(onAcpAgentCreated).toHaveBeenCalled());
		expect(agentApi.createGlobalAgent).toHaveBeenCalledTimes(1);
		expect(agentApi.setGlobalAgentRole).toHaveBeenCalledTimes(1);
	});

	it("explains a refusal", async () => {
		const user = userEvent.setup();
		agentApi.setGlobalAgentRole.mockRejectedValueOnce(
			Object.assign(new Error("forbidden"), {
				response: { data: { error_code: "FORBIDDEN" } },
			}),
		);
		renderDialog({ permissions: CAN_ASSIGN });
		await createGlobalAgentAndGoToRoleStep(user);
		await user.click(screen.getByRole("radio", { name: "ADMIN" }));

		await user.click(button(/assign role/i));

		expect(
			await screen.findByText(
				"You don't have permission to change global roles.",
			),
		).toBeInTheDocument();
	});
});
