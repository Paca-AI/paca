import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import type { GlobalRole } from "@/lib/admin-api";
import {
	type Agent,
	clearGlobalAgentRole,
	setGlobalAgentRole,
} from "@/lib/agent-api";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";

interface UseSetAgentGlobalRoleOptions {
	/** Called with the agent as the server returns it, once its role is changed. */
	onChanged?: (agent: Agent) => void;
}

/**
 * The one request behind every "give this global agent a role, or take it
 * away" surface: the agent's Global role tab and the last step of the create
 * wizard. It owns the failure message so both word it the same way, the
 * counterpart of useAssignUserRole for users.
 *
 * `role` is the role to bind, or `null` to unbind the agent from its role.
 */
export function useSetAgentGlobalRole({
	onChanged,
}: UseSetAgentGlobalRoleOptions = {}) {
	const { t } = useTranslation("projects");
	const [error, setError] = useState<string | null>(null);

	const mutation = useMutation({
		mutationFn: ({
			agentId,
			role,
		}: {
			agentId: string;
			role: GlobalRole | null;
		}) =>
			role
				? setGlobalAgentRole(agentId, role.id)
				: clearGlobalAgentRole(agentId),
		onSuccess: (agent) => {
			setError(null);
			onChanged?.(agent);
		},
		onError: (err: unknown) => {
			const code = getApiErrorCode(err);
			const messages: Partial<Record<string, string>> = {
				[ApiErrorCode.GlobalRoleNotFound]: t(
					"agents.detail.globalRole.errors.roleNotFound",
				),
				[ApiErrorCode.Forbidden]: t(
					"agents.detail.globalRole.errors.forbidden",
				),
			};
			setError(
				(code && messages[code]) ??
					t("agents.detail.globalRole.errors.generic"),
			);
		},
	});

	return {
		setRole: (agentId: string, role: GlobalRole | null) =>
			mutation.mutate({ agentId, role }),
		isPending: mutation.isPending,
		error,
		clearError: () => setError(null),
	};
}
