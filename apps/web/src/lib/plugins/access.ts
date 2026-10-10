import { useEffectiveActions } from "@/hooks/use-effective-actions";

/**
 * Whether the caller holds a plugin registration's `requiredPermission`
 * (a registration without one is open to everyone). Inside a project the
 * project's effective actions decide, because the server checks an
 * in-project plugin action on the project's own plugin resource, which a
 * workspace-wide grant does not cover; outside a project the workspace-wide
 * actions decide. While the permissions are still loading nothing that needs
 * one is shown.
 */
export function usePluginAccess(projectId?: string) {
	const workspace = useEffectiveActions();
	const project = useEffectiveActions(projectId ?? "");
	return (reg: { requiredPermission?: string }) =>
		!reg.requiredPermission ||
		(projectId
			? project.has(reg.requiredPermission)
			: workspace.has(reg.requiredPermission));
}
