import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";

import { projectQueryOptions } from "@/lib/project-api";

export const Route = createFileRoute("/_authenticated/projects/$projectId")({
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		await queryClient
			.ensureQueryData(projectQueryOptions(projectId))
			.catch(() => {
				throw redirect({ to: "/home" });
			});
	},
	component: ProjectLayout,
});

function ProjectLayout() {
	const { projectId } = Route.useParams();
	const { data: project, isError } = useQuery(projectQueryOptions(projectId));

	if (isError || !project) {
		return (
			<div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-muted-foreground">
				<AlertCircle className="size-8 opacity-40" />
				<p className="text-sm">Project not found or access denied.</p>
			</div>
		);
	}

	return <Outlet />;
}
