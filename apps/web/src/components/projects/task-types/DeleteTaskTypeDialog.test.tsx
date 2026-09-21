import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/project-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/project-api")>(
			"@/lib/project-api",
		);
	return { ...actual, deleteTaskType: vi.fn() };
});

import { deleteTaskType, type TaskType } from "@/lib/project-api";
import { renderWithQueries } from "@/test/render-with-queries";
import { DeleteTaskTypeDialog } from "./DeleteTaskTypeDialog";

const TYPE: TaskType = {
	id: "t1",
	project_id: "p1",
	name: "Bug",
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
};

function renderDialog(onOpenChange = vi.fn()) {
	const view = renderWithQueries(
		<DeleteTaskTypeDialog
			projectId="p1"
			taskType={TYPE}
			open
			onOpenChange={onOpenChange}
		/>,
		{ roles: null },
	);
	return { onOpenChange, ...view };
}

const deleteButton = () => screen.getByRole("button", { name: "Delete type" });

beforeEach(() => {
	vi.resetAllMocks();
	vi.mocked(deleteTaskType).mockResolvedValue(undefined);
});

describe("DeleteTaskTypeDialog", () => {
	it("deletes the type, refreshes the list, and closes", async () => {
		const { onOpenChange, client } = renderDialog();
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await userEvent.click(deleteButton());

		await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
		expect(deleteTaskType).toHaveBeenCalledWith("p1", "t1");
		expect(invalidate).toHaveBeenCalledWith({
			queryKey: ["projects", "p1", "task-types"],
		});
	});

	it("says the default type can't be deleted when the server refuses, and stays open", async () => {
		vi.mocked(deleteTaskType).mockRejectedValue({
			response: { data: { error_code: "TASK_TYPE_IS_DEFAULT" } },
		});
		const { onOpenChange } = renderDialog();

		await userEvent.click(deleteButton());

		expect(
			await screen.findByText(
				"The default type can't be deleted. Make another type the default first.",
			),
		).toBeInTheDocument();
		expect(onOpenChange).not.toHaveBeenCalled();
	});

	it("just closes when the type is already gone", async () => {
		vi.mocked(deleteTaskType).mockRejectedValue({
			response: { data: { error_code: "TASK_TYPE_NOT_FOUND" } },
		});
		const { onOpenChange } = renderDialog();

		await userEvent.click(deleteButton());

		await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
	});

	it("falls back to a general message for any other failure", async () => {
		vi.mocked(deleteTaskType).mockRejectedValue(new Error("boom"));
		renderDialog();

		await userEvent.click(deleteButton());

		expect(
			await screen.findByText("Failed to delete task type. Please try again."),
		).toBeInTheDocument();
	});
});
