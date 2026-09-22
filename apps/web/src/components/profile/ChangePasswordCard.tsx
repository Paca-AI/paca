import { useMutation, useQueryClient } from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardFooter,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { changeMyPassword } from "@/lib/auth-api";
import {
	validateConfirmPassword,
	validateNewPassword,
} from "@/lib/auth-validation";

interface ChangePasswordCardProps {
	mustChange: boolean;
}

export function ChangePasswordCard({ mustChange }: ChangePasswordCardProps) {
	const { t } = useTranslation("profile");
	const { t: tCommon } = useTranslation("common");
	const queryClient = useQueryClient();
	const [currentPassword, setCurrentPassword] = useState("");
	const [newPassword, setNewPassword] = useState("");
	const [confirmPassword, setConfirmPassword] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [success, setSuccess] = useState(false);

	const mutation = useMutation({
		mutationFn: () => changeMyPassword(currentPassword, newPassword),
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
			setCurrentPassword("");
			setNewPassword("");
			setConfirmPassword("");
			setError(null);
			setSuccess(true);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			const message =
				code === ApiErrorCode.InvalidCurrentPassword
					? t("changePassword.errors.currentPasswordIncorrect")
					: code === ApiErrorCode.Unauthenticated
						? t("changePassword.errors.sessionExpired")
						: code === ApiErrorCode.InternalError
							? t("changePassword.errors.serverError")
							: t("changePassword.errors.changeFailed");
			setError(message);
			setSuccess(false);
		},
	});

	const handleSubmit = () => {
		const lengthError = validateNewPassword(
			newPassword,
			currentPassword,
			tCommon,
		);
		if (lengthError) {
			setError(lengthError);
			setSuccess(false);
			return;
		}
		const confirmError = validateConfirmPassword(
			confirmPassword,
			newPassword,
			tCommon,
		);
		if (confirmError) {
			setError(confirmError);
			setSuccess(false);
			return;
		}
		setError(null);
		mutation.mutate();
	};

	return (
		<Card>
			<CardHeader>
				<div className="flex items-center gap-3">
					<div className="flex size-8 items-center justify-center rounded-lg bg-muted">
						<KeyRound className="size-4 text-muted-foreground" />
					</div>
					<div>
						<CardTitle className="text-base">
							{t("changePassword.title")}
						</CardTitle>
						<CardDescription className="mt-0.5">
							{mustChange
								? t("changePassword.descriptionMustChange")
								: t("changePassword.description")}
						</CardDescription>
					</div>
				</div>
				{mustChange ? (
					<div className="mt-3 rounded-md bg-amber-50 border border-amber-200 px-3 py-2 text-xs text-amber-700 dark:bg-amber-950/30 dark:border-amber-800 dark:text-amber-400">
						{t("changePassword.temporaryPasswordNotice")}
					</div>
				) : null}
			</CardHeader>

			<Separator />

			<CardContent className="pt-5">
				<div className="flex flex-col gap-4 max-w-sm">
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="current-password">
							{t("changePassword.fields.currentPassword")}
						</Label>
						<Input
							id="current-password"
							type="password"
							value={currentPassword}
							onChange={(e) => setCurrentPassword(e.target.value)}
							autoComplete="current-password"
						/>
					</div>
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="new-password">
							{t("changePassword.fields.newPassword")}
						</Label>
						<Input
							id="new-password"
							type="password"
							value={newPassword}
							onChange={(e) => setNewPassword(e.target.value)}
							autoComplete="new-password"
						/>
					</div>
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="confirm-password">
							{t("changePassword.fields.confirmPassword")}
						</Label>
						<Input
							id="confirm-password"
							type="password"
							value={confirmPassword}
							onChange={(e) => setConfirmPassword(e.target.value)}
							autoComplete="new-password"
						/>
					</div>
					{error ? <p className="text-sm text-destructive">{error}</p> : null}
					{success ? (
						<p className="text-sm text-primary">
							{t("changePassword.success")}
						</p>
					) : null}
				</div>
			</CardContent>

			<CardFooter className="border-t pt-4">
				<Button
					size="sm"
					onClick={handleSubmit}
					disabled={
						mutation.isPending ||
						!currentPassword ||
						!newPassword ||
						!confirmPassword
					}
				>
					{mutation.isPending
						? t("changePassword.actions.updating")
						: t("changePassword.actions.submit")}
				</Button>
			</CardFooter>
		</Card>
	);
}
