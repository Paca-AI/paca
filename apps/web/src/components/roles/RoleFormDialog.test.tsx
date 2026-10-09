import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockCreateRole, mockValidate, mockSimulate } = vi.hoisted(() => ({
	mockCreateRole: vi.fn(),
	mockValidate: vi.fn(),
	mockSimulate: vi.fn(),
}));

const SCHEMA = [
	"task.sprint_id",
	"doc.folder_id",
	"doc.ancestor_folder_ids",
	"environment.type",
].map((key) => ({
	key,
	resource_kind: key.split(".")[0],
	type: "string",
	multi_valued: false,
	label_key: `roles.attributes.${key}`,
}));

vi.mock("@/lib/role-api", () => ({
	createRole: mockCreateRole,
	updateRole: vi.fn(),
	validatePolicy: mockValidate,
	simulatePolicy: mockSimulate,
	knownActionsQueryOptions: () => ({
		queryKey: ["roles", "actions"],
		queryFn: async () => ["tasks:read", "tasks:write", "docs:read"],
	}),
	attributeSchemaQueryOptions: () => ({
		queryKey: ["roles", "attribute-schema"],
		queryFn: async () => SCHEMA,
	}),
	projectRolesQueryOptions: (projectId: string) => ({
		queryKey: ["projects", projectId, "roles"],
	}),
	platformRolesQueryOptions: { queryKey: ["admin", "roles"] },
}));

vi.mock("@/lib/interaction-api", async (orig) => ({
	...(await orig<typeof import("@/lib/interaction-api")>()),
	sprintsQueryOptions: () => ({
		queryKey: ["sprints"],
		queryFn: async () => [{ id: "s1", name: "Sprint 1" }],
	}),
}));
vi.mock("@/lib/doc-api", async (orig) => ({
	...(await orig<typeof import("@/lib/doc-api")>()),
	docFoldersQueryOptions: () => ({
		queryKey: ["folders"],
		queryFn: async () => [{ id: "f1", name: "Specs" }],
	}),
}));
vi.mock("@/lib/environment-api", async (orig) => ({
	...(await orig<typeof import("@/lib/environment-api")>()),
	environmentsQueryOptions: () => ({
		queryKey: ["environments"],
		queryFn: async () => [{ id: "e1", name: "Production" }],
	}),
}));

import { RoleFormDialog as GlobalRoleFormDialog } from "@/components/admin/global-roles/RoleFormDialog";
import { ProjectRoleFormDialog } from "@/components/projects/roles/ProjectRoleFormDialog";

