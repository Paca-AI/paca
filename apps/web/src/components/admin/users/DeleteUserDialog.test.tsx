import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
	mutate: vi.fn(),
	invalidateQueries: vi.fn(),
	isPending: false,
	onSuccess: null as null | (() => void),
	onError: null as null | ((err: unknown) => void),
}));

vi.mock("@tanstack/react-query", async () => {
	const actual =
		await vi.importActual<typeof import("@tanstack/react-query")>(
			"@tanstack/react-query",
		);
	return {
		...actual,
		useQueryClient: () => ({ invalidateQueries: mocks.invalidateQueries }),
		useMutation: (opts: {
			mutationFn: () => Promise<unknown>;
			onSuccess: () => void;
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
		deleteUser: vi.fn(),
	};
});

import type { User } from "@/lib/admin-api";
import { DeleteUserDialog } from "./DeleteUserDialog";

const mockUser: User = {
	id: "u1",
	username: "alice",
	full_name: "Alice Smith",
	role: "Admin",
	must_change_password: false,
	created_at: "2026-01-15T00:00:00.000Z",
};

describe("DeleteUserDialog", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.isPending = false;
		mocks.onSuccess = null;
		mocks.onError = null;
	});

	it("renders username in dialog", () => {
		render(
			<DeleteUserDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		expect(screen.getByText("alice")).toBeInTheDocument();
	});

	it("calls mutation.mutate when Delete user button is clicked", async () => {
		render(
			<DeleteUserDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		await userEvent.click(
			screen.getByRole("button", { name: /delete user/i }),
		);

		expect(mocks.mutate).toHaveBeenCalledTimes(1);
	});

	it("shows error message on mutation failure", async () => {
		render(
			<DeleteUserDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		mocks.onError?.(new Error("Something went wrong."));

		await waitFor(() => {
			expect(screen.getByText("Something went wrong.")).toBeInTheDocument();
		});
	});

	it("calls onOpenChange(false) on successful deletion", async () => {
		const onOpenChange = vi.fn();
		render(
			<DeleteUserDialog
				user={mockUser}
				open={true}
				onOpenChange={onOpenChange}
			/>,
		);

		mocks.onSuccess?.();

		await waitFor(() => {
			expect(onOpenChange).toHaveBeenCalledWith(false);
		});
	});

	it("shows 'Deleting…' text while mutation is pending", () => {
		mocks.isPending = true;

		render(
			<DeleteUserDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		expect(screen.getByRole("button", { name: /deleting/i })).toBeDisabled();
	});
});
