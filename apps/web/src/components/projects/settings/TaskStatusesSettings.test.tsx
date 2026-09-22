import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const { STATUSES } = vi.hoisted(() => ({
	STATUSES: [
		{
			id: "s1",
			project_id: "p1",
			name: "Inbox",
			position: 0,
			category: "backlog",
			is_default: true,
			created_at: "2026-01-01T00:00:00.000Z",
			updated_at: "2026-01-01T00:00:00.000Z",
		},
		{
			id: "s2",
			project_id: "p1",
			name: "Doing",
			position: 1,
			category: "inprogress",
			is_default: false,
			created_at: "2026-01-01T00:00:00.000Z",
			updated_at: "2026-01-01T00:00:00.000Z",
		},
	],
}));

vi.mock("@/hooks/use-project-permissions", () => ({
	useProjectPermissions: () => ({
		hasProjectPermission: () => true,
		isLoading: false,
	}),
}));

vi.mock("@/lib/project-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/project-api")>(
			"@/lib/project-api",
		);
	return {
		...actual,
		taskStatusesQueryOptions: (projectId: string) => ({
			queryKey: ["projects", projectId, "task-statuses"],
			queryFn: async () => STATUSES,
		}),
	};
});

import { renderWithQueries } from "@/test/render-with-queries";
import { TaskStatusesSettings } from "./TaskStatusesSettings";

async function renderSettings(canWrite = true) {
	renderWithQueries(
		<TaskStatusesSettings projectId="p1" canWrite={canWrite} />,
		{
			roles: null,
		},
	);
	await screen.findByText("Inbox");
}

const rowOf = (name: string) =>
	screen.getByText(name, { exact: true }).closest("tr") as HTMLElement;

describe("TaskStatusesSettings — the default status", () => {
	it("does not let the default status be deleted, and says why", async () => {
		await renderSettings();

		const remove = within(rowOf("Inbox")).getByRole("button", {
			name: "Delete status",
		});
		expect(remove).toBeDisabled();
		expect(remove.closest("span[title]")).toHaveAttribute(
			"title",
			"The default status can't be deleted. Make another status the default first.",
		);
	});

	it("still deletes any other status, after a confirmation", async () => {
		await renderSettings();

		await userEvent.click(
			within(rowOf("Doing")).getByRole("button", { name: "Delete status" }),
		);

		expect(await screen.findByRole("dialog")).toHaveTextContent(
			/delete.*doing/i,
		);
	});

	it("offers to make another status the default, but not the one that already is", async () => {
		await renderSettings();

		expect(
			within(rowOf("Inbox")).queryByRole("button", {
				name: "Set as default status",
			}),
		).not.toBeInTheDocument();
		expect(
			within(rowOf("Doing")).getByRole("button", {
				name: "Set as default status",
			}),
		).toBeInTheDocument();
	});

	it("shows no actions to someone who may only read", async () => {
		await renderSettings(false);

		expect(within(rowOf("Inbox")).queryAllByRole("button")).toHaveLength(0);
	});
});
