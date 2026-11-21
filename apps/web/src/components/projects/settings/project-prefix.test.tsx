import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@tanstack/react-router", async () => ({
	...(await vi.importActual<typeof import("@tanstack/react-router")>(
		"@tanstack/react-router",
	)),
	createFileRoute: () => (options: unknown) => options,
	Link: () => null,
}));
vi.mock("@/components/shared/avatar-upload", () => ({
	AvatarUpload: () => null,
}));
const { createProject, updateProject } = vi.hoisted(() => ({
	createProject: vi.fn(),
	updateProject: vi.fn(),
}));
vi.mock("@/lib/project-api", async () => ({
	...(await vi.importActual<typeof import("@/lib/project-api")>(
		"@/lib/project-api",
	)),
	createProject,
	updateProject,
}));

import { projectQueryOptions } from "@/lib/project-api";
import { CreateProjectDialog } from "@/routes/_authenticated/home/index";
import { GeneralSettings } from "./GeneralSettings";

const conflict = {
	response: { status: 409, data: { error_code: "PROJECT_PREFIX_TAKEN" } },
};
const prefixTaken = "This prefix is already used by another project.";
const project = {
	id: "project-1",
	name: "Current",
	description: "",
	task_id_prefix: "CURR",
	settings: {},
	jev_configured: false,
	jev_base_url: "",
	jev_model: "",
	created_at: "2026-01-01T00:00:00Z",
};
function renderForm(element: ReactElement) {
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, staleTime: Infinity },
			mutations: { retry: false },
		},
	});
	client.setQueryData(projectQueryOptions(project.id).queryKey, project);
	render(<QueryClientProvider client={client}>{element}</QueryClientProvider>);
}

beforeEach(() => {
	vi.resetAllMocks();
});
afterEach(cleanup);

describe("project prefix conflicts", () => {
	it("shows an inline create error, keeps entered fields and clears it when edited", async () => {
		createProject.mockRejectedValue(conflict);
		const user = userEvent.setup();
		renderForm(<CreateProjectDialog open onOpenChange={vi.fn()} />);
		await user.type(screen.getByLabelText(/^Project name/i), "Payments");
		const prefix = screen.getByLabelText(/^Task ID prefix/i);
		await user.clear(prefix);
		await user.type(prefix, "PAY");
		await user.click(screen.getByRole("button", { name: "Create project" }));
		expect(await screen.findByText(prefixTaken)).toBeVisible();
		expect(prefix).toHaveValue("PAY");
		expect(createProject).toHaveBeenCalledWith(
			expect.objectContaining({ task_id_prefix: "PAY" }),
		);
		await user.type(prefix, "2");
		expect(screen.queryByText(prefixTaken)).not.toBeInTheDocument();
	});
	it("lets the server allocate an untouched suggested prefix", async () => {
		createProject.mockResolvedValue(project);
		const user = userEvent.setup();
		renderForm(<CreateProjectDialog open onOpenChange={vi.fn()} />);
		await user.type(screen.getByLabelText(/^Project name/i), "Payments");
		await user.click(screen.getByRole("button", { name: "Create project" }));
		await waitFor(() =>
			expect(createProject).toHaveBeenCalledWith(
				expect.objectContaining({
					name: "Payments",
					task_id_prefix: undefined,
				}),
			),
		);
	});
	it("shows an inline edit conflict and permits correcting and saving the prefix", async () => {
		updateProject
			.mockRejectedValueOnce(conflict)
			.mockResolvedValue({ ...project, task_id_prefix: "PAY2" });
		const user = userEvent.setup();
		renderForm(<GeneralSettings projectId={project.id} canEdit />);
		const prefix = screen.getByLabelText(/^Task ID prefix/i);
		await user.clear(prefix);
		await user.type(prefix, "PAY");
		await user.click(screen.getByRole("button", { name: "Save changes" }));
		expect(await screen.findByText(prefixTaken)).toBeVisible();
		await user.type(prefix, "2");
		expect(screen.queryByText(prefixTaken)).not.toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Save changes" }));
		await waitFor(() =>
			expect(updateProject).toHaveBeenLastCalledWith(
				project.id,
				expect.objectContaining({ task_id_prefix: "PAY2" }),
			),
		);
	});
});
