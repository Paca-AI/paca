import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { makeRole, renderWithQueries } from "@/test/render-with-queries";

vi.mock("@/lib/admin-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/admin-api")>("@/lib/admin-api");
	return {
		...actual,
		assignUserGlobalRole: vi.fn(),
		createUser: vi.fn(),
		updateUser: vi.fn(),
	};
});

vi.mock("@/lib/generate-password", () => ({
	generatePassword: () => "MockPw1!MockPw1!",
}));

import {
	assignUserGlobalRole,
	createUser,
	type User,
	updateUser,
} from "@/lib/admin-api";
import { UserFormDialog } from "./UserFormDialog";

const mockUser: User = {
	id: "u1",
	username: "alice",
	full_name: "Alice Smith",
	role: "Admin",
	must_change_password: false,
	created_at: "2026-01-15T00:00:00.000Z",
};

const ROLES = [
	makeRole("role-user", "USER", { "tasks.read": true }, { isDefault: true }),
	makeRole("role-admin", "ADMIN", { "users.read": true }),
	makeRole("role-root", "SUPER_ADMIN", { "*": true }),
];
const CAN_ASSIGN = ["global_roles.assign", "global_roles.read"];

beforeEach(() => {
	vi.clearAllMocks();
	vi.mocked(createUser).mockResolvedValue({
		...mockUser,
		id: "new-user",
		username: "newuser",
		role: "USER",
	});
	vi.mocked(updateUser).mockResolvedValue(mockUser);
	vi.mocked(assignUserGlobalRole).mockResolvedValue(undefined);
});

const renderCreate = (
	permissions: string[] = CAN_ASSIGN,
	onOpenChange: (open: boolean) => void = vi.fn(),
) =>
	renderWithQueries(
		<UserFormDialog open={true} onOpenChange={onOpenChange} />,
		{ permissions, roles: ROLES },
	);

const button = (name: RegExp | string) => screen.getByRole("button", { name });

async function fillDetails(username = "newuser", fullName = "New User") {
	await userEvent.type(screen.getByLabelText(/username/i), username);
	await userEvent.type(screen.getByLabelText(/full name/i), fullName);
}

/** Details → (creates the account) → Role. Needs the role step, so CAN_ASSIGN. */
async function goToRoleStep(onOpenChange?: (open: boolean) => void) {
	const view = renderCreate(CAN_ASSIGN, onOpenChange);
	await fillDetails();
	await userEvent.click(button("Create user"));
	await screen.findByRole("radiogroup", { name: "Role" });
	return view;
}

const pickRole = (name: string) =>
	userEvent.click(screen.getByRole("radio", { name }));

// ---------------------------------------------------------------------------
// Creating an account: 1 Details → 2 Role → 3 Password. Step 1's button creates
// the account (the server gives it the default role); the role step is a
// separate request that changes that role, offered to someone who may assign
// roles (its own permission); the one-time password comes last.
// ---------------------------------------------------------------------------

