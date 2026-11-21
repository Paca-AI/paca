import "@blocknote/core/fonts/inter.css";
import "@blocknote/shadcn/style.css";

import type { PartialBlock } from "@blocknote/core";
import { SuggestionMenu } from "@blocknote/core/extensions";
import { SideMenuController, useCreateBlockNote } from "@blocknote/react";
import { BlockNoteView } from "@blocknote/shadcn";
import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import { useThemeMode } from "@/hooks/use-theme-mode";
import { matchAnnotationLinkOnly } from "@/lib/annotation-link";
import { safeHref } from "@/lib/chat-content";
import type { SlashCommand } from "@/lib/slash-commands";
import { createAnnotationPasteHandler } from "./blocknote-annotation-paste-handler";
import { CustomSideMenu } from "./blocknote-custom-side-menu";
import { customSchema } from "./blocknote-schema";
import { MentionSuggestionMenus } from "./mention-suggestion-menus";

export interface CommentEditorHandle {
	getBlocks: () => unknown[];
	focus: () => void;
	clear: () => void;
}

export type { SlashCommand } from "@/lib/slash-commands";

export interface CommentEditorProps {
	initialBlocks?: unknown[];
	onSubmit?: () => void;
	/** Called after every change to the document (typing, paste, clear). */
	onChange?: (blocks: unknown[]) => void;
	projectId?: string | null;
	placeholder?: string;
	autoFocus?: boolean;
	/**
	 * Which key submits. Comments use "mod-enter" (Enter is a new block); chat
	 * uses "enter" (Shift+Enter is a new line, and Mod+Enter still submits from
	 * a code block, where Enter is a newline).
	 */
	submitKey?: "mod-enter" | "enter";
	/** Escape pressed with no suggestion menu open (cancels an edit). */
	onEscape?: () => void;
	/** Listed in the "/" menu above the block types (see useSlashCommands). */
	commands?: SlashCommand[];
	/** A command that needs no arguments was picked from the "/" menu. */
	onCommand?: (command: SlashCommand) => void;
}

export const CommentEditor = forwardRef<
	CommentEditorHandle,
	CommentEditorProps
>(function CommentEditor(
	{
		initialBlocks,
		onSubmit,
		onChange,
		projectId,
		placeholder,
		autoFocus,
		submitKey = "mod-enter",
		onEscape,
		commands,
		onCommand,
	},
	ref,
) {
	const { resolvedMode } = useThemeMode();
	const initializedRef = useRef(false);
	const live = useRef({ onSubmit, onEscape, submitKey });
	live.current = { onSubmit, onEscape, submitKey };

	const editor = useCreateBlockNote({
		schema: customSchema,
		pasteHandler: createAnnotationPasteHandler(),
		placeholders: placeholder
			? { default: placeholder, emptyDocument: placeholder }
			: undefined,
		initialContent:
			initialBlocks && initialBlocks.length > 0
				? (initialBlocks as PartialBlock[])
				: undefined,
		_tiptapOptions: {
			editorProps: {
				handleKeyDown: (view, event) => {
					if (event.isComposing) return false;
					const menuOpen = () => {
						try {
							return editor?.getExtension(SuggestionMenu)?.shown() ?? false;
						} catch {
							return false;
						}
					};
					if (event.key === "Escape") {
						if (live.current.onEscape && !menuOpen()) {
							event.preventDefault();
							live.current.onEscape();
							return true;
						}
						return false;
					}
					if (event.key !== "Enter") return false;
					const mod = event.ctrlKey || event.metaKey;
					if (live.current.submitKey === "mod-enter") {
						if (!mod) return false;
					} else {
						if (event.shiftKey || event.altKey) return false;
						// Enter picks a suggestion while a menu is open.
						if (menuOpen()) return false;
						const inCode = view.state.selection.$from.parent.type.name
							.toLowerCase()
							.includes("code");
						if (inCode && !mod) return false;
					}
					event.preventDefault();
					live.current.onSubmit?.();
					return true;
				},
			},
		},
	});

	const onChangeRef = useRef(onChange);
	onChangeRef.current = onChange;
	useEffect(
		() =>
			editor.onChange(() => {
				onChangeRef.current?.(editor.document as unknown[]);
			}),
		[editor],
	);

	useEffect(() => {
		if (autoFocus) editor.focus();
	}, [autoFocus, editor]);

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
				projectId={projectId}
				commands={commands}
				onCommand={onCommand}
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
		initialContent:
			blocks && blocks.length > 0 ? (blocks as PartialBlock[]) : undefined,
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
		return convertAnnotationLinks(
			convertMermaidCodeBlocks(stripUnsafeLinks(content)),
		);
	}
	if (typeof content === "string" && content.trim().length > 0) {
		return textToBlocks(content);
	}
	return [];
}

/**
 * Replaces a link whose target is not http(s) or mailto (`javascript:` and
 * the like) with its plain text, so stored content can never render as a
 * script link. Recurses into children; returns the input when nothing changed.
 */
export function stripUnsafeLinks(blocks: unknown[]): unknown[] {
	let changed = false;
	const cleanInline = (nodes: unknown): unknown => {
		if (!Array.isArray(nodes)) return nodes;
		return nodes.flatMap((n) => {
			const node = n as { type?: string; href?: string; content?: unknown };
			if (node && node.type === "link") {
				const inner = cleanInline(node.content) as unknown[];
				if (!safeHref(node.href)) {
					changed = true;
					return inner;
				}
				return [{ ...node, content: inner }];
			}
			return [n];
		});
	};
	const cleanBlock = (b: unknown): unknown => {
		if (!b || typeof b !== "object") return b;
		const block = b as { content?: unknown; children?: unknown };
		return {
			...block,
			content: cleanInline(block.content),
			children: Array.isArray(block.children)
				? block.children.map(cleanBlock)
				: block.children,
		};
	};
	const out = blocks.map(cleanBlock);
	return changed ? out : blocks;
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
 *
 * Uses matchAnnotationLinkOnly, not matchAnnotationLink's plain substring
 * search: this replaces the *entire* paragraph block, so it must only fire
 * when the paragraph is nothing but the link itself (the actual shape
 * CreateTaskFromAnnotation's description generates) — matching on a link
 * merely present somewhere inside a longer sentence would silently drop
 * the rest of that sentence every time the content reloads.
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
		const match = matchAnnotationLinkOnly(text);
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
