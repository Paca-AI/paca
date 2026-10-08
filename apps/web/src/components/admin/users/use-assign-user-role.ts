import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { roleErrorKey } from "@/components/roles/role-errors";
import { usersQueryOptions } from "@/lib/admin-api";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { type Role, replaceUserRoles } from "@/lib/role-api";

interface UseAssignUserRoleOptions {
	userId: string;
	/** The roles are being changed on the signed-in person's own account. */
	isSelf?: boolean;
	onAssigned?: (roles: Role[]) => void;
}

/**
 * The one request behind every "give this user roles" surface: the dialog on
 * the users table and the step shown right after an account is created. It
 * replaces the user's whole set of roles, and owns the failure message so both
 * word it the same way.
 */
export function useAssignUserRole({
	userId,
	isSelf = false,
	onAssigned,
}: UseAssignUserRoleOptions) {
	const { t } = useTranslation("admin");
	const { t: tr } = useTranslation("roles");
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: (roleIds: string[]) => replaceUserRoles(userId, roleIds),
		onSuccess: (roles) => {
			setError(null);
			void queryClient.invalidateQueries({
				queryKey: usersQueryOptions().queryKey.slice(0, 2),
			});
			if (isSelf) {
				// Your own roles decide what you may do next: refresh who you are and
				// what you can do (["auth", "me"] also covers the permissions list).
				void queryClient.invalidateQueries({
					queryKey: currentUserQueryOptions.queryKey,
				});
			}
			onAssigned?.(roles);
		},
		onError: (err: unknown) => {
			if (getApiErrorCode(err) === ApiErrorCode.UserNotFound) {
				setError(t("users.formDialog.errors.userNotFound"));
				return;
			}
			setError(tr(`errors.${roleErrorKey(err)}` as never));
		},
	});

	return {
		assign: (roleIds: string[]) => mutation.mutate(roleIds),
		isPending: mutation.isPending,
		error,
		clearError: () => setError(null),
	};
}
