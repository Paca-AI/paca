import { describe, expect, it } from "vitest";
import {
	descriptionFromMarkdown,
	mergeConclusionIntoDescription,
} from "./conclusion-description";

describe("mergeConclusionIntoDescription", () => {
	it("preserves the description and appends a current-conclusion proposal", () => {
		const original = [{ type: "paragraph", content: [{ text: "Scope" }] }];
		const proposed = mergeConclusionIntoDescription(
			original,
			"Use the new contract",
			"Current conclusion",
		);
		expect(proposed).toHaveLength(3);
		expect(proposed[0]).toEqual(original[0]);
		expect(proposed[2]).toMatchObject({
			type: "paragraph",
			content: [{ text: "Use the new contract" }],
		});
		expect(original).toHaveLength(1);
	});

	it("replaces the recognized conclusion instead of appending duplicates", () => {
		const original = mergeConclusionIntoDescription(
			[],
			"First",
			"Current conclusion",
		);
		const proposed = mergeConclusionIntoDescription(
			original,
			"Revised",
			"当前结论",
		);
		expect(proposed).toHaveLength(2);
		expect(proposed[0]).toMatchObject({
			type: "heading",
			content: [{ text: "当前结论" }],
		});
		expect(proposed[1]).toMatchObject({
			content: [{ text: "Revised" }],
		});
	});

	it("turns a structured session draft into description blocks", () => {
		const original = [
			{ type: "paragraph", content: [{ text: "Scope" }] },
			{ type: "heading", props: { level: 2 }, content: [{ text: "Next" }] },
		];
		const proposed = mergeConclusionIntoDescription(
			original,
			"## Session conclusion\n\n### Discussion\n1. New scope\n\n### Key conclusion\nUse checkpoints.",
			"Current conclusion",
		);

		expect(proposed.slice(-6)).toMatchObject([
			{ type: "heading", content: [{ text: "Next" }] },
			{ type: "heading", content: [{ text: "Current conclusion" }] },
			{
				type: "heading",
				props: { level: 3 },
				content: [{ text: "Discussion" }],
			},
			{ type: "paragraph", content: [{ text: "1. New scope" }] },
			{
				type: "heading",
				props: { level: 3 },
				content: [{ text: "Key conclusion" }],
			},
			{ type: "paragraph", content: [{ text: "Use checkpoints." }] },
		]);
	});
});

describe("descriptionFromMarkdown", () => {
	it("turns a standalone AI description into structured editable blocks", () => {
		const blocks = descriptionFromMarkdown(
			"# Login reliability\n\nKeep **existing facts**.\n\n## Acceptance criteria\n- Login succeeds\n1. Audit remains available",
		);

		expect(blocks).toMatchObject([
			{ type: "heading", props: { level: 1 } },
			{
				type: "paragraph",
				content: expect.arrayContaining([
					expect.objectContaining({
						text: "existing facts",
						styles: { bold: true },
					}),
				]),
			},
			{ type: "heading", props: { level: 2 } },
			{ type: "bulletListItem" },
			{ type: "numberedListItem" },
		]);
	});
});
