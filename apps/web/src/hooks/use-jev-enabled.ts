import { useQuery } from "@tanstack/react-query";
import { projectQueryOptions } from "@/lib/project-api";

/** Whether this project has Jev (the AI decision API) configured — see
 * Project.jev_configured's doc comment. Credentials are per-project (set in
 * the project's Jev settings tab), not instance-wide, so this always needs a
 * project to check against; pass undefined when there isn't one in scope
 * (e.g. a global/cross-project surface) and it degrades to false. Reads off
 * the same projectQueryOptions cache entry the page's own project query
 * already warms, so this is normally cache-only, no extra fetch. Gates
 * every Jev-dependent affordance: the project settings tab's credentials
 * form is always shown, but the Jev Choice/Score/Noul automation node
 * types and Auto mode for chat/task assignment are hidden until configured. */
export function useJevEnabled(projectId: string | undefined): boolean {
	const { data: project } = useQuery({
		...projectQueryOptions(projectId ?? ""),
		enabled: !!projectId,
	});
	return project?.jev_configured ?? false;
}
