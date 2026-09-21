import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

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
	const { t } = useTranslation("admin");
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
				[ApiErrorCode.GlobalRoleNotFound]: t(
					"globalRoles.deleteDialog.errors.roleNotFound",
				),
				[ApiErrorCode.GlobalRoleHasUsers]: t(
					"globalRoles.deleteDialog.errors.hasUsersWarning",
				),
				[ApiErrorCode.GlobalRoleIsDefault]: t(
					"globalRoles.deleteDialog.errors.isDefault",
				),
				[ApiErrorCode.Forbidden]: t(
					"globalRoles.deleteDialog.errors.forbidden",
				),
				[ApiErrorCode.InternalError]: t(
					"globalRoles.deleteDialog.errors.internalError",
				),
			};
			const fallback =
				err instanceof Error
					? err.message
					: t("globalRoles.deleteDialog.errors.generic");
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
