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
	const { t } = useTranslation();
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
				[ApiErrorCode.UserNotFound]: t("admin.users.errUserNotFound"),
				[ApiErrorCode.Forbidden]: t("admin.users.errForbiddenDelete"),
				[ApiErrorCode.InternalError]: t("admin.users.errServer"),
			};
			const fallback =
				err instanceof Error ? err.message : t("admin.users.errFallback");
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
					<DialogTitle>{t("admin.users.deleteUser")}</DialogTitle>
					<DialogDescription className="mt-1 space-y-1">
						<span>
							<Trans
								i18nKey="admin.users.deleteConfirm"
								values={{ username: user.username }}
								components={{
									name: (
										<span className="font-mono font-semibold text-foreground" />
									),
								}}
							/>
						</span>{" "}
						<span className="font-medium text-foreground">
							{t("admin.users.cannotUndo")}
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
						{t("admin.users.cancel")}
					</DialogClose>
					<Button
						variant="destructive"
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending}
					>
						{mutation.isPending
							? t("admin.users.deleting")
							: t("admin.users.deleteUser")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