describe("UserFormDialog — create wizard (with the role step)", () => {
	it("starts on the details step of three, with no role field yet", () => {
		renderCreate();

		expect(screen.getByText("Create User")).toBeInTheDocument();
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
		expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
		expect(screen.getByLabelText(/full name/i)).toBeInTheDocument();
		expect(screen.getByLabelText(/email/i)).toBeInTheDocument();
		expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
		expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
		// The details step is where the account is created.
		expect(button("Create user")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Continue" }),
		).not.toBeInTheDocument();
	});

	it("validates the details before creating anything, next to the field", async () => {
		renderCreate();

		await userEvent.click(button("Create user"));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Full name is required.",
		);

		await userEvent.type(screen.getByLabelText(/full name/i), "New User");
		await userEvent.click(button("Create user"));
		expect(
			await screen.findByText("Username is required."),
		).toBeInTheDocument();

		await userEvent.type(screen.getByLabelText(/username/i), "newuser");
		await userEvent.type(screen.getByLabelText(/email/i), "not-an-email");
		await userEvent.click(button("Create user"));
		expect(
			await screen.findByText("Please enter a valid email address."),
		).toBeInTheDocument();

		// Still on the first step, still nothing created.
		expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
		expect(createUser).not.toHaveBeenCalled();
	});

	it("creates the account without a role, then shows it holding the default one and asks nothing more of the role API", async () => {
		await goToRoleStep();

		expect(createUser).toHaveBeenCalledTimes(1);
		// The request carries no role: the server gives the account the default.
		expect(createUser).toHaveBeenCalledWith({
			username: "newuser",
			password: "MockPw1!MockPw1!",
			full_name: "New User",
			email: undefined,
		});
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
		expect(screen.getByText("2 / 3")).toBeInTheDocument();
		expect(
			screen.getByText(
				"newuser was created with the USER role. Keep it, or choose a different one.",
			),
		).toBeInTheDocument();
		const held = screen.getByRole("radio", { name: "USER" });
		expect(held).toBeChecked();
		expect(held).toHaveAccessibleDescription(/Current/);
		expect(held).toHaveAccessibleDescription(/Default/);
		expect(screen.getAllByRole("radio")).toHaveLength(3);
		// The account exists, so there is no going back and no cancelling — and
		// its password is not shown until the last step.
		expect(
			screen.queryByRole("button", { name: "Back" }),
		).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Cancel" }),
		).not.toBeInTheDocument();
		expect(
			screen.queryByDisplayValue("MockPw1!MockPw1!"),
		).not.toBeInTheDocument();
	});

	it("keeps the role it has and shows the password last", async () => {
		await goToRoleStep();

		await userEvent.click(button("Continue"));

		expect(await screen.findByText("User created")).toBeInTheDocument();
		expect(screen.getByText("3 / 3")).toBeInTheDocument();
		expect(screen.getByDisplayValue("MockPw1!MockPw1!")).toBeInTheDocument();
		// The role it holds is confirmed beside the password.
		expect(screen.getByText("USER")).toBeInTheDocument();
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
		expect(createUser).toHaveBeenCalledTimes(1);
	});

	it("assigns the picked role through its own request, after the account exists, then shows the password", async () => {
		await goToRoleStep();
		await pickRole("ADMIN");

		await userEvent.click(button("Assign role"));

		expect(await screen.findByText("User created")).toBeInTheDocument();
		expect(assignUserGlobalRole).toHaveBeenCalledWith("new-user", "role-admin");
		// Created first, role second: the role needs the account's id.
		expect(vi.mocked(createUser).mock.invocationCallOrder[0]).toBeLessThan(
			vi.mocked(assignUserGlobalRole).mock.invocationCallOrder[0] as number,
		);
		expect(createUser).toHaveBeenCalledTimes(1);
		expect(screen.getByText("ADMIN")).toBeInTheDocument();
		expect(screen.getByDisplayValue("MockPw1!MockPw1!")).toBeInTheDocument();
	});

	it("labels the button for what it will do, and goes back to Continue when the current role is picked again", async () => {
		await goToRoleStep();
		expect(button("Continue")).toBeInTheDocument();

		await pickRole("ADMIN");
		expect(button("Assign role")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Continue" }),
		).not.toBeInTheDocument();

		await pickRole("USER");
		expect(button("Continue")).toBeInTheDocument();
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
	});

	it("warns before handing out a full-access role", async () => {
		await goToRoleStep();

		expect(screen.queryByText(/this role has full access/i)).toBeNull();
		await pickRole("SUPER_ADMIN");

		expect(screen.getByText(/this role has full access/i)).toBeInTheDocument();
	});

	it("stays on the role step when the role cannot be assigned, and lets the person retry", async () => {
		vi.mocked(assignUserGlobalRole).mockRejectedValueOnce({
			response: { data: { error_code: "FORBIDDEN" } },
		});
		await goToRoleStep();
		await pickRole("ADMIN");

		await userEvent.click(button("Assign role"));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"You don't have permission to perform this action.",
		);
		// Still the role step, and still no password: it is shown last.
		expect(screen.getByText("2 / 3")).toBeInTheDocument();
		expect(
			screen.queryByDisplayValue("MockPw1!MockPw1!"),
		).not.toBeInTheDocument();
		expect(button("Assign role")).toBeEnabled();

		await userEvent.click(button("Assign role"));

		expect(await screen.findByText("User created")).toBeInTheDocument();
		expect(assignUserGlobalRole).toHaveBeenCalledTimes(2);
		expect(assignUserGlobalRole).toHaveBeenLastCalledWith(
			"new-user",
			"role-admin",
		);
		expect(createUser).toHaveBeenCalledTimes(1);
		expect(screen.getByText("ADMIN")).toBeInTheDocument();
	});

	it("explains why the role could not be assigned when it no longer exists", async () => {
		vi.mocked(assignUserGlobalRole).mockRejectedValue({
			response: { data: { error_code: "GLOBAL_ROLE_NOT_FOUND" } },
		});
		await goToRoleStep();
		await pickRole("ADMIN");

		await userEvent.click(button("Assign role"));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"That role no longer exists.",
		);
	});

	it("lets the person carry on with the role the account has after a failed change", async () => {
		vi.mocked(assignUserGlobalRole).mockRejectedValueOnce({
			response: { data: { error_code: "FORBIDDEN" } },
		});
		await goToRoleStep();
		await pickRole("ADMIN");
		await userEvent.click(button("Assign role"));
		await screen.findByRole("alert");

		await pickRole("USER");
		// Picking again clears the failure: nothing is being attempted any more.
		expect(screen.queryByRole("alert")).not.toBeInTheDocument();
		await userEvent.click(button("Continue"));

		expect(await screen.findByText("User created")).toBeInTheDocument();
		expect(screen.getByText("USER")).toBeInTheDocument();
		expect(assignUserGlobalRole).toHaveBeenCalledTimes(1);
	});

	it("does not lose the password when the person closes the dialog on the role step: it goes on to it instead", async () => {
		const onOpenChange = vi.fn();
		await goToRoleStep(onOpenChange);

		await userEvent.click(screen.getByRole("button", { name: "Close" }));

		expect(onOpenChange).not.toHaveBeenCalled();
		expect(await screen.findByText("User created")).toBeInTheDocument();
		expect(screen.getByDisplayValue("MockPw1!MockPw1!")).toBeInTheDocument();
		expect(assignUserGlobalRole).not.toHaveBeenCalled();

		// From there it closes as usual.
		await userEvent.click(button("Done"));
		expect(onOpenChange).toHaveBeenCalledWith(false);
	});

	it("does not close while the role is being assigned", async () => {
		vi.mocked(assignUserGlobalRole).mockReturnValue(new Promise(() => {}));
		const onOpenChange = vi.fn();
		await goToRoleStep(onOpenChange);
		await pickRole("ADMIN");

		await userEvent.click(button("Assign role"));
		expect(
			await screen.findByRole("button", { name: /assigning/i }),
		).toBeDisabled();
		expect(screen.getByRole("radio", { name: "USER" })).toBeDisabled();
		await userEvent.click(screen.getByRole("button", { name: "Close" }));

		expect(onOpenChange).not.toHaveBeenCalled();
		expect(screen.getByText("2 / 3")).toBeInTheDocument();
	});

	it("stays on the details when the username is taken", async () => {
		vi.mocked(createUser).mockRejectedValue({
			response: { data: { error_code: "USER_USERNAME_TAKEN" } },
		});
		renderCreate();
		await fillDetails();

		await userEvent.click(button("Create user"));

		// The error belongs to a field of this step, so that is where it shows.
		expect(
			await screen.findByText(/this username is already taken/i),
		).toBeInTheDocument();
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
		expect(screen.getByLabelText(/username/i)).toHaveValue("newuser");
		expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
	});

	it("stays on the details when the email is taken", async () => {
		vi.mocked(createUser).mockRejectedValue({
			response: { data: { error_code: "USER_EMAIL_TAKEN" } },
		});
		renderCreate();
		await fillDetails();
		await userEvent.type(screen.getByLabelText(/email/i), "a@b.co");

		await userEvent.click(button("Create user"));

		expect(
			await screen.findByText(/this email is already in use/i),
		).toBeInTheDocument();
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
	});

	it("shows any other failure on the details step and lets the person try again", async () => {
		vi.mocked(createUser).mockRejectedValueOnce({
			response: { data: { error_code: "INTERNAL_ERROR" } },
		});
		renderCreate();
		await fillDetails();

		await userEvent.click(button("Create user"));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Something went wrong on the server. Please try again.",
		);
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
		expect(assignUserGlobalRole).not.toHaveBeenCalled();

		await userEvent.click(button("Create user"));
		expect(
			await screen.findByRole("radiogroup", { name: "Role" }),
		).toBeInTheDocument();
	});

	it("says so when there is no default role to give the account", async () => {
		vi.mocked(createUser).mockRejectedValue({
			response: { data: { error_code: "GLOBAL_ROLE_NO_DEFAULT" } },
		});
		renderCreate();
		await fillDetails();

		await userEvent.click(button("Create user"));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			/no default role to give the new account/i,
		);
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
	});

	it("shows 'Creating…', locks the step, and does not close while the request is in flight", async () => {
		vi.mocked(createUser).mockReturnValue(new Promise(() => {}));
		const onOpenChange = vi.fn();
		renderCreate(CAN_ASSIGN, onOpenChange);
		await fillDetails();

		await userEvent.click(button("Create user"));

		expect(
			await screen.findByRole("button", { name: /creating/i }),
		).toBeDisabled();
		await userEvent.click(screen.getByRole("button", { name: "Close" }));
		expect(onOpenChange).not.toHaveBeenCalled();
	});

	it("starts over from the details each time it is opened", async () => {
		const onOpenChange = vi.fn();
		await goToRoleStep(onOpenChange);
		await userEvent.click(button("Continue"));
		await userEvent.click(await screen.findByRole("button", { name: "Done" }));

		expect(onOpenChange).toHaveBeenCalledWith(false);
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
		expect(screen.getByLabelText(/username/i)).toHaveValue("");
		expect(
			screen.queryByDisplayValue("MockPw1!MockPw1!"),
		).not.toBeInTheDocument();
	});
});

