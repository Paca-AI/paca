import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/project-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/project-api")>(
			"@/lib/project-api",
		);
	return { ...actual, deleteTaskStatus: vi.fn() };
});

import { deleteTaskStatus, type TaskStatus } from "@/lib/project-api";
import { renderWithQueries } from "@/test/render-with-queries";
import { DeleteTaskStatusDialog } from "./DeleteTaskStatusDialog";

const STATUS: TaskStatus = {
	id: "s1",
	project_id: "p1",
	name: "Backlog",
	position: 0,
	category: "backlog",
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
};

function renderDialog(onOpenChange = vi.fn()) {
	const view = renderWithQueries(
		<DeleteTaskStatusDialog
			projectId="p1"
			status={STATUS}
			open
			onOpenChange={onOpenChange}
		/>,
		{ roles: null },
	);
	return { onOpenChange, ...view };
}

const deleteButton = () =>
	screen.getByRole("button", { name: "Delete status" });

beforeEach(() => {
	vi.resetAllMocks();
	vi.mocked(deleteTaskStatus).mockResolvedValue(undefined);
});

describe("DeleteTaskStatusDialog", () => {
	it("deletes the status, refreshes the list, and closes", async () => {
		const { onOpenChange, client } = renderDialog();
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await userEvent.click(deleteButton());

		await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
		expect(deleteTaskStatus).toHaveBeenCalledWith("p1", "s1");
		expect(invalidate).toHaveBeenCalledWith({
			queryKey: ["projects", "p1", "task-statuses"],
		});
	});

	it("says the default status can't be deleted when the server refuses, and stays open", async () => {
		vi.mocked(deleteTaskStatus).mockRejectedValue({
			response: { data: { error_code: "TASK_STATUS_IS_DEFAULT" } },
		});
		const { onOpenChange } = renderDialog();

		await userEvent.click(deleteButton());

		expect(
			await screen.findByText(
				"The default status can't be deleted. Make another status the default first.",
			),
		).toBeInTheDocument();
		expect(onOpenChange).not.toHaveBeenCalled();
	});

	it("just closes when the status is already gone", async () => {
		vi.mocked(deleteTaskStatus).mockRejectedValue({
			response: { data: { error_code: "TASK_STATUS_NOT_FOUND" } },
		});
		const { onOpenChange } = renderDialog();

		await userEvent.click(deleteButton());

		await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
	});

	it("falls back to a general message for any other failure", async () => {
		vi.mocked(deleteTaskStatus).mockRejectedValue(new Error("boom"));
		renderDialog();

		await userEvent.click(deleteButton());

		expect(
			await screen.findByText("Failed to delete status. Please try again."),
		).toBeInTheDocument();
	});
});
