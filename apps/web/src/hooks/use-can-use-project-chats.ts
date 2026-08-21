import { usePermissions } from "./use-permissions";
import { useProjectPermissions } from "./use-project-permissions";

export interface ProjectChatPermissions {
	canChat: boolean;
	canUseTaskContext: boolean;
	canPublishConclusion: boolean;
}

export function useProjectChatPermissions(
	projectId: string,
): ProjectChatPermissions {
	const { hasPermission } = usePermissions();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const allowed = (permission: string) =>
		hasPermission(permission) || hasProjectPermission(permission);
	const canChat = allowed("agents.read");
	const canReadTasks = allowed("tasks.read");
	const canWriteTasks = allowed("tasks.write");
	return {
		canChat,
		canUseTaskContext: canChat && canReadTasks,
		canPublishConclusion: canChat && canReadTasks && canWriteTasks,
	};
}

export function useCanUseProjectChats(projectId: string): boolean {
	return useProjectChatPermissions(projectId).canChat;
}

export function useCanStartTaskChat(projectId: string): boolean {
	return useProjectChatPermissions(projectId).canUseTaskContext;
}
