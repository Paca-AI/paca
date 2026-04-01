import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { User } from "@/lib/admin-api";
import { UsersTable } from "./UsersTable";

const baseUser: User = {
	id: "u1",
	username: "alice",
	full_name: "Alice Smith",
	role: "Admin",
	must_change_password: false,
	created_at: "2026-01-15T00:00:00.000Z",
};

const anotherUser: User = {
	id: "u2",
	username: "bob",
	full_name: "Bob Jones",
	role: "User",
	must_change_password: true,
	created_at: "2026-02-20T00:00:00.000Z",
};

function renderTable(
	users: User[],
	opts: {
		canWrite?: boolean;
		currentUserId?: string;
		onEdit?: (user: User) => void;
		onDelete?: (user: User) => void;
		onResetPassword?: (user: User) => void;
	} = {},
) {
	const {
		canWrite = true,
		currentUserId,
		onEdit = vi.fn<(user: User) => void>(),
		onDelete = vi.fn<(user: User) => void>(),
		onResetPassword = vi.fn<(user: User) => void>(),
	} = opts;

	render(
		<UsersTable
			users={users}
			canWrite={canWrite}
			currentUserId={currentUserId}
			onEdit={onEdit}
			onDelete={onDelete}
			onResetPassword={onResetPassword}
		/>,
	);

	return { onEdit, onDelete, onResetPassword };
}

describe("UsersTable", () => {
	it("renders all users", () => {
		renderTable([baseUser, anotherUser]);

		expect(screen.getByText("alice")).toBeInTheDocument();
		expect(screen.getByText("Alice Smith")).toBeInTheDocument();
		expect(screen.getByText("bob")).toBeInTheDocument();
		expect(screen.getByText("Bob Jones")).toBeInTheDocument();
	});

	it("renders role badges", () => {
		renderTable([baseUser]);

		expect(screen.getByText("Admin")).toBeInTheDocument();
	});

	it("shows 'you' badge for current user", () => {
		renderTable([baseUser], { currentUserId: "u1" });

		expect(screen.getByText("you")).toBeInTheDocument();
	});

	it("does not show 'you' badge for other users", () => {
		renderTable([baseUser], { currentUserId: "u2" });

		expect(screen.queryByText("you")).not.toBeInTheDocument();
	});

	it("shows 'pwd reset' badge for must_change_password user", () => {
		renderTable([anotherUser]);

		expect(screen.getByText("pwd reset")).toBeInTheDocument();
	});

	it("shows em dash when full_name is empty", () => {
		const userNoName: User = { ...baseUser, full_name: "" };
		renderTable([userNoName]);

		expect(screen.getByText("—")).toBeInTheDocument();
	});

	it("hides action column when canWrite is false", () => {
		renderTable([baseUser], { canWrite: false });

		expect(screen.queryByTitle("Edit user")).not.toBeInTheDocument();
		expect(screen.queryByTitle("Delete user")).not.toBeInTheDocument();
		expect(screen.queryByTitle("Reset password")).not.toBeInTheDocument();
	});

	it("calls onEdit when Edit button is clicked", async () => {
		const { onEdit } = renderTable([baseUser]);

		await userEvent.click(screen.getByTitle("Edit user"));

		expect(onEdit).toHaveBeenCalledWith(baseUser);
	});

	it("calls onDelete when Delete button is clicked", async () => {
		const { onDelete } = renderTable([baseUser], { currentUserId: "u999" });

		await userEvent.click(screen.getByTitle("Delete user"));

		expect(onDelete).toHaveBeenCalledWith(baseUser);
	});

	it("calls onResetPassword when Reset password button is clicked", async () => {
		const { onResetPassword } = renderTable([baseUser]);

		await userEvent.click(screen.getByTitle("Reset password"));

		expect(onResetPassword).toHaveBeenCalledWith(baseUser);
	});

	it("hides Delete button for the current user (self)", () => {
		renderTable([baseUser], { currentUserId: "u1" });

		expect(screen.queryByTitle("Delete user")).not.toBeInTheDocument();
	});
});
