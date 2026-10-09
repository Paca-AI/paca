import { useEffectiveActions } from "@/hooks/use-effective-actions";

/**
 * Returns a `hasProjectPermission` checker scoped to the current user's roles
 * within a specific project. Takes an IAM action ("tasks:write") and supports
 * the wildcards a role may hold ("tasks:*", "*"). A thin wrapper over
 * `useEffectiveActions(projectId)`, kept so call sites do not change.
 */
export function useProjectPermissions(projectId: string) {
	const { has, isLoading } = useEffectiveActions(projectId);

	// Callers gate a `noPermission` (vs. loading) render decision on `isLoading`:
	// while the request is in flight `has` is false like a confirmed denial.
	return { hasProjectPermission: has, isLoading };
}
