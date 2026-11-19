import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Navigate, notFound } from "@tanstack/react-router";

import { Skeleton } from "@/components/ui/skeleton";
import { isTaskKey, taskByKeyQueryOptions } from "@/lib/interaction-api";

// Short links such as `paca.example.com/PRJ-1`: resolve the key to a task and
// redirect to its canonical page. Static top-level routes (/home, /profile…)
// outrank this dynamic segment, and anything not shaped like a key is a 404.
export const Route = createFileRoute("/_authenticated/$taskKey")({
	beforeLoad: ({ params }) => {
		if (!isTaskKey(params.taskKey)) throw notFound();
	},
	component: TaskKeyRedirect,
});

function TaskKeyRedirect() {
	const { taskKey } = Route.useParams();
	const { data: task, isError } = useQuery(taskByKeyQueryOptions(taskKey));

	if (isError) throw notFound();
	if (!task) {
		return (
			<div className="p-6">
				<Skeleton className="h-8 w-1/2" />
			</div>
		);
	}
	return (
		<Navigate
			to="/projects/$projectId/tasks/$taskId"
			params={{ projectId: task.project_id, taskId: task.id }}
			replace
		/>
	);
}
