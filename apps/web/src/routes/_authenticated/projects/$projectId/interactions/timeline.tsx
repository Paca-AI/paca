import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";

import { InteractionLayout } from "@/components/projects/interactions/interaction-layout";
import { useProjectPermissions } from "@/hooks/use-project-permissions";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/interactions/timeline",
)({
	component: TimelinePage,
});

function TimelinePage() {
	const { t } = useTranslation("projects");
	const { projectId } = Route.useParams();
	const { hasProjectPermission } = useProjectPermissions(projectId);

	const canCreate = hasProjectPermission("tasks.write");
	const canEdit = hasProjectPermission("tasks.write");
	// views.* was split out from sprints.write as its own permission (see
	// authz.PermissionViewsWrite's doc comment) — projects.write governs
	// only the project entity itself (name/description), an unrelated
	// permission that happened to be reused here.
	const canManageViews = hasProjectPermission("views.write");
	// New/Start Sprint is a sprint-entity action, not a task one — sprints.write,
	// not tasks.write (see authz.PermissionSprintsWrite). Unused on this
	// "timeline" context today (that button only renders in "backlog"), kept
	// for interface consistency with InteractionLayoutProps.
	const canManageSprints = hasProjectPermission("sprints.write");

	return (
		<InteractionLayout
			projectId={projectId}
			interactionKey={`timeline:${projectId}`}
			title={t("layout.timeline.title")}
			description={t("layout.timeline.description")}
			canCreate={canCreate}
			canEdit={canEdit}
			canManageViews={canManageViews}
			canManageSprints={canManageSprints}
			sprintId={null}
			context="timeline"
		/>
	);
}
