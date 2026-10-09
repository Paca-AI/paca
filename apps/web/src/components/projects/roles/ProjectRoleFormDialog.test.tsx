import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockCreateRole, mockUpdateRole, mockValidate } = vi.hoisted(() => ({
	mockCreateRole: vi.fn(),
	mockUpdateRole: vi.fn(),
	mockValidate: vi.fn(),
}));

vi.mock("@/lib/role-api", () => ({
	createRole: mockCreateRole,
	updateRole: mockUpdateRole,
	validatePolicy: mockValidate,
	simulatePolicy: vi.fn(),
	knownActionsQueryOptions: () => ({
		queryKey: ["roles", "actions"],
		queryFn: async () => [],
	}),
	attributeSchemaQueryOptions: () => ({
		queryKey: ["roles", "attribute-schema"],
		queryFn: async () => [],
	}),
	projectRolesQueryOptions: (projectId: string) => ({
		queryKey: ["projects", projectId, "roles"],
	}),
}));

import { actionsToPolicy, type Policy } from "@/lib/policy";
import type { Role } from "@/lib/role-api";
import { ProjectRoleFormDialog } from "./ProjectRoleFormDialog";

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeQueryClient() {
	return new QueryClient({
		defaultOptions: {
			mutations: { retry: false },
			queries: { retry: false, gcTime: 0 },
		},
	});
}

function Wrapper({ children }: { children: ReactNode }) {
	return (
		<QueryClientProvider client={makeQueryClient()}>
			{children}
		</QueryClientProvider>
	);
}

const existingRole: Role = {
	id: "r1",
	project_id: "p1",
	name: "DEVELOPER",
	description: "keeps tasks moving",
	policy: actionsToPolicy(["tasks:*"], "project", "p1"),
	is_system: false,
	is_default: false,
	attachment_count: 1,
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
};

function renderCreate(onOpenChange = vi.fn()) {
	render(
		<Wrapper>
			<ProjectRoleFormDialog projectId="p1" open onOpenChange={onOpenChange} />
		</Wrapper>,
	);
	return { onOpenChange };
}

function renderEdit(role: Role = existingRole, onOpenChange = vi.fn()) {
	render(
		<Wrapper>
			<ProjectRoleFormDialog
				projectId="p1"
				open
				role={role}
				onOpenChange={onOpenChange}
			/>
		</Wrapper>,
	);
	return { onOpenChange };
}

const submit = (name: RegExp) => screen.getByRole("button", { name });
const policySent = (mock: typeof mockCreateRole, callIndex = 0): Policy =>
	(
		mock.mock.calls[callIndex][
			callIndex === 0 && mock === mockUpdateRole ? 1 : 0
		] as {
			policy: Policy;
		}
	).policy;

// ── Tests ─────────────────────────────────────────────────────────────────────

