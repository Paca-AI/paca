import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
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
	platformRolesQueryOptions,
	type Role,
} from "@/lib/role-api";

interface DeleteRoleDialogProps {
	role: Role;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function DeleteRoleDialog({
	role,
	open,
	onOpenChange,
}: DeleteRoleDialogProps) {
	const { t } = useTranslation("admin");
	const { t: tr } = useTranslation("roles");
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => deleteRole(role.id),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: platformRolesQueryOptions.queryKey,
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
					<DialogTitle>{t("globalRoles.deleteDialog.title")}</DialogTitle>
					<DialogDescription className="mt-1 space-y-1">
						<span>
							{t("globalRoles.deleteDialog.confirmTextPrefix")}{" "}
							<span className="font-mono font-semibold text-foreground">
								{role.name}
							</span>
							{t("globalRoles.deleteDialog.confirmTextSuffix")}
						</span>{" "}
						<span className="font-medium text-foreground">
							{t("globalRoles.deleteDialog.cannotBeUndone")}
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
						{t("globalRoles.deleteDialog.cancel")}
					</DialogClose>
					<Button
						variant="destructive"
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending}
					>
						{mutation.isPending
							? t("globalRoles.deleteDialog.deleting")
							: t("globalRoles.deleteDialog.confirmButton")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
