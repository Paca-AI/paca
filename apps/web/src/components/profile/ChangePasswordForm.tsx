import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";

import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useIsDark } from "@/hooks/use-is-dark";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { changeMyPassword } from "@/lib/auth-api";
import { cn } from "@/lib/utils";

interface ChangePasswordFormProps {
	/** When true, hides the success message — the parent will unmount the form on success. */
	onSuccess?: () => void;
}

type ChangePasswordField =
	| "currentPassword"
	| "newPassword"
	| "confirmPassword";

type TouchedState = Record<ChangePasswordField, boolean>;

const initialTouchedState: TouchedState = {
	currentPassword: false,
	newPassword: false,
	confirmPassword: false,
};

function validateCurrentPassword(value: string) {
	if (!value) {
		return "Current password is required.";
	}

	return undefined;
}

function validateNewPassword(currentPassword: string, value: string) {
	if (!value) {
		return "New password is required.";
	}

	if (value.length < 8) {
		return "New password must be at least 8 characters.";
	}

	if (value === currentPassword) {
		return "New password must be different from current password.";
	}

	return undefined;
}

function validateConfirmPassword(newPassword: string, value: string) {
	if (!value) {
		return "Please confirm your new password.";
	}

	if (newPassword !== value) {
		return "Passwords do not match.";
	}

	return undefined;
}

