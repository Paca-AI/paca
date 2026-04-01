import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { deleteUser, type User, usersQueryOptions } from "@/lib/admin-api";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";

interface DeleteUserDialogProps {
	user: User;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function DeleteUserDialog({
	user,
	open,
	onOpenChange,
}: DeleteUserDialogProps) {
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => deleteUser(user.id),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: usersQueryOptions().queryKey.slice(0, 2),
			});
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.UserNotFound]: "This user no longer exists.",
				[ApiErrorCode.Forbidden]:
					"You don't have permission to delete this user.",
				[ApiErrorCode.InternalError]: "Something went wrong. Please try again.",
			};
			const fallback =
				err instanceof Error ? err.message : "Something went wrong.";
			setError((code && messages[code]) ?? fallback);
		},
	});

	return (
		<Dialog
			open={open}
			onOpenChange={(next) => {
				if (!next) setError(null);
				onOpenChange(next);
			}}
		>
			<DialogContent className="sm:max-w-sm">
				<DialogHeader>
					<div className="mb-1 flex size-9 items-center justify-center rounded-lg bg-destructive/10">
						<Trash2 className="size-4 text-destructive" />
					</div>
					<DialogTitle>Delete user</DialogTitle>
					<DialogDescription className="mt-1 space-y-1">
						<span>
							Are you sure you want to delete{" "}
							<span className="font-mono font-semibold text-foreground">
								{user.username}
							</span>
							? Their account will be permanently removed.
						</span>{" "}
						<span className="font-medium text-foreground">
							This action cannot be undone.
						</span>
					</DialogDescription>
				</DialogHeader>
				{error ? (
					<div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
						<span className="shrink-0">⚠</span>
						<span>{error}</span>
					</div>
				) : null}
				<DialogFooter>
					<DialogClose render={<Button variant="outline" />}>
						Cancel
					</DialogClose>
					<Button
						variant="destructive"
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending}
					>
						{mutation.isPending ? "Deleting…" : "Delete user"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
