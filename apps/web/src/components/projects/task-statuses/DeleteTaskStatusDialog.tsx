import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
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
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	deleteTaskStatus,
	type TaskStatus,
	taskStatusesQueryOptions,
} from "@/lib/project-api";

interface DeleteTaskStatusDialogProps {
	projectId: string;
	status: TaskStatus;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function DeleteTaskStatusDialog({
	projectId,
	status,
	open,
	onOpenChange,
}: DeleteTaskStatusDialogProps) {
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => deleteTaskStatus(projectId, status.id),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: taskStatusesQueryOptions(projectId).queryKey,
			});
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.TaskStatusNotFound) {
				onOpenChange(false);
				return;
			}
			setError("Failed to delete status. Please try again.");
		},
	});

	return (
		<Dialog
			open={open}
			onOpenChange={(o) => {
				onOpenChange(o);
				if (!o) setError(null);
			}}
		>
			<DialogContent className="sm:max-w-sm">
				<DialogHeader>
					<div className="flex size-10 items-center justify-center rounded-full bg-destructive/10 mb-2">
						<Trash2 className="size-5 text-destructive" />
					</div>
					<DialogTitle>Delete status</DialogTitle>
					<DialogDescription>
						Delete{" "}
						<span className="font-semibold text-foreground">
							&ldquo;{status.name}&rdquo;
						</span>
						? Tasks using this status will lose their status. This action cannot
						be undone.
					</DialogDescription>
				</DialogHeader>

				{error ? (
					<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
						{error}
					</p>
				) : null}

				<DialogFooter>
					<DialogClose
						render={
							<Button
								variant="outline"
								size="sm"
								disabled={mutation.isPending}
							/>
						}
					>
						Cancel
					</DialogClose>
					<Button
						variant="destructive"
						size="sm"
						disabled={mutation.isPending}
						onClick={() => mutation.mutate()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : (
							<Trash2 className="size-3.5" />
						)}
						Delete status
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
