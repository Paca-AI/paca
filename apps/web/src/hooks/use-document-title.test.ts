import { describe, expect, it } from "vitest";
import { buildDocumentTitle } from "./use-document-title";

describe("buildDocumentTitle", () => {
	it("returns the bare app title with no project and no unread", () => {
		expect(buildDocumentTitle(0, null, "Paca")).toBe("Paca");
	});

	it("returns the bare app title for a negative count", () => {
		expect(buildDocumentTitle(-1, null, "Paca")).toBe("Paca");
	});

	it("prefixes the title with the unread count", () => {
		expect(buildDocumentTitle(3, null, "Paca")).toBe("(3) Paca");
	});

	it("caps the displayed count at 99+", () => {
		expect(buildDocumentTitle(100, null, "Paca")).toBe("(99+) Paca");
	});

	it("does not cap exactly 99", () => {
		expect(buildDocumentTitle(99, null, "Paca")).toBe("(99) Paca");
	});

	it("inserts the project name before the app title", () => {
		expect(buildDocumentTitle(0, "Acme", "Paca")).toBe("Acme · Paca");
	});

	it("combines the unread prefix with the project name", () => {
		expect(buildDocumentTitle(3, "Acme", "Paca")).toBe("(3) Acme · Paca");
	});

	it("ignores an empty-string project name", () => {
		expect(buildDocumentTitle(0, "", "Paca")).toBe("Paca");
	});
});
