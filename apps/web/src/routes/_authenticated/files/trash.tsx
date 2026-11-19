import { createFileRoute } from "@tanstack/react-router";
import { FilesPage, parseFilesSearch } from "@/components/files/files-page";

export const Route = createFileRoute("/_authenticated/files/trash")({
	validateSearch: parseFilesSearch,
	component: FilesRoute,
});

function FilesRoute() {
	const search = Route.useSearch();
	return <FilesPage section="trash" search={search} />;
}
