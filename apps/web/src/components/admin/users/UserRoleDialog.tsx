import { ShieldCheck } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
	isFullAccessRole,
	RolePicker,
} from "@/components/admin/global-roles/role-picker";
import { useAssignUserRole } from "@/components/admin/users/use-assign-user-role";
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
import type { GlobalRole, User } from "@/lib/admin-api";

interface UserRoleDialogProps {
	user: User;
	/** The signed-in person is changing their own role. */
	isSelf?: boolean;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

/**
 * Changes a user's global role. It is a dialog of its own, apart from editing
 * the profile, because it is a different privilege (`global_roles.assign`) and
 * a different kind of decision: it changes what the person may do.
 */
export function UserRoleDialog({
	user,
	isSelf = false,
	open,
	onOpenChange,
}: UserRoleDialogProps) {
	const { t } = useTranslation("admin");
	const [selected, setSelected] = useState<GlobalRole | null>(null);

	const handleOpenChange = (next: boolean) => {
		if (!next) {
			setSelected(null);
			clearError();
		}
		onOpenChange(next);
	};

	const { assign, isPending, error, clearError } = useAssignUserRole({
		userId: user.id,
		isSelf,
		onAssigned: () => handleOpenChange(false),
	});

	// Picking the role they already hold changes nothing.
	const change = selected && selected.name !== user.role ? selected : null;

	return (
		<Dialog open={open} onOpenChange={handleOpenChange}>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<div className="flex items-center gap-2.5">
						<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
							<ShieldCheck className="size-4" />
						</div>
						<DialogTitle className="text-base">
							{t("users.roleDialog.title")}
						</DialogTitle>
					</div>
					<DialogDescription className="mt-2">
						{t("users.roleDialog.description", { username: user.username })}
					</DialogDescription>
				</DialogHeader>

				<div className="flex flex-col gap-3 py-1">
					<RolePicker
						label={t("users.roleDialog.title")}
						value={selected?.id ?? null}
						onChange={(role) => {
							setSelected(role);
							clearError();
						}}
						currentRoleName={user.role}
						disabled={isPending}
					/>
					{change && isFullAccessRole(change) ? (
						<InlineNotice tone="warning">
							{t("users.roleDialog.fullAccessWarning")}
						</InlineNotice>
					) : null}
					{change && isSelf ? (
						<InlineNotice tone="warning">
							{t("users.roleDialog.selfWarning")}
						</InlineNotice>
					) : null}
					{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
				</div>

				<DialogFooter>
					<DialogClose render={<Button variant="outline" />}>
						{t("users.formDialog.cancel")}
					</DialogClose>
					<Button
						onClick={() => change && assign(change)}
						disabled={!change || isPending}
					>
						{isPending
							? t("users.roleDialog.assigning")
							: t("users.roleDialog.assign")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
