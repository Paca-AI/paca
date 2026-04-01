import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
	mutate: vi.fn(),
	isPending: false,
	onSuccess: null as null | ((pw: string) => void),
	onError: null as null | ((err: unknown) => void),
}));

vi.mock("@tanstack/react-query", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
		"@tanstack/react-query",
	);
	return {
		...actual,
		useMutation: (opts: {
			mutationFn: () => Promise<string>;
			onSuccess: (pw: string) => void;
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
		resetUserPassword: vi.fn(),
	};
});

vi.mock("@/lib/generate-password", () => ({
	generatePassword: () => "MockPw1!MockPw1!",
}));

import type { User } from "@/lib/admin-api";
import { ResetPasswordDialog } from "./ResetPasswordDialog";

const mockUser: User = {
	id: "u1",
	username: "alice",
	full_name: "Alice Smith",
	role: "Admin",
	must_change_password: false,
	created_at: "2026-01-15T00:00:00.000Z",
};

describe("ResetPasswordDialog", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.isPending = false;
		mocks.onSuccess = null;
		mocks.onError = null;
	});

	it("renders username and reset description", () => {
		render(
			<ResetPasswordDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		expect(
			screen.getByRole("heading", { name: /reset password/i }),
		).toBeInTheDocument();
		expect(screen.getByText("alice")).toBeInTheDocument();
	});

	it("shows Reset password button", () => {
		render(
			<ResetPasswordDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		expect(
			screen.getByRole("button", { name: /reset password/i }),
		).toBeInTheDocument();
	});

	it("calls mutation.mutate when Reset password button is clicked", async () => {
		render(
			<ResetPasswordDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		await userEvent.click(
			screen.getByRole("button", { name: /reset password/i }),
		);

		expect(mocks.mutate).toHaveBeenCalledTimes(1);
	});

	it("shows 'Resetting…' and disables button while pending", () => {
		mocks.isPending = true;

		render(
			<ResetPasswordDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		expect(screen.getByRole("button", { name: /resetting/i })).toBeDisabled();
	});

	it("shows generated password after successful reset", async () => {
		render(
			<ResetPasswordDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		mocks.onSuccess?.("MockPw1!MockPw1!");

		await waitFor(() => {
			expect(screen.getByText("Temporary Password")).toBeInTheDocument();
		});
	});

	it("shows a Done button after successful reset", async () => {
		render(
			<ResetPasswordDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		mocks.onSuccess?.("MockPw1!MockPw1!");

		await waitFor(() => {
			expect(screen.getByRole("button", { name: /done/i })).toBeInTheDocument();
		});
	});

	it("toggles password visibility when show/hide button is clicked", async () => {
		render(
			<ResetPasswordDialog
				user={mockUser}
				open={true}
				onOpenChange={vi.fn()}
			/>,
		);

		mocks.onSuccess?.("MockPw1!MockPw1!");

		await waitFor(() => {
			expect(screen.getByLabelText(/show password/i)).toBeInTheDocument();
		});

		const pwInput = document.querySelector<HTMLInputElement>("input[readonly]");
		expect(pwInput).toBeTruthy();
		const input = pwInput as HTMLInputElement;
		expect(input.type).toBe("password");

		await userEvent.click(screen.getByLabelText(/show password/i));

		expect(input.type).toBe("text");
	});

	it("shows error message on mutation failure", async () => {
		render(
			<ResetPasswordDialog
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
});
