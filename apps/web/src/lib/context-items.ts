// Shared "context item" concept for the chat context-injection feature: a
// reference to a Task, Doc, Conversation, or Automation the user attaches to
// an outgoing chat message so the agent can pull in its full content (via
// get_task/read_doc/get_automation/read_conversation on the agent-runner
// side — see services/agent-runner's context_item.go). Kept field-identical
// across three languages on purpose:
//   - this file (camelCase, app-internal state/UI shape)
//   - the wire shape below (snake_case, matches services/api's
//     ContextItemRef JSON tags exactly)
//   - services/api's Go ContextItemRef / services/agent-runner's copy of it

export type ContextItemType =
	| "task"
	| "doc"
	| "conversation"
	| "automation"
	| "annotation";

export interface ContextItem {
	type: ContextItemType;
	id: string;
	projectId?: string;
	title: string;
}

const CONTEXT_ITEM_TYPES: readonly ContextItemType[] = [
	"task",
	"doc",
	"conversation",
	"automation",
	"annotation",
];

function isContextItemType(value: unknown): value is ContextItemType {
	return (
		typeof value === "string" &&
		(CONTEXT_ITEM_TYPES as readonly string[]).includes(value)
	);
}

/** The exact JSON shape services/api expects/returns — snake_case
 *  `project_id`, omitted entirely (never `null`) when the item has none.
 *  Mirrors `ContextItemRef` in services/api/internal/domain/agent/entity.go. */
export interface WireContextItem {
	type: ContextItemType;
	id: string;
	project_id?: string;
	title: string;
}

/** Maps staged ContextItems to the wire shape for an outgoing request body —
 *  used by lib/agent-api.ts's send/start functions. */
export function toWireContextItems(
	items: readonly ContextItem[],
): WireContextItem[] {
	return items.map((item) => ({
		type: item.type,
		id: item.id,
		title: item.title,
		...(item.projectId !== undefined ? { project_id: item.projectId } : {}),
	}));
}

/**
 * Parses an arbitrary JSON value into ContextItem[], dropping any malformed
 * entries defensively rather than throwing. Accepts either the wire shape's
 * `project_id` or the already-camelCase `projectId` (whichever a caller
 * happens to be reading from — a freshly-parsed API response payload in the
 * first case, or a `ThreadMessageLike.metadata.custom` value we set
 * ourselves in the second), so this one helper covers both:
 *   - conversation-to-thread-messages.ts reading a persisted event's
 *     `payload.context_items` (snake_case, straight from the JSON API)
 *   - thread.tsx reading a message's `metadata.custom.contextItems` (already
 *     camelCase, since that's what we stored there ourselves)
 */
export function parseContextItems(raw: unknown): ContextItem[] {
	if (!Array.isArray(raw)) return [];
	const items: ContextItem[] = [];
	for (const entry of raw) {
		if (!entry || typeof entry !== "object") continue;
		const rec = entry as Record<string, unknown>;
		const { type, id, title } = rec;
		if (
			!isContextItemType(type) ||
			typeof id !== "string" ||
			typeof title !== "string"
		) {
			continue;
		}
		const projectId =
			typeof rec.project_id === "string"
				? rec.project_id
				: typeof rec.projectId === "string"
					? rec.projectId
					: undefined;
		items.push({
			type,
			id,
			title,
			...(projectId !== undefined ? { projectId } : {}),
		});
	}
	return items;
}
