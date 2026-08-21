import type { ProjectChatContextSourceRef } from "./agent-api";

export function taskChatInitialContextSources(
	taskId: string | undefined,
): ProjectChatContextSourceRef[] {
	return taskId ? [{ type: "task", id: taskId }] : [];
}

export function mergeRequiredProjectChatSources(
	current: ProjectChatContextSourceRef[],
	required: ProjectChatContextSourceRef[],
): ProjectChatContextSourceRef[] {
	const seen = new Set(required.map((source) => `${source.type}:${source.id}`));
	return [
		...required,
		...current.filter((source) => {
			const key = `${source.type}:${source.id}`;
			if (seen.has(key)) return false;
			seen.add(key);
			return true;
		}),
	];
}

export function projectChatContextSourcesEqual(
	left: ProjectChatContextSourceRef[],
	right: ProjectChatContextSourceRef[],
): boolean {
	return (
		left.length === right.length &&
		left.every(
			(source, index) =>
				source.type === right[index]?.type && source.id === right[index]?.id,
		)
	);
}

export function shouldShowProjectChatMainOnMobile(
	sessionId: string | undefined,
	search: { contextTaskId?: string; draft?: string },
): boolean {
	return !!sessionId || !!search.contextTaskId || !!search.draft;
}

export function newTaskChatSearch(taskId: string, agentId?: string) {
	return {
		contextTaskId: taskId,
		draft: crypto.randomUUID(),
		agentId,
	};
}

/**
 * A task entry always opens a brand-new local draft. Persistence only starts
 * when the first message is submitted; the nonce prevents a mounted draft
 * route from retaining another entry's composer/idempotency state.
 */
export function newTaskChatHref(
	projectId: string,
	taskId: string,
	agentId?: string,
): string {
	const values = newTaskChatSearch(taskId, agentId);
	const search = new URLSearchParams({
		contextTaskId: values.contextTaskId,
		draft: values.draft,
	});
	if (values.agentId) search.set("agentId", values.agentId);
	return `/projects/${projectId}/chats?${search.toString()}`;
}
