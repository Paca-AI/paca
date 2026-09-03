import { useQuery } from "@tanstack/react-query";
import { listDocuments } from "./doc-api";
import { listAllTasks } from "./interaction-api";
import { listProjectMembers, type ProjectMember } from "./project-api";

export interface TeamMember {
	id: string;
	name: string;
	username: string;
	avatarUrl?: string | null | undefined;
	avatarThumbUrl?: string | null | undefined;
}

export interface MentionableTask {
	id: string;
	title: string;
	task_number: number;
	status?: string | null | undefined;
}

export interface MentionableDocument {
	id: string;
	title: string;
}

/** Trims, collapses internal whitespace (a comment body can be
 * multi-paragraph), and truncates for use as a short display label —
 * mirrors annotationsvc.taskTitleFromBody's own short-title-from-body
 * derivation on the Go side. Used for a pasted comment's attach-context
 * chip title (thread.tsx) and the annotation card block's fallback title
 * (blocknote-annotation-card-block.tsx). */
export function excerptOf(body: string, max = 60): string {
	const collapsed = body.trim().replace(/\s+/g, " ");
	if (collapsed.length <= max) return collapsed || "Comment";
	return `${collapsed.slice(0, max - 1)}…`;
}

export interface MentionData {
	teamMembers: TeamMember[];
	documents: MentionableDocument[];
}

const teamMembersQueryOptions = (projectId: string) => ({
	queryKey: ["projects", projectId, "members"],
	queryFn: () => listProjectMembers(projectId),
	staleTime: 5 * 60 * 1000,
});

export const MENTIONABLE_TASKS_PAGE_SIZE = 20;

/** Live server-side search backing the "#" task-reference mention menu.
 *  Queried fresh for whatever the user has typed after "#" — the project's
 *  task list has no fixed upper bound and the backend caps page_size at 200,
 *  so unlike team members/docs this can't just be preloaded once and
 *  filtered client-side without silently hiding tasks past the first page. */
export async function searchMentionableTasks(
	projectId: string,
	query: string,
): Promise<MentionableTask[]> {
	const result = await listAllTasks(projectId, {
		search: query,
		pageSize: MENTIONABLE_TASKS_PAGE_SIZE,
	});
	return result.items.map((task) => ({
		id: task.id,
		title: task.title,
		task_number: task.task_number,
		status: task.status_id || undefined,
	}));
}

const documentsQueryOptions = (projectId: string) => ({
	queryKey: ["projects", projectId, "docs", "mentions"],
	queryFn: () => listDocuments(projectId),
	staleTime: 2 * 60 * 1000,
});

export function useMentionData(projectId?: string | null) {
	const { data: members = [] } = useQuery({
		...teamMembersQueryOptions(projectId ?? ""),
		enabled: !!projectId,
	});

	const { data: documents = [] } = useQuery({
		...documentsQueryOptions(projectId ?? ""),
		enabled: !!projectId,
	});

	const teamMembers: TeamMember[] = members.map((member: ProjectMember) => ({
		id:
			member.member_type === "agent"
				? (member.agent_id ?? member.user_id)
				: member.user_id,
		name: member.full_name,
		username: member.username,
		avatarUrl: member.avatar_url,
		avatarThumbUrl: member.avatar_thumb_url,
	}));

	const mentionDocs: MentionableDocument[] = documents.map((doc) => ({
		id: doc.id,
		title: doc.title,
	}));

	return {
		teamMembers,
		documents: mentionDocs,
		isLoading: !projectId,
	};
}
