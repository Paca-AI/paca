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
	const { projectId } = Route.useParams();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const { t } = useTranslation("projects");

	const canCreate = hasProjectPermission("tasks.write");
	const canEdit = hasProjectPermission("tasks.write");
	// views.* was split out from sprints.write as its own permission (see
	// authz.PermissionViewsWrite's doc comment) — projects.write governs
	// only the project entity itself (name/description), an unrelated
	// permission that happened to be reused here.
	const canManageViews = hasProjectPermission("views.write");
	// New/Start Sprint is a sprint-entity action, not a task one — sprints.write,
	// not tasks.write (see authz.PermissionSprintsWrite).
	const canManageSprints = hasProjectPermission("sprints.write");

	return (
		<InteractionLayout
			projectId={projectId}
			interactionKey={`backlog:${projectId}`}
			title={t("board.backlog.title")}
			description={t("board.backlog.description")}
			canCreate={canCreate}
			canEdit={canEdit}
			canManageViews={canManageViews}
			canManageSprints={canManageSprints}
			sprintId={null}
			context="backlog"
		/>
	);
}
