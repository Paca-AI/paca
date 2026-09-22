import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({ fail: false }));

const NOW = "2026-01-01T00:00:00.000Z";
const ROLES = [
	{
		id: "pr-owner",
		role_name: "Owner",
		permissions: { "*": true },
		created_at: NOW,
		updated_at: NOW,
	},
	{
		id: "pr-editor",
		role_name: "Editor",
		permissions: {
			"project.members.write": true,
			"tasks.write": true,
			"docs.read": false,
		},
		created_at: NOW,
		updated_at: NOW,
	},
];

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
				if (state.fail) throw new Error("roles unavailable");
				return ROLES;
			},
		}),
	};
});

import { renderWithQueries } from "@/test/render-with-queries";
import { ProjectRolePicker } from "./project-role-picker";

function renderPicker(
	props: Partial<React.ComponentProps<typeof ProjectRolePicker>> = {},
) {
	const onChange = vi.fn();
	const view = renderWithQueries(
		<ProjectRolePicker
			projectId="proj-1"
			label="Project Role"
			value={null}
			onChange={onChange}
			{...props}
		/>,
		{ roles: null },
	);
	return { onChange, ...view };
}

beforeEach(() => {
	state.fail = false;
});

describe("ProjectRolePicker", () => {
	it("lists the project's roles by name, as radios in a labelled group", async () => {
		renderPicker();

		expect(
			await screen.findByRole("radiogroup", { name: "Project Role" }),
		).toBeInTheDocument();
		expect(screen.getByRole("radio", { name: "Owner" })).toBeInTheDocument();
		expect(screen.getByRole("radio", { name: "Editor" })).toBeInTheDocument();
	});

	it("shows what each role grants in the project, ignoring permissions that are off", async () => {
		renderPicker();

		expect(await screen.findByText("Full access")).toBeInTheDocument();
		expect(screen.getByText("tasks.write")).toBeInTheDocument();
		expect(screen.queryByText("docs.read")).not.toBeInTheDocument();
	});

	it("colours the badges as the project roles settings do, not as global permissions", async () => {
		renderPicker();

		// The project palette has a colour for project.members.*; the global one
		// leaves it neutral.
		expect(await screen.findByText("project.members.write")).toHaveClass(
			"bg-violet-50",
		);
	});

	it("reports the chosen role as {id, name, permissions}", async () => {
		const { onChange } = renderPicker();

		await userEvent.click(await screen.findByRole("radio", { name: "Editor" }));

		expect(onChange).toHaveBeenCalledWith({
			id: "pr-editor",
			name: "Editor",
			permissions: {
				"project.members.write": true,
				"tasks.write": true,
				"docs.read": false,
			},
		});
	});

	it("selects nothing until a role is picked, and shows the picked one", async () => {
		const { rerender } = renderPicker();
		for (const radio of await screen.findAllByRole("radio")) {
			expect(radio).not.toBeChecked();
		}

		rerender(
			<ProjectRolePicker
				projectId="proj-1"
				label="Project Role"
				value="pr-owner"
				onChange={() => {}}
			/>,
		);

		expect(screen.getByRole("radio", { name: "Owner" })).toBeChecked();
	});

	it("offers a retry when the roles cannot be loaded, and recovers", async () => {
		state.fail = true;
		renderPicker();
		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Couldn't load the roles.",
		);

		state.fail = false;
		await userEvent.click(screen.getByRole("button", { name: "Try again" }));

		expect(
			await screen.findByRole("radio", { name: "Owner" }),
		).toBeInTheDocument();
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});
});
