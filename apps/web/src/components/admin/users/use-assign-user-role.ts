import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
	assignUserGlobalRole,
	type GlobalRole,
	usersQueryOptions,
} from "@/lib/admin-api";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { currentUserQueryOptions } from "@/lib/auth-api";

interface UseAssignUserRoleOptions {
	userId: string;
	/** The role is being changed on the signed-in person's own account. */
	isSelf?: boolean;
	onAssigned?: (role: GlobalRole) => void;
}

/**
 * The one request behind every "give this user a role" surface: the dialog on
 * the users table and the step shown right after an account is created. It
 * owns the failure message so both word it the same way.
 */
export function useAssignUserRole({
	userId,
	isSelf = false,
	onAssigned,
}: UseAssignUserRoleOptions) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: async (role: GlobalRole) => {
			await assignUserGlobalRole(userId, role.id);
			return role;
		},
		onSuccess: (role) => {
			setError(null);
			void queryClient.invalidateQueries({
				queryKey: usersQueryOptions().queryKey.slice(0, 2),
			});
			if (isSelf) {
				// Your own role decides what you may do next: refresh who you are and
				// what you can do (["auth", "me"] also covers the permissions list).
				void queryClient.invalidateQueries({
					queryKey: currentUserQueryOptions.queryKey,
				});
			}
			onAssigned?.(role);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.GlobalRoleNotFound]: t(
					"users.roleDialog.errors.roleNotFound",
				),
				[ApiErrorCode.UserNotFound]: t("users.formDialog.errors.userNotFound"),
				[ApiErrorCode.Forbidden]: t("users.formDialog.errors.forbidden"),
				[ApiErrorCode.InternalError]: t(
					"users.formDialog.errors.internalError",
				),
			};
			setError(
				(code && messages[code]) ?? t("users.formDialog.errors.generic"),
			);
		},
	});

	return {
		assign: (role: GlobalRole) => mutation.mutate(role),
		isPending: mutation.isPending,
		error,
		clearError: () => setError(null),
	};
}
