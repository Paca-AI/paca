import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { roleErrorKey } from "@/components/roles/role-errors";
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
	deleteRole,
	projectRolesQueryOptions,
	type Role,
} from "@/lib/role-api";

interface DeleteProjectRoleDialogProps {
	projectId: string;
	role: Role;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function DeleteProjectRoleDialog({
	projectId,
	role,
	open,
	onOpenChange,
}: DeleteProjectRoleDialogProps) {
	const { t } = useTranslation("projects");
	const { t: tr } = useTranslation("roles");
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => deleteRole(role.id, projectId),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: projectRolesQueryOptions(projectId).queryKey,
			});
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			setError(tr(`errors.${roleErrorKey(err)}` as never));
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
					<DialogTitle>{t("roles.deleteDialog.title")}</DialogTitle>
					<DialogDescription className="mt-1">
						{t("roles.deleteDialog.confirmTextPrefix")}{" "}
						<span className="font-mono font-semibold text-foreground">
							{role.name}
						</span>
						{t("roles.deleteDialog.confirmTextSuffix")}
					</DialogDescription>
				</DialogHeader>

				{error ? (
					<div className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
						<span className="shrink-0">⚠</span>
						<span>{error}</span>
					</div>
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
						{t("roles.deleteDialog.cancel")}
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
						{t("roles.deleteDialog.deleteRole")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