// ---------------------------------------------------------------------------
// Someone who may not assign roles gets no role step: Details → Password, and
// the account starts with the default role. The role can still be changed by
// whoever holds global_roles.assign, from the users table.
// ---------------------------------------------------------------------------

describe("UserFormDialog — create wizard (without the role step)", () => {
	it.each([
		["no role permissions", ["users.write"]],
		["global_roles.read but not global_roles.assign", ["global_roles.read"]],
		["global_roles.assign but not global_roles.read", ["global_roles.assign"]],
	])("has two steps and creates straight from the details with %s", async (_label, permissions) => {
		renderCreate(permissions);

		expect(screen.getByText("1 / 2")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Continue" }),
		).not.toBeInTheDocument();

		await fillDetails();
		await userEvent.click(button("Create user"));

		expect(await screen.findByText("User created")).toBeInTheDocument();
		expect(screen.getByText("2 / 2")).toBeInTheDocument();
		expect(createUser).toHaveBeenCalledTimes(1);
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
		expect(screen.getByDisplayValue("MockPw1!MockPw1!")).toBeInTheDocument();
		// No role step, but the role the account got is still confirmed.
		expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
		expect(screen.getByText("USER")).toBeInTheDocument();
	});

	it("validates before creating", async () => {
		renderCreate(["users.write"]);

		await userEvent.click(button("Create user"));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Full name is required.",
		);
		expect(createUser).not.toHaveBeenCalled();
	});

	it("shows a username-taken error next to the username field", async () => {
		vi.mocked(createUser).mockRejectedValue({
			response: { data: { error_code: "USER_USERNAME_TAKEN" } },
		});
		renderCreate(["users.write"]);
		await fillDetails();

		await userEvent.click(button("Create user"));

		expect(
			await screen.findByText(/this username is already taken/i),
		).toBeInTheDocument();
	});

	it("falls back to the error's own message on an unknown failure", async () => {
		vi.mocked(createUser).mockRejectedValue(new Error("Disk on fire"));
		renderCreate(["users.write"]);
		await fillDetails();

		await userEvent.click(button("Create user"));

		expect(await screen.findByRole("alert")).toHaveTextContent("Disk on fire");
	});

	it("shows 'Creating…' and disables the button while the request is in flight", async () => {
		vi.mocked(createUser).mockReturnValue(new Promise(() => {}));
		renderCreate(["users.write"]);
		await fillDetails();

		await userEvent.click(button("Create user"));

		expect(
			await screen.findByRole("button", { name: /creating/i }),
		).toBeDisabled();
	});
});

