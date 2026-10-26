import { BlockNoteEditor } from "@blocknote/core";
import { JSDOM } from "jsdom";

// Initialize JSDOM to provide a browser-like environment for BlockNote
const dom = new JSDOM("");
const { window } = dom;

const globals = {
	window,
	document: window.document,
	navigator: window.navigator,
	HTMLElement: window.HTMLElement,
	Node: window.Node,
};

for (const [key, value] of Object.entries(globals)) {
	Object.defineProperty(global, key, {
		value,
		writable: true,
		configurable: true,
	});
}

let editor: BlockNoteEditor | null = null;

function getEditor(): BlockNoteEditor {
	if (!editor) {
		editor = BlockNoteEditor.create();
	}
	return editor;
}

/**
 * Recursively converts mentions to text format with IDs in the blocks.
 * @param content - Inline content array or single content item
 * @returns Converted content
 */
function convertMentionsToText(content: any): any {
	if (!content) return content;

	if (Array.isArray(content)) {
		return content.map((item) => convertMentionsToText(item));
	}

	if (typeof content === "object" && content !== null) {
		const result = { ...content };

		if (result.type === "teamMention") {
			const memberName = result.props?.name || "Unknown";
			const memberId = result.props?.id || "";
			const text = memberId
				? `@${memberName} (id: ${memberId})`
				: `@${memberName}`;
			return {
				type: "text",
				text,
				styles: {},
			};
		}

		if (result.type === "taskReference") {
			const taskTitle = result.props?.title || "Unknown";
			const taskId = result.props?.id || "";
			const text = taskId ? `#${taskTitle} (id: ${taskId})` : `#${taskTitle}`;
			return {
				type: "text",
				text,
				styles: {},
			};
		}

		if (result.type === "docReference") {
			const docTitle = result.props?.title || "Unknown";
			const docId = result.props?.id || "";
			const text = docId ? `📄 ${docTitle} (id: ${docId})` : `📄 ${docTitle}`;
			return {
				type: "text",
				text,
				styles: {},
			};
		}

		// mermaid/annotationCard are custom BLOCK types (see
		// apps/web/src/components/shared/blocknote-schema.tsx) — unlike the
		// three inline mention types above, the editor instance this file
		// builds (getEditor(), a bare BlockNoteEditor.create() with no custom
		// schema) has never heard of either. Left unconverted, BlockNoteEditor's
		// own block-to-markdown export looks up the block's type in its schema
		// and crashes with "Cannot read properties of undefined (reading
		// 'propSchema')" the moment it hits one — confirmed live: this is what
		// broke every `list_tasks` call for a project with a mermaid diagram or
		// a comment-reference card in any task's description (conversation
		// 269da86a-05c8-4e12-9619-49397a85949e). Degrading them to plain
		// default-schema blocks here — mirroring the three inline mentions
		// above — fixes that the same way; each block's own toExternalHTML
		// (mermaid) already documents this exact "unknown client" fallback as
		// the intended degradation, just not reachable from a schema that
		// doesn't know the type exists in the first place.
		if (result.type === "mermaid") {
			const code = result.props?.code || "";
			return {
				type: "codeBlock",
				props: { language: "mermaid" },
				content: [{ type: "text", text: code, styles: {} }],
			};
		}

		if (result.type === "annotationCard") {
			const cardId = result.props?.id || "";
			const text = cardId
				? `[Comment reference (id: ${cardId})]`
				: "[Comment reference]";
			return {
				type: "paragraph",
				content: [{ type: "text", text, styles: {} }],
			};
		}

		if (result.content) {
			result.content = convertMentionsToText(result.content);
		}

		if (result.children) {
			result.children = convertMentionsToText(result.children);
		}

		return result;
	}

	return content;
}

/**
 * Checks if blocks contain any mention types.
 * @param blocks - Array of BlockNote block objects
 * @returns True if mentions are found
 */
function hasMentions(blocks: any[] | null): boolean {
	if (!blocks || blocks.length === 0) return false;

	for (const block of blocks) {
		if (!block) continue;

		if (
			block.type === "teamMention" ||
			block.type === "taskReference" ||
			block.type === "docReference" ||
			block.type === "mermaid" ||
			block.type === "annotationCard"
		) {
			return true;
		}

		if (block.content) {
			if (hasMentionsInContent(block.content)) {
				return true;
			}
		}

		if (block.children) {
			if (hasMentions(block.children)) {
				return true;
			}
		}
	}

	return false;
}

function hasMentionsInContent(content: any): boolean {
	if (!content) return false;

	if (Array.isArray(content)) {
		for (const item of content) {
			if (hasMentionsInContent(item)) {
				return true;
			}
		}
		return false;
	}

	if (typeof content === "object" && content !== null) {
		if (
			content.type === "teamMention" ||
			content.type === "taskReference" ||
			content.type === "docReference"
		) {
			return true;
		}

		if (content.content && hasMentionsInContent(content.content)) {
			return true;
		}

		if (content.children && hasMentions(content.children)) {
			return true;
		}
	}

	return false;
}

