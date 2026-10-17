import { describe, expect, it } from "vitest";
import { blocknoteToMarkdown } from "../../utils/converters.js";

const text = (value: string) => ({ type: "text", text: value, styles: {} });
const paragraph = (content: any[], children: any[] = []) => ({
	id: "comment",
	type: "paragraph",
	props: {},
	content,
	children,
});

describe("blocknoteToMarkdown with malformed links (#589)", () => {
	it.each([
		[
			"missing href and content",
			{ type: "link", text: "legacy label", styles: {} },
			"legacy label",
		],
		[
			"missing content",
			{ type: "link", href: "https://example.com", text: "legacy label" },
			"legacy label",
		],
		[
			"null content",
			{
				type: "link",
				href: "https://example.com",
				content: null,
				text: "legacy label",
			},
			"legacy label",
		],
		[
			"string content",
			{ type: "link", href: "https://example.com", content: "legacy label" },
			"legacy label",
		],
		[
			"missing href",
			{ type: "link", content: [text("first"), text(" second")] },
			"first second",
		],
		[
			"non-string href",
			{ type: "link", href: 42, content: [text("label")] },
			"label",
		],
		[
			"string array item",
			{ type: "link", href: "https://example.com", content: ["retained"] },
			"retained",
		],
		[
			"null array item",
			{ type: "link", href: "https://example.com", content: [null] },
			"",
		],
		[
			"number array item",
			{ type: "link", href: "https://example.com", content: [42] },
			"",
		],
		[
			"empty object item",
			{ type: "link", href: "https://example.com", content: [{}] },
			"",
		],
		[
			"text with invalid styles",
			{
				type: "link",
				href: "https://example.com",
				content: [{ type: "text", text: "retained", styles: "bold" }],
			},
			"retained",
		],
		[
			"mixed array items",
			{
				type: "link",
				href: "https://example.com",
				content: [text("first"), null, " second"],
			},
			"first second",
		],
		["no recoverable text", { type: "link", text: "", styles: {} }, ""],
	])("preserves surrounding text for %s", (_name, link, label) => {
		const input = [paragraph([text("Before "), link, text(" after")])];
		const before = structuredClone(input);
		const result = blocknoteToMarkdown(input);
		expect(result).toContain("Before");
		expect(result).toContain("after");
		expect(result).not.toContain("](");
		if (label) expect(result).toContain(label);
		expect(input).toEqual(before);
	});

	it.each([
		undefined,
		null,
	])("preserves a legacy link with %s styles", (styles) => {
		const part =
			styles === undefined
				? { type: "text", text: "legacy label" }
				: { type: "text", text: "legacy label", styles };
		const input = [
			paragraph([
				text("Before "),
				{ type: "link", href: "https://example.com", content: [part] },
				text(" after"),
			]),
		];
		const before = structuredClone(input);
		expect(blocknoteToMarkdown(input)).toContain(
			"Before [legacy label](https://example.com) after",
		);
		expect(input).toEqual(before);
	});

	it("keeps a comment readable with all 16 malformed inline nodes from the report", () => {
		const content = [
			text("Retain this comment"),
			...Array.from({ length: 16 }, () => ({
				text: "",
				type: "link",
				styles: {},
			})),
		];
		expect(blocknoteToMarkdown([paragraph(content)])).toContain(
			"Retain this comment",
		);
	});

	it("handles malformed links in nested child blocks", () => {
		const input = [
			paragraph(
				[text("Parent")],
				[paragraph([text("Child "), { type: "link", text: "retained" }])],
			),
		];
		const result = blocknoteToMarkdown(input);
		expect(result).toContain("Parent");
		expect(result).toContain("Child retained");
	});

	it("handles malformed links inside table cells", () => {
		const input = [
			{
				type: "table",
				content: {
					type: "tableContent",
					rows: [
						{ cells: [[text("Cell "), { type: "link", text: "retained" }]] },
					],
				},
			},
		];
		expect(blocknoteToMarkdown(input)).toContain("Cell retained");
	});

	it("preserves valid links, formatting, and custom mentions beside malformed links", () => {
		const input = [
			paragraph([
				{
					type: "link",
					href: "https://example.com",
					content: [text("valid link")],
				},
				text(" "),
				{ type: "link", content: [text("unlinked label")] },
				text(" "),
				{ type: "teamMention", props: { name: "Alice", id: "user-1" } },
				text(" "),
				{ type: "text", text: "bold", styles: { bold: true } },
			]),
		];
		const before = structuredClone(input);
		const result = blocknoteToMarkdown(input);
		expect(result).toContain("[valid link](https://example.com)");
		expect(result).toContain("unlinked label");
		expect(result).toContain("@Alice (id: user-1)");
		expect(result).toContain("**bold**");
		expect(input).toEqual(before);
	});
});
