import { describe, expect, it } from "vitest";
import {
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
