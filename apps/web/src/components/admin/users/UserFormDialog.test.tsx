import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
	mutate: vi.fn(),
	invalidateQueries: vi.fn(),
	isPending: false,
	onSuccess: null as null | ((result: unknown) => void),
	onError: null as null | ((err: unknown) => void),
	mutationFn: null as null | (() => Promise<unknown>),
	permissions: [] as string[],
	rolesData: [] as Array<{
		id: string;
		name: string;
		permissions: Record<string, boolean>;
		created_at: string;
		updated_at: string;
	}>,
}));

vi.mock("@tanstack/react-query", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
		"@tanstack/react-query",
	);
	return {
		...actual,
		useQueryClient: () => ({ invalidateQueries: mocks.invalidateQueries }),
		useQuery: () => ({ data: mocks.rolesData }),
		useMutation: (opts: {
			mutationFn: () => Promise<unknown>;
			onSuccess: (result: unknown) => void;
			onError: (err: unknown) => void;
		}) => {
			mocks.mutationFn = opts.mutationFn;
			mocks.onSuccess = opts.onSuccess;
			mocks.onError = opts.onError;
			return { mutate: mocks.mutate, isPending: mocks.isPending };
		},
	};
});

vi.mock("@/hooks/use-permissions", () => ({
	usePermissions: () => ({
		permissions: mocks.permissions,
		hasPermission: (p: string) => mocks.permissions.includes(p),
		hasAnyPermission: (ps: string[]) =>
			ps.some((p) => mocks.permissions.includes(p)),
		isLoading: false,
	}),
}));

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

describe("UserFormDialog — create mode", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.isPending = false;
		mocks.onSuccess = null;
		mocks.onError = null;
	});

	it("shows Create User title", () => {
		render(<UserFormDialog open={true} onOpenChange={vi.fn()} />);

		expect(screen.getByText("Create User")).toBeInTheDocument();
	});

	it("renders username and full name inputs", () => {
		render(<UserFormDialog open={true} onOpenChange={vi.fn()} />);

		expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
		expect(screen.getByLabelText(/full name/i)).toBeInTheDocument();
	});

	it("calls mutation.mutate when Create user button is clicked", async () => {
		render(<UserFormDialog open={true} onOpenChange={vi.fn()} />);

		await userEvent.click(screen.getByRole("button", { name: /create user/i }));

		expect(mocks.mutate).toHaveBeenCalledTimes(1);
	});

	it("shows generated password screen after successful creation", async () => {
		render(<UserFormDialog open={true} onOpenChange={vi.fn()} />);

		mocks.onSuccess?.("MockPw1!MockPw1!");

		await waitFor(() => {
			expect(screen.getByText("User created")).toBeInTheDocument();
		});
	});

	it("shows username-taken error when onError is called with UsernameTaken code", async () => {
		render(<UserFormDialog open={true} onOpenChange={vi.fn()} />);

		const err = { response: { data: { error_code: "USER_USERNAME_TAKEN" } } };
		mocks.onError?.(err);

		await waitFor(() => {
			expect(
				screen.getByText(/this username is already taken/i),
			).toBeInTheDocument();
		});
	});

	it("shows generic error message on unknown error", async () => {
		render(<UserFormDialog open={true} onOpenChange={vi.fn()} />);

		mocks.onError?.(new Error("Something went wrong."));

		await waitFor(() => {
			expect(screen.getByText("Something went wrong.")).toBeInTheDocument();
		});
	});

	it("shows 'Creating…' and disables button while pending", () => {
		mocks.isPending = true;

		render(<UserFormDialog open={true} onOpenChange={vi.fn()} />);

		expect(screen.getByRole("button", { name: /creating/i })).toBeDisabled();
	});
});

describe("UserFormDialog — edit mode", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.isPending = false;
		mocks.onSuccess = null;
		mocks.onError = null;
	});

	it("shows Edit User title", () => {
		render(
			<UserFormDialog user={mockUser} open={true} onOpenChange={vi.fn()} />,
		);

		expect(screen.getByText("Edit User")).toBeInTheDocument();
	});

	it("hides the Username field in edit mode", () => {
		render(
			<UserFormDialog user={mockUser} open={true} onOpenChange={vi.fn()} />,
		);

		expect(screen.queryByLabelText(/username/i)).not.toBeInTheDocument();
	});

	it("pre-fills Full Name with existing user value", () => {
		render(
			<UserFormDialog user={mockUser} open={true} onOpenChange={vi.fn()} />,
		);

		expect(screen.getByLabelText(/full name/i)).toHaveValue("Alice Smith");
	});

	it("shows Save changes button", () => {
		render(
			<UserFormDialog user={mockUser} open={true} onOpenChange={vi.fn()} />,
		);

		expect(
			screen.getByRole("button", { name: /save changes/i }),
		).toBeInTheDocument();
	});

	it("calls onOpenChange(false) after successful edit", async () => {
		const onOpenChange = vi.fn();
		render(
			<UserFormDialog
				user={mockUser}
				open={true}
				onOpenChange={onOpenChange}
			/>,
		);

		mocks.onSuccess?.(mockUser);

		await waitFor(() => {
			expect(onOpenChange).toHaveBeenCalledWith(false);
		});
	});

	it("shows 'Saving…' and disables button while pending", () => {
		mocks.isPending = true;

		render(
			<UserFormDialog user={mockUser} open={true} onOpenChange={vi.fn()} />,
		);

		expect(screen.getByRole("button", { name: /saving/i })).toBeDisabled();
	});
});

