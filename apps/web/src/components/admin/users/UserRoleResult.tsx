import { Check } from "lucide-react";
import { useTranslation } from "react-i18next";

import { useAssignUserRole } from "@/components/admin/users/use-assign-user-role";
import { InlineNotice } from "@/components/shared/inline-notice";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import type { GlobalRole, User } from "@/lib/admin-api";

interface UserRoleResultProps {
	/** The account that was just created, with the role it actually holds. */
	user: User;
	/** The non-default role that was picked for it, if any. */
	role: GlobalRole | null;
	/** Whether `role` has been assigned. */
	assigned: boolean;
	onAssigned: () => void;
}

/**
 * Where the new account's role ended up, shown beside its one-time password.
 * The role is a separate request from creating the account, so it can fail on
 * its own: then the account exists (and its password is on screen), and the
 * role can be retried from here or assigned later from the users table.
 */
export function UserRoleResult({
	user,
	role,
	assigned,
	onAssigned,
}: UserRoleResultProps) {
	const { t } = useTranslation("admin");
	const { assign, isPending, error } = useAssignUserRole({
		userId: user.id,
		onAssigned,
	});

	const held = role && assigned ? role.name : user.role;
	const failed = role !== null && !assigned;

	return (
		<section className="flex flex-col gap-2 border-t pt-4">
			<Label className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
				{t("users.formDialog.roleStep.title")}
			</Label>
			<div className="flex flex-wrap items-center gap-x-2 gap-y-1">
				<span className="inline-flex items-center rounded-full border px-2 py-0.5 font-mono text-xs font-medium leading-none text-foreground/80">
					{held}
				</span>
				{role && assigned ? (
					<span className="inline-flex items-center gap-1 text-xs text-emerald-600 dark:text-emerald-400">
						<Check className="size-3.5" aria-hidden="true" />
						{t("users.formDialog.roleStep.assigned")}
					</span>
				) : null}
			</div>
			{failed ? (
				<>
					<InlineNotice tone="warning">
						{t("users.formDialog.roleStep.failed")}
					</InlineNotice>
					{error ? <InlineNotice tone="error">{error}</InlineNotice> : null}
					<div>
						<Button
							variant="outline"
							size="sm"
							onClick={() => assign(role)}
							disabled={isPending}
						>
							{isPending
								? t("users.roleDialog.assigning")
								: t("users.formDialog.roleStep.retry")}
						</Button>
					</div>
				</>
			) : null}
		</section>
	);
}
