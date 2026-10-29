import { afterEach, describe, expect, it } from "vitest";
import {
	composerDraftKey,
	loadComposerDraft,
	saveComposerDraft,
} from "./composer-draft";

describe("composer draft", () => {
	afterEach(() => window.sessionStorage.clear());

	it("round-trips a draft and drops it once the composer is emptied", () => {
		const key = composerDraftKey("/projects/p1/tasks/t1");
		expect(loadComposerDraft(key)).toBeNull();

		saveComposerDraft(key, "half-written question");
		expect(loadComposerDraft(key)).toBe("half-written question");

		// Sending clears the composer, which must clear the stored draft too.
		saveComposerDraft(key, "");
		expect(loadComposerDraft(key)).toBeNull();
	});

	it("keeps the drafts of different pages apart", () => {
		saveComposerDraft(composerDraftKey("/a"), "for a");
		saveComposerDraft(composerDraftKey("/b"), "for b");

		expect(loadComposerDraft(composerDraftKey("/a"))).toBe("for a");
		expect(loadComposerDraft(composerDraftKey("/b"))).toBe("for b");
	});
});
