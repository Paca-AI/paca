import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { useTranslation } from "react-i18next";

import { useProjectRealtime } from "@/hooks/use-project-realtime";
import { projectQueryOptions } from "@/lib/project-api";

export const Route = createFileRoute("/_authenticated/projects/$projectId")({
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		const user = queryClient.getQueryData(["auth", "me-optional"]);
		await queryClient
			.ensureQueryData(projectQueryOptions(projectId))
			.catch(() => {
				throw redirect({ to: user ? "/home" : "/" });
			});
	},
	component: ProjectLayout,
});

function ProjectLayout() {
	const { t } = useTranslation("projects");
	const { projectId } = Route.useParams();
	const { data: project, isError } = useQuery(projectQueryOptions(projectId));

	// Join realtime rooms for this project.  The hook subscribes on mount and
	// leaves / cleans up on unmount (i.e. when navigating away from the project).
	useProjectRealtime(projectId);

	if (isError || !project) {
		return (
			<div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-muted-foreground">
				<AlertCircle className="size-8 opacity-40" />
				<p className="text-sm">{t("project.notFound")}</p>
			</div>
		);
	}

	// Project chat creation is session-first under /chats. The former floating
	// widget called legacy conversation-first write routes, so it must not stay
	// mounted as a hidden semantic bypass. Global chat keeps its separate widget.
	return <Outlet />;
}
