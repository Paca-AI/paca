import { describe, expect, it } from "vitest";
import { collectInlineMedia, fileUri, parseMediaUri } from "./inline-media";

describe("parseMediaUri", () => {
	it("reads task attachments and project files", () => {
		expect(parseMediaUri("attachment://p1/t1/a1")).toEqual({
			source: "attachment",
			projectId: "p1",
			taskId: "t1",
			id: "a1",
		});
		expect(parseMediaUri(fileUri("p1", "f1"))).toEqual({
			source: "file",
			projectId: "p1",
			id: "f1",
		});
	});
	it("ignores everything else and malformed references", () => {
		expect(parseMediaUri("https://example.com/a.png")).toBeNull();
		expect(parseMediaUri("attachment://p1/t1")).toBeNull();
		expect(parseMediaUri("files://p1")).toBeNull();
		expect(parseMediaUri(undefined)).toBeNull();
	});
});

describe("collectInlineMedia", () => {
	it("finds image and file blocks in document order, nested ones included", () => {
		const blocks = [
			{ id: "1", type: "paragraph" },
			{ id: "2", type: "image", props: { url: "attachment://p/t/a" } },
			{
				id: "3",
				type: "paragraph",
				children: [{ id: "4", type: "file", props: { url: "files://p/f" } }],
			},
			{ id: "5", type: "image", props: { url: "https://x.test/y.png" } },
			{ id: "6", type: "video", props: { url: "attachment://p/t/v" } },
		];
		expect(
			collectInlineMedia(blocks).map((r) => [r.blockId, r.source]),
		).toEqual([
			["2", "attachment"],
			["4", "file"],
		]);
	});
});
