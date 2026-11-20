import { PresenceViewers } from "@/components/shared/presence-viewers";

/** Avatars of the other people looking at this task right now. */
export function TaskViewers({
	projectId,
	taskId,
}: {
	projectId?: string;
	taskId: string;
}) {
	return (
		<PresenceViewers
			kind="task"
			projectId={projectId}
			id={taskId}
			testId="task-viewers"
		/>
	);
}
