import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { DM_SCOPE } from "@/lib/chat-api";

/**
 * What the caller may do in a chat scope: inside a project, their project
 * actions; for direct messages (DM_SCOPE), their workspace-wide ones. The
 * server decides each request; this only tells the UI what to offer.
 */
export function useChatPermissions(scope: string) {
	const isDM = scope === DM_SCOPE;
	const workspace = usePermissions();
	// An empty project id queries nothing, so a direct message costs no request.
	const project = useProjectPermissions(isDM ? "" : scope);
	return {
		can: (action: string) =>
			isDM
				? workspace.hasPermission(action)
				: project.hasProjectPermission(action),
		isLoading: isDM ? workspace.isLoading : project.isLoading,
	};
}
