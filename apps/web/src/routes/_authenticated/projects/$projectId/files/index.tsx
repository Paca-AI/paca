import { createFileRoute } from "@tanstack/react-router";
import { FilesPage, parseFilesSearch } from "@/components/files/files-page";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/files/",
)({
	validateSearch: parseFilesSearch,
	component: ProjectFilesRoute,
});

function ProjectFilesRoute() {
	const { projectId } = Route.useParams();
	const search = Route.useSearch();
	return <FilesPage projectId={projectId} section="browse" search={search} />;
}
