import { useMutation, useQueryClient } from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { useState } from "react";

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
import { changeMyPassword } from "@/lib/auth-api";

interface ChangePasswordCardProps {
	mustChange: boolean;
}

export function ChangePasswordCard({ mustChange }: ChangePasswordCardProps) {
	const queryClient = useQueryClient();
	const [currentPassword, setCurrentPassword] = useState("");
	const [newPassword, setNewPassword] = useState("");
	const [confirmPassword, setConfirmPassword] = useState("");
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
			void queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
			setCurrentPassword("");
			setNewPassword("");
			setConfirmPassword("");
			setError(null);
			setSuccess(true);
		},
		onError: (err: Error) => {
			setError(err.message ?? "Failed to change password.");
			setSuccess(false);
		},
	});

	return (
		<Card>
			<CardHeader>
				<div className="flex items-center gap-3">
					<div className="flex size-8 items-center justify-center rounded-lg bg-muted">
						<KeyRound className="size-4 text-muted-foreground" />
					</div>
					<div>
						<CardTitle className="text-base">Change Password</CardTitle>
						<CardDescription className="mt-0.5">
							{mustChange
								? "You must set a new password before continuing."
								: "Update your account password."}
						</CardDescription>
					</div>
				</div>
				{mustChange ? (
					<div className="mt-3 rounded-md bg-amber-50 border border-amber-200 px-3 py-2 text-xs text-amber-700 dark:bg-amber-950/30 dark:border-amber-800 dark:text-amber-400">
						A temporary password was set for your account. Please change it now.
					</div>
				) : null}
			</CardHeader>

			<Separator />

			<CardContent className="pt-5">
				<div className="flex flex-col gap-4 max-w-sm">
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="current-password">Current password</Label>
						<Input
							id="current-password"
							type="password"
							value={currentPassword}
							onChange={(e) => setCurrentPassword(e.target.value)}
							autoComplete="current-password"
						/>
					</div>
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="new-password">New password</Label>
						<Input
							id="new-password"
							type="password"
							value={newPassword}
							onChange={(e) => setNewPassword(e.target.value)}
							autoComplete="new-password"
						/>
					</div>
					<div className="flex flex-col gap-1.5">
						<Label htmlFor="confirm-password">Confirm new password</Label>
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
						<p className="text-sm text-green-600 dark:text-green-400">
							Password changed successfully.
						</p>
					) : null}
				</div>
			</CardContent>

			<CardFooter className="border-t pt-4">
				<Button
					size="sm"
					onClick={() => mutation.mutate()}
					disabled={
						mutation.isPending ||
						!currentPassword ||
						!newPassword ||
						!confirmPassword
					}
				>
					{mutation.isPending ? "Updating…" : "Change password"}
				</Button>
			</CardFooter>
		</Card>
	);
}
