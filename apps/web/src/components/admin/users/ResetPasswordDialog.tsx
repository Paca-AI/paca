import { useMutation } from "@tanstack/react-query";
import { Check, Copy, Eye, EyeOff, KeyRound } from "lucide-react";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { resetUserPassword, type User } from "@/lib/admin-api";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { generatePassword } from "@/lib/generate-password";

interface ResetPasswordDialogProps {
	user: User;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function ResetPasswordDialog({
	user,
	open,
	onOpenChange,
}: ResetPasswordDialogProps) {
	const [generatedPassword, setGeneratedPassword] = useState<string | null>(
		null,
	);
	const [revealed, setRevealed] = useState(false);
	const [copied, setCopied] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const handleOpenChange = (next: boolean) => {
		if (!next) {
			setGeneratedPassword(null);
			setRevealed(false);
			setCopied(false);
			setError(null);
		}
		onOpenChange(next);
	};

	const mutation = useMutation({
		mutationFn: async () => {
			const pw = generatePassword();
			await resetUserPassword(user.id, pw);
			return pw;
		},
		onSuccess: (pw) => {
			setGeneratedPassword(pw);
			setError(null);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.UserNotFound]: "This user no longer exists.",
				[ApiErrorCode.Forbidden]: "You don't have permission to reset this user's password.",
				[ApiErrorCode.InternalError]: "Something went wrong. Please try again.",
			};
			const fallback = err instanceof Error ? err.message : "Something went wrong.";
			setError((code && messages[code]) ?? fallback);
		},
	});

	const handleCopy = () => {
		if (!generatedPassword) return;
		navigator.clipboard.writeText(generatedPassword);
		setCopied(true);
	};

	useEffect(() => {
		if (!copied) return;
		const t = setTimeout(() => setCopied(false), 2000);
		return () => clearTimeout(t);
	}, [copied]);

	return (
		<Dialog open={open} onOpenChange={handleOpenChange}>
			<DialogContent className="sm:max-w-sm">
				<DialogHeader>
					<div className="flex items-center gap-2.5">
						<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-amber-100 text-amber-600 dark:bg-amber-900/30 dark:text-amber-400">
							<KeyRound className="size-4" />
						</div>
						<DialogTitle className="text-base">Reset password</DialogTitle>
					</div>
					<DialogDescription className="mt-2">
						{generatedPassword ? (
							<>Password for <span className="font-mono font-semibold text-foreground">{user.username}</span> has been reset. Share the temporary password below — it will not be shown again.</>
						) : (
							<>A strong temporary password will be generated and assigned to <span className="font-mono font-semibold text-foreground">{user.username}</span>. They will be required to change it on next login.</>
						)}
					</DialogDescription>
				</DialogHeader>

				{generatedPassword ? (
					<div className="flex flex-col gap-3 py-1">
						<div className="flex flex-col gap-1.5">
							<Label className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
								Temporary Password
							</Label>
							<div className="flex gap-2">
								<Input
									readOnly
									type={revealed ? "text" : "password"}
									value={generatedPassword}
									className="font-mono"
								/>
								<Button
									variant="outline"
									size="icon"
									onClick={() => setRevealed((v) => !v)}
									aria-label={revealed ? "Hide password" : "Show password"}
								>
									{revealed ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
								</Button>
								<Button
									variant="outline"
									size="icon"
									onClick={handleCopy}
									aria-label="Copy password"
								>
									{copied ? <Check className="size-4 text-green-500" /> : <Copy className="size-4" />}
								</Button>
							</div>
						</div>
						<p className="text-xs text-muted-foreground">
							⚠ This password will not be shown again after closing.
						</p>
					</div>
				) : error ? (
					<div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
						<span className="shrink-0">⚠</span>
						<span>{error}</span>
					</div>
				) : null}

				<DialogFooter>
					{generatedPassword ? (
						<Button onClick={() => handleOpenChange(false)}>Done</Button>
					) : (
						<>
							<Button variant="outline" onClick={() => handleOpenChange(false)}>Cancel</Button>
							<Button
								onClick={() => mutation.mutate()}
								disabled={mutation.isPending}
							>
								{mutation.isPending ? "Resetting…" : "Reset password"}
							</Button>
						</>
					)}
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
