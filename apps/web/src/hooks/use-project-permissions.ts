import { useQuery } from "@tanstack/react-query";

import { hasPermission } from "@/lib/permissions";
import { myProjectPermissionsQueryOptions } from "@/lib/project-api";

/**
 * Returns a `hasProjectPermission` checker scoped to the current user's role
 * within a specific project. Supports wildcard notation (e.g. `sprints.*`).
 *
 * Permissions are fetched from GET /projects/:projectId/members/me/permissions,
 * a lightweight endpoint that returns only the caller's own permission map
 * without requiring access to the full members or roles lists.
 */
export function useProjectPermissions(projectId: string) {
	const { data: permissionsMap = {} } = useQuery({
		...myProjectPermissionsQueryOptions(projectId),
		enabled: !!projectId,
	});

	// Convert the {perm: boolean} map to the string[] form expected by the
	// shared hasPermission helper.
	const projectPermissions = Object.entries(permissionsMap)
		.filter(([, v]) => v === true)
		.map(([k]) => k);

	const hasProjectPermission = (permission: string): boolean =>
		hasPermission(projectPermissions, permission);

	return { hasProjectPermission };
}
