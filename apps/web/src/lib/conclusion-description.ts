type BlockNoteBlock = Record<string, unknown>;

function inlineText(value: unknown): string {
	if (!Array.isArray(value)) return "";
	return value
		.map((item) => {
			if (typeof item === "string") return item;
			if (!item || typeof item !== "object") return "";
			return typeof (item as { text?: unknown }).text === "string"
				? String((item as { text: string }).text)
				: "";
		})
		.join("");
}

function inlineContent(text: string): BlockNoteBlock[] {
	const content: BlockNoteBlock[] = [];
	const normalized = text.replace(/\[([^\]]+)\]\(([^)]+)\)/g, "$1 ($2)");
	const tokenPattern = /(\*\*[^*]+\*\*|`[^`]+`)/g;
	let cursor = 0;
	for (const match of normalized.matchAll(tokenPattern)) {
		const index = match.index ?? 0;
		if (index > cursor) {
			content.push({
				type: "text",
				text: normalized.slice(cursor, index),
				styles: {},
			});
		}
		const token = match[0];
		const bold = token.startsWith("**");
		content.push({
			type: "text",
			text: token.slice(bold ? 2 : 1, bold ? -2 : -1),
			styles: bold ? { bold: true } : { code: true },
		});
		cursor = index + token.length;
	}
	if (cursor < normalized.length) {
		content.push({
			type: "text",
			text: normalized.slice(cursor),
			styles: {},
		});
	}
	return content.length > 0
		? content
		: [{ type: "text", text: "", styles: {} }];
}

function textBlock(
	type: "heading" | "paragraph" | "bulletListItem" | "numberedListItem",
	text: string,
	headingLevel = 2,
): BlockNoteBlock {
	return {
		type,
		...(type === "heading" ? { props: { level: headingLevel } } : {}),
		content: inlineContent(text),
		children: [],
	};
}

function headingLevel(block: unknown): number | undefined {
	if (!block || typeof block !== "object") return undefined;
	const record = block as BlockNoteBlock;
	if (record.type !== "heading") return undefined;
	const level = (record.props as { level?: unknown } | undefined)?.level;
	return typeof level === "number" ? level : 1;
}

function summaryBlocks(summary: string): BlockNoteBlock[] {
	const blocks: BlockNoteBlock[] = [];
	let skippedDocumentTitle = false;
	for (const line of summary.split(/\r?\n/)) {
		const value = line.trim();
		if (!value) continue;
		const heading = /^(#{2,6})\s+(.+)$/.exec(value);
		if (heading) {
			if (!skippedDocumentTitle) {
				skippedDocumentTitle = true;
				continue;
			}
			blocks.push(textBlock("heading", heading[2] ?? "", 3));
			continue;
		}
		blocks.push(textBlock("paragraph", value));
	}
	return blocks.length > 0 ? blocks : [textBlock("paragraph", summary.trim())];
}

/** Converts the agent's standalone Markdown proposal into editable BlockNote blocks. */
export function descriptionFromMarkdown(markdown: string): unknown[] {
	const blocks: BlockNoteBlock[] = [];
	let paragraph: string[] = [];
	let insideFence = false;
	const flushParagraph = () => {
		const text = paragraph.join(" ").trim();
		if (text) blocks.push(textBlock("paragraph", text));
		paragraph = [];
	};

	for (const rawLine of markdown.split(/\r?\n/)) {
		const value = rawLine.trim();
		if (/^```/.test(value)) {
			flushParagraph();
			insideFence = !insideFence;
			continue;
		}
		if (!value) {
			flushParagraph();
			continue;
		}
		if (insideFence) {
			paragraph.push(value);
			continue;
		}
		const heading = /^(#{1,6})\s+(.+)$/.exec(value);
		if (heading) {
			flushParagraph();
			blocks.push(
				textBlock(
					"heading",
					heading[2] ?? "",
					Math.min(3, heading[1]?.length ?? 1),
				),
			);
			continue;
		}
		const bullet = /^[-*+]\s+(.+)$/.exec(value);
		if (bullet) {
			flushParagraph();
			blocks.push(textBlock("bulletListItem", bullet[1] ?? ""));
			continue;
		}
		const numbered = /^\d+[.)]\s+(.+)$/.exec(value);
		if (numbered) {
			flushParagraph();
			blocks.push(textBlock("numberedListItem", numbered[1] ?? ""));
			continue;
		}
		paragraph.push(value.replace(/^>\s*/, ""));
	}
	flushParagraph();

	return blocks.length > 0 ? blocks : [textBlock("paragraph", markdown.trim())];
}

/**
 * Builds a reviewable task-description proposal without mutating the task.
 * A later revision replaces the recognized current-conclusion section instead
 * of accumulating another blind append.
 */
export function mergeConclusionIntoDescription(
	description: unknown[] | null | undefined,
	summary: string,
	heading: string,
): unknown[] {
	const blocks = Array.isArray(description)
		? description.map((block) =>
				block && typeof block === "object" ? structuredClone(block) : block,
			)
		: [];
	const recognizedHeadings = new Set([
		heading.trim(),
		"当前结论",
		"Current conclusion",
	]);
	const headingIndex = blocks.findIndex((block) => {
		if (!block || typeof block !== "object") return false;
		const record = block as BlockNoteBlock;
		return (
			record.type === "heading" &&
			recognizedHeadings.has(inlineText(record.content).trim())
		);
	});
	const proposedBlocks = summaryBlocks(summary);
	if (headingIndex >= 0) {
		blocks[headingIndex] = textBlock("heading", heading.trim());
		let sectionEnd = headingIndex + 1;
		while (sectionEnd < blocks.length) {
			const level = headingLevel(blocks[sectionEnd]);
			if (level !== undefined && level <= 2) break;
			sectionEnd += 1;
		}
		blocks.splice(
			headingIndex + 1,
			sectionEnd - headingIndex - 1,
			...proposedBlocks,
		);
		return blocks;
	}
	return [...blocks, textBlock("heading", heading.trim()), ...proposedBlocks];
}