// Legacy/corrupt comments may contain link nodes without href or content
// (#589). Repair only those nodes before export; keep the remaining document
// readable without inventing a URL or changing the stored BlockNote data.
function normalizeMalformedLinks(value: any): any {
	if (Array.isArray(value)) return value.map(normalizeMalformedLinks);
	if (!value || typeof value !== "object") return value;

	const node = Object.fromEntries(
		Object.entries(value).map(([key, child]) => [
			key,
			normalizeMalformedLinks(child),
		]),
	);
	if (
		node.type !== "link" ||
		(typeof node.href === "string" &&
			Array.isArray(node.content) &&
			node.content.every(
				(part: any) =>
					part !== null &&
					typeof part === "object" &&
					part.type === "text" &&
					typeof part.text === "string" &&
					(part.styles == null ||
						(typeof part.styles === "object" && !Array.isArray(part.styles))),
			))
	) {
		return node;
	}

	const contentText = Array.isArray(node.content)
		? node.content
				.map((part: any) =>
					typeof part === "string"
						? part
						: typeof part?.text === "string"
							? part.text
							: "",
				)
				.join("")
		: typeof node.content === "string"
			? node.content
			: "";
	return {
		type: "text",
		text: contentText || (typeof node.text === "string" ? node.text : ""),
		styles: {},
	};
}

/**
 * Converts BlockNote JSON blocks to Markdown string.
 * @param blocks - Array of BlockNote block objects
 * @returns Markdown string representation
 */
export function blocknoteToMarkdown(
	blocks: any[] | { content?: unknown; text?: unknown } | null,
): string {
	if (!blocks) return "";
	if (!Array.isArray(blocks)) {
		if (typeof blocks.text === "string") {
			return blocks.text;
		}
		if (!Array.isArray(blocks.content)) {
			return "";
		}
		blocks = blocks.content;
	}
	if (blocks.length === 0) return "";

	let _blocksToConvert = normalizeMalformedLinks(blocks);

	if (hasMentions(_blocksToConvert)) {
		_blocksToConvert = convertMentionsToText(_blocksToConvert);
	}

	const e = getEditor();
	return (e as any)._exportManager.blocksToMarkdownLossy(_blocksToConvert);
}

/**
 * Converts Markdown string to BlockNote JSON blocks.
 * @param markdown - Markdown string
 * @returns Array of BlockNote block objects
 */
export function markdownToBlocknote(markdown: string): any[] {
	if (!markdown || markdown.trim() === "") return [];
	const e = getEditor();
	return (e as any)._exportManager.tryParseMarkdownToBlocks(markdown);
}

/** The fields of a project member needed to turn an `@mention` into a chip. */
export interface MentionableMember {
	id: string;
	user_id?: string | null;
	agent_id?: string | null;
	member_type?: string | null;
	username: string;
	full_name: string;
}

// `@Full Name (id: <uuid>)` is how teamMention chips read back (see
// convertMentionsToText), so agents copy it verbatim; `@username` is the
// short form. Preceded by start/whitespace/opening punctuation so e-mail
// addresses are left alone.
const MENTION_PATTERN =
	/(?<![\w@])@(?:[^@\n()]{1,80}?\s*\(id:\s*([0-9a-fA-F-]{36})\)|([\w.-]+))/g;

function findMember(
	members: MentionableMember[],
	id: string | undefined,
	username: string | undefined,
): MentionableMember | undefined {
	if (id) {
		const lower = id.toLowerCase();
		return members.find((m) =>
			[m.id, m.user_id, m.agent_id].some((v) => v?.toLowerCase() === lower),
		);
	}
	const lower = username?.replace(/[.]+$/, "").toLowerCase();
	return members.find((m) => m.username.toLowerCase() === lower);
}

/** The id the web UI embeds in a teamMention: the agent id for agent members
 *  (the API resolves it to start the agent), the user id otherwise. */
function mentionId(member: MentionableMember): string {
	return member.member_type === "agent"
		? (member.agent_id ?? member.user_id ?? member.id)
		: (member.user_id ?? member.id);
}

function linkInlineContent(
	content: any[],
	members: MentionableMember[],
): any[] {
	const out: any[] = [];
	for (const node of content) {
		if (
			node?.type !== "text" ||
			typeof node.text !== "string" ||
			node.styles?.code
		) {
			out.push(node);
			continue;
		}
		let last = 0;
		const text: string = node.text;
		for (const match of text.matchAll(MENTION_PATTERN)) {
			const member = findMember(members, match[1], match[2]);
			if (!member) continue;
			const start = match.index ?? 0;
			// A trailing '.' on a bare @username is sentence punctuation.
			const matched =
				match[2] && match[2].endsWith(".")
					? match[0].replace(/\.+$/, "")
					: match[0];
			if (start > last) {
				out.push({ ...node, text: text.slice(last, start) });
			}
			out.push({
				type: "teamMention",
				props: { id: mentionId(member), name: member.full_name, avatar: "" },
			});
			last = start + matched.length;
		}
		if (last === 0) {
			out.push(node);
		} else if (last < text.length) {
			out.push({ ...node, text: text.slice(last) });
		}
	}
	return out;
}

/**
 * Replaces `@username` / `@Name (id: <uuid>)` text in BlockNote blocks with
 * structured teamMention chips for known project members, so the API
 * notifies the person or starts the agent. Unknown names stay plain text.
 */
export function linkTeamMentions(
	blocks: any[],
	members: MentionableMember[],
): any[] {
	return blocks.map((block) => {
		if (!block || block.type === "codeBlock") return block;
		const next = { ...block };
		if (Array.isArray(block.content)) {
			next.content = linkInlineContent(block.content, members);
		}
		if (Array.isArray(block.children)) {
			next.children = linkTeamMentions(block.children, members);
		}
		return next;
	});
}
