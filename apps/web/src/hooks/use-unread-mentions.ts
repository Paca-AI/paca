import { useQuery } from "@tanstack/react-query";
import { unreadMentionsQueryOptions } from "@/lib/notification-api";

/** How many unread mentions of you a task carries (0 for none). One shared
 *  query per project; each row selects only its own count. */
export function useTaskUnreadMentions(
	projectId: string | undefined,
	taskId: string,
): number {
	const { data } = useQuery({
		...unreadMentionsQueryOptions(projectId),
		enabled: !!projectId,
		select: (rows) => rows.find((r) => r.task_id === taskId)?.count ?? 0,
	});
	return data ?? 0;
}

/** How many tasks of a sprint mention you and are unread. */
export function useSprintUnreadMentionTasks(
	projectId: string | undefined,
	sprintId: string | undefined,
): number {
	const { data } = useQuery({
		...unreadMentionsQueryOptions(projectId),
		enabled: !!projectId && !!sprintId,
		select: (rows) => rows.filter((r) => r.sprint_id === sprintId).length,
	});
	return data ?? 0;
}
