import { createFileRoute } from "@tanstack/react-router";
import { DocumentPane } from "@/components/projects/docs/document-pane";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/docs/$docId",
)({
	component: DocEditorPage,
});

function DocEditorPage() {
	const { projectId, docId } = Route.useParams();
	return <DocumentPane projectId={projectId} docId={docId} />;
}