describe("ProjectRoleFormDialog", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mockValidate.mockResolvedValue({ valid: true, issues: [] });
	});

	// ── Create mode ────────────────────────────────────────────────────────────

	describe("create mode", () => {
		it("shows 'New Role' as the title", () => {
			renderCreate();
			expect(screen.getByText("New Role")).toBeInTheDocument();
		});

		it("renders an empty role name input", () => {
			renderCreate();
			expect(screen.getByLabelText(/role name/i)).toHaveValue("");
		});

		it("asks for a name when submitted without one, and sends nothing", async () => {
			renderCreate();
			await userEvent.click(submit(/create role/i));

			expect(
				await screen.findByText(/role name of up to 100/i),
			).toBeInTheDocument();
			expect(mockCreateRole).not.toHaveBeenCalled();
		});

		it("creates the role inside the project with the policy document", async () => {
			mockCreateRole.mockResolvedValue(existingRole);
			const { onOpenChange } = renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "REVIEWER");
			await userEvent.click(submit(/create role/i));

			await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
			expect(mockCreateRole).toHaveBeenCalledWith(
				expect.objectContaining({ name: "REVIEWER" }),
				"p1",
			);
			expect(mockUpdateRole).not.toHaveBeenCalled();
		});
	});

	// ── Edit mode ──────────────────────────────────────────────────────────────

	describe("edit mode", () => {
		it("shows 'Edit Role' and pre-fills the name", () => {
			renderEdit();
			expect(screen.getByText("Edit Role")).toBeInTheDocument();
			expect(screen.getByLabelText(/role name/i)).toHaveValue("DEVELOPER");
			expect(submit(/save changes/i)).toBeInTheDocument();
		});

		it("replaces the role by id inside the project, keeping the description", async () => {
			mockUpdateRole.mockResolvedValue(existingRole);
			renderEdit();

			const input = screen.getByLabelText(/role name/i);
			await userEvent.clear(input);
			await userEvent.type(input, "LEAD");
			await userEvent.click(submit(/save changes/i));

			await waitFor(() =>
				expect(mockUpdateRole).toHaveBeenCalledWith(
					"r1",
					expect.objectContaining({
						name: "LEAD",
						description: "keeps tasks moving",
					}),
					"p1",
				),
			);
			expect(mockCreateRole).not.toHaveBeenCalled();
		});

		it("closes after a successful update", async () => {
			mockUpdateRole.mockResolvedValue(existingRole);
			const { onOpenChange } = renderEdit();

			await userEvent.click(submit(/save changes/i));

			await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
		});
	});

	// ── Description ────────────────────────────────────────────────────────────

	describe("description", () => {
		it("starts empty on create and is sent trimmed", async () => {
			mockCreateRole.mockResolvedValue(existingRole);
			renderCreate();
			const box = screen.getByLabelText(/^description/i);
			expect(box).toHaveValue("");

			await userEvent.type(screen.getByLabelText(/role name/i), "REVIEWER");
			await userEvent.type(box, "  reads everything  ");
			await userEvent.click(submit(/create role/i));

			await waitFor(() =>
				expect(mockCreateRole).toHaveBeenCalledWith(
					expect.objectContaining({
						name: "REVIEWER",
						description: "reads everything",
					}),
					"p1",
				),
			);
		});

		it("is pre-filled on edit and can be changed or cleared", async () => {
			mockUpdateRole.mockResolvedValue(existingRole);
			renderEdit();
			const box = screen.getByLabelText(/^description/i);
			expect(box).toHaveValue("keeps tasks moving");

			await userEvent.clear(box);
			await userEvent.click(submit(/save changes/i));

			await waitFor(() =>
				expect(mockUpdateRole).toHaveBeenCalledWith(
					"r1",
					expect.objectContaining({ description: "" }),
					"p1",
				),
			);
		});
	});

	// ── Simple view: search and groups ─────────────────────────────────────────

	describe("permission list", () => {
		it("filters permissions by label, description or key and recovers from no match", async () => {
			renderCreate();
			const search = screen.getByRole("searchbox", {
				name: /search permissions/i,
			});
			expect(screen.getByRole("switch", { name: "View Tasks" })).toBeVisible();

			await userEvent.type(search, "tasks:read");
			expect(screen.getByRole("switch", { name: "View Tasks" })).toBeVisible();
			expect(
				screen.queryByRole("switch", { name: "Edit Tasks" }),
			).not.toBeInTheDocument();

			await userEvent.clear(search);
			await userEvent.type(search, "zzzz-nothing");
			expect(screen.queryAllByRole("switch")).toHaveLength(0);
			expect(screen.getByText(/no permissions match/i)).toBeInTheDocument();

			await userEvent.click(
				screen.getByRole("button", { name: /clear search/i }),
			);
			expect(screen.getByRole("switch", { name: "View Tasks" })).toBeVisible();
		});

		it("selects and clears a whole group and shows its count", async () => {
			renderCreate();
			const all = screen.getByRole("button", { name: /select all in tasks/i });
			await userEvent.click(all);

			const switches = screen
				.getAllByRole("switch")
				.filter((el) => el.getAttribute("aria-checked") === "true");
			expect(switches.length).toBeGreaterThan(1);
			expect(
				screen.getByText(`${switches.length} enabled`, { exact: true }),
			).toBeInTheDocument();

			await userEvent.click(
				screen.getByRole("button", { name: /clear all in tasks/i }),
			);
			expect(
				screen
					.getAllByRole("switch")
					.every((el) => el.getAttribute("aria-checked") === "false"),
			).toBe(true);
		});
	});

	// ── Error handling ─────────────────────────────────────────────────────────

	describe("error handling", () => {
		const failWith = (code: string) =>
			mockCreateRole.mockRejectedValue({
				response: { data: { error_code: code } },
			});
		const fillAndSubmit = async (name = "DEVELOPER") => {
			await userEvent.type(screen.getByLabelText(/role name/i), name);
			await userEvent.click(submit(/create role/i));
		};

		it("shows an inline field error when the role name is already taken", async () => {
			failWith("ROLE_NAME_TAKEN");
			renderCreate();
			await fillAndSubmit();

			expect(
				await screen.findByText(/role with this name already exists/i),
			).toBeInTheDocument();
		});

		it("shows an inline field error when the role name is invalid", async () => {
			failWith("ROLE_NAME_INVALID");
			renderCreate();
			await fillAndSubmit("x");

			expect(
				await screen.findByText(/role name of up to 100/i),
			).toBeInTheDocument();
		});

		it.each([
			["FORBIDDEN", /don't have permission/i],
			["INTERNAL_ERROR", /something went wrong on the server/i],
			["ROLE_POLICY_INVALID", /policy has problems/i],
			["ROLE_LAST_FULL_ACCESS", /nobody with full access/i],
		])("shows a general error for %s", async (code, message) => {
			failWith(code);
			renderCreate();
			await fillAndSubmit("NEW_ROLE");

			expect(await screen.findByRole("alert")).toHaveTextContent(message);
		});

		it("clears the name error when the user types again", async () => {
			failWith("ROLE_NAME_TAKEN");
			renderCreate();
			await fillAndSubmit();
			await screen.findByText(/role with this name already exists/i);

			await userEvent.type(screen.getByLabelText(/role name/i), "X");

			expect(
				screen.queryByText(/role with this name already exists/i),
			).not.toBeInTheDocument();
		});

		it("stays open when creation fails", async () => {
			mockCreateRole.mockRejectedValue(new Error("Server down"));
			const { onOpenChange } = renderCreate();
			await fillAndSubmit("NEW_ROLE");

			expect(await screen.findByRole("alert")).toHaveTextContent(
				/something went wrong/i,
			);
			expect(onOpenChange).not.toHaveBeenCalledWith(false);
		});
	});

	// ── Simple view: permissions ───────────────────────────────────────────────

	describe("permissions", () => {
		it("renders toggles for every permission group", () => {
			renderCreate();
			for (const group of [
				"Project",
				"Members",
				"Settings",
				"Tasks",
				"Sprints",
				"Views",
				"Documents",
				"Annotations",
			]) {
				expect(screen.getByText(group)).toBeInTheDocument();
			}
		});

		it("pre-selects what the role grants and writes it back as a policy", async () => {
			mockUpdateRole.mockResolvedValue(existingRole);
			renderEdit();
			expect(screen.getByRole("switch", { name: "View Tasks" })).toBeChecked();

			await userEvent.click(submit(/save changes/i));

			await waitFor(() => expect(mockUpdateRole).toHaveBeenCalled());
			expect(policySent(mockUpdateRole)).toEqual(
				actionsToPolicy(["tasks:*"], "project", "p1"),
			);
		});

		it("sends a policy with no statements when nothing is toggled", async () => {
			mockCreateRole.mockResolvedValue(existingRole);
			renderCreate();

			await userEvent.type(screen.getByLabelText(/role name/i), "VIEWER");
			await userEvent.click(submit(/create role/i));

			await waitFor(() => expect(mockCreateRole).toHaveBeenCalled());
			expect(policySent(mockCreateRole).statements).toEqual([]);
		});

		it("collapses a fully checked domain into its wildcard", async () => {
			mockCreateRole.mockResolvedValue(existingRole);
			renderCreate();
			await userEvent.type(screen.getByLabelText(/role name/i), "WORKER");
			await userEvent.click(screen.getByRole("switch", { name: "View Tasks" }));
			await userEvent.click(screen.getByRole("switch", { name: "Edit Tasks" }));
			await userEvent.click(submit(/create role/i));

			await waitFor(() => expect(mockCreateRole).toHaveBeenCalled());
			expect(policySent(mockCreateRole).statements[0].actions).toEqual([
				"tasks:*",
			]);
		});

		// A role stored as the bare wildcard is saved as the wildcard again unless
		// it is touched, so a permission added later is still covered.
		const fullAccessRole: Role = {
			...existingRole,
			name: "Admin",
			policy: actionsToPolicy(["*"], "project", "p1"),
		};

		it("shows a full-access badge and explanation for a role stored as the bare wildcard", () => {
			renderEdit(fullAccessRole);

			expect(screen.getByText("Full access")).toBeInTheDocument();
			expect(
				screen.getByText(/includes every permission/i),
			).toBeInTheDocument();
		});

		it("saves an untouched full-access role as the bare wildcard", async () => {
			mockUpdateRole.mockResolvedValue(fullAccessRole);
			renderEdit(fullAccessRole);

			await userEvent.click(submit(/save changes/i));

			await waitFor(() => expect(mockUpdateRole).toHaveBeenCalled());
			expect(policySent(mockUpdateRole).statements[0].actions).toEqual(["*"]);
		});

		it("leaves full access and saves the narrowed set once any switch is toggled", async () => {
			mockUpdateRole.mockResolvedValue(fullAccessRole);
			renderEdit(fullAccessRole);

			await userEvent.click(screen.getAllByRole("switch")[0]);
			expect(screen.queryByText("Full access")).not.toBeInTheDocument();
			await userEvent.click(submit(/save changes/i));

			await waitFor(() => expect(mockUpdateRole).toHaveBeenCalled());
			expect(policySent(mockUpdateRole).statements[0].actions).not.toContain(
				"*",
			);
		});
	});

	// ── Advanced view: the policy as JSON ──────────────────────────────────────

	describe("advanced (JSON) view", () => {
		const jsonBox = () =>
			screen.getByRole("textbox", { name: /policy \(json\)/i });
		const setJson = (value: string) => {
			fireEvent.change(jsonBox(), { target: { value } });
		};
		const goAdvanced = () =>
			userEvent.click(screen.getByRole("tab", { name: /advanced/i }));

		it("shows the checked permissions as a policy document", async () => {
			renderEdit();
			await goAdvanced();

			expect(JSON.parse((jsonBox() as HTMLTextAreaElement).value)).toEqual(
				actionsToPolicy(["tasks:*"], "project", "p1"),
			);
		});

		it("saves the JSON as edited, not what the switches say", async () => {
			mockUpdateRole.mockResolvedValue(existingRole);
			renderEdit();
			await goAdvanced();
			const edited: Policy = {
				version: "2026-10-01",
				statements: [
					{
						effect: "Allow",
						actions: ["tasks:read"],
						resources: ["project/*"],
					},
					{
						effect: "Deny",
						actions: ["tasks:write"],
						resources: ["project/*"],
						conditions: { StringEquals: { "task.sprint_id": "s6" } },
					},
				],
			};
			setJson(JSON.stringify(edited));
			await waitFor(() => expect(submit(/save changes/i)).toBeEnabled());
			await userEvent.click(submit(/save changes/i));

			await waitFor(() => expect(mockUpdateRole).toHaveBeenCalled());
			expect(policySent(mockUpdateRole)).toEqual(edited);
		});

		it("opens a role it cannot show as switches in the JSON view, with a notice", () => {
			renderEdit({
				...existingRole,
				policy: {
					statements: [
						{
							effect: "Allow",
							actions: ["tasks:read"],
							resources: ["project/*"],
						},
						{
							effect: "Deny",
							actions: ["tasks:write"],
							resources: ["project/*"],
						},
					],
				},
			});

			expect(
				screen.getByText(/can only be edited as JSON/i),
			).toBeInTheDocument();
			expect(jsonBox()).toBeInTheDocument();
			expect(screen.getByRole("tab", { name: /advanced/i })).toHaveAttribute(
				"aria-selected",
				"true",
			);
		});

		it("does not drop statements the switches cannot express when switching back", async () => {
			mockUpdateRole.mockResolvedValue(existingRole);
			renderEdit();
			await goAdvanced();
			const edited: Policy = {
				statements: [
					{
						effect: "Allow",
						actions: ["tasks:read"],
						resources: ["project/p1/task/*"],
					},
				],
			};
			setJson(JSON.stringify(edited));

			await userEvent.click(screen.getByRole("tab", { name: /simple/i }));

			expect(
				screen.getByText(/can only be edited as JSON/i),
			).toBeInTheDocument();
			expect(jsonBox()).toBeInTheDocument();
			await waitFor(() => expect(submit(/save changes/i)).toBeEnabled());
			await userEvent.click(submit(/save changes/i));
			await waitFor(() => expect(mockUpdateRole).toHaveBeenCalled());
			expect(policySent(mockUpdateRole)).toEqual(edited);
		});

		it("switches back to the switches when the JSON is something they can show", async () => {
			renderEdit();
			await goAdvanced();
			setJson(JSON.stringify(actionsToPolicy(["docs:read"], "project", "p1")));

			await userEvent.click(screen.getByRole("tab", { name: /simple/i }));

			expect(
				screen.getByRole("switch", { name: "View Documents" }),
			).toBeChecked();
			expect(
				screen.getByRole("switch", { name: "View Tasks" }),
			).not.toBeChecked();
		});

		it("refuses to save JSON that does not parse", async () => {
			renderEdit();
			await goAdvanced();
			setJson("{ not json");

			expect(await screen.findByRole("alert")).toHaveTextContent(/not valid/i);
			expect(submit(/save changes/i)).toBeDisabled();
		});

		it("lists the problems the server finds, by path, and blocks saving", async () => {
			mockValidate.mockResolvedValue({
				valid: false,
				issues: [
					{
						path: "statements[0].actions[0]",
						message: 'unknown action "nope:read"',
					},
				],
			});
			renderEdit();
			await goAdvanced();
			setJson(
				JSON.stringify({
					statements: [
						{ effect: "Allow", actions: ["nope:read"], resources: ["*"] },
					],
				}),
			);

			expect(
				await screen.findByText("statements[0].actions[0]"),
			).toBeInTheDocument();
			expect(screen.getByText(/unknown action/)).toBeInTheDocument();
			expect(submit(/save changes/i)).toBeDisabled();
		});

		it("validates inside the project", async () => {
			renderEdit();
			await goAdvanced();
			setJson(JSON.stringify(actionsToPolicy(["tasks:read"], "project", "p1")));

			await waitFor(() =>
				expect(mockValidate).toHaveBeenCalledWith(expect.anything(), "p1"),
			);
		});
	});
});
