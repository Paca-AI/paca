import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";

/**
 * Whether the viewer can hand out roles inside a project (`roles:assign`,
 * workspace-wide or in that project).
 *
 * Assigning is its own privilege: changing an existing member's roles needs
 * only `roles:assign` (not `project.members:write`); adding a member or agent
 * needs it next to their own gate (`project.members:write` / `agents:write`).
 * Every role a request adds or removes is authorized on its own resource, like
 * `iam:PassRole`. There is no
 * endpoint that says which roles one may assign, so this only mirrors the
 * action; the UI offers all roles and an unassignable one is refused by the
 * API with FORBIDDEN.
 */
export function useCanAssignProjectRole(projectId: string): {
	canAssignRoles: boolean;
	isLoading: boolean;
} {
	const { hasPermission, isLoading: isGlobalLoading } = usePermissions();
	const { hasProjectPermission, isLoading: isProjectLoading } =
		useProjectPermissions(projectId);
	return {
		canAssignRoles:
			hasPermission("roles:assign") || hasProjectPermission("roles:assign"),
		isLoading: isGlobalLoading || isProjectLoading,
	};
}
