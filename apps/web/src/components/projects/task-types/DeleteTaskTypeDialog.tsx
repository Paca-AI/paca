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
	deleteTaskType,
	type TaskType,
	taskTypesQueryOptions,
} from "@/lib/project-api";

interface DeleteTaskTypeDialogProps {
	projectId: string;
	taskType: TaskType;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function DeleteTaskTypeDialog({
	projectId,
	taskType,
	open,
	onOpenChange,
}: DeleteTaskTypeDialogProps) {
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => deleteTaskType(projectId, taskType.id),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: taskTypesQueryOptions(projectId).queryKey,
			});
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.TaskTypeNotFound) {
				onOpenChange(false);
				return;
			}
			setError("Failed to delete task type. Please try again.");
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
					<DialogTitle>Delete task type</DialogTitle>
					<DialogDescription>
						Delete{" "}
						<span className="font-semibold text-foreground">
							&ldquo;{taskType.name}&rdquo;
						</span>
						? Tasks using this type will lose their type assignment. This action
						cannot be undone.
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
						Delete type
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
