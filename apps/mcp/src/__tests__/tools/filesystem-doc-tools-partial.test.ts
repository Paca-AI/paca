// read_doc's partial views, grep_docs and edit_doc, with the real Markdown
// converters (only the API clients are mocked), so line numbers and block
// identity are checked against what BlockNote really renders.
import { describe, expect, it, vi } from "vitest";
import { PacaAPIError } from "../../api/doc-client.js";
import { handleFilesystemDocTool } from "../../tools/filesystem-doc-tools.js";
import {
	blocknoteToMarkdown,
	markdownToBlocknote,
} from "../../utils/converters.js";

const PLAN_MD = [
	"# Plan",
	"",
	"Intro text.",
	"",
	"## Setup",
	"",
	"Install it.",
	"",
	"```bash",
	"# not a heading",
	"```",
	"",
	"## Usage",
	"",
	"Run it.",
	"",
].join("\n");

function docOf(id: string, title: string, md: string, extra: any = {}) {
	return {
		id,
		title,
		folder_id: null,
		position: 0,
		created_at: "2026-11-14T00:00:00Z",
		updated_at: "2026-11-14T10:00:00Z",
		content: JSON.parse(JSON.stringify(markdownToBlocknote(md))),
		content_hash: `hash-${id}`,
		source_locked: false,
		...extra,
	};
}

function text(result: any): string {
	return result.content[0].text;
}

/** Clients for a server with by-path, batch-get and search. */
function clients(opts: {
	docs?: any[];
	folders?: any[];
	search?: any[];
	batchUnsupported?: boolean;
	members?: any[];
}) {
	const docs = opts.docs ?? [];
	const folders = opts.folders ?? [];
	const byId = new Map(docs.map((d) => [d.id, d]));
	const pathOf = (d: any) => {
		const parts = [d.title];
		let f = folders.find((x) => x.id === d.folder_id);
		while (f) {
			parts.unshift(f.name);
			const parent = f.parent_id;
			f = folders.find((x) => x.id === parent);
		}
		return parts.join("/");
	};
	const folderPath = (id: string) => {
		const chain: any[] = [];
		let f = folders.find((x) => x.id === id);
		while (f) {
			chain.unshift(f);
			const parent = f.parent_id;
			f = folders.find((x) => x.id === parent);
		}
		return chain;
	};
	const apiClient = {
		getDocument: vi.fn(async (_p: string, id: string) => {
			const d = byId.get(id);
			if (!d) throw new Error("404");
			return d;
		}),
		listDocuments: vi.fn(async (_p: string, o: any = {}) => {
			const wanted = o.folderId === "null" ? null : (o.folderId ?? null);
			const items = docs.filter(
				(d) =>
					(d.folder_id ?? null) === wanted &&
					(!o.title || d.title.toLowerCase() === o.title.toLowerCase()),
			);
			return { items, nextCursor: null };
		}),
	} as any;
	const docClient = {
		getByPath: vi.fn(async (_p: string, path: string) => {
			const d = docs.find((x) => pathOf(x) === path);
			if (d) return { status: "found", doc: d, etag: `"${d.content_hash}-m"` };
			if (
				folders.some(
					(f) => [...folderPath(f.id).map((x) => x.name)].join("/") === path,
				)
			)
				return { status: "folder" };
			return { status: "not_found" };
		}),
		listFolders: vi.fn(async (_p: string, o: any = {}) => ({
			items: folders.filter(
				(f) =>
					(f.parent_id ?? null) === (o.parentId ?? null) &&
					(!o.name || f.name.toLowerCase() === o.name.toLowerCase()),
			),
			nextCursor: null,
		})),
		getFolderPath: vi.fn(async (_p: string, id: string) => folderPath(id)),
		searchDocuments: vi.fn(async () => opts.search ?? []),
		batchGet: vi.fn(async (_p: string, ids: string[]) =>
			opts.batchUnsupported
				? { status: "unsupported" }
				: {
						status: "ok",
						items: ids.map((id) => byId.get(id)).filter(Boolean),
					},
		),
		patchDocument: vi.fn(),
	} as any;
	const extendedClient = {
		listProjectMembers: vi.fn(async () => ({
			items: opts.members ?? [],
			nextCursor: null,
		})),
	} as any;
	return { apiClient, docClient, extendedClient };
}

