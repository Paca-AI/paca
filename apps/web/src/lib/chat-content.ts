import type { ChatBlock, ChatInline } from "./chat-api";

// Message content is a list of BlockNote-compatible blocks, so mentions and
// task and doc references can use the comment composer's format later. The
// composer today writes plain paragraphs; this renders any block set the API
// stores without a BlockNote editor per message.

export const MAX_MESSAGE_LENGTH = 20_000;

/** Someone a message mentions: an account id and the name shown. */
export interface ChatMention {
	id: string;
	name: string;
}

/** The inline node of a mention, in the format comments use, so one renderer
 * and one server-side parser serve both. */
export function mentionNode(m: ChatMention): ChatInline {
	return { type: "teamMention", props: { id: m.id, name: m.name } };
}

/**
 * One paragraph per line; blank lines are dropped. `@Name` for each mention
 * becomes a mention node (a name no longer in the text is simply not one).
 * Returns [] for no text.
 */
export function textToBlocks(
	text: string,
	mentions: ChatMention[] = [],
): ChatBlock[] {
	// Longest names first, so "@Ann Lee" wins over "@Ann".
	const named = [...mentions].sort((a, b) => b.name.length - a.name.length);
	const lineToInline = (line: string): ChatInline[] => {
		const out: ChatInline[] = [];
		let rest = line;
		while (rest.length > 0) {
			let at = -1;
			let hit: ChatMention | undefined;
			for (const m of named) {
				const i = rest.indexOf(`@${m.name}`);
				// A mention ends at the end of the text or before a non-name character.
				const end = i + m.name.length + 1;
				const boundary =
					end >= rest.length || /[\s.,;:!?)\]]/.test(rest[end] ?? "");
				if (i !== -1 && boundary && (at === -1 || i < at)) {
					at = i;
					hit = m;
				}
			}
			if (!hit || at === -1) {
				out.push({ type: "text", text: rest, styles: {} });
				break;
			}
			if (at > 0)
				out.push({ type: "text", text: rest.slice(0, at), styles: {} });
			out.push(mentionNode(hit));
			rest = rest.slice(at + hit.name.length + 1);
		}
		return out;
	};
	return text
		.split("\n")
		.map((line) => line.trimEnd())
		.filter((line) => line.trim() !== "")
		.map((line) => ({ type: "paragraph", content: lineToInline(line) }));
}

function inlineText(nodes: ChatInline[] | undefined): string {
	return (nodes ?? [])
		.map((n) =>
			n.type === "teamMention"
				? `@${String(n.props?.name ?? "")}`
				: (n.text ?? inlineText(n.content)),
		)
		.join("");
}

/** The plain text of blocks, one line per block, for editing and previews. */
export function blocksToText(blocks: ChatBlock[] | undefined): string {
	return (blocks ?? [])
		.flatMap((b) => [inlineText(b.content), blocksToText(b.children)])
		.filter((s) => s !== "")
		.join("\n");
}

/** The mentions a block list holds, so an edit keeps them linked. */
export function mentionsOf(blocks: ChatBlock[] | undefined): ChatMention[] {
	const found = new Map<string, ChatMention>();
	const walk = (nodes: ChatInline[] | undefined) => {
		for (const n of nodes ?? []) {
			if (n.type === "teamMention" && typeof n.props?.id === "string") {
				found.set(n.props.id, {
					id: n.props.id,
					name: String(n.props.name ?? ""),
				});
			}
			walk(n.content);
		}
	};
	const visit = (bs: ChatBlock[] | undefined) => {
		for (const b of bs ?? []) {
			walk(b.content);
			visit(b.children);
		}
	};
	visit(blocks);
	return [...found.values()];
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
