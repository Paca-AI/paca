import { useQuery } from "@tanstack/react-query";

import { myPermissionsQueryOptions } from "@/lib/admin-api";
import { hasAction, hasAnyAction } from "@/lib/policy/actions";
import { myProjectPermissionsQueryOptions } from "@/lib/project-api";

/** Stable reference so the default doesn't change identity on every render. */
const NO_ACTIONS: string[] = [];

export interface EffectiveActions {
	/** The caller's effective actions, as granted (may hold "*" and "domain:*"). */
	actions: string[];
	/** Whether the granted actions cover `action` ("tasks:write"), honouring
	 *  the wildcards "*" and "<domain>:*". */
	has: (action: string) => boolean;
	/** Whether any of `actions` is covered. */
	hasAny: (actions: readonly string[]) => boolean;
	/** True while the request is in flight. Callers deciding between "no
	 *  permission" and "loading" need it: until the answer is back, `has` is
	 *  false exactly as it is for a confirmed denial. */
	isLoading: boolean;
}

/**
 * The caller's effective IAM actions: inside a project when `projectId` is
 * given, workspace-wide when it is omitted. Backed by the my-permissions
 * queries (GET /auth/me/permissions and
 * GET /projects/:id/members/me/permissions); the server stays the authority.
 * An empty-string project id means "not known yet" and queries nothing.
 */
export function useEffectiveActions(projectId?: string): EffectiveActions {
	const { data = NO_ACTIONS, isLoading } = useQuery(
		projectId === undefined
			? myPermissionsQueryOptions
			: {
					...myProjectPermissionsQueryOptions(projectId),
					enabled: !!projectId,
				},
	);

	return {
		actions: data,
		has: (action) => hasAction(data, action),
		hasAny: (actions) => hasAnyAction(data, actions),
		isLoading,
	};
}