async function call(
	name: string,
	args: any,
	c: ReturnType<typeof clients>,
): Promise<any> {
	return handleFilesystemDocTool(
		name,
		args,
		c.apiClient,
		c.docClient,
		c.extendedClient,
	);
}

// ── read_doc ──────────────────────────────────────────────────────────────────

describe("read_doc partial views", () => {
	const plan = docOf("d1", "Plan", PLAN_MD);
	const md = blocknoteToMarkdown(plan.content);
	const lines = md.split("\n");
	const lineOf = (s: string) => lines.indexOf(s) + 1;

	it("puts the ETag in the header of every read", async () => {
		const c = clients({ docs: [plan] });
		const res = await call("read_doc", { projectId: "p1", path: "Plan" }, c);
		expect(text(res)).toContain('ETag: "hash-d1-m"');
		const byId = await call("read_doc", { projectId: "p1", docId: "d1" }, c);
		// A read by id has the content hash, which If-Match accepts.
		expect(text(byId)).toContain("ETag: hash-d1");
	});

	it("outline lists headings with their lines, skipping fenced code", async () => {
		const c = clients({ docs: [plan] });
		const res = await call(
			"read_doc",
			{ projectId: "p1", path: "Plan", outline: true },
			c,
		);
		const out = text(res);
		expect(out).toContain(`${lineOf("# Plan")}: # Plan`);
		expect(out).toContain(`${lineOf("## Setup")}: ## Setup`);
		expect(out).toContain(`${lineOf("## Usage")}: ## Usage`);
		expect(out).not.toContain("not a heading");
		expect(out).toContain(`${lines.length - 1} lines`);
	});

	it("section returns that heading's part with line numbers", async () => {
		const c = clients({ docs: [plan] });
		const res = await call(
			"read_doc",
			{ projectId: "p1", path: "Plan", section: "setup" },
			c,
		);
		const out = text(res);
		const start = lineOf("## Setup");
		expect(out).toContain(`${start}: ## Setup`);
		expect(out).toContain(`${lineOf("Install it.")}: Install it.`);
		expect(out).toContain(`${lineOf("# not a heading")}: # not a heading`);
		expect(out).not.toContain("Usage");
		expect(out).not.toContain("Intro text.");
	});

	it("an ambiguous section lists the headings and returns none", async () => {
		const doc = docOf(
			"d2",
			"Notes",
			"# A\n\n## Notes\n\nx\n\n# B\n\n## Notes\n\ny\n",
		);
		const c = clients({ docs: [doc] });
		const res = await call(
			"read_doc",
			{ projectId: "p1", path: "Notes", section: "Notes" },
			c,
		);
		expect(res.isError).toBe(true);
		expect(text(res)).toContain("3: ## Notes");
		expect(text(res)).toContain("9: ## Notes");
		expect(text(res)).not.toContain("x\n");
	});

	it("lines returns a clamped range with line numbers", async () => {
		const c = clients({ docs: [plan] });
		const total = lines.length - 1;
		const res = await call(
			"read_doc",
			{ projectId: "p1", path: "Plan", lines: `${total - 1}-999` },
			c,
		);
		expect(text(res)).toContain(`${total}: Run it.`);
		expect(text(res)).toContain(`Lines ${total - 1}-${total} of ${total}`);
		expect(text(res)).not.toContain("Install it.");
	});

	it("refuses more than one view at once", async () => {
		const c = clients({ docs: [plan] });
		const res = await call(
			"read_doc",
			{ projectId: "p1", path: "Plan", outline: true, lines: "1-2" },
			c,
		);
		expect(res.isError).toBe(true);
	});
});

// ── grep_docs ─────────────────────────────────────────────────────────────────

