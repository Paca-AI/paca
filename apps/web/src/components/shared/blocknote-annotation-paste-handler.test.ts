import { describe, expect, it, vi } from "vitest";
import { createAnnotationPasteHandler } from "./blocknote-annotation-paste-handler";

const url =
	"https://paca.example.com/projects/p1/environments/e1/port-forwards/pf1/comments/a1";

function makeContext(text: string) {
	const insertBlocks = vi.fn();
	const defaultPasteHandler = vi.fn(() => true);
	const editor = {
		getTextCursorPosition: () => ({ block: { id: "current-block" } }),
		insertBlocks,
	};
	const event = {
		clipboardData: { getData: () => text },
	} as unknown as ClipboardEvent;
	return {
		context: {
			event,
			editor: editor as never,
			defaultPasteHandler,
		},
		insertBlocks,
		defaultPasteHandler,
	};
}

describe("createAnnotationPasteHandler", () => {
	it("inserts an annotationCard and skips the default handler when the paste is exactly the link", () => {
		const handler = createAnnotationPasteHandler();
		const { context, insertBlocks, defaultPasteHandler } = makeContext(url);

		const result = handler(context);

		expect(result).toBe(true);
		expect(insertBlocks).toHaveBeenCalledTimes(1);
		expect(insertBlocks.mock.calls[0][0]).toEqual([
			{
				type: "annotationCard",
				props: {
					id: "a1",
					projectId: "p1",
					environmentId: "e1",
					portForwardId: "pf1",
				},
			},
		]);
		expect(defaultPasteHandler).not.toHaveBeenCalled();
	});

	it("falls through to the default paste handler, without dropping the surrounding text, when the link shares the paste with other content", () => {
		// Regression test: this used to match on a plain substring, insert
		// just the card, and return true — silently discarding the rest of
		// the pasted text instead of letting it paste normally.
		const handler = createAnnotationPasteHandler();
		const { context, insertBlocks, defaultPasteHandler } = makeContext(
			`Check this out: ${url} — thanks!`,
		);

		const result = handler(context);

		expect(insertBlocks).not.toHaveBeenCalled();
		expect(defaultPasteHandler).toHaveBeenCalledTimes(1);
		expect(result).toBe(true);
	});

	it("falls through to the default paste handler when there is no link at all", () => {
		const handler = createAnnotationPasteHandler();
		const { context, insertBlocks, defaultPasteHandler } =
			makeContext("just some text");

		handler(context);

		expect(insertBlocks).not.toHaveBeenCalled();
		expect(defaultPasteHandler).toHaveBeenCalledTimes(1);
	});
});
