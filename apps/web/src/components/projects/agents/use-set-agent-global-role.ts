import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { roleErrorKey } from "@/components/roles/role-errors";
import { type Role, replaceAgentRoles } from "@/lib/role-api";

interface UseSetAgentGlobalRoleOptions {
	/** Called with the roles the agent holds once they are changed. */
	onChanged?: (roles: Role[]) => void;
}

/**
 * The one request behind every "give this global agent roles, or take them
 * away" surface: the agent's Roles tab and the last step of the create wizard.
 * It owns the failure message so both word it the same way, the counterpart of
 * useAssignUserRole for users.
 *
 * `roleIds` is the complete set of roles the agent should hold; an empty set
 * leaves it with no permissions at all.
 */
export function useSetAgentGlobalRole({
	onChanged,
}: UseSetAgentGlobalRoleOptions = {}) {
	const { t } = useTranslation("roles");
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: ({
			agentId,
			roleIds,
		}: {
			agentId: string;
			roleIds: string[];
		}) => replaceAgentRoles(agentId, roleIds),
		onSuccess: (roles) => {
			setError(null);
			onChanged?.(roles);
		},
		onError: (err: unknown) => {
			setError(t(`errors.${roleErrorKey(err)}` as never));
		},
	});

	return {
		setRoles: (agentId: string, roleIds: string[]) =>
			mutation.mutate({ agentId, roleIds }),
		isPending: mutation.isPending,
		error,
		clearError: () => setError(null),
	};
}
