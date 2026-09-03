import "@blocknote/core/fonts/inter.css";
import "@blocknote/shadcn/style.css";

import type { PartialBlock } from "@blocknote/core";
import { SideMenuController, useCreateBlockNote } from "@blocknote/react";
import { BlockNoteView } from "@blocknote/shadcn";
import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { useThemeMode } from "@/hooks/use-theme-mode";
import { matchAnnotationLink } from "@/lib/annotation-link";
import { useMentionData } from "@/lib/mention-api";
import { createAnnotationPasteHandler } from "./blocknote-annotation-paste-handler";
import { CustomSideMenu } from "./blocknote-custom-side-menu";
import { customSchema } from "./blocknote-schema";
import { MentionSuggestionMenus } from "./mention-suggestion-menus";

export interface CommentEditorHandle {
	getBlocks: () => unknown[];
	focus: () => void;
	clear: () => void;
}

interface CommentEditorProps {
	initialBlocks?: unknown[];
	onSubmit?: () => void;
	projectId?: string | null;
}

export const CommentEditor = forwardRef<
	CommentEditorHandle,
	CommentEditorProps
>(function CommentEditor({ initialBlocks, onSubmit, projectId }, ref) {
	const { resolvedMode } = useThemeMode();
	const initializedRef = useRef(false);
	const { teamMembers, documents } = useMentionData(projectId);

	const editor = useCreateBlockNote({
		schema: customSchema,
		pasteHandler: createAnnotationPasteHandler(),
		initialContent:
			initialBlocks && initialBlocks.length > 0
				? (initialBlocks as PartialBlock[])
				: undefined,
		_tiptapOptions: {
			editorProps: {
				handleKeyDown: (_view, event) => {
					if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
						event.preventDefault();
						onSubmit?.();
						return true;
					}
					return false;
				},
			},
		},
	});

	useImperativeHandle(ref, () => ({
		getBlocks: () => {
			const blocks = editor.document as unknown[];
			return stripTrailingEmptyBlocks(blocks);
		},
		focus: () => editor.focus(),
		clear: () => {
			editor.removeBlocks(editor.document);
		},
	}));

	useEffect(() => {
		if (initializedRef.current) return;
		initializedRef.current = true;
		if (initialBlocks && initialBlocks.length > 0) {
			editor.replaceBlocks(editor.document, initialBlocks as PartialBlock[]);
		}
	}, [initialBlocks, editor]);

	return (
		<BlockNoteView
			editor={editor}
			editable
			theme={resolvedMode}
			className="bn-shadcn"
			sideMenu={false}
			slashMenu={false}
		>
			<SideMenuController sideMenu={CustomSideMenu} />
			<MentionSuggestionMenus
				editor={editor}
				teamMembers={teamMembers}
				projectId={projectId}
				documents={documents}
			/>
		</BlockNoteView>
	);
});

interface CommentDisplayProps {
	blocks: unknown[];
}

export function CommentDisplay({ blocks }: CommentDisplayProps) {
	const { resolvedMode } = useThemeMode();

	const editor = useCreateBlockNote({
		schema: customSchema,
		trailingBlock: false,
	});

	useEffect(() => {
		if (blocks && blocks.length > 0) {
			editor.replaceBlocks(editor.document, blocks as PartialBlock[]);
		} else {
			editor.replaceBlocks(editor.document, []);
		}
	}, [blocks, editor]);

	return (
		<BlockNoteView
			editor={editor}
			editable={false}
			theme={resolvedMode}
			className="bn-comment-display"
			sideMenu={false}
		/>
	);
}

export function textToBlocks(text: string): unknown[] {
	if (!text) return [];
	return [
		{
			type: "paragraph",
			props: {
				textColor: "default",
				backgroundColor: "default",
				textAlignment: "left",
			},
			content: [{ type: "text", text, styles: {} }],
			children: [],
		},
	];
}

export function blocksToText(blocks: unknown[]): string {
	if (!Array.isArray(blocks)) return "";
	const parts: string[] = [];
	for (const block of blocks) {
		const b = block as { content?: Array<{ text?: string }> };
		if (Array.isArray(b.content)) {
			for (const inline of b.content) {
				if (inline.text) parts.push(inline.text);
			}
		}
	}
	return parts.join(" ");
}

export function isBlocksContent(content: unknown): content is unknown[] {
	return Array.isArray(content);
}

