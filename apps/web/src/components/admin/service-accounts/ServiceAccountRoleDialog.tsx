import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { isFullAccessRole } from "@/components/admin/global-roles/role-picker";
import { useRoleChoices } from "@/components/admin/global-roles/use-role-choices";
import { roleErrorKey } from "@/components/roles/role-errors";
import { InlineNotice } from "@/components/shared/inline-notice";
import { RoleChangeDialog } from "@/components/shared/role-change-dialog";
import { serviceAccountAvatar } from "@/lib/provider-logos";
import {
	replaceServiceAccountRoles,
	type ServiceAccount,
	serviceAccountsKey,
} from "@/lib/service-account-api";

interface ServiceAccountRoleDialogProps {
	account: ServiceAccount;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

/**
 * Changes a service account's workspace roles in the same dialog as a user's
 * (see RoleChangeDialog), from the accounts list and from the Roles tab of its
 * detail page. A new account holds none, so it can do nothing until someone
 * grants it roles (`roles:assign`); unticking every role removes them all.
 */
export function ServiceAccountRoleDialog({
	account,
	open,
	onOpenChange,
}: ServiceAccountRoleDialogProps) {
	const { t } = useTranslation("admin");
	const { t: tr } = useTranslation("roles");
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	// The account carries the roles it holds (the list and detail both load it).
	const currentIds = account.roles.map((r) => r.id);
	const { roles, knownRoles, remote, status } = useRoleChoices(
		open,
		currentIds,
	);

	const mutation = useMutation({
		meta: { success: "updated", subject: "serviceAccountRoles" },
		mutationFn: (roleIds: string[]) =>
			replaceServiceAccountRoles(account.id, roleIds),
		onSuccess: () => {
			setError(null);
			// The roles shown on the list and on the detail page come from the
			// account itself, so refresh those with the account's own role set.
			void queryClient.invalidateQueries({
				queryKey: serviceAccountsKey,
			});
			onOpenChange(false);
		},
		onError: (err: unknown) =>
			setError(tr(`errors.${roleErrorKey(err)}` as never)),
	});

	return (
		<RoleChangeDialog
			open={open}
			onOpenChange={(next) => {
				if (!next) setError(null);
				onOpenChange(next);
			}}
			title={t("serviceAccounts.roleDialog.title")}
			description={t("serviceAccounts.roleDialog.description", {
				name: account.name,
			})}
			person={{ avatar: serviceAccountAvatar(account) }}
			roles={roles}
			knownRoles={knownRoles}
			remote={remote}
			status={status}
			currentIds={currentIds}
			saveLabel={t("serviceAccounts.roleDialog.assign")}
			savingLabel={t("serviceAccounts.roleDialog.assigning")}
			cancelLabel={t("serviceAccounts.formDialog.cancel")}
			isPending={mutation.isPending}
			error={error}
			onPick={() => setError(null)}
			onSave={(ids) => mutation.mutate(ids)}
			warnings={({ roles: picked }) =>
				picked.some(
					(r) => !currentIds.includes(r.id) && isFullAccessRole(r),
				) ? (
					<InlineNotice tone="warning">
						{t("serviceAccounts.roleDialog.fullAccessWarning")}
					</InlineNotice>
				) : null
			}
		/>
	);
}
