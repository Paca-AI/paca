import { useQuery } from "@tanstack/react-query";
import {
	createFileRoute,
	Outlet,
	redirect,
	useMatches,
} from "@tanstack/react-router";
import { AlertCircle } from "lucide-react";
import { useTranslation } from "react-i18next";

import { AIChatFloat } from "@/components/projects/ai-chat-float";
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

	// The Conversations page has its own dedicated "New conversation" entry
	// point in its header, so the floating chat launcher would just be a
	// redundant second way to start a chat there — hide it on that page (and
	// its nested conversation routes) while keeping it available everywhere
	// else in the project.
	const matches = useMatches();
	const onConversationsPage = matches.some((m) =>
		m.routeId.startsWith("/_authenticated/projects/$projectId/conversations"),
	);

	if (isError || !project) {
		return (
			<div className="flex flex-1 flex-col items-center justify-center gap-3 p-8 text-muted-foreground">
				<AlertCircle className="size-8 opacity-40" />
				<p className="text-sm">{t("project.notFound")}</p>
			</div>
		);
	}

	return (
		<>
			<Outlet />
			{!onConversationsPage && <AIChatFloat projectId={projectId} />}
		</>
	);
}
