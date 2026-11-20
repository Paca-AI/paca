import { useQueries } from "@tanstack/react-query";
import { useEffectiveActions } from "@/hooks/use-effective-actions";
import type { FileScope } from "@/lib/files-api";
import { hasAction } from "@/lib/policy/actions";
import { myProjectPermissionsQueryOptions } from "@/lib/project-api";

/**
 * Whether the caller may change files in a scope: files:write on the project,
 * or on the general scope for workspace files. Backed by the cached
 * my-permissions queries; the server stays the authority.
 *
 * Takes every scope the page shows (the global Recent/Starred/Trash views
 * span several projects) and returns a predicate over a scope.
 */
export function useFileWriteAccess(
	scopes: readonly FileScope[],
): (scope: FileScope) => boolean {
	const general = useEffectiveActions();
	const projectIds = [
		...new Set(
			scopes.map((s) => s.projectId).filter((id): id is string => !!id),
		),
	];
	const results = useQueries({
		queries: projectIds.map((id) => myProjectPermissionsQueryOptions(id)),
	});
	const byProject = new Map(
		projectIds.map((id, i) => [id, results[i]?.data ?? []] as const),
	);
	return (scope) =>
		scope.projectId
			? hasAction(byProject.get(scope.projectId) ?? [], "files:write")
			: general.has("files:write");
}

/**
 * Whether the caller may change documentation in a project: docs:write on it.
 * Files in the Documentation folder need this as well as files:write.
 */
export function useDocsWriteAccess(
	scopes: readonly FileScope[],
): (scope: FileScope) => boolean {
	const projectIds = [
		...new Set(
			scopes.map((s) => s.projectId).filter((id): id is string => !!id),
		),
	];
	const results = useQueries({
		queries: projectIds.map((id) => myProjectPermissionsQueryOptions(id)),
	});
	const byProject = new Map(
		projectIds.map((id, i) => [id, results[i]?.data ?? []] as const),
	);
	return (scope) =>
		!!scope.projectId &&
		hasAction(byProject.get(scope.projectId) ?? [], "docs:write");
}
