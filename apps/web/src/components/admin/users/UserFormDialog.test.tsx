import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
	mutate: vi.fn(),
	invalidateQueries: vi.fn(),
	isPending: false,
	onSuccess: null as null | ((result: unknown) => void),
	onError: null as null | ((err: unknown) => void),
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
			mocks.onSuccess = opts.onSuccess;
			mocks.onError = opts.onError;
			return { mutate: mocks.mutate, isPending: mocks.isPending };
		},
	};
});

vi.mock("@/lib/admin-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/admin-api")>("@/lib/admin-api");
	return {
		...actual,
		createUser: vi.fn(),
		updateUser: vi.fn(),
	};
});

vi.mock("@/lib/generate-password", () => ({
	generatePassword: () => "MockPw1!MockPw1!",
}));

import type { User } from "@/lib/admin-api";
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