export function ChangePasswordForm({ onSuccess }: ChangePasswordFormProps) {
	const queryClient = useQueryClient();
	const isDark = useIsDark();
	const [currentPassword, setCurrentPassword] = useState("");
	const [newPassword, setNewPassword] = useState("");
	const [confirmPassword, setConfirmPassword] = useState("");
	const [showCurrentPassword, setShowCurrentPassword] = useState(false);
	const [showNewPassword, setShowNewPassword] = useState(false);
	const [showConfirmPassword, setShowConfirmPassword] = useState(false);
	const [touched, setTouched] = useState<TouchedState>(initialTouchedState);
	const [formError, setFormError] = useState<string | null>(null);
	const [currentPasswordServerError, setCurrentPasswordServerError] = useState<
		string | null
	>(null);
	const [success, setSuccess] = useState(false);

	const currentPasswordError =
		currentPasswordServerError ?? validateCurrentPassword(currentPassword);
	const newPasswordError = validateNewPassword(currentPassword, newPassword);
	const confirmPasswordError = validateConfirmPassword(
		newPassword,
		confirmPassword,
	);
	const hasValidationErrors = Boolean(
		currentPasswordError || newPasswordError || confirmPasswordError,
	);

	function setFieldTouched(field: ChangePasswordField) {
		setTouched((current) =>
			current[field] ? current : { ...current, [field]: true },
		);
	}

	function handleFieldChange(field: ChangePasswordField, value: string) {
		setSuccess(false);
		setFormError(null);

		if (field === "currentPassword") {
			setCurrentPasswordServerError(null);
			setCurrentPassword(value);
			return;
		}

		if (field === "newPassword") {
			setNewPassword(value);
			return;
		}

		setConfirmPassword(value);
	}

	const mutation = useMutation({
		mutationFn: async () => {
			const validationError =
				currentPasswordError || newPasswordError || confirmPasswordError;
			if (validationError) {
				throw new Error(validationError);
			}

			return changeMyPassword(currentPassword, newPassword);
		},
		onSuccess: () => {
			setCurrentPassword("");
			setNewPassword("");
			setConfirmPassword("");
			setTouched(initialTouchedState);
			setFormError(null);
			setCurrentPasswordServerError(null);

			if (onSuccess) {
				// Caller owns post-success side effects (cache update + navigation).
				onSuccess();
			} else {
				// Standalone usage (profile page): just invalidate so the card reflects the change.
				void queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
				setSuccess(true);
			}
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.InvalidCurrentPassword]: "Current password is incorrect.",
				[ApiErrorCode.Unauthenticated]:
					"Your session has expired. Please log in again.",
				[ApiErrorCode.InternalError]:
					"Something went wrong on the server. Please try again.",
			};
			const fallback =
				err instanceof Error ? err.message : "Failed to change password.";

			if (code === ApiErrorCode.InvalidCurrentPassword) {
				setCurrentPasswordServerError(messages[code] ?? fallback);
				setTouched((current) => ({ ...current, currentPassword: true }));
				setFormError(null);
			} else {
				setCurrentPasswordServerError(null);
				setFormError((code && messages[code]) ?? fallback);
			}

			setSuccess(false);
		},
	});

	return (
		<form
			onSubmit={(event) => {
				event.preventDefault();
				event.stopPropagation();
				setSuccess(false);
				setFormError(null);
				setTouched({
					currentPassword: true,
					newPassword: true,
					confirmPassword: true,
				});

				if (hasValidationErrors) {
					return;
				}

				mutation.mutate();
			}}
			className="space-y-5"
		>
			<div className="space-y-1.5">
				<Label
					htmlFor="current-password"
					className="text-xs font-semibold uppercase tracking-wide text-(--sea-ink)"
				>
					Current password
				</Label>
				<div className="relative">
					<Input
						id="current-password"
						type={showCurrentPassword ? "text" : "password"}
						value={currentPassword}
						onChange={(e) =>
							handleFieldChange("currentPassword", e.target.value)
						}
						onBlur={() => setFieldTouched("currentPassword")}
						autoComplete="current-password"
						placeholder="••••••••"
						aria-invalid={touched.currentPassword && !!currentPasswordError}
						aria-describedby={
							touched.currentPassword && currentPasswordError
								? "current-password-error"
								: undefined
						}
						className={cn(
							"h-10 pr-10",
							touched.currentPassword && currentPasswordError
								? "border-destructive focus-visible:ring-destructive/20"
								: undefined,
						)}
					/>
					<button
						type="button"
						onClick={() => setShowCurrentPassword((current) => !current)}
						className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-(--sea-ink-soft) transition-colors hover:text-(--sea-ink)"
						aria-label={
							showCurrentPassword
								? "Hide current password"
								: "Show current password"
						}
					>
						{showCurrentPassword ? (
							<EyeOff className="size-4" />
						) : (
							<Eye className="size-4" />
						)}
					</button>
				</div>
				{touched.currentPassword && currentPasswordError ? (
					<p
						id="current-password-error"
						role="alert"
						className="mt-1 text-xs text-red-600 dark:text-red-400"
					>
						{currentPasswordError}
					</p>
				) : null}
			</div>

			<div className="space-y-1.5">
				<Label
					htmlFor="new-password"
					className="text-xs font-semibold uppercase tracking-wide text-(--sea-ink)"
				>
					New password
				</Label>
				<div className="relative">
					<Input
						id="new-password"
						type={showNewPassword ? "text" : "password"}
						value={newPassword}
						onChange={(e) => handleFieldChange("newPassword", e.target.value)}
						onBlur={() => setFieldTouched("newPassword")}
						autoComplete="new-password"
						placeholder="••••••••"
						aria-invalid={touched.newPassword && !!newPasswordError}
						aria-describedby={
							touched.newPassword && newPasswordError
								? "new-password-error"
								: undefined
						}
						className={cn(
							"h-10 pr-10",
							touched.newPassword && newPasswordError
								? "border-destructive focus-visible:ring-destructive/20"
								: undefined,
						)}
					/>
					<button
						type="button"
						onClick={() => setShowNewPassword((current) => !current)}
						className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-(--sea-ink-soft) transition-colors hover:text-(--sea-ink)"
						aria-label={
							showNewPassword ? "Hide new password" : "Show new password"
						}
					>
						{showNewPassword ? (
							<EyeOff className="size-4" />
						) : (
							<Eye className="size-4" />
						)}
					</button>
				</div>
				{touched.newPassword && newPasswordError ? (
					<p
						id="new-password-error"
						role="alert"
						className="mt-1 text-xs text-red-600 dark:text-red-400"
					>
						{newPasswordError}
					</p>
				) : (
					<p className="mt-1 text-xs text-(--sea-ink-soft)">
						Use at least 8 characters and choose something different from your
						current password.
					</p>
				)}
			</div>

			<div className="space-y-1.5">
				<Label
					htmlFor="confirm-password"
					className="text-xs font-semibold uppercase tracking-wide text-(--sea-ink)"
				>
					Confirm new password
				</Label>
				<div className="relative">
					<Input
						id="confirm-password"
						type={showConfirmPassword ? "text" : "password"}
						value={confirmPassword}
						onChange={(e) =>
							handleFieldChange("confirmPassword", e.target.value)
						}
						onBlur={() => setFieldTouched("confirmPassword")}
						autoComplete="new-password"
						placeholder="••••••••"
						aria-invalid={touched.confirmPassword && !!confirmPasswordError}
						aria-describedby={
							touched.confirmPassword && confirmPasswordError
								? "confirm-password-error"
								: undefined
						}
						className={cn(
							"h-10 pr-10",
							touched.confirmPassword && confirmPasswordError
								? "border-destructive focus-visible:ring-destructive/20"
								: undefined,
						)}
					/>
					<button
						type="button"
						onClick={() => setShowConfirmPassword((current) => !current)}
						className="absolute right-2.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-(--sea-ink-soft) transition-colors hover:text-(--sea-ink)"
						aria-label={
							showConfirmPassword
								? "Hide confirm password"
								: "Show confirm password"
						}
					>
						{showConfirmPassword ? (
							<EyeOff className="size-4" />
						) : (
							<Eye className="size-4" />
						)}
					</button>
				</div>
				{touched.confirmPassword && confirmPasswordError ? (
					<p
						id="confirm-password-error"
						role="alert"
						className="mt-1 text-xs text-red-600 dark:text-red-400"
					>
						{confirmPasswordError}
					</p>
				) : null}
			</div>

			{formError ? (
				<p className="text-sm text-destructive">{formError}</p>
			) : null}
			{success ? (
				<p className="text-sm text-green-600 dark:text-green-400">
					Password changed successfully.
				</p>
			) : null}

			<button
				type="submit"
				className={cn(
					buttonVariants({ size: "lg" }),
					"mt-1 h-11 w-full font-semibold tracking-wide",
				)}
				style={{
					background: mutation.isPending
						? undefined
						: isDark
							? "linear-gradient(135deg, #4a6cf7 0%, #3352d8 100%)"
							: "linear-gradient(135deg, #2e4980 0%, #1b3360 100%)",
				}}
				disabled={mutation.isPending || hasValidationErrors}
			>
				{mutation.isPending ? "Updating…" : "Change password"}
			</button>
		</form>
	);
}
