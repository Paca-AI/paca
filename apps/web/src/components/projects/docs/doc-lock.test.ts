// Tests for how the doc page reacts to a save rejected because the document
// was locked while it was open.

import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";
import { type Document, docQueryKeys } from "@/lib/doc-api";
import { isDocLockedError, markDocLocked } from "./doc-lock";

const lockedError = {
	isAxiosError: true,
	response: {
		status: 409,
		data: { error_code: "DOC_LOCKED", error: "document is locked" },
	},
};

const doc: Document = {
	id: "d1",
	title: "Plan",
	content: null,
	position: 0,
	created_at: "2026-11-14T00:00:00Z",
	updated_at: "2026-11-14T00:00:00Z",
	source: { repository: "github.com/acme/app", path: "docs/plan.md" },
	source_locked: false,
};

describe("isDocLockedError", () => {
	it("recognises the 409 DOC_LOCKED answer and nothing else", () => {
		expect(isDocLockedError(lockedError)).toBe(true);
		expect(
			isDocLockedError({
				response: { status: 409, data: { error_code: "DOC_PATH_CHANGED" } },
			}),
		).toBe(false);
		expect(isDocLockedError(new Error("network"))).toBe(false);
		expect(isDocLockedError(undefined)).toBe(false);
	});
});

describe("markDocLocked", () => {
	it("marks the cached document locked at once and refetches it", async () => {
		const qc = new QueryClient();
		const key = docQueryKeys.detail("p1", "d1");
		qc.setQueryData(key, doc);

		markDocLocked(qc, "p1", "d1");

		expect(qc.getQueryData<Document>(key)?.source_locked).toBe(true);
		expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
	});

	it("leaves an uncached document alone", () => {
		const qc = new QueryClient();
		markDocLocked(qc, "p1", "missing");
		expect(qc.getQueryData(docQueryKeys.detail("p1", "missing"))).toBe(
			undefined,
		);
	});
});
