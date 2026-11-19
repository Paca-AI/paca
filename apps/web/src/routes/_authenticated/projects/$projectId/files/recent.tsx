import { createFileRoute } from "@tanstack/react-router";
import { FilesPage, parseFilesSearch } from "@/components/files/files-page";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/files/recent",
)({
	validateSearch: parseFilesSearch,
	component: ProjectFilesRoute,
});

function ProjectFilesRoute() {
	const { projectId } = Route.useParams();
	const search = Route.useSearch();
	return <FilesPage projectId={projectId} section="recent" search={search} />;
}
