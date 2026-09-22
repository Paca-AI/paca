import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { makeRole } from "@/test/render-with-queries";
import { GlobalRolesTable } from "./GlobalRolesTable";

const USER = makeRole(
	"role-user",
	"USER",
	{ "tasks.read": true },
	{ isDefault: true },
);
const ADMIN = makeRole("role-admin", "ADMIN", { "users.read": true });

function renderTable(props: { canWrite?: boolean } = {}) {
	const handlers = {
		onEdit: vi.fn(),
		onDelete: vi.fn(),
		onSetDefault: vi.fn(),
	};
	render(
		<GlobalRolesTable
			roles={[USER, ADMIN]}
			canWrite={props.canWrite ?? true}
			{...handlers}
		/>,
	);
	return handlers;
}

/** The table row of the role with this name. */
const rowOf = (name: string) =>
	within(screen.getByText(name, { exact: true }).closest("tr") as HTMLElement);

describe("GlobalRolesTable", () => {
	it("has a Default column instead of the created date", () => {
		renderTable();

		expect(
			screen.getByRole("columnheader", { name: "Default" }),
		).toBeInTheDocument();
		expect(
			screen.queryByRole("columnheader", { name: "Created" }),
		).not.toBeInTheDocument();
	});

	it("marks the default role in that column, and only that one", () => {
		renderTable();

		expect(rowOf("USER").getByText("Default")).toBeInTheDocument();
		expect(rowOf("ADMIN").queryByText("Default")).not.toBeInTheDocument();
	});

	it("offers to make any other role the default, but not the one that already is", async () => {
		const { onSetDefault } = renderTable();

		expect(
			rowOf("USER").queryByRole("button", { name: "Set as default role" }),
		).not.toBeInTheDocument();

		await userEvent.click(
			rowOf("ADMIN").getByRole("button", { name: "Set as default role" }),
		);
		expect(onSetDefault).toHaveBeenCalledWith(ADMIN);
	});

	it("does not let the default role be deleted, and says why", () => {
		renderTable();

		const remove = rowOf("USER").getByRole("button", { name: "Delete role" });
		expect(remove).toBeDisabled();
		expect(remove.closest("span[title]")).toHaveAttribute(
			"title",
			"The default role can't be deleted. Make another role the default first.",
		);
	});

	it("still deletes any other role", async () => {
		const { onDelete } = renderTable();

		const remove = rowOf("ADMIN").getByRole("button", { name: "Delete role" });
		expect(remove).toBeEnabled();
		await userEvent.click(remove);

		expect(onDelete).toHaveBeenCalledWith(ADMIN);
	});

	it("still edits the default role", async () => {
		const { onEdit } = renderTable();

		await userEvent.click(
			rowOf("USER").getByRole("button", { name: "Edit role" }),
		);

		expect(onEdit).toHaveBeenCalledWith(USER);
	});

	it("shows no actions to someone who may not write roles, but still marks the default", () => {
		renderTable({ canWrite: false });

		expect(screen.queryAllByRole("button")).toHaveLength(0);
		expect(rowOf("USER").getByText("Default")).toBeInTheDocument();
	});
});
