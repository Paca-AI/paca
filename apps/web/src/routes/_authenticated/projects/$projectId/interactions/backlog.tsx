import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { InteractionLayout } from "@/components/projects/interactions/interaction-layout";
import { useProjectPermissions } from "@/hooks/use-project-permissions";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/interactions/backlog",
)({
	component: BacklogPage,
});

function BacklogPage() {
	const { t } = useTranslation();
	const { projectId } = Route.useParams();
	const { hasProjectPermission } = useProjectPermissions(projectId);

	const canCreate = hasProjectPermission("tasks.write");
	const canEdit = hasProjectPermission("tasks.write");
	const canManageViews = hasProjectPermission("projects.write");

	return (
		<InteractionLayout
			projectId={projectId}
			interactionKey={`backlog:${projectId}`}
			title={t("common.productBacklog")}
			description={t("project.interactions.backlog.description")}
			canCreate={canCreate}
			canEdit={canEdit}
			canManageViews={canManageViews}
			sprintId={null}
			context="backlog"
		/>
	);
}
