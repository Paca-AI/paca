import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/admin-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/admin-api")>("@/lib/admin-api");
	return { ...actual, assignUserGlobalRole: vi.fn() };
});

import { assignUserGlobalRole, type User } from "@/lib/admin-api";
import { makeRole, renderWithQueries } from "@/test/render-with-queries";
import { UserRoleDialog } from "./UserRoleDialog";

const alice: User = {
	id: "u1",
	username: "alice",
	full_name: "Alice Smith",
	role: "USER",
	must_change_password: false,
	created_at: "2026-01-15T00:00:00.000Z",
};

const ROLES = [
	makeRole("role-user", "USER", { "tasks.read": true }),
	makeRole("role-admin", "ADMIN", { "users.read": true }),
	makeRole("role-root", "SUPER_ADMIN", { "*": true }),
];
const CAN_ASSIGN = ["global_roles.assign", "global_roles.read"];

function renderDialog(
	props: Partial<React.ComponentProps<typeof UserRoleDialog>> = {},
) {
	const onOpenChange = vi.fn();
	const view = renderWithQueries(
		<UserRoleDialog user={alice} open onOpenChange={onOpenChange} {...props} />,
		{ permissions: CAN_ASSIGN, roles: ROLES },
	);
	return { onOpenChange, ...view };
}

const assignButton = () => screen.getByRole("button", { name: /assign role/i });

beforeEach(() => {
	vi.clearAllMocks();
	vi.mocked(assignUserGlobalRole).mockResolvedValue(undefined);
});

describe("UserRoleDialog", () => {
	it("names the user and starts on their current role with nothing to assign yet", () => {
		renderDialog();

		expect(
			screen.getByRole("heading", { name: "Change role" }),
		).toBeInTheDocument();
		expect(screen.getByText(/Choose a role for alice/)).toBeInTheDocument();
		expect(screen.getByRole("radio", { name: "USER" })).toBeChecked();
		expect(assignButton()).toBeDisabled();
	});

	it("assigns the picked role to that user through the role endpoint and closes", async () => {
		const { onOpenChange } = renderDialog();

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(assignButton());

		await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
		expect(assignUserGlobalRole).toHaveBeenCalledTimes(1);
		expect(assignUserGlobalRole).toHaveBeenCalledWith("u1", "role-admin");
	});

	it("does nothing when the role they already hold is picked again", async () => {
		renderDialog();

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(screen.getByRole("radio", { name: "USER" }));

		expect(assignButton()).toBeDisabled();
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
	});

	it("refreshes the users list after a change", async () => {
		const { client } = renderDialog();
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(assignButton());

		await waitFor(() =>
			expect(invalidate).toHaveBeenCalledWith({ queryKey: ["admin", "users"] }),
		);
	});

	it("does not touch the signed-in person's own identity when changing someone else", async () => {
		const { client } = renderDialog();
		const invalidate = vi.spyOn(client, "invalidateQueries");

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(assignButton());
		await waitFor(() => expect(assignUserGlobalRole).toHaveBeenCalled());

		expect(invalidate).not.toHaveBeenCalledWith({ queryKey: ["auth", "me"] });
	});

	it("warns before handing out a full-access role", async () => {
		renderDialog();
		expect(
			screen.queryByText(/full access\. It can do everything/i),
		).toBeNull();

		await userEvent.click(screen.getByRole("radio", { name: "SUPER_ADMIN" }));

		expect(
			screen.getByText(/this role has full access\. It can do everything/i),
		).toBeInTheDocument();
	});

	it("warns the signed-in person that changing their own role can lock them out, and refreshes who they are", async () => {
		const { client } = renderDialog({ isSelf: true });
		const invalidate = vi.spyOn(client, "invalidateQueries");
		expect(screen.queryByText(/your own account/i)).not.toBeInTheDocument();

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		expect(screen.getByText(/this is your own account/i)).toBeInTheDocument();

		await userEvent.click(assignButton());

		// Their permissions and identity follow the role they now hold.
		await waitFor(() =>
			expect(invalidate).toHaveBeenCalledWith({ queryKey: ["auth", "me"] }),
		);
	});

	it("does not show the self warning to someone editing another user", async () => {
		renderDialog({ isSelf: false });

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));

		expect(screen.queryByText(/your own account/i)).not.toBeInTheDocument();
	});

	it("disables the choices and the button while assigning", async () => {
		vi.mocked(assignUserGlobalRole).mockReturnValue(new Promise(() => {}));
		renderDialog();

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(assignButton());

		expect(
			await screen.findByRole("button", { name: /assigning/i }),
		).toBeDisabled();
		expect(screen.getByRole("radio", { name: "USER" })).toBeDisabled();
	});

	it.each([
		["FORBIDDEN", "You don't have permission to perform this action."],
		[
			"GLOBAL_ROLE_NOT_FOUND",
			"That role no longer exists. Close this dialog and try again.",
		],
		["USER_NOT_FOUND", "User not found. They may have already been deleted."],
		["INTERNAL_ERROR", "Something went wrong on the server. Please try again."],
	])("explains a %s failure and stays open to retry", async (code, message) => {
		vi.mocked(assignUserGlobalRole).mockRejectedValue({
			response: { data: { error_code: code } },
		});
		const { onOpenChange } = renderDialog();

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(assignButton());

		expect(await screen.findByRole("alert")).toHaveTextContent(message);
		expect(onOpenChange).not.toHaveBeenCalledWith(false);
		expect(assignButton()).toBeEnabled();
	});

	it("falls back to a generic message for an unexpected failure", async () => {
		vi.mocked(assignUserGlobalRole).mockRejectedValue(
			new Error("Request failed with status code 500"),
		);
		renderDialog();

		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(assignButton());

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Something went wrong.",
		);
	});

	it("clears an old error as soon as another role is picked", async () => {
		vi.mocked(assignUserGlobalRole).mockRejectedValue({
			response: { data: { error_code: "FORBIDDEN" } },
		});
		renderDialog();
		await userEvent.click(screen.getByRole("radio", { name: "ADMIN" }));
		await userEvent.click(assignButton());
		await screen.findByRole("alert");

		await userEvent.click(screen.getByRole("radio", { name: "SUPER_ADMIN" }));

		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
	});

	it("closes without assigning on Cancel", async () => {
		const { onOpenChange } = renderDialog();

		await userEvent.click(screen.getByRole("button", { name: "Cancel" }));

		expect(onOpenChange).toHaveBeenCalledWith(false);
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
	});
});
