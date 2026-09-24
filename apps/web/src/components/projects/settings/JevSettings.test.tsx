import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { api, state } = vi.hoisted(() => ({
	api: {
		updateProject: vi.fn(),
		updateProjectJevConfig: vi.fn(),
		testProjectJevConfig: vi.fn(),
	},
	state: {
		project: {} as Record<string, unknown>,
		customFields: [] as Record<string, unknown>[],
	},
}));

vi.mock("@/lib/project-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/project-api")>(
			"@/lib/project-api",
		);
	return {
		...actual,
		projectQueryOptions: (projectId: string) => ({
			queryKey: ["projects", projectId],
			queryFn: async () => state.project,
		}),
		customFieldsQueryOptions: (projectId: string) => ({
			queryKey: ["projects", projectId, "custom-fields"],
			queryFn: async () => state.customFields,
		}),
		updateProject: api.updateProject,
		updateProjectJevConfig: api.updateProjectJevConfig,
		testProjectJevConfig: api.testProjectJevConfig,
	};
});

import { renderWithQueries } from "@/test/render-with-queries";
import { JevSettings } from "./JevSettings";

function makeProject(overrides: Record<string, unknown> = {}) {
	return {
		id: "p1",
		name: "Project",
		settings: {},
		jev_configured: false,
		jev_base_url: "",
		jev_model: "",
		...overrides,
	};
}

async function renderJev({
	project = makeProject(),
	customFields = [] as Record<string, unknown>[],
	canEdit = true,
} = {}) {
	state.project = project;
	state.customFields = customFields;
	// The component reads the project synchronously for its initial form
	// state (the settings route's loader prefetches it), so prime the cache
	// before it mounts.
	const view = renderWithQueries(<div />);
	view.client.setQueryData(["projects", "p1"], project);
	view.client.setQueryData(["projects", "p1", "custom-fields"], customFields);
	view.rerender(<JevSettings projectId="p1" canEdit={canEdit} />);
	return view;
}

beforeEach(() => {
	api.updateProject.mockReset();
	api.updateProjectJevConfig.mockReset();
	api.testProjectJevConfig.mockReset();
});

describe("JevSettings — unconfigured project", () => {
	it("shows the not-configured state and hides feature toggles", async () => {
		await renderJev();
		expect(screen.getByText("Not configured")).toBeInTheDocument();
		expect(screen.getByText("Jev isn't configured yet")).toBeInTheDocument();
		expect(screen.queryByText("Task Auto-fill")).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Test connection" }),
		).not.toBeInTheDocument();
	});

	it("keeps Save credentials disabled until something changes", async () => {
		await renderJev();
		const save = screen.getByRole("button", { name: "Save credentials" });
		expect(save).toBeDisabled();
		await userEvent.type(screen.getByLabelText(/API Key/), "abc");
		expect(save).toBeEnabled();
	});

	it("saves trimmed credentials and clears the key input", async () => {
		api.updateProjectJevConfig.mockResolvedValue({
			configured: true,
			base_url: "https://jev.example",
			model: "m1",
		});
		await renderJev();

		const keyInput = screen.getByLabelText(/API Key/);
		await userEvent.type(keyInput, "  secret-key  ");
		await userEvent.type(
			screen.getByLabelText("Host (optional)"),
			" https://jev.example ",
		);
		await userEvent.type(screen.getByLabelText("Model (optional)"), "m1");
		await userEvent.click(
			screen.getByRole("button", { name: "Save credentials" }),
		);

		await waitFor(() =>
			expect(api.updateProjectJevConfig).toHaveBeenCalledWith("p1", {
				api_key: "secret-key",
				base_url: "https://jev.example",
				model: "m1",
			}),
		);
		expect(await screen.findByText("Saved ✓")).toBeInTheDocument();
		expect(keyInput).toHaveValue("");
	});

	it("omits api_key when the key field is left blank", async () => {
		api.updateProjectJevConfig.mockResolvedValue({
			configured: false,
			base_url: "",
			model: "m2",
		});
		await renderJev();
		await userEvent.type(screen.getByLabelText("Model (optional)"), "m2");
		await userEvent.click(
			screen.getByRole("button", { name: "Save credentials" }),
		);
		await waitFor(() => expect(api.updateProjectJevConfig).toHaveBeenCalled());
		expect(api.updateProjectJevConfig.mock.calls[0][1]).toEqual({
			base_url: "",
			model: "m2",
		});
	});

	it("shows an error when saving credentials fails", async () => {
		api.updateProjectJevConfig.mockRejectedValue(new Error("boom"));
		await renderJev();
		await userEvent.type(screen.getByLabelText(/API Key/), "k");
		await userEvent.click(
			screen.getByRole("button", { name: "Save credentials" }),
		);
		expect(
			await screen.findByText("Failed to update settings. Please try again."),
		).toBeInTheDocument();
	});

	it("toggles API key visibility", async () => {
		await renderJev();
		const keyInput = screen.getByLabelText(/API Key/);
		expect(keyInput).toHaveAttribute("type", "password");
		await userEvent.click(screen.getByRole("button", { name: /show/i }));
		expect(keyInput).toHaveAttribute("type", "text");
	});
});

