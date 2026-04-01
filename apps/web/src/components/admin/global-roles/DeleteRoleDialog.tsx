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
import {
	deleteGlobalRole,
	type GlobalRole,
	globalRolesQueryOptions,
} from "@/lib/admin-api";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";

interface DeleteRoleDialogProps {
	role: GlobalRole;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function DeleteRoleDialog({
	role,
	open,
	onOpenChange,
}: DeleteRoleDialogProps) {
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => deleteGlobalRole(role.id),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: globalRolesQueryOptions.queryKey,
			});
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.GlobalRoleNotFound]: "This role no longer exists.",
				[ApiErrorCode.GlobalRoleHasUsers]:
					"This role cannot be deleted because it is still assigned to one or more users.",
				[ApiErrorCode.Forbidden]:
					"You don't have permission to delete this role.",
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
					<DialogTitle>Delete role</DialogTitle>
					<DialogDescription className="mt-1 space-y-1">
						<span>
							Are you sure you want to delete{" "}
							<span className="font-mono font-semibold text-foreground">
								{role.name}
							</span>
							? This will also remove all user assignments for this role.
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
						{mutation.isPending ? "Deleting…" : "Delete role"}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
