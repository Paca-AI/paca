import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Loader2, Trash2 } from "lucide-react";
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
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import {
	deleteProjectRole,
	type ProjectRole,
	projectRolesQueryOptions,
} from "@/lib/project-api";

interface DeleteProjectRoleDialogProps {
	projectId: string;
	role: ProjectRole;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function DeleteProjectRoleDialog({
	projectId,
	role,
	open,
	onOpenChange,
}: DeleteProjectRoleDialogProps) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => deleteProjectRole(projectId, role.id),
		onSuccess: () => {
			void queryClient.invalidateQueries({
				queryKey: projectRolesQueryOptions(projectId).queryKey,
			});
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.ProjectRoleNotFound]: t("project.roles.errRoleNotFound"),
				[ApiErrorCode.ProjectRoleHasMembers]: t(
					"project.roles.errRoleHasMembers",
				),
				[ApiErrorCode.Forbidden]: t("project.roles.errForbiddenDelete"),
				[ApiErrorCode.InternalError]: t("project.roles.errServerDelete"),
			};
			const fallback =
				err instanceof Error ? err.message : t("project.roles.errFallback");
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
					<DialogTitle>{t("project.roles.deleteRole")}</DialogTitle>
					<DialogDescription className="mt-1">
						<Trans
							i18nKey="project.roles.deleteConfirm"
							values={{ name: role.role_name }}
							components={{
								name: (
									<span className="font-mono font-semibold text-foreground" />
								),
							}}
						/>
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
						{t("project.roles.cancel")}
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
						{t("project.roles.deleteRole")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