describe("JevSettings — configured project", () => {
	const configured = () =>
		makeProject({
			jev_configured: true,
			jev_base_url: "https://jev.example",
			jev_model: "m1",
			settings: { theme: "keep-me" },
		});

	it("prefills host/model and never shows the stored key", async () => {
		await renderJev({ project: configured() });
		expect(screen.getByText("Configured")).toBeInTheDocument();
		expect(screen.getByLabelText("Host (optional)")).toHaveValue(
			"https://jev.example",
		);
		expect(screen.getByLabelText("Model (optional)")).toHaveValue("m1");
		expect(screen.getByLabelText(/API Key/)).toHaveValue("");
		expect(screen.getByLabelText(/API Key/)).toHaveAttribute(
			"placeholder",
			"Leave blank to keep the current key",
		);
	});

	it("reports a successful connection test", async () => {
		api.testProjectJevConfig.mockResolvedValue({ success: true });
		await renderJev({ project: configured() });
		await userEvent.click(
			screen.getByRole("button", { name: "Test connection" }),
		);
		expect(
			await screen.findByText("Connection successful"),
		).toBeInTheDocument();
		expect(api.testProjectJevConfig).toHaveBeenCalledWith("p1");
	});

	it("reports a failed connection test", async () => {
		api.testProjectJevConfig.mockRejectedValue(new Error("401"));
		await renderJev({ project: configured() });
		await userEvent.click(
			screen.getByRole("button", { name: "Test connection" }),
		);
		expect(
			await screen.findByText(
				"Couldn't connect. Check your credentials and try again.",
			),
		).toBeInTheDocument();
	});

	it("lists fixed fields plus only enumerable custom fields", async () => {
		await renderJev({
			project: configured(),
			customFields: [
				{ field_key: "sev", display_name: "Severity", field_type: "select" },
				{ field_key: "flag", display_name: "Flagged", field_type: "boolean" },
				{ field_key: "note", display_name: "Notes", field_type: "text" },
			],
		});
		for (const name of [
			"Importance",
			"Task Type",
			"Story Points",
			"Tags",
			"Epic",
			"Severity",
			"Flagged",
		]) {
			expect(screen.getByRole("button", { name })).toBeInTheDocument();
		}
		expect(
			screen.queryByRole("button", { name: "Notes" }),
		).not.toBeInTheDocument();
	});

	it("saves excluded fields and scope, preserving other settings", async () => {
		api.updateProject.mockImplementation(
			async (_id: string, body: { settings: Record<string, unknown> }) => ({
				...configured(),
				settings: body.settings,
			}),
		);
		await renderJev({ project: configured() });

		const save = screen.getByRole("button", { name: "Save changes" });
		expect(save).toBeDisabled();

		await userEvent.click(screen.getByRole("button", { name: "Importance" }));
		await userEvent.click(
			screen.getByRole("button", { name: "Human members only" }),
		);
		expect(
			screen.getByText("Excludes AI agents from consideration."),
		).toBeInTheDocument();
		expect(save).toBeEnabled();
		await userEvent.click(save);

		await waitFor(() =>
			expect(api.updateProject).toHaveBeenCalledWith("p1", {
				settings: {
					theme: "keep-me",
					jev: {
						autofill_enabled: true,
						autofill_excluded_fields: ["importance"],
						auto_assign_scope: "human",
					},
				},
			}),
		);
		expect(await screen.findByText("Saved ✓")).toBeInTheDocument();
	});

	it("re-including an excluded field makes the form clean again", async () => {
		await renderJev({
			project: makeProject({
				jev_configured: true,
				settings: { jev: { autofill_excluded_fields: ["tags"] } },
			}),
		});
		const save = screen.getByRole("button", { name: "Save changes" });
		await userEvent.click(screen.getByRole("button", { name: "Tags" }));
		expect(save).toBeEnabled();
		await userEvent.click(screen.getByRole("button", { name: "Tags" }));
		expect(save).toBeDisabled();
	});

	it("hides the field list when auto-fill is switched off", async () => {
		await renderJev({ project: configured() });
		expect(screen.getByText("Fields Jev can set")).toBeInTheDocument();
		await userEvent.click(screen.getByRole("switch"));
		expect(screen.queryByText("Fields Jev can set")).not.toBeInTheDocument();
	});

	it("falls back to defaults for malformed stored settings", async () => {
		await renderJev({
			project: makeProject({
				jev_configured: true,
				settings: {
					jev: {
						autofill_enabled: "yes",
						autofill_excluded_fields: [1, "tags"],
						auto_assign_scope: "robots",
					},
				},
			}),
		});
		expect(screen.getByRole("switch")).toBeChecked();
		expect(
			screen.queryByText("Excludes AI agents from consideration."),
		).not.toBeInTheDocument();
		expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
	});

	it("shows an error when saving settings fails", async () => {
		api.updateProject.mockRejectedValue(new Error("boom"));
		await renderJev({ project: configured() });
		await userEvent.click(screen.getByRole("button", { name: "Tags" }));
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
		expect(
			await screen.findByText("Failed to update settings. Please try again."),
		).toBeInTheDocument();
	});
});

describe("JevSettings — read-only", () => {
	it("disables inputs and hides save buttons without edit permission", async () => {
		await renderJev({
			project: makeProject({ jev_configured: true }),
			canEdit: false,
		});
		expect(screen.getByLabelText(/API Key/)).toBeDisabled();
		expect(
			screen.queryByRole("button", { name: "Save credentials" }),
		).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Save changes" }),
		).not.toBeInTheDocument();
		expect(
			screen.getByText("You don't have permission to edit this project."),
		).toBeInTheDocument();
	});
});
