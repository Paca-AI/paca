import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({ fail: false }));

const NOW = "2026-01-01T00:00:00.000Z";
const role = (
	id: string,
	name: string,
	policy: {
		effect: "Allow" | "Deny";
		actions: string[];
		resources?: string[];
	}[],
) => ({
	id,
	name,
	description: "",
	policy: {
		statements: policy.map((s) => ({ resources: ["project/*"], ...s })),
	},
	project_id: "proj-1",
	is_system: false,
	is_default: false,
	attachment_count: 0,
	created_at: NOW,
	updated_at: NOW,
});
const ROLES = [
	role("pr-owner", "Owner", [{ effect: "Allow", actions: ["*"] }]),
	role("pr-editor", "Editor", [
		{ effect: "Allow", actions: ["project.members:write", "tasks:write"] },
		{ effect: "Deny", actions: ["docs:read"] },
	]),
];

vi.mock("@/lib/role-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/role-api")>("@/lib/role-api");
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
			values={[]}
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

async function openList() {
	await userEvent.click(
		await screen.findByRole("combobox", { name: "Project Role" }),
	);
	return screen.findByRole("listbox", { name: "Project Role" });
}

describe("ProjectRolePicker", () => {
	it("lists the project's roles by name, as options in a labelled list", async () => {
		renderPicker();
		await openList();

		expect(screen.getByRole("option", { name: "Owner" })).toBeInTheDocument();
		expect(screen.getByRole("option", { name: "Editor" })).toBeInTheDocument();
	});

	it("shows what each role grants in the project, ignoring what a Deny statement takes away from the glance", async () => {
		renderPicker();
		await openList();

		expect(screen.getByText("Full access")).toBeInTheDocument();
		expect(screen.getByText("tasks:write")).toBeInTheDocument();
		expect(screen.queryByText("docs:read")).not.toBeInTheDocument();
	});

	it("colours the badges as the project roles settings do, not as global permissions", async () => {
		renderPicker();
		await openList();

		// The project palette has a colour for project.members.*; the global one
		// leaves it neutral.
		expect(screen.getByText("project.members:write")).toHaveClass(
			"bg-violet-50",
		);
	});

	it("reports the picked ids and the roles themselves, and allows several", async () => {
		const { onChange } = renderPicker({ values: ["pr-owner"] });
		await openList();

		await userEvent.click(screen.getByRole("option", { name: "Editor" }));

		expect(onChange).toHaveBeenCalledWith(
			["pr-owner", "pr-editor"],
			[ROLES[0], ROLES[1]],
		);
	});

	it("selects nothing until a role is picked, and shows the picked one", async () => {
		const { rerender } = renderPicker();
		await openList();
		for (const option of screen.getAllByRole("option")) {
			expect(option).toHaveAttribute("aria-selected", "false");
		}

		rerender(
			<ProjectRolePicker
				projectId="proj-1"
				label="Project Role"
				values={["pr-owner"]}
				onChange={() => {}}
			/>,
		);

		expect(screen.getByRole("option", { name: "Owner" })).toHaveAttribute(
			"aria-selected",
			"true",
		);
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
			await screen.findByRole("combobox", { name: "Project Role" }),
		).toBeInTheDocument();
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});
});