// ---------------------------------------------------------------------------
// Editing is the profile only: never a step indicator, never a role.
// ---------------------------------------------------------------------------

describe("UserFormDialog — edit mode", () => {
	const renderEdit = (
		onOpenChange = vi.fn(),
		permissions: string[] = CAN_ASSIGN,
	) =>
		renderWithQueries(
			<UserFormDialog
				user={mockUser}
				open={true}
				onOpenChange={onOpenChange}
			/>,
			{ permissions, roles: ROLES },
		);

	it("shows Edit User title and describes it as a name and email edit", () => {
		renderEdit();

		expect(screen.getByText("Edit User")).toBeInTheDocument();
		expect(
			screen.getByText("Update the user's display name and email."),
		).toBeInTheDocument();
	});

	it("is a single step, not a wizard, even for someone who can assign roles", () => {
		renderEdit();

		expect(screen.queryByText(/^\d \/ \d$/)).not.toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Continue" }),
		).not.toBeInTheDocument();
		expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
		expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
	});

	it("hides the Username field in edit mode", () => {
		renderEdit();

		expect(screen.queryByLabelText(/username/i)).not.toBeInTheDocument();
	});

	it("pre-fills Full Name with existing user value", () => {
		renderEdit();

		expect(screen.getByLabelText(/full name/i)).toHaveValue("Alice Smith");
	});

	it("saves the profile through updateUser, never touches the role, and closes", async () => {
		const onOpenChange = vi.fn();
		renderEdit(onOpenChange);

		await userEvent.clear(screen.getByLabelText(/full name/i));
		await userEvent.type(screen.getByLabelText(/full name/i), "Alice Jones");
		await userEvent.click(button("Save changes"));

		await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
		expect(updateUser).toHaveBeenCalledWith("u1", {
			full_name: "Alice Jones",
			email: undefined,
		});
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
	});

	it("requires a full name", async () => {
		renderEdit();

		await userEvent.clear(screen.getByLabelText(/full name/i));
		await userEvent.click(button("Save changes"));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Full name is required.",
		);
		expect(updateUser).not.toHaveBeenCalled();
	});

	it("shows 'Saving…' and disables the button while the request is in flight", async () => {
		vi.mocked(updateUser).mockReturnValue(new Promise(() => {}));
		renderEdit();

		await userEvent.click(button("Save changes"));

		expect(
			await screen.findByRole("button", { name: /saving/i }),
		).toBeDisabled();
	});
});