// ---------------------------------------------------------------------------
// Assigning a role is its own privilege (global_roles.assign) and its own
// endpoint: create/update send profile fields only, and the role — when the
// admin picked one — goes through assignUserGlobalRole afterwards.
// ---------------------------------------------------------------------------

const role = (id: string, name: string) => ({
	id,
	name,
	permissions: {},
	created_at: "2026-01-01T00:00:00.000Z",
	updated_at: "2026-01-01T00:00:00.000Z",
});

async function pickRole(name: string) {
	await userEvent.click(screen.getByRole("combobox"));
	await userEvent.click(await screen.findByRole("option", { name }));
}

async function runMutation() {
	let result: unknown;
	await act(async () => {
		result = await mocks.mutationFn?.();
	});
	return result;
}

describe("UserFormDialog — role assignment", () => {
	const userWithRole: User = { ...mockUser, role: "USER" };

	beforeEach(() => {
		vi.clearAllMocks();
		mocks.isPending = false;
		mocks.mutationFn = null;
		mocks.permissions = ["global_roles.assign", "global_roles.read"];
		mocks.rolesData = [role("role-user", "USER"), role("role-admin", "ADMIN")];
		vi.mocked(updateUser).mockResolvedValue(userWithRole);
		vi.mocked(createUser).mockResolvedValue({
			...userWithRole,
			id: "new-user",
		});
		vi.mocked(assignUserGlobalRole).mockResolvedValue(undefined);
	});

	it("saves only profile fields and makes no role call when the role is unchanged", async () => {
		render(
			<UserFormDialog user={userWithRole} open={true} onOpenChange={vi.fn()} />,
		);

		await runMutation();

		expect(updateUser).toHaveBeenCalledWith("u1", {
			full_name: "Alice Smith",
			email: undefined,
		});
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
	});

	it("assigns the role through its own endpoint when the selection changed", async () => {
		render(
			<UserFormDialog user={userWithRole} open={true} onOpenChange={vi.fn()} />,
		);

		await pickRole("ADMIN");
		await runMutation();

		expect(updateUser).toHaveBeenCalledWith("u1", {
			full_name: "Alice Smith",
			email: undefined,
		});
		expect(assignUserGlobalRole).toHaveBeenCalledWith("u1", "role-admin");
	});

	it("locks the role field, and never assigns, without global_roles.assign", async () => {
		mocks.permissions = ["global_roles.read"];
		render(
			<UserFormDialog user={userWithRole} open={true} onOpenChange={vi.fn()} />,
		);

		expect(screen.getByRole("combobox")).toBeDisabled();
		await runMutation();

		expect(updateUser).toHaveBeenCalledTimes(1);
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
	});

	it("creates the user without a role, then assigns a non-default one", async () => {
		render(<UserFormDialog open={true} onOpenChange={vi.fn()} />);

		await userEvent.type(screen.getByLabelText(/username/i), "newuser");
		await userEvent.type(screen.getByLabelText(/full name/i), "New User");
		await pickRole("ADMIN");
		const result = await runMutation();

		expect(createUser).toHaveBeenCalledWith({
			username: "newuser",
			password: "MockPw1!MockPw1!",
			full_name: "New User",
			email: undefined,
		});
		expect(assignUserGlobalRole).toHaveBeenCalledWith("new-user", "role-admin");
		expect(result).toBe("MockPw1!MockPw1!");
	});

	it("makes no role call when the default USER role is chosen at creation", async () => {
		render(<UserFormDialog open={true} onOpenChange={vi.fn()} />);

		await userEvent.type(screen.getByLabelText(/username/i), "newuser");
		await userEvent.type(screen.getByLabelText(/full name/i), "New User");
		await pickRole("USER");
		await runMutation();

		expect(createUser).toHaveBeenCalledTimes(1);
		expect(assignUserGlobalRole).not.toHaveBeenCalled();
	});

	it("still hands back the one-time password when the role assignment fails", async () => {
		vi.mocked(assignUserGlobalRole).mockRejectedValue(new Error("boom"));
		render(<UserFormDialog open={true} onOpenChange={vi.fn()} />);

		await userEvent.type(screen.getByLabelText(/username/i), "newuser");
		await userEvent.type(screen.getByLabelText(/full name/i), "New User");
		await pickRole("ADMIN");
		const result = await runMutation();

		// The user exists and the generated password is shown only once, so
		// the failure must not throw it away.
		expect(result).toBe("MockPw1!MockPw1!");
		expect(
			await screen.findByText(/role could not be assigned/i),
		).toBeInTheDocument();
	});
});
