import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	applyFileChange,
	fileCategory,
	filesBase,
	filesScopeKey,
	formatFileSize,
	GENERAL_SCOPE,
} from "./files-api";

describe("filesBase", () => {
	it("targets the project or the general scope", () => {
		expect(filesBase({ projectId: "p1" })).toBe("/projects/p1/files");
		expect(filesBase(GENERAL_SCOPE)).toBe("/files");
	});
	it("keys caches per scope", () => {
		expect(filesScopeKey({ projectId: "p1" })).toEqual(["files", "p1"]);
		expect(filesScopeKey(GENERAL_SCOPE)).toEqual(["files", "general"]);
	});
});

describe("fileCategory", () => {
	const file = (name: string, content_type = "") =>
		fileCategory({ kind: "file", name, content_type });
	it("prefers the MIME type", () => {
		expect(file("x", "image/png")).toBe("image");
		expect(file("x", "video/mp4")).toBe("video");
		expect(file("x", "application/pdf")).toBe("pdf");
	});
	it("falls back to the extension", () => {
		expect(file("Report.DOCX")).toBe("document");
		expect(file("data.xlsx", "application/octet-stream")).toBe("spreadsheet");
		expect(file("deck.pptx")).toBe("presentation");
		expect(file("main.go")).toBe("code");
		expect(file("notes", "text/plain")).toBe("text");
		expect(file("blob.bin")).toBe("other");
	});
	it("tells folders apart", () => {
		expect(fileCategory({ kind: "folder", name: "a.pdf" })).toBe("folder");
	});
});

describe("formatFileSize", () => {
	it("scales units", () => {
		expect(formatFileSize(0)).toBe("0 B");
		expect(formatFileSize(1023)).toBe("1023 B");
		expect(formatFileSize(1536)).toBe("1.5 KB");
		expect(formatFileSize(10 * 1024 * 1024)).toBe("10 MB");
		expect(formatFileSize(3 * 1024 ** 3)).toBe("3.0 GB");
	});
});

describe("applyFileChange", () => {
	const scope = { projectId: "p1" };
	function seeded() {
		const qc = new QueryClient();
		const keys = {
			root: ["files", "p1", "nodes", { parentId: null }],
			rootUndefined: ["files", "p1", "nodes", {}],
			a: ["files", "p1", "nodes", { parentId: "a" }],
			b: ["files", "p1", "nodes", { parentId: "b" }],
			search: ["files", "p1", "nodes", { parentId: "b", q: "x" }],
			trash: ["files", "p1", "section", "trash"],
			path: ["files", "p1", "path", "a"],
			otherScope: ["files", "p2", "nodes", { parentId: "a" }],
			general: ["files", "general", "nodes", { parentId: null }],
		};
		for (const k of Object.values(keys)) qc.setQueryData(k, []);
		const stale = () =>
			Object.entries(keys)
				.filter(([, k]) => qc.getQueryState(k)?.isInvalidated)
				.map(([name]) => name)
				.sort();
		return { qc, stale };
	}

	it("refetches only the folders the change names, searches and sections", () => {
		const { qc, stale } = seeded();
		applyFileChange(qc, scope, { change: "created", folder_ids: ["a"] });
		expect(stale()).toEqual(["a", "path", "search", "trash"]);
	});

	it("treats the scope's top level as a listing of its own", () => {
		const { qc, stale } = seeded();
		applyFileChange(qc, scope, {
			change: "moved",
			root: true,
			folder_ids: ["b"],
		});
		expect(stale()).toEqual([
			"b",
			"path",
			"root",
			"rootUndefined",
			"search",
			"trash",
		]);
	});

	it("also refreshes the listing of a folder that was trashed", () => {
		const { qc, stale } = seeded();
		applyFileChange(qc, scope, {
			change: "trashed",
			kind: "folder",
			node_id: "b",
			folder_ids: ["a"],
		});
		expect(stale()).toEqual(["a", "b", "path", "search", "trash"]);
	});

	it("never touches another scope", () => {
		const { qc, stale } = seeded();
		applyFileChange(qc, scope, { root: true, folder_ids: ["a"] });
		expect(stale()).not.toContain("otherScope");
		expect(stale()).not.toContain("general");
	});

	it("refreshes the contents of a restored folder", () => {
		const { qc, stale } = seeded();
		applyFileChange(qc, scope, {
			change: "restored",
			kind: "folder",
			node_id: "b",
			folder_ids: ["a"],
		});
		expect(stale()).toEqual(["a", "b", "path", "search", "trash"]);
	});

	describe("bursts", () => {
		afterEach(() => vi.useRealTimers());

		it("applies the first change at once and merges the rest into one pass", () => {
			vi.useFakeTimers();
			const { qc, stale } = seeded();
			applyFileChange(qc, scope, { folder_ids: ["a"] });
			expect(stale()).toEqual(["a", "path", "search", "trash"]);
			applyFileChange(qc, scope, { folder_ids: ["b"] });
			expect(stale()).not.toContain("b");
			vi.advanceTimersByTime(300);
			expect(stale()).toContain("b");
		});
	});
});