describe("grep_docs", () => {
	const folders = [{ id: "f1", parent_id: null, name: "Guides" }];
	const a = docOf(
		"a",
		"Setup",
		"Install **foo** bar now.\n\nSecond line.\n\nfoo again\n",
		{
			folder_id: "f1",
		},
	);
	const b = docOf("b", "Readme", "Nothing here.\n\nfoo bar at the end\n");

	it("a literal phrase across bold is found although the server search misses it", async () => {
		// Server search matches one text run at a time: "foo bar" finds
		// neither "**foo** bar" nor anything else here.
		const c = clients({ docs: [a, b], folders, search: [] });
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "foo bar" },
			c,
		);
		const out = text(res);
		expect(out).toContain("Guides/Setup:1: Install **foo** bar now.");
		expect(out).toContain("Readme:3: foo bar at the end");
		expect(out).not.toContain("foo again");
		// Server search found no candidates for "foo", so it scanned.
		expect(c.docClient.searchDocuments.mock.calls).toEqual([["p1", "foo", 50]]);
	});

	it.each([
		["## Setup", "Guide:1: ## Setup"],
		["- [ ] task", "Guide:5: * [ ] task"],
		["`code`", "Guide:3: run `code` now"],
		["[link](", "Guide:7: a [link](http://x.y) here"],
		["| a |", "Guide:9: | a |"],
	])("a literal with Markdown syntax (%s) is found in the Markdown read_doc shows", async (pattern, want) => {
		const md =
			"## Setup\n\nrun `code` now\n\n- [ ] task\n\na [link](http://x.y) here\n\n| a |\n|---|\n| 1 |\n";
		const g = docOf("g", "Guide", md);
		const c = clients({ docs: [g], search: [] });
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: pattern.replace("- [ ]", "* [ ]") },
			c,
		);
		expect(text(res)).toContain(want);
	});

	it("a single word is narrowed through the server search, once, and scans when search finds nothing", async () => {
		const snake = docOf("s", "Env", "Set PACA_E2E to 1.\n");
		const c = clients({
			docs: [snake, a],
			folders,
			search: [
				{ id: "s", folder_id: null, title: "Env", matched_in: "content" },
			],
		});
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "PACA_E2E" },
			c,
		);
		expect(text(res)).toContain("Env:1: Set PACA_E2E to 1.");
		expect(c.docClient.searchDocuments.mock.calls).toEqual([
			["p1", "PACA_E2E", 50],
		]);
		expect(c.docClient.batchGet).toHaveBeenCalledWith("p1", ["s"]);

		c.docClient.searchDocuments.mockClear();
		c.docClient.searchDocuments.mockResolvedValueOnce([]);
		const again = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "again" },
			c,
		);
		expect(text(again)).toContain("Guides/Setup:5: foo again");
	});

	it("a catastrophic regex stops after three slow documents, quickly", async () => {
		const docs = Array.from({ length: 6 }, (_, i) =>
			docOf(`e${i}`, `Evil ${i}`, `${"a".repeat(40)}b\n`),
		);
		const c = clients({ docs });
		const started = Date.now();
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "^(a+)+$", regex: true },
			c,
		);
		expect(Date.now() - started).toBeLessThan(4000);
		expect(res.isError).toBeFalsy();
		const out = text(res);
		expect(out).toContain(
			"The regular expression ran longer than 500 ms and was stopped in: Evil 0, Evil 1, Evil 2.",
		);
		expect(out).toContain(
			"Stopped after 3 slow documents: 3 more were not searched. Simplify the regular expression (avoid nested quantifiers such as (a+)+) or narrow with path.",
		);
		expect(out).toMatch(/in 0 documents searched/);
	}, 10_000);

	it("a regex slow in one document still reports the others' matches", async () => {
		const evil = docOf("e", "Evil", `${"a".repeat(40)}b\n`);
		const good = docOf("g", "Good", "aaaa\n");
		const c = clients({ docs: [evil, good] });
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "^(a+)+$", regex: true },
			c,
		);
		expect(res.isError).toBeFalsy();
		expect(text(res)).toContain("Good:1: aaaa");
		expect(text(res)).toContain(
			"The regular expression ran longer than 500 ms and was stopped in: Evil.",
		);
	}, 10_000);

	it("a phrase across bold is found in a document past the first 200 scanned", async () => {
		const docs = Array.from({ length: 250 }, (_, i) =>
			docOf(`d${i}`, `Doc ${i}`, "zzz\n"),
		);
		docs[240] = docOf("d240", "Deep", "Install **foo** bar now.\n");
		const c = clients({
			docs,
			search: [
				{ id: "d240", folder_id: null, title: "Deep", matched_in: "content" },
			],
		});
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "Install foo bar" },
			c,
		);
		expect(text(res)).toContain("Deep:1: Install **foo** bar now.");
		expect(text(res)).not.toMatch(/first 200/);
		expect(c.docClient.searchDocuments.mock.calls).toEqual([
			["p1", "Install", 50],
		]);
		expect(c.docClient.batchGet.mock.calls.flatMap((x: any) => x[1])).toEqual([
			"d240",
		]);
	});

	it("scans when the candidates from search do not match, and says when the scan is cut", async () => {
		const docs = Array.from({ length: 205 }, (_, i) =>
			docOf(`d${i}`, `Doc ${i}`, "zzz\n"),
		);
		docs[3] = docOf("d3", "Hit", "Install **foo** bar now.\n");
		docs[204] = docOf("d204", "Decoy", "Install it.\n");
		const c = clients({
			docs,
			search: [
				{ id: "d204", folder_id: null, title: "Decoy", matched_in: "content" },
			],
		});
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "Install foo bar" },
			c,
		);
		expect(text(res)).toContain("Hit:1: Install **foo** bar now.");
		expect(text(res)).toMatch(/first 200 documents/);
	});

	it("does not search for a pattern without a word of three letters", async () => {
		const g = docOf("g", "Guide", "| a |\n|---|\n| 1 |\n");
		const c = clients({ docs: [g] });
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "| a |" },
			c,
		);
		expect(text(res)).toContain("Guide:1: | a |");
		expect(c.docClient.searchDocuments).not.toHaveBeenCalled();
	});

	it("says how many documents could not be read on an older server", async () => {
		const c = clients({ docs: [a, b], folders, batchUnsupported: true });
		c.apiClient.getDocument.mockImplementation(
			async (_p: string, id: string) => {
				if (id === "a") throw new Error("HTTP 500 Internal Server Error");
				return b;
			},
		);
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "foo bar" },
			c,
		);
		expect(text(res)).toContain("Readme:3: foo bar at the end");
		expect(text(res)).toMatch(/1 document could not be read/);
	});

	it("regex scans the documents under path", async () => {
		const c = clients({ docs: [a, b], folders });
		const res = await call(
			"grep_docs",
			{
				projectId: "p1",
				pattern: "^foo\\s+\\w+$",
				regex: true,
				path: "Guides",
			},
			c,
		);
		const out = text(res);
		expect(out).toContain("Guides/Setup:5: foo again");
		expect(out).not.toContain("Readme");
		expect(c.docClient.searchDocuments).not.toHaveBeenCalled();
	});

	it("regex without path scans the whole project", async () => {
		const c = clients({ docs: [a, b], folders });
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "foo", regex: true },
			c,
		);
		expect(text(res)).toContain("Readme:3:");
		expect(text(res)).toContain("Guides/Setup:5:");
	});

	it("prints context lines with '-' and separates groups with --", async () => {
		const doc = docOf(
			"c",
			"Long",
			"l1\n\nl2\n\nhit one\n\nl4\n\nl5\n\nl6\n\nl7\n\nhit two\n",
		);
		const c = clients({
			docs: [doc],
			search: [
				{ id: "c", folder_id: null, title: "Long", matched_in: "content" },
			],
		});
		const md = blocknoteToMarkdown(doc.content).split("\n");
		const h1 = md.indexOf("hit one") + 1;
		const h2 = md.indexOf("hit two") + 1;
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "hit", context: 1 },
			c,
		);
		const out = text(res);
		expect(out).toContain(`Long-${h1 - 1}- `);
		expect(out).toContain(`Long:${h1}: hit one`);
		expect(out).toContain(`Long-${h1 + 1}- `);
		expect(out).toContain("\n--\n");
		expect(out).toContain(`Long:${h2}: hit two`);
	});

	it("stops at the limit and says so", async () => {
		const many = docOf(
			"m",
			"Many",
			Array.from({ length: 10 }, (_, i) => `x ${i}`).join("\n\n"),
		);
		const c = clients({
			docs: [many],
			search: [
				{ id: "m", folder_id: null, title: "Many", matched_in: "content" },
			],
		});
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "x", limit: 3 },
			c,
		);
		const out = text(res);
		expect(out.match(/^Many:\d+:/gm)).toHaveLength(3);
		expect(out).toMatch(/limit of 3/);
	});

	it("caps a scan at 200 documents with a notice", async () => {
		const docs = Array.from({ length: 205 }, (_, i) =>
			docOf(`d${i}`, `Doc ${i}`, "zzz\n"),
		);
		const c = clients({ docs });
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "nomatch", regex: true },
			c,
		);
		expect(text(res)).toMatch(/first 200 documents/);
		const asked = c.docClient.batchGet.mock.calls.flatMap((x: any) => x[1]);
		expect(asked).toHaveLength(200);
		expect(c.docClient.batchGet.mock.calls[0][1]).toHaveLength(100);
	});

	it("falls back to one read per document on a server without batch-get", async () => {
		const c = clients({
			docs: [a, b],
			folders,
			batchUnsupported: true,
			search: [
				{ id: "b", folder_id: null, title: "Readme", matched_in: "content" },
			],
		});
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "foo bar" },
			c,
		);
		expect(text(res)).toContain("Readme:3: foo bar at the end");
		expect(c.apiClient.getDocument).toHaveBeenCalledWith("p1", "b");
	});

	it("an invalid regex is a tool error", async () => {
		const c = clients({ docs: [a] });
		const res = await call(
			"grep_docs",
			{ projectId: "p1", pattern: "(unclosed", regex: true },
			c,
		);
		expect(res.isError).toBe(true);
		expect(text(res)).toMatch(/invalid regular expression/i);
		expect(c.docClient.batchGet).not.toHaveBeenCalled();
	});

	it("reports no matches", async () => {
		const c = clients({ docs: [a], search: [] });
		const res = await call("grep_docs", { projectId: "p1", pattern: "qqq" }, c);
		expect(res.isError).toBeFalsy();
		expect(text(res)).toMatch(/no matches/i);
	});
});

