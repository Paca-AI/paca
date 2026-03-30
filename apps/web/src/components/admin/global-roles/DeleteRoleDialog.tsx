import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";

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

	const mutation = useMutation({
		mutationFn: () => deleteGlobalRole(role.id),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: globalRolesQueryOptions.queryKey,
			});
			onOpenChange(false);
		},
	});

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
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
