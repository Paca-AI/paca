import { usePermissions } from "@/hooks/use-permissions";

/**
 * Whether the viewer can hand out global roles.
 *
 * Assigning a role is its own privilege (`global_roles.assign`), separate from
 * editing the user or agent it goes to, and choosing one needs the list of
 * roles (`global_roles.read`). This mirrors what the API enforces on the
 * role-assignment routes so the UI never offers what the server would refuse;
 * the server stays the authority.
 */
export function useCanAssignGlobalRole(): boolean {
	const { hasPermission } = usePermissions();
	return (
		hasPermission("global_roles.assign") && hasPermission("global_roles.read")
	);
}
