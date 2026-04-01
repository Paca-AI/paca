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

export function ChangePasswordForm({ onSuccess }: ChangePasswordFormProps) {
	const queryClient = useQueryClient();
	const isDark = useIsDark();
	const [currentPassword, setCurrentPassword] = useState("");
	const [newPassword, setNewPassword] = useState("");
	const [confirmPassword, setConfirmPassword] = useState("");
	const [showCurrentPassword, setShowCurrentPassword] = useState(false);
	const [showNewPassword, setShowNewPassword] = useState(false);
	const [showConfirmPassword, setShowConfirmPassword] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [success, setSuccess] = useState(false);

	const mutation = useMutation({
		mutationFn: async () => {
			if (newPassword.length < 8)
				throw new Error("New password must be at least 8 characters.");
			if (newPassword !== confirmPassword)
				throw new Error("Passwords do not match.");
			return changeMyPassword(currentPassword, newPassword);
		},
		onSuccess: () => {
			setCurrentPassword("");
			setNewPassword("");
			setConfirmPassword("");
			setError(null);

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
			setError((code && messages[code]) ?? fallback);
			setSuccess(false);
		},
	});

	return (
		<form
			onSubmit={(event) => {
				event.preventDefault();
				event.stopPropagation();
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
						onChange={(e) => setCurrentPassword(e.target.value)}
						autoComplete="current-password"
						placeholder="••••••••"
						className="h-10 pr-10"
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
						onChange={(e) => setNewPassword(e.target.value)}
						autoComplete="new-password"
						placeholder="••••••••"
						className="h-10 pr-10"
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
						onChange={(e) => setConfirmPassword(e.target.value)}
						autoComplete="new-password"
						placeholder="••••••••"
						className="h-10 pr-10"
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
			</div>

			{error ? <p className="text-sm text-destructive">{error}</p> : null}
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
				disabled={
					mutation.isPending ||
					!currentPassword ||
					!newPassword ||
					!confirmPassword
				}
			>
				{mutation.isPending ? "Updating…" : "Change password"}
			</button>
		</form>
	);
}
