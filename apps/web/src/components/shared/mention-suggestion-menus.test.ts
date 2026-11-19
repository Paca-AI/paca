import { describe, expect, it } from "vitest";
import { groupContiguously } from "./mention-suggestion-menus";

describe("groupContiguously", () => {
	it("keeps each group together in order of first appearance", () => {
		const items = [
			{ title: "H1", group: "Headings" },
			{ title: "Table", group: "Advanced" },
			{ title: "Emoji", group: "Others" },
			// Added last, but belongs with "Advanced" — it must not start a second run.
			{ title: "Mermaid", group: "Advanced" },
		];
		expect(groupContiguously(items).map((i) => i.title)).toEqual([
			"H1",
			"Table",
			"Mermaid",
			"Emoji",
		]);
	});

	it("leaves an already grouped list unchanged", () => {
		const items = [
			{ title: "a", group: "x" },
			{ title: "b", group: "x" },
			{ title: "c", group: "y" },
		];
		expect(groupContiguously(items)).toEqual(items);
	});

	it("keeps ungrouped items together", () => {
		const items = [{ title: "a" }, { title: "b", group: "x" }, { title: "c" }];
		expect(groupContiguously(items).map((i) => i.title)).toEqual([
			"a",
			"c",
			"b",
		]);
	});
});