/**
 * Normalizes arbitrary stored content into a BlockNote block array so
 * editors/viewers never receive non-array data (which crashes BlockNote's
 * internal `.map()` calls over blocks). Legacy plain-text content — e.g.
 * data saved before content validation existed — is wrapped into a single
 * paragraph block so it stays visible instead of silently disappearing.
 */
export function normalizeBlockContent(content: unknown): unknown[] {
	if (Array.isArray(content)) {
		return convertAnnotationLinks(convertMermaidCodeBlocks(content));
	}
	if (typeof content === "string" && content.trim().length > 0) {
		return textToBlocks(content);
	}
	return [];
}

/**
 * Rewrites any `codeBlock` whose language is "mermaid" into the custom
 * `mermaid` block so it renders as a diagram instead of showing raw source.
 * This runs on the load path (via normalizeBlockContent), so a ```mermaid
 * fence — already stored in a doc, or typed/pasted and then saved+reopened —
 * renders as a diagram; a fence typed into an already-open editor stays a code
 * block until the content is reloaded (live in-editor conversion is out of
 * scope here). Pure and shallow (top-level blocks only; BlockNote code blocks
 * can't nest), so it's cheap to run on every load and trivially testable. The
 * custom block's `toExternalHTML` degrades back to a ```mermaid fence, so
 * nothing is trapped for a client that lacks the block type.
 */
export function convertMermaidCodeBlocks(blocks: unknown[]): unknown[] {
	let changed = false;
	const out = blocks.map((b) => {
		if (!b || typeof b !== "object") return b;
		const block = b as {
			type?: string;
			props?: { language?: string };
			content?: Array<{ type?: string; text?: string }>;
		};
		if (
			block.type !== "codeBlock" ||
			block.props?.language?.toLowerCase() !== "mermaid"
		) {
			return b;
		}
		const code = Array.isArray(block.content)
			? block.content
					.map((c) => (c?.type === "text" ? (c.text ?? "") : ""))
					.join("")
			: "";
		changed = true;
		return { type: "mermaid", props: { code }, content: [] };
	});
	return changed ? out : blocks;
}

/**
 * Rewrites any `paragraph` whose text is (or contains) a comment-detail-page
 * URL into the custom `annotationCard` block, so a task/doc description
 * generated with just that link (see services/api's
 * annotation_service.go/task_description.go — CreateTaskFromAnnotation
 * deliberately writes only the URL, not the comment body/page/console/
 * network context as text) renders the rich, live-fetching preview instead
 * of a bare link. Same load-path trick as convertMermaidCodeBlocks — this
 * runs via normalizeBlockContent, so an unconverted link already stored in a
 * doc (or one the paste handler somehow missed) still upgrades once the
 * content reloads. Pure and shallow (top-level blocks only), matching
 * convertMermaidCodeBlocks's own scope.
 */
export function convertAnnotationLinks(blocks: unknown[]): unknown[] {
	let changed = false;
	const out = blocks.map((b) => {
		if (!b || typeof b !== "object") return b;
		const block = b as {
			type?: string;
			content?: Array<{ type?: string; text?: string }>;
		};
		if (block.type !== "paragraph" || !Array.isArray(block.content)) {
			return b;
		}
		const text = block.content
			.map((c) => (c?.type === "text" ? (c.text ?? "") : ""))
			.join("");
		const match = matchAnnotationLink(text);
		if (!match) return b;
		changed = true;
		return {
			type: "annotationCard",
			props: {
				id: match.annotationId,
				projectId: match.projectId,
				environmentId: match.environmentId,
				portForwardId: match.portForwardId,
			},
			content: [],
		};
	});
	return changed ? out : blocks;
}

function stripTrailingEmptyBlocks(blocks: unknown[]): unknown[] {
	if (!Array.isArray(blocks) || blocks.length === 0) return blocks;
	const lastBlock = blocks[blocks.length - 1] as { content?: unknown[] };
	if (hasContent(lastBlock)) return blocks;
	return blocks.slice(0, -1);
}

function hasContent(block: { content?: unknown[] }): boolean {
	if (!block.content || !Array.isArray(block.content)) return false;
	for (const item of block.content) {
		const inline = item as { text?: string } | null;
		if (inline?.text && inline.text.trim() !== "") return true;
	}
	return false;
}
