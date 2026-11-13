import { describe, expect, it } from "vitest";
import {
	type DocNodeDragPayload,
	isMoveBlocked,
	parseDocNodeDragPayload,
} from "./doc-dnd";

const folder: DocNodeDragPayload = {
	kind: "folder",
	id: "f1",
	projectId: "p1",
	parentId: null,
};

describe("parseDocNodeDragPayload", () => {
	it("accepts a valid payload", () => {
		expect(parseDocNodeDragPayload(JSON.stringify(folder), "p1")).toEqual(
			folder,
		);
	});

	it("rejects malformed JSON, bad shapes and other projects", () => {
		expect(parseDocNodeDragPayload("{", "p1")).toBeNull();
		expect(parseDocNodeDragPayload("null", "p1")).toBeNull();
		expect(
			parseDocNodeDragPayload(JSON.stringify({ ...folder, kind: "x" }), "p1"),
		).toBeNull();
		expect(
			parseDocNodeDragPayload(JSON.stringify({ ...folder, id: 3 }), "p1"),
		).toBeNull();
		expect(
			parseDocNodeDragPayload(JSON.stringify({ ...folder, parentId: 3 }), "p1"),
		).toBeNull();
		expect(parseDocNodeDragPayload(JSON.stringify(folder), "p2")).toBeNull();
	});
});

describe("isMoveBlocked", () => {
	it("blocks dropping into the current parent", () => {
		expect(isMoveBlocked(folder, null, [])).toBe(true);
		expect(isMoveBlocked({ ...folder, parentId: "a" }, "a", ["a"])).toBe(true);
	});

	it("blocks a folder entering itself or a descendant", () => {
		expect(isMoveBlocked(folder, "f1", ["f1"])).toBe(true);
		expect(isMoveBlocked(folder, "c", ["f1", "b", "c"])).toBe(true);
	});

	it("allows other moves, and documents into any folder", () => {
		expect(isMoveBlocked(folder, "x", ["x"])).toBe(false);
		expect(isMoveBlocked({ ...folder, kind: "doc" }, "f1", ["f1"])).toBe(false);
		expect(isMoveBlocked({ ...folder, parentId: "a" }, null, [])).toBe(false);
	});
});
