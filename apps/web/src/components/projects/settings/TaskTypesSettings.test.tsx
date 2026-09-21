import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

const { TYPES } = vi.hoisted(() => ({
	TYPES: [
		{
			id: "t1",
			project_id: "p1",
			name: "Bug",
			is_default: true,
			created_at: "2026-01-01T00:00:00.000Z",
			updated_at: "2026-01-01T00:00:00.000Z",
		},
		{
			id: "t2",
			project_id: "p1",
			name: "Feature",
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
		taskTypesQueryOptions: (projectId: string) => ({
			queryKey: ["projects", projectId, "task-types"],
			queryFn: async () => TYPES,
		}),
	};
});

import { renderWithQueries } from "@/test/render-with-queries";
import { TaskTypesSettings } from "./TaskTypesSettings";

async function renderSettings(canWrite = true) {
	renderWithQueries(<TaskTypesSettings projectId="p1" canWrite={canWrite} />, {
		roles: null,
	});
	await screen.findByText("Bug");
}

const rowOf = (name: string) =>
	screen.getByText(name, { exact: true }).closest("tr") as HTMLElement;

describe("TaskTypesSettings — the default type", () => {
	it("does not let the default type be deleted, and says why", async () => {
		await renderSettings();

		const remove = within(rowOf("Bug")).getByRole("button", {
			name: "Delete type",
		});
		expect(remove).toBeDisabled();
		expect(remove.closest("span[title]")).toHaveAttribute(
			"title",
			"The default type can't be deleted. Make another type the default first.",
		);
	});

	it("still deletes any other type, after a confirmation", async () => {
		await renderSettings();

		await userEvent.click(
			within(rowOf("Feature")).getByRole("button", { name: "Delete type" }),
		);

		expect(await screen.findByRole("dialog")).toHaveTextContent(
			/delete.*feature/i,
		);
	});

	it("offers to make another type the default, but not the one that already is", async () => {
		await renderSettings();

		expect(
			within(rowOf("Bug")).queryByRole("button", {
				name: "Set as default type",
			}),
		).not.toBeInTheDocument();
		expect(
			within(rowOf("Feature")).getByRole("button", {
				name: "Set as default type",
			}),
		).toBeInTheDocument();
	});

	it("shows no actions to someone who may only read", async () => {
		await renderSettings(false);

		expect(within(rowOf("Bug")).queryAllByRole("button")).toHaveLength(0);
	});
});
