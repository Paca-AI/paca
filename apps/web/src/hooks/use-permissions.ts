import { useEffectiveActions } from "@/hooks/use-effective-actions";

/** The caller's workspace-wide effective actions; a thin wrapper over
 *  `useEffectiveActions()` kept so call sites do not change. */
export function usePermissions() {
	const { actions, has, hasAny, isLoading } = useEffectiveActions();

	return {
		permissions: actions,
		hasPermission: has,
		hasAnyPermission: hasAny,
		isLoading,
	};
}
