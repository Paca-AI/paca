// grep_docs's notice when the regex scan stops at the total time budget
// (bot review on Paca-AI/paca#770): the documents cut off are not reported
// as slow. regexScan is replaced so the 10 s budget need not elapse.
import { describe, expect, it, vi } from "vitest";
import { handleFilesystemDocTool } from "../../tools/filesystem-doc-tools.js";
import { markdownToBlocknote } from "../../utils/converters.js";

vi.mock("../../utils/markdown-sections.js", async (importOriginal) => ({
	...(await importOriginal<
		typeof import("../../utils/markdown-sections.js")
	>()),
	regexScan: vi.fn(async (_p: string, _i: boolean, texts: string[]) => ({
		hits: texts.map((_t, i) => (i === 0 ? [0] : null)),
		timedOut: [],
		unsearched: texts.map((_t, i) => i).slice(1),
		stoppedBy: "time",
	})),
}));

const doc = (id: string, title: string) => ({
	id,
	title,
	folder_id: null,
	content: JSON.parse(JSON.stringify(markdownToBlocknote("aaa\n"))),
	content_hash: `h-${id}`,
});

describe("grep_docs regex time budget", () => {
	it("reports documents cut off by the total budget as not searched, not as slow", async () => {
		const docs = [doc("a", "One"), doc("b", "Two"), doc("c", "Three")];
		const apiClient = {
			listDocuments: vi.fn(async () => ({ items: docs, nextCursor: null })),
		} as any;
		const docClient = {
			listFolders: vi.fn(async () => ({ items: [], nextCursor: null })),
			batchGet: vi.fn(async (_p: string, ids: string[]) => ({
				status: "ok",
				items: docs.filter((d) => ids.includes(d.id)),
			})),
		} as any;
		const res = await handleFilesystemDocTool(
			"grep_docs",
			{ projectId: "p1", pattern: "a+", regex: true },
			apiClient,
			docClient,
		);
		const out = (res as any).content[0].text as string;
		expect(out).toContain("One:1: aaa");
		expect(out).toContain("(1 searched)");
		expect(out).not.toMatch(/ran longer than 500 ms/);
		expect(out).toContain(
			"Stopped at the time limit of 10 s: 2 more documents were not searched. Narrow with path to search them.",
		);
	});
});
