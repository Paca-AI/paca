import {
	type ChattableAgent,
	listAgents,
	listChattableAgents,
} from "./agent-api";
import {
	IdDirectory,
	MAX_IDS_PER_LOOKUP,
	useIdDirectory,
} from "./id-directory";

/** Resolves agent ids to agents without loading the whole agent list. With a
 *  projectId it looks agents up among those visible in that project; with
 *  null, among the global agents anyone may chat with. The latter carry no
 *  configuration, so entries are typed as what both lookups return. */
export class AgentDirectory extends IdDirectory<ChattableAgent> {
	constructor(projectId: string | null) {
		super(
			async (ids) =>
				(projectId
					? await listAgents(projectId, { ids, pageSize: MAX_IDS_PER_LOOKUP })
					: await listChattableAgents({ ids, pageSize: MAX_IDS_PER_LOOKUP })
				).items,
			(a) => [a.id],
		);
	}
}

const directories = new Map<string, AgentDirectory>();

export function getAgentDirectory(projectId: string | null): AgentDirectory {
	const key = projectId ?? "global";
	let dir = directories.get(key);
	if (!dir) {
		dir = new AgentDirectory(projectId);
		directories.set(key, dir);
	}
	return dir;
}

/** Call after agents are created, edited or deleted so cached names and
 *  avatars are fetched again. */
export function invalidateAgentDirectory(projectId: string | null): void {
	directories.get(projectId ?? "global")?.invalidate();
}

/** Lazy id → agent lookup (project-visible agents, or the global ones when
 *  projectId is null). The returned `getAgent` changes identity whenever new
 *  agents resolve. */
export function useAgentDirectory(projectId: string | null | undefined) {
	return useIdDirectory(getAgentDirectory(projectId || null));
}
