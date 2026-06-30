import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { Trans, useTranslation } from "react-i18next";

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
	const { t } = useTranslation();
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
					"admin.globalRoles.errRoleNotFound",
				),
				[ApiErrorCode.GlobalRoleHasUsers]: t(
					"admin.globalRoles.errRoleHasUsers",
				),
				[ApiErrorCode.Forbidden]: t("admin.globalRoles.errForbiddenDelete"),
				[ApiErrorCode.InternalError]: t("admin.globalRoles.errServer"),
			};
			const fallback =
				err instanceof Error ? err.message : t("admin.globalRoles.errFallback");
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
					<DialogTitle>{t("admin.globalRoles.deleteRole")}</DialogTitle>
					<DialogDescription className="mt-1 space-y-1">
						<span>
							<Trans
								i18nKey="admin.globalRoles.deleteConfirm"
								values={{ name: role.name }}
								components={{
									name: (
										<span className="font-mono font-semibold text-foreground" />
									),
								}}
							/>
						</span>{" "}
						<span className="font-medium text-foreground">
							{t("admin.globalRoles.cannotUndo")}
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
						{t("admin.globalRoles.cancel")}
					</DialogClose>
					<Button
						variant="destructive"
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending}
					>
						{mutation.isPending
							? t("admin.globalRoles.deleting")
							: t("admin.globalRoles.deleteRole")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
