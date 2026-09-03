import type { BlockNoteEditor } from "@blocknote/core";
import { matchAnnotationLink } from "@/lib/annotation-link";
import type { customSchema } from "./blocknote-schema";

type Editor = BlockNoteEditor<
	typeof customSchema.blockSchema,
	typeof customSchema.inlineContentSchema,
	typeof customSchema.styleSchema
>;

interface PasteHandlerContext {
	event: ClipboardEvent;
	editor: Editor;
	defaultPasteHandler: (context?: {
		prioritizeMarkdownOverHTML?: boolean;
		plainTextAsMarkdown?: boolean;
	}) => boolean | undefined;
}

/** Recognizes a comment-detail-page link (copied via the extension's pin
 * popover or the comment detail page's own Copy button — see
 * lib/annotation-link.ts) pasted into a BlockNote editor and inserts a rich
 * annotationCard block instead of the raw URL text. Fully synchronous: the
 * card only needs the four IDs parsed from the link itself and does its
 * own live fetch when it renders (see blocknote-annotation-card-block.tsx),
 * so — unlike a design that bakes in a title/screenshot fetched up front —
 * there's no async work to coordinate with BlockNote's synchronous
 * pasteHandler contract, and the card never shows stale data. */
export function createAnnotationPasteHandler() {
	return (context: PasteHandlerContext): boolean | undefined => {
		const text = context.event.clipboardData?.getData("text/plain") ?? "";
		const match = matchAnnotationLink(text);
		if (!match) return context.defaultPasteHandler();

		const { editor } = context;
		const currentBlock = editor.getTextCursorPosition().block;
		editor.insertBlocks(
			[
				{
					type: "annotationCard",
					props: {
						id: match.annotationId,
						projectId: match.projectId,
						environmentId: match.environmentId,
						portForwardId: match.portForwardId,
					},
				},
			],
			currentBlock,
			"after",
		);
		return true;
	};
}
