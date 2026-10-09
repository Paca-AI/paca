import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Star } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { isFullAccessRole } from "@/components/admin/global-roles/role-picker";
import { roleErrorKey } from "@/components/roles/role-errors";
import { InlineNotice } from "@/components/shared/inline-notice";
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
	platformRolesQueryOptions,
	type Role,
	setDefaultRole,
} from "@/lib/role-api";

interface SetDefaultRoleDialogProps {
	role: Role;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

/**
 * Confirms making a role the default. The star on a row would be one click, but
 * the effect isn't obvious from it: from now on every new user and every new
 * global agent starts with this role, so say so (and flag a full-access role).
 */
export function SetDefaultRoleDialog({
	role,
	open,
	onOpenChange,
}: SetDefaultRoleDialogProps) {
	const { t } = useTranslation("admin");
	const { t: tr } = useTranslation("roles");
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: () => setDefaultRole(role.id),
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
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<div className="flex items-center gap-2.5">
						<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-amber-100 text-amber-600 dark:bg-amber-900/30 dark:text-amber-400">
							<Star className="size-4" />
						</div>
						<DialogTitle className="text-base">
							{t("globalRoles.defaultDialog.title")}
						</DialogTitle>
					</div>
					<DialogDescription className="mt-2">
						{t("globalRoles.defaultDialog.description", { role: role.name })}
					</DialogDescription>
				</DialogHeader>

				{isFullAccessRole(role) ? (
					<InlineNotice tone="warning">
						{t("globalRoles.defaultDialog.fullAccessWarning")}
					</InlineNotice>
				) : null}
				{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}

				<DialogFooter>
					<DialogClose render={<Button variant="outline" />}>
						{t("globalRoles.defaultDialog.cancel")}
					</DialogClose>
					<Button
						onClick={() => mutation.mutate()}
						disabled={mutation.isPending}
					>
						{mutation.isPending
							? t("globalRoles.defaultDialog.confirming")
							: t("globalRoles.defaultDialog.confirm")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
