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
	makeRole("role-user", "USER", { "tasks.read": true }),
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

const renderCreate = (permissions: string[] = CAN_ASSIGN) =>
	renderWithQueries(<UserFormDialog open={true} onOpenChange={vi.fn()} />, {
		permissions,
		roles: ROLES,
	});

const button = (name: RegExp | string) => screen.getByRole("button", { name });

async function fillDetails(username = "newuser", fullName = "New User") {
	await userEvent.type(screen.getByLabelText(/username/i), username);
	await userEvent.type(screen.getByLabelText(/full name/i), fullName);
}

/** Details → Role (needs the role step, so CAN_ASSIGN). */
async function goToRoleStep() {
	await fillDetails();
	await userEvent.click(button("Continue"));
	await screen.findByRole("radiogroup", { name: "Role" });
}

const pickRole = (name: string) =>
	userEvent.click(screen.getByRole("radio", { name }));

// ---------------------------------------------------------------------------
// Creating an account: 1 Details → 2 Role → 3 Password. The role step is
// there for someone who may assign roles (its own permission); nothing is
// created until the role step's button, and the one-time password is last.
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
		// Continuing is not creating.
		expect(button("Continue")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Create user" }),
		).not.toBeInTheDocument();
	});

	it("continues to the role step with USER selected and marked as the default, creating nothing yet", async () => {
		renderCreate();

		await goToRoleStep();

		expect(screen.getByText("2 / 3")).toBeInTheDocument();
		expect(screen.getByText(/Choose a role for newuser/)).toBeInTheDocument();
		const user = screen.getByRole("radio", { name: "USER" });
		expect(user).toBeChecked();
		expect(user).toHaveAccessibleDescription(/Default/);
		expect(screen.getAllByRole("radio")).toHaveLength(3);
		expect(createUser).not.toHaveBeenCalled();
	});

	it("validates the details before continuing, next to the field", async () => {
		renderCreate();

		await userEvent.click(button("Continue"));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Full name is required.",
		);

		await userEvent.type(screen.getByLabelText(/full name/i), "New User");
		await userEvent.click(button("Continue"));
		expect(
			await screen.findByText("Username is required."),
		).toBeInTheDocument();

		await userEvent.type(screen.getByLabelText(/username/i), "newuser");
		await userEvent.type(screen.getByLabelText(/email/i), "not-an-email");
		await userEvent.click(button("Continue"));
		expect(
			await screen.findByText("Please enter a valid email address."),
		).toBeInTheDocument();

		// Still on the first step, still nothing created.
		expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
		expect(createUser).not.toHaveBeenCalled();
	});

	it("goes back to the details with everything still filled in, keeping the picked role", async () => {
		renderCreate();
		await goToRoleStep();
		await pickRole("ADMIN");

		await userEvent.click(button("Back"));

		expect(screen.getByText("1 / 3")).toBeInTheDocument();
		expect(screen.getByLabelText(/username/i)).toHaveValue("newuser");
		expect(screen.getByLabelText(/full name/i)).toHaveValue("New User");

		await userEvent.click(button("Continue"));
		expect(await screen.findByRole("radio", { name: "ADMIN" })).toBeChecked();
	});

	it("creates the account with the default role without assigning anything, then shows the password last", async () => {
		renderCreate();
		await goToRoleStep();

		await userEvent.click(button("Create user"));

		expect(await screen.findByText("User created")).toBeInTheDocument();
		expect(createUser).toHaveBeenCalledWith({
			username: "newuser",
			password: "MockPw1!MockPw1!",
			full_name: "New User",
			email: undefined,
		});
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
		expect(screen.getByText("3 / 3")).toBeInTheDocument();
		expect(screen.getByDisplayValue("MockPw1!MockPw1!")).toBeInTheDocument();
		// The role it holds is confirmed beside the password.
		expect(screen.getByText("USER")).toBeInTheDocument();
		expect(screen.queryByText("Role assigned")).not.toBeInTheDocument();
	});

	it("creates the account and then assigns the picked role through its own request", async () => {
		renderCreate();
		await goToRoleStep();
		await pickRole("ADMIN");

		await userEvent.click(button("Create user"));

		expect(await screen.findByText("User created")).toBeInTheDocument();
		expect(assignUserGlobalRole).toHaveBeenCalledWith("new-user", "role-admin");
		// Created first, role second: the role needs the account's id.
		expect(vi.mocked(createUser).mock.invocationCallOrder[0]).toBeLessThan(
			vi.mocked(assignUserGlobalRole).mock.invocationCallOrder[0] as number,
		);
		expect(screen.getByText("Role assigned")).toBeInTheDocument();
		expect(screen.getByText("ADMIN")).toBeInTheDocument();
		expect(screen.getByDisplayValue("MockPw1!MockPw1!")).toBeInTheDocument();
	});

	it("warns before handing out a full-access role", async () => {
		renderCreate();
		await goToRoleStep();

		expect(screen.queryByText(/this role has full access/i)).toBeNull();
		await pickRole("SUPER_ADMIN");

		expect(screen.getByText(/this role has full access/i)).toBeInTheDocument();
	});

	it("still shows the one-time password when the role cannot be assigned, and lets the person retry", async () => {
		vi.mocked(assignUserGlobalRole).mockRejectedValueOnce({
			response: { data: { error_code: "FORBIDDEN" } },
		});
		renderCreate();
		await goToRoleStep();
		await pickRole("ADMIN");

		await userEvent.click(button("Create user"));

		// The account exists and its password is on screen — the failure did not
		// take either away.
		expect(await screen.findByText("User created")).toBeInTheDocument();
		expect(screen.getByDisplayValue("MockPw1!MockPw1!")).toBeInTheDocument();
		expect(
			screen.getByText(
				/the account was created, but its role couldn't be assigned/i,
			),
		).toBeInTheDocument();
		expect(screen.queryByText("Role assigned")).not.toBeInTheDocument();
		expect(screen.getByText("USER")).toBeInTheDocument();

		await userEvent.click(button("Try again"));

		expect(await screen.findByText("Role assigned")).toBeInTheDocument();
		expect(assignUserGlobalRole).toHaveBeenCalledTimes(2);
		expect(assignUserGlobalRole).toHaveBeenLastCalledWith(
			"new-user",
			"role-admin",
		);
		expect(screen.queryByText(/couldn't be assigned/i)).not.toBeInTheDocument();
		expect(screen.getByText("ADMIN")).toBeInTheDocument();
	});

	it("explains why a retry failed and keeps the retry available", async () => {
		vi.mocked(assignUserGlobalRole).mockRejectedValue({
			response: { data: { error_code: "GLOBAL_ROLE_NOT_FOUND" } },
		});
		renderCreate();
		await goToRoleStep();
		await pickRole("ADMIN");
		await userEvent.click(button("Create user"));
		await screen.findByText("User created");

		await userEvent.click(button("Try again"));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"That role no longer exists.",
		);
		expect(button("Try again")).toBeEnabled();
	});

	it("goes back to the details when the username is taken", async () => {
		vi.mocked(createUser).mockRejectedValue({
			response: { data: { error_code: "USER_USERNAME_TAKEN" } },
		});
		renderCreate();
		await goToRoleStep();

		await userEvent.click(button("Create user"));

		// The error belongs to a field of the first step, so that is where it shows.
		expect(
			await screen.findByText(/this username is already taken/i),
		).toBeInTheDocument();
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
		expect(screen.getByLabelText(/username/i)).toHaveValue("newuser");
	});

	it("goes back to the details when the email is taken", async () => {
		vi.mocked(createUser).mockRejectedValue({
			response: { data: { error_code: "USER_EMAIL_TAKEN" } },
		});
		renderCreate();
		await fillDetails();
		await userEvent.type(screen.getByLabelText(/email/i), "a@b.co");
		await userEvent.click(button("Continue"));
		await screen.findByRole("radiogroup");

		await userEvent.click(button("Create user"));

		expect(
			await screen.findByText(/this email is already in use/i),
		).toBeInTheDocument();
		expect(screen.getByText("1 / 3")).toBeInTheDocument();
	});

	it("shows any other failure on the role step and lets the person try again", async () => {
		vi.mocked(createUser).mockRejectedValueOnce({
			response: { data: { error_code: "INTERNAL_ERROR" } },
		});
		renderCreate();
		await goToRoleStep();

		await userEvent.click(button("Create user"));

		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Something went wrong on the server. Please try again.",
		);
		expect(screen.getByText("2 / 3")).toBeInTheDocument();
		expect(assignUserGlobalRole).not.toHaveBeenCalled();

		await userEvent.click(button("Create user"));
		expect(await screen.findByText("User created")).toBeInTheDocument();
	});

	it("shows 'Creating…' and locks the step while the request is in flight", async () => {
		vi.mocked(createUser).mockReturnValue(new Promise(() => {}));
		renderCreate();
		await goToRoleStep();

		await userEvent.click(button("Create user"));

		expect(
			await screen.findByRole("button", { name: /creating/i }),
		).toBeDisabled();
		expect(button("Back")).toBeDisabled();
		expect(screen.getByRole("radio", { name: "USER" })).toBeDisabled();
	});
});

// ---------------------------------------------------------------------------
// Someone who may not assign roles gets no role step: Details → Password, and
// the account starts as USER. The role can still be changed by whoever holds
// global_roles.assign, from the users table.
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
		// No role step, so no role line on the password step either.
		expect(screen.queryByText("Role")).not.toBeInTheDocument();
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
