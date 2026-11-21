import type { ChatBlock, ChatInline } from "./chat-api";

// Message content is a list of BlockNote-compatible blocks, so mentions and
// task and doc references can use the comment composer's format later. The
// composer today writes plain paragraphs; this renders any block set the API
// stores without a BlockNote editor per message.

export const MAX_MESSAGE_LENGTH = 20_000;

/**
 * Something a message links to, written in the text as a token: `@Name` for a
 * person, `#…` for a task or a document. `name` is the text after the prefix.
 *
 * A task or document chip stores only ids, never the title: the title is read
 * when the message is shown, with the reader's own access, so a person who may
 * not open the task sees a locked chip instead of its name.
 */
export interface ChatMention {
	id: string;
	name: string;
	/** Defaults to a person. */
	kind?: "user" | "task" | "doc";
	/** The project a task or document belongs to. */
	projectId?: string;
	/** A task's number, which is its stored and edited label. */
	number?: number;
}

/** The label a stored reference is written as when a message is edited as text. */
export function referenceLabel(
	kind: "task" | "doc",
	id: string,
	num?: number,
): string {
	return kind === "task" && num ? String(num) : `doc:${id.slice(0, 8)}`;
}

/** The inline node of a mention or reference. A person mention uses the format
 * comments use, so one renderer and one server-side parser serve both. */
export function mentionNode(m: ChatMention): ChatInline {
	if (m.kind === "task") {
		return {
			type: "taskReference",
			props: { id: m.id, projectId: m.projectId ?? "", number: m.number ?? 0 },
		};
	}
	if (m.kind === "doc") {
		return {
			type: "docReference",
			props: { id: m.id, projectId: m.projectId ?? "" },
		};
	}
	return { type: "teamMention", props: { id: m.id, name: m.name } };
}

function referenceOf(n: ChatInline): ChatMention | null {
	const id = n.props?.id;
	if (typeof id !== "string") return null;
	const projectId = String(n.props?.projectId ?? "");
	if (n.type === "taskReference") {
		const num = Number(n.props?.number ?? 0);
		return {
			id,
			kind: "task",
			projectId,
			number: num,
			name: referenceLabel("task", id, num),
		};
	}
	if (n.type === "docReference") {
		return {
			id,
			kind: "doc",
			projectId,
			name: referenceLabel("doc", id),
		};
	}
	return null;
}

function inlineText(nodes: ChatInline[] | undefined): string {
	return (nodes ?? [])
		.map((n) => {
			if (n.type === "teamMention") return `@${String(n.props?.name ?? "")}`;
			const ref = referenceOf(n);
			if (ref) return `#${ref.name}`;
			return n.text ?? inlineText(n.content);
		})
		.join("");
}

/** The plain text of blocks, one line per block, for editing and previews. */
export function blocksToText(blocks: ChatBlock[] | undefined): string {
	return (blocks ?? [])
		.flatMap((b) => [inlineText(b.content), blocksToText(b.children)])
		.filter((s) => s !== "")
		.join("\n");
}

const SAFE_HREF = /^(https?:|mailto:)/i;

/** A link target that is safe to put in an href: http(s) and mailto only. */
export function safeHref(href: string | undefined): string | undefined {
	return href && SAFE_HREF.test(href.trim()) ? href.trim() : undefined;
}

/** Whether two blocks arrays carry the same text (an edit that changed
 * nothing is not sent). */
export function sameText(a: ChatBlock[], b: ChatBlock[]): boolean {
	return blocksToText(a) === blocksToText(b);
}
