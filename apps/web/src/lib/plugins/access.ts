import { useEffectiveActions } from "@/hooks/use-effective-actions";

/**
 * Whether the caller holds a plugin registration's `requiredPermission`
 * (a registration without one is open to everyone). Inside a project the
 * project's effective actions count as well as the workspace-wide ones.
 * While the permissions are still loading nothing that needs one is shown.
 */
export function usePluginAccess(projectId?: string) {
	const workspace = useEffectiveActions();
	const project = useEffectiveActions(projectId ?? "");
	return (reg: { requiredPermission?: string }) =>
		!reg.requiredPermission ||
		workspace.has(reg.requiredPermission) ||
		(!!projectId && project.has(reg.requiredPermission));
}
