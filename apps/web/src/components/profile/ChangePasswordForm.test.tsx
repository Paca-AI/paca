import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { ApiErrorCode } from "../../lib/api-error";

const mocks = vi.hoisted(() => ({
	mutate: vi.fn(),
	invalidateQueries: vi.fn(),
	isPending: false,
	onSuccess: null as null | (() => void),
	onError: null as null | ((err: unknown) => void),
}));

vi.mock("@tanstack/react-query", async () => {
	const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
		"@tanstack/react-query",
	);
	return {
		...actual,
		useQueryClient: () => ({ invalidateQueries: mocks.invalidateQueries }),
		useMutation: (opts: {
			mutationFn: () => Promise<void>;
			onSuccess: () => void;
			onError: (err: unknown) => void;
		}) => {
			mocks.onSuccess = opts.onSuccess;
			mocks.onError = opts.onError;
			return { mutate: mocks.mutate, isPending: mocks.isPending };
		},
	};
});

vi.mock("@/hooks/use-is-dark", () => ({
	useIsDark: () => false,
}));

import { ChangePasswordForm } from "./ChangePasswordForm";

describe("ChangePasswordForm", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		mocks.isPending = false;
		mocks.onSuccess = null;
		mocks.onError = null;
	});

	it("shows inline validation messages and clears them as the user fixes each field", async () => {
		render(<ChangePasswordForm />);
		const user = userEvent.setup();

		const currentPassword = screen.getByLabelText(/current password/i, {
			selector: "input",
		});
		const newPassword = screen.getByLabelText(/^new password$/i, {
			selector: "input",
		});
		const confirmPassword = screen.getByLabelText(/confirm new password/i, {
			selector: "input",
		});

		await user.click(currentPassword);
		await user.tab();
		expect(
			screen.getByText("Current password is required."),
		).toBeInTheDocument();

		await user.type(currentPassword, "CurrentPass1!");
		expect(
			screen.queryByText("Current password is required."),
		).not.toBeInTheDocument();

		await user.click(newPassword);
		await user.tab();
		expect(screen.getByText("New password is required.")).toBeInTheDocument();

		await user.type(newPassword, "short");
		expect(
			screen.getByText("New password must be at least 8 characters."),
		).toBeInTheDocument();

		await user.clear(newPassword);
		await user.type(newPassword, "CurrentPass1!");
		expect(
			screen.getByText("New password must be different from current password."),
		).toBeInTheDocument();

		await user.clear(newPassword);
		await user.type(newPassword, "NextPass123!");
		expect(
			screen.queryByText("New password is required."),
		).not.toBeInTheDocument();
		expect(
			screen.queryByText("New password must be at least 8 characters."),
		).not.toBeInTheDocument();
		expect(
			screen.queryByText(
				"New password must be different from current password.",
			),
		).not.toBeInTheDocument();

		await user.click(confirmPassword);
		await user.tab();
		expect(
			screen.getByText("Please confirm your new password."),
		).toBeInTheDocument();

		await user.type(confirmPassword, "Mismatch123!");
		expect(screen.getByText("Passwords do not match.")).toBeInTheDocument();

		await user.clear(confirmPassword);
		await user.type(confirmPassword, "NextPass123!");
		expect(
			screen.queryByText("Please confirm your new password."),
		).not.toBeInTheDocument();
		expect(
			screen.queryByText("Passwords do not match."),
		).not.toBeInTheDocument();
	});

	it("keeps submit disabled until the password rules are satisfied", async () => {
		render(<ChangePasswordForm />);
		const user = userEvent.setup();

		const currentPassword = screen.getByLabelText(/current password/i, {
			selector: "input",
		});
		const newPassword = screen.getByLabelText(/^new password$/i, {
			selector: "input",
		});
		const confirmPassword = screen.getByLabelText(/confirm new password/i, {
			selector: "input",
		});
		const submitButton = screen.getByRole("button", {
			name: /change password/i,
		});

		expect(submitButton).toBeDisabled();

		await user.type(currentPassword, "CurrentPass1!");
		await user.type(newPassword, "CurrentPass1!");
		await user.type(confirmPassword, "CurrentPass1!");
		expect(submitButton).toBeDisabled();

		await user.clear(newPassword);
		await user.clear(confirmPassword);
		await user.type(newPassword, "NextPass123!");
		await user.type(confirmPassword, "Mismatch123!");
		expect(submitButton).toBeDisabled();

		await user.clear(confirmPassword);
		await user.type(confirmPassword, "NextPass123!");
		expect(submitButton).toBeEnabled();
	});

	it("shows invalid current password inline and clears it when the field changes", async () => {
		render(<ChangePasswordForm />);
		const user = userEvent.setup();

		const currentPassword = screen.getByLabelText(/current password/i, {
			selector: "input",
		});
		const newPassword = screen.getByLabelText(/^new password$/i, {
			selector: "input",
		});
		const confirmPassword = screen.getByLabelText(/confirm new password/i, {
			selector: "input",
		});

		await user.type(currentPassword, "CurrentPass1!");
		await user.type(newPassword, "NextPass123!");
		await user.type(confirmPassword, "NextPass123!");
		await user.click(screen.getByRole("button", { name: /change password/i }));

		expect(mocks.mutate).toHaveBeenCalledTimes(1);

		await act(async () => {
			mocks.onError?.({
				response: {
					data: { error_code: ApiErrorCode.InvalidCurrentPassword },
				},
			});
		});

		await waitFor(() => {
			expect(
				screen.getByText("Current password is incorrect."),
			).toBeInTheDocument();
		});

		await user.type(currentPassword, "x");
		await waitFor(() => {
			expect(
				screen.queryByText("Current password is incorrect."),
			).not.toBeInTheDocument();
		});
	});
});
