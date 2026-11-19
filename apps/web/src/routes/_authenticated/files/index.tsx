import { createFileRoute } from "@tanstack/react-router";
import { FilesPage, parseFilesSearch } from "@/components/files/files-page";

export const Route = createFileRoute("/_authenticated/files/")({
	validateSearch: parseFilesSearch,
	component: FilesRoute,
});

function FilesRoute() {
	const search = Route.useSearch();
	return <FilesPage section="browse" search={search} />;
}