// ── edit_doc ──────────────────────────────────────────────────────────────────

const TEN = Array.from(
	{ length: 10 },
	(_, i) => `Paragraph ${i + 1} text.`,
).join("\n\n");

describe("edit_doc", () => {
	function setup(extra: any = {}, members: any[] = []) {
		const doc = docOf("e1", "Spec", TEN, extra);
		const c = clients({ docs: [doc], members });
		return { doc, c };
	}

	it("writes the spliced blocks with PATCH and If-Match, untouched blocks as read", async () => {
		const { doc, c } = setup();
		const before = doc.content.map((b: any) => JSON.stringify(b));
		c.docClient.patchDocument.mockResolvedValue({ doc, etag: '"h2-m"' });

		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				path: "Spec",
				old_string: "Paragraph 4 text.",
				new_string: "Paragraph four.",
				note: "wording",
				task: "PACA-88",
			},
			c,
		);

		expect(res.isError).toBeFalsy();
		expect(text(res)).toMatch(/Edited Spec: replaced 1 occurrence/);
		const [pid, id, body, opts] = c.docClient.patchDocument.mock.calls[0];
		expect([pid, id]).toEqual(["p1", "e1"]);
		expect(opts).toEqual({ ifMatch: '"hash-e1-m"' });
		expect(body.note).toBe("wording");
		expect(body.task).toBe("PACA-88");
		expect(body.content).toHaveLength(10);
		body.content.forEach((b: any, i: number) => {
			if (i === 3) {
				expect(JSON.stringify(b)).not.toBe(before[i]);
				expect(b.id).toBe(doc.content[3].id);
			} else {
				expect(b).toBe(doc.content[i]);
				expect(JSON.stringify(b)).toBe(before[i]);
			}
		});
	});

	it("re-reads and retries once after a 412", async () => {
		const { doc, c } = setup();
		const newer = docOf(
			"e1",
			"Spec",
			TEN.replace("Paragraph 9", "Paragraph nine"),
			{
				content_hash: "hash-newer",
			},
		);
		c.docClient.getByPath
			.mockResolvedValueOnce({ status: "found", doc, etag: '"hash-e1-m"' })
			.mockResolvedValueOnce({
				status: "found",
				doc: newer,
				etag: '"hash-newer-m"',
			});
		c.docClient.patchDocument
			.mockRejectedValueOnce(
				new PacaAPIError("conflict", 412, "VERSION_CONFLICT", {
					content_hash: "hash-newer",
					updated_at: "2026-11-14T10:05:00Z",
				}),
			)
			.mockResolvedValueOnce({ doc: newer, etag: '"h3-m"' });

		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				path: "Spec",
				old_string: "Paragraph 2",
				new_string: "Paragraph two",
			},
			c,
		);

		expect(res.isError).toBeFalsy();
		expect(c.docClient.patchDocument).toHaveBeenCalledTimes(2);
		const second = c.docClient.patchDocument.mock.calls[1];
		expect(second[3]).toEqual({ ifMatch: '"hash-newer-m"' });
		// The retry applies the edit to the newer version, keeping its change.
		const md = blocknoteToMarkdown(second[2].content);
		expect(md).toContain("Paragraph two");
		expect(md).toContain("Paragraph nine");
		expect(text(res)).toMatch(/changed after it was read/);
	});

	it("a second 412 is a tool error naming the other writer", async () => {
		const { c } = setup({}, [
			{ id: "m1", user_id: "u2", username: "ana", full_name: "Ana Diaz" },
		]);
		const conflict = new PacaAPIError("conflict", 412, "VERSION_CONFLICT", {
			content_hash: "hash-x",
			updated_at: "2026-11-14T10:05:00Z",
			updated_by: "u2",
			note: "rewrote setup",
			client: "claude-code/s9",
			task: "PACA-90",
		});
		c.docClient.patchDocument.mockRejectedValue(conflict);

		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				path: "Spec",
				old_string: "Paragraph 2",
				new_string: "x",
			},
			c,
		);

		expect(res.isError).toBe(true);
		expect(c.docClient.patchDocument).toHaveBeenCalledTimes(2);
		const out = text(res);
		for (const s of [
			"Ana Diaz",
			"2026-11-14T10:05:00Z",
			"rewrote setup",
			"claude-code/s9",
			"PACA-90",
			"read_doc",
		]) {
			expect(out).toContain(s);
		}
	});

	it("old_string gone after the re-read is a tool error with the other writer's note", async () => {
		const { doc, c } = setup();
		const newer = docOf(
			"e1",
			"Spec",
			TEN.replace("Paragraph 2 text.", "Rewritten."),
		);
		c.docClient.getByPath
			.mockResolvedValueOnce({ status: "found", doc, etag: '"a"' })
			.mockResolvedValueOnce({ status: "found", doc: newer, etag: '"b"' });
		c.docClient.patchDocument.mockRejectedValueOnce(
			new PacaAPIError("conflict", 412, "VERSION_CONFLICT", {
				updated_by: "u7",
				updated_at: "2026-11-14T10:06:00Z",
				note: "renamed paragraph 2",
			}),
		);

		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				path: "Spec",
				old_string: "Paragraph 2 text.",
				new_string: "x",
			},
			c,
		);

		expect(res.isError).toBe(true);
		expect(c.docClient.patchDocument).toHaveBeenCalledTimes(1);
		expect(text(res)).toContain("renamed paragraph 2");
		expect(text(res)).toContain("u7");
		expect(text(res)).toMatch(/no longer/);
	});

	it("zero and multiple matches are tool errors and write nothing", async () => {
		const { c } = setup();
		const none = await call(
			"edit_doc",
			{ projectId: "p1", path: "Spec", old_string: "absent", new_string: "x" },
			c,
		);
		expect(none.isError).toBe(true);
		expect(text(none)).toMatch(/not found/);
		const many = await call(
			"edit_doc",
			{ projectId: "p1", path: "Spec", old_string: "text.", new_string: "x" },
			c,
		);
		expect(many.isError).toBe(true);
		expect(text(many)).toMatch(/10 times/);
		expect(c.docClient.patchDocument).not.toHaveBeenCalled();
	});

	it("replaceAll replaces every match", async () => {
		const { doc, c } = setup();
		c.docClient.patchDocument.mockResolvedValue({ doc });
		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				docId: "e1",
				old_string: "text.",
				new_string: "words.",
				replaceAll: true,
			},
			c,
		);
		expect(text(res)).toMatch(/replaced 10 occurrences/);
		// A read by id writes with the content hash.
		expect(c.docClient.patchDocument.mock.calls[0][3]).toEqual({
			ifMatch: "hash-e1",
		});
	});

	it("a locked managed copy is refused without a write, naming the repository file", async () => {
		const { c } = setup({
			source: { repository: "paca", path: "docs/spec.md" },
			source_locked: true,
		});
		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				path: "Spec",
				old_string: "Paragraph 2",
				new_string: "x",
			},
			c,
		);
		expect(res.isError).toBe(true);
		expect(text(res)).toContain("docs/spec.md");
		expect(text(res)).toContain("paca");
		expect(text(res)).toMatch(/edit the file.*push it/i);
		expect(c.docClient.patchDocument).not.toHaveBeenCalled();
	});

	it("409 DOC_LOCKED from the write is not retried", async () => {
		const { c } = setup({
			source: { repository: "paca", path: "docs/spec.md" },
		});
		c.docClient.patchDocument.mockRejectedValue(
			new PacaAPIError("locked", 409, "DOC_LOCKED"),
		);
		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				path: "Spec",
				old_string: "Paragraph 2",
				new_string: "x",
			},
			c,
		);
		expect(res.isError).toBe(true);
		expect(c.docClient.patchDocument).toHaveBeenCalledTimes(1);
		expect(text(res)).toContain("docs/spec.md");
		expect(text(res)).toMatch(/push it/);
	});

	it("old_string equal to new_string writes nothing", async () => {
		const { c } = setup();
		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				path: "Spec",
				old_string: "Paragraph 2",
				new_string: "Paragraph 2",
			},
			c,
		);
		expect(res.isError).toBeFalsy();
		expect(text(res)).toMatch(/nothing changed/i);
		expect(c.docClient.patchDocument).not.toHaveBeenCalled();
	});

	it("old_string ambiguous after the re-read carries the other writer's details", async () => {
		const { doc, c } = setup();
		const newer = docOf("e1", "Spec", `${TEN}\n\nParagraph 2 text. again`);
		c.docClient.getByPath
			.mockResolvedValueOnce({ status: "found", doc, etag: '"a"' })
			.mockResolvedValueOnce({ status: "found", doc: newer, etag: '"b"' });
		c.docClient.patchDocument.mockRejectedValueOnce(
			new PacaAPIError("conflict", 412, "VERSION_CONFLICT", {
				updated_by: "u7",
				updated_at: "2026-11-14T10:06:00Z",
				note: "copied paragraph 2",
				client: "claude-code/s3",
				task: "PACA-91",
			}),
		);
		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				path: "Spec",
				old_string: "Paragraph 2 text.",
				new_string: "x",
			},
			c,
		);
		expect(res.isError).toBe(true);
		const out = text(res);
		for (const s of [
			"2 times",
			"u7",
			"2026-11-14T10:06:00Z",
			"copied paragraph 2",
			"claude-code/s3",
			"PACA-91",
		]) {
			expect(out).toContain(s);
		}
	});

	it("409 DOC_LOCKED after the read names the repository file from a fresh read", async () => {
		const { doc, c } = setup();
		const locked = {
			...doc,
			source: { repository: "paca", path: "docs/spec.md" },
			source_locked: true,
		};
		c.docClient.getByPath
			.mockResolvedValueOnce({ status: "found", doc, etag: '"a"' })
			.mockResolvedValueOnce({ status: "found", doc: locked, etag: '"b"' });
		c.docClient.patchDocument.mockRejectedValue(
			new PacaAPIError("locked", 409, "DOC_LOCKED"),
		);
		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				path: "Spec",
				old_string: "Paragraph 2",
				new_string: "x",
			},
			c,
		);
		expect(res.isError).toBe(true);
		expect(c.docClient.patchDocument).toHaveBeenCalledTimes(1);
		expect(text(res)).toContain("docs/spec.md in paca");
	});

	it("an unlocked managed copy is edited and the reply warns about the next push", async () => {
		const { doc, c } = setup({
			source: { repository: "paca", path: "docs/spec.md" },
		});
		c.docClient.patchDocument.mockResolvedValue({ doc });
		const res = await call(
			"edit_doc",
			{
				projectId: "p1",
				path: "Spec",
				old_string: "Paragraph 2",
				new_string: "x",
			},
			c,
		);
		expect(res.isError).toBeFalsy();
		expect(text(res)).toMatch(
			/next push from the repository overwrites this edit unless it is merged into the file/,
		);
	});
});
