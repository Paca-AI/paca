import type { ChatBlock, ChatInline } from "./chat-api";
import { blocksToText } from "./chat-content";

// What the BlockNote message editor produces, and what is sent: helpers kept
// out of the component so they can be tested without an editor.

function isEmptyInline(nodes: ChatInline[] | undefined): boolean {
	return (nodes ?? []).every(
		(n) => n.type === "text" && (n.text ?? "").trim() === "",
	);
}

/** A block with no text, no reference and no children (the editor's trailing
 * empty paragraph). */
function isEmptyBlock(b: ChatBlock): boolean {
	if (b.type !== "paragraph") return false;
	return isEmptyInline(b.content) && !(b.children && b.children.length > 0);
}

/** The blocks without the empty paragraphs the editor keeps at the end. */
export function trimTrailingEmpty(blocks: ChatBlock[]): ChatBlock[] {
	let end = blocks.length;
	while (end > 0 && isEmptyBlock(blocks[end - 1] as ChatBlock)) end--;
	return blocks.slice(0, end);
}

export function isEmptyMessage(blocks: ChatBlock[]): boolean {
	return trimTrailingEmpty(blocks).length === 0;
}

function cleanInline(n: ChatInline): ChatInline {
	if (n.type === "taskReference") {
		const p = n.props ?? {};
		return {
			type: "taskReference",
			props: { id: p.id, projectId: p.projectId ?? "", number: p.number ?? 0 },
		};
	}
	if (n.type === "docReference") {
		const p = n.props ?? {};
		return {
			type: "docReference",
			props: { id: p.id, projectId: p.projectId ?? "" },
		};
	}
	if (n.type === "teamMention") {
		const p = n.props ?? {};
		return { type: "teamMention", props: { id: p.id, name: p.name } };
	}
	return n.content ? { ...n, content: n.content.map(cleanInline) } : n;
}

function cleanBlock(b: ChatBlock): ChatBlock {
	return {
		...b,
		content: b.content?.map(cleanInline),
		children: b.children?.map(cleanBlock),
	};
}

/**
 * The content to store: no trailing empty paragraph, and a task or document
 * chip reduced to ids. The editor holds the title so the author sees it; the
 * message must not, so a reader without access to the task never learns it.
 */
export function prepareForSend(blocks: ChatBlock[]): ChatBlock[] {
	return trimTrailingEmpty(blocks).map(cleanBlock);
}

const COMMAND_RE = /^\/([A-Za-z]+)(?:\s+([\s\S]*))?$/;

/** A message that is a slash command: its name and what follows it. */
export function parseCommand(
	blocks: ChatBlock[],
): { name: string; args: string } | null {
	const m = COMMAND_RE.exec(blocksToText(trimTrailingEmpty(blocks)).trim());
	return m
		? { name: (m[1] ?? "").toLowerCase(), args: (m[2] ?? "").trim() }
		: null;
}
