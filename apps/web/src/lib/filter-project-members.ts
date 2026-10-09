import type { ProjectMember } from "@/lib/project-api";

/** Narrows a project's member list. The endpoint already returns every
 *  member (teams are small and the list is cached server-side), so this runs
 *  client-side. Every whitespace-separated word of `search` must appear
 *  (case-insensitive) in the username, full name, or — for agents — the
 *  agent's name or handle; `role` is an exact role name (the member may hold others too). */
export function filterProjectMembers(
	members: ProjectMember[],
	search: string,
	role: string,
): ProjectMember[] {
	const words = search.toLowerCase().split(/\s+/).filter(Boolean);
	return members.filter((m) => {
		if (role && !m.roles.some((r) => r.name === role)) return false;
		if (words.length === 0) return true;
		const haystack = [m.username, m.full_name, m.agent_name, m.agent_handle]
			.filter(Boolean)
			.join("\n")
			.toLowerCase();
		return words.every((w) => haystack.includes(w));
	});
}
