import { BlockNoteEditor } from "@blocknote/core";
import { createDocument, type Document } from "./doc-api";

let editor: BlockNoteEditor | null = null;

function parser(): BlockNoteEditor {
	editor ??= BlockNoteEditor.create();
	return editor;
}

/** A Markdown file: by extension, or by the type the browser reports. */
export function isMarkdownFile(file: { name: string; type?: string }): boolean {
	return /\.(md|markdown)$/i.test(file.name) || file.type === "text/markdown";
}

interface BlockLike {
	type?: string;
	props?: { level?: number };
	content?: unknown;
}

function plainText(content: unknown): string {
	if (!Array.isArray(content)) return "";
	return content
		.map((c: { text?: unknown; content?: unknown }) =>
			typeof c?.text === "string" ? c.text : plainText(c?.content),
		)
		.join("")
		.trim();
}

/**
 * Turns the text of a Markdown file into a document: the blocks BlockNote's own
 * Markdown parser makes, and a title. A file that starts with a level-1 heading
 * (as a downloaded document does) takes it as the title and drops it from the
 * body; any other file is titled by its name without the extension.
 */
export function markdownToDocument(
	markdown: string,
	fileName: string,
): { title: string; blocks: unknown[] } {
	const text = markdown.replace(/^﻿/, "");
	const blocks: unknown[] =
		text.trim() === "" ? [] : parser().tryParseMarkdownToBlocks(text);
	const fallback = fileName.replace(/\.(md|markdown)$/i, "").trim();
	const first = blocks[0] as BlockLike | undefined;
	if (first?.type === "heading" && first.props?.level === 1) {
		const heading = plainText(first.content);
		if (heading) return { title: heading, blocks: blocks.slice(1) };
	}
	return { title: fallback || "Untitled", blocks };
}

/**
 * Creates a document from a Markdown file in a docs folder (null: the top of
 * Documentation), so an uploaded Markdown file becomes a document rather than
 * an attachment.
 */
export async function importMarkdownFile(
	projectId: string,
	folderId: string | null,
	file: File,
): Promise<Document> {
	const { title, blocks } = markdownToDocument(await file.text(), file.name);
	return createDocument(projectId, {
		title,
		folder_id: folderId,
		content: blocks,
	});
}