function Wrapper({ children }: { children: ReactNode }) {
	const client = new QueryClient({
		defaultOptions: {
			mutations: { retry: false },
			queries: { retry: false, gcTime: 0 },
		},
	});
	return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function renderProject() {
	render(
		<Wrapper>
			<ProjectRoleFormDialog projectId="p1" open onOpenChange={vi.fn()} />
		</Wrapper>,
	);
}

const jsonBox = () =>
	screen.getByLabelText("Policy (JSON)") as HTMLTextAreaElement;

async function toggle(...names: string[]) {
	for (const name of names) {
		await userEvent.click(screen.getByRole("switch", { name }));
	}
}

beforeEach(() => {
	vi.clearAllMocks();
	mockValidate.mockResolvedValue({ valid: true, issues: [] });
	mockCreateRole.mockResolvedValue({});
});

describe("Advanced view documentation link", () => {
	it("links to the IAM guide, only in the Advanced view", async () => {
		renderProject();
		expect(
			screen.queryByRole("link", { name: /iam roles guide/i }),
		).not.toBeInTheDocument();

		await userEvent.click(screen.getByRole("tab", { name: /advanced/i }));

		const link = screen.getByRole("link", { name: /iam roles guide/i });
		expect(link).toHaveAttribute(
			"href",
			expect.stringContaining("docs/guides/iam-authorization.md"),
		);
		expect(link).toHaveAttribute("target", "_blank");
		expect(link).toHaveAttribute("rel", expect.stringContaining("noopener"));
	});
});

describe("Simulate panel", () => {
	async function openAdvanced() {
		await userEvent.click(screen.getByRole("tab", { name: /advanced/i }));
	}

	it("is only shown in the Advanced view", async () => {
		renderProject();
		expect(screen.queryByText("Simulate a request")).not.toBeInTheDocument();
		await openAdvanced();
		expect(screen.getByText("Simulate a request")).toBeInTheDocument();
	});

	it("simulates the current JSON in the project and names the deciding statement", async () => {
		mockSimulate.mockResolvedValue({
			allowed: true,
			matched: [
				{ role_id: "policy", sid: "OnlyThisSprint", effect: "Allow", index: 1 },
			],
		});
		renderProject();
		await toggle("View Tasks");
		await openAdvanced();

		await userEvent.type(screen.getByLabelText("Action"), "tasks:read");
		await userEvent.type(
			screen.getByLabelText("Resource"),
			"project/p1/task/t1",
		);
		fireEvent.change(screen.getByLabelText(/attributes/i), {
			target: { value: "task.sprint_id=s1\nfoo=a, b" },
		});
		await userEvent.click(screen.getByRole("button", { name: "Simulate" }));

		expect(await screen.findByText("Allowed")).toBeInTheDocument();
		expect(
			screen.getByText(/Decided by OnlyThisSprint \(Allow\)/),
		).toBeVisible();
		const [input, projectId] = mockSimulate.mock.calls[0];
		expect(projectId).toBe("p1");
		expect(input).toMatchObject({
			action: "tasks:read",
			resource: "project/p1/task/t1",
			attributes: { "task.sprint_id": "s1", foo: ["a", "b"] },
		});
		expect(input.policy.statements[0].actions).toEqual(["tasks:read"]);
	});

	it("shows a deny with the Deny statement that decided it", async () => {
		mockSimulate.mockResolvedValue({
			allowed: false,
			matched: [
				{ role_id: "policy", sid: "Allowed", effect: "Allow", index: 0 },
				{ role_id: "policy", sid: "DenyEnvironment", effect: "Deny", index: 1 },
			],
		});
		renderProject();
		await openAdvanced();
		await userEvent.type(screen.getByLabelText("Action"), "environments:read");
		await userEvent.type(screen.getByLabelText("Resource"), "project/p1");
		await userEvent.click(screen.getByRole("button", { name: "Simulate" }));

		expect(await screen.findByText("Denied")).toBeInTheDocument();
		expect(
			screen.getByText(/Decided by DenyEnvironment \(Deny\)/),
		).toBeVisible();
	});

	it("simulates without a project for a workspace role", async () => {
		mockSimulate.mockResolvedValue({ allowed: false, matched: [] });
		render(
			<Wrapper>
				<GlobalRoleFormDialog open onOpenChange={vi.fn()} />
			</Wrapper>,
		);
		await userEvent.click(screen.getByRole("tab", { name: /advanced/i }));
		await userEvent.type(screen.getByLabelText("Action"), "users:read");
		await userEvent.type(screen.getByLabelText("Resource"), "user/u1");
		await userEvent.click(screen.getByRole("button", { name: "Simulate" }));

		expect(await screen.findByText("Denied")).toBeInTheDocument();
		expect(screen.getByText(/denied by default/i)).toBeVisible();
		expect(mockSimulate.mock.calls[0][1]).toBeUndefined();
	});

	it("is disabled while the JSON does not parse", async () => {
		renderProject();
		await openAdvanced();
		fireEvent.change(jsonBox(), { target: { value: "{" } });
		await userEvent.type(screen.getByLabelText("Action"), "tasks:read");
		await userEvent.type(screen.getByLabelText("Resource"), "project/p1");
		expect(screen.getByRole("button", { name: "Simulate" })).toBeDisabled();
	});
});

describe("JSON suggestions", () => {
	it("completes an action being typed from the known actions", async () => {
		renderProject();
		await userEvent.click(screen.getByRole("tab", { name: /advanced/i }));
		// Let the known actions load.
		await waitFor(() => expect(mockValidate).toHaveBeenCalled());

		fireEvent.change(jsonBox(), { target: { value: '{"actions": ["tas' } });
		const option = await screen.findByRole("option", { name: "tasks:write" });
		await userEvent.click(option);

		expect(jsonBox().value).toBe('{"actions": ["tasks:write');
	});

	it("completes a condition key from the attribute schema", async () => {
		renderProject();
		await userEvent.click(screen.getByRole("tab", { name: /advanced/i }));
		await waitFor(() => expect(mockValidate).toHaveBeenCalled());

		fireEvent.change(jsonBox(), { target: { value: '{"x": {"doc.anc' } });
		expect(
			await screen.findByRole("option", { name: "doc.ancestor_folder_ids" }),
		).toBeInTheDocument();
	});
});
