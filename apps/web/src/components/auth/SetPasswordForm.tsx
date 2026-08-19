import { useMutation } from "@tanstack/react-query";
import { Eye, EyeOff } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { setPasswordWithToken } from "@/lib/auth-api";
import {
	validateConfirmPassword,
	validateNewPassword,
} from "@/lib/auth-validation";
import { cn } from "@/lib/utils";

interface SetPasswordFormProps {
	token: string;
	onSuccess: () => void;
	onInvalidToken: () => void;
}

type Field = "newPassword" | "confirmPassword";
type TouchedState = Record<Field, boolean>;

const initialTouchedState: TouchedState = {
	newPassword: false,
	confirmPassword: false,
};

export function SetPasswordForm({
	token,
	onSuccess,
	onInvalidToken,
}: SetPasswordFormProps) {
	const { t } = useTranslation("auth");
	const { t: tCommon } = useTranslation("common");
	const [newPassword, setNewPassword] = useState("");
	const [confirmPassword, setConfirmPassword] = useState("");
	const [showNewPassword, setShowNewPassword] = useState(false);
	const [showConfirmPassword, setShowConfirmPassword] = useState(false);
	const [touched, setTouched] = useState<TouchedState>(initialTouchedState);
	const [formError, setFormError] = useState<string | null>(null);

	const newPasswordError = validateNewPassword(newPassword, undefined, tCommon);
	const confirmPasswordError = validateConfirmPassword(
		confirmPassword,
		newPassword,
		tCommon,
	);
	const hasValidationErrors = Boolean(newPasswordError || confirmPasswordError);

	function setFieldTouched(field: Field) {
		setTouched((current) =>
			current[field] ? current : { ...current, [field]: true },
		);
	}

	const mutation = useMutation({
		mutationFn: async () => {
			if (hasValidationErrors) {
				throw new Error(newPasswordError || confirmPasswordError);
			}
			return setPasswordWithToken(token, newPassword);
		},
		onSuccess: () => {
			setFormError(null);
			onSuccess();
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.PasswordSetTokenInvalid) {
				onInvalidToken();
				return;
			}
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.InternalError]: t("setPassword.errors.serverError"),
			};
			const fallback =
				err instanceof Error
					? err.message
					: t("setPassword.errors.genericFailed");
			setFormError((code && messages[code]) ?? fallback);
		},
	});

	return (
		<form
			onSubmit={(event) => {
				event.preventDefault();
				event.stopPropagation();
				setFormError(null);
				setTouched({ newPassword: true, confirmPassword: true });
				if (hasValidationErrors) return;
				mutation.mutate();
			}}
			className="space-y-5"
		>
			<div className="space-y-1.5">
				<Label
					htmlFor="new-password"
					className="text-xs font-semibold uppercase tracking-wide text-(--sea-ink)"
				>
					{t("setPassword.fields.newPassword")}
				</Label>
				<div className="relative">
					<Input
						id="new-password"
						type={showNewPassword ? "text" : "password"}
						value={newPassword}
						onChange={(e) => {
							setNewPassword(e.target.value);
							setFormError(null);
						}}
						onBlur={() => setFieldTouched("newPassword")}
						autoComplete="new-password"
						autoFocus
						placeholder={t("setPassword.fields.passwordPlaceholder")}
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
							showNewPassword
								? t("setPassword.aria.hideNewPassword")
								: t("setPassword.aria.showNewPassword")
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
						{t("setPassword.fields.newPasswordHint")}
					</p>
				)}
			</div>

			<div className="space-y-1.5">
				<Label
					htmlFor="confirm-password"
					className="text-xs font-semibold uppercase tracking-wide text-(--sea-ink)"
				>
					{t("setPassword.fields.confirmPassword")}
				</Label>
				<div className="relative">
					<Input
						id="confirm-password"
						type={showConfirmPassword ? "text" : "password"}
						value={confirmPassword}
						onChange={(e) => {
							setConfirmPassword(e.target.value);
							setFormError(null);
						}}
						onBlur={() => setFieldTouched("confirmPassword")}
						autoComplete="new-password"
						placeholder={t("setPassword.fields.passwordPlaceholder")}
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
								? t("setPassword.aria.hideConfirmPassword")
								: t("setPassword.aria.showConfirmPassword")
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

			<button
				type="submit"
				className={cn(
					buttonVariants({ size: "lg" }),
					"mt-1 h-11 w-full font-semibold tracking-wide",
				)}
				disabled={mutation.isPending || hasValidationErrors}
			>
				{mutation.isPending
					? t("setPassword.actions.submitting")
					: t("setPassword.actions.submit")}
			</button>
		</form>
	);
}
