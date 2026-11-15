import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PacaAPIError } from "../../api/doc-client.js";
import { type PushOptions, runPush } from "../../cli/push.js";
import { UnreachableError } from "../../cli/state.js";
import { blocknoteToMarkdown, markdownToBlocknote } from "../../utils/index.js";

const DOC_ID = "11111111-aaaa-4bbb-8ccc-000000000001";

function git(cwd: string, ...args: string[]) {
	return execFileSync("git", args, {
		cwd,
		encoding: "utf8",
		env: {
			...process.env,
			GIT_AUTHOR_NAME: "t",
			GIT_AUTHOR_EMAIL: "t@example.com",
			GIT_COMMITTER_NAME: "t",
			GIT_COMMITTER_EMAIL: "t@example.com",
			// Ignore the developer's global and system git config (signing, hooks).
			GIT_CONFIG_GLOBAL: "/dev/null",
			GIT_CONFIG_NOSYSTEM: "1",
		},
	}).trim();
}

function written(
	opts: {
		created?: boolean;
		changed?: boolean;
		etag?: string;
		id?: string;
	} = {},
) {
	return {
		status: "written" as const,
		doc: {
			id: opts.id ?? DOC_ID,
			title: "plan",
			content: [],
			position: 0,
			created_at: "",
			updated_at: "",
			content_hash: "hash",
		},
		created: opts.created ?? false,
		changed: opts.changed ?? true,
		etag: opts.etag ?? '"e1-0000"',
	};
}

describe("runPush", () => {
	let root: string;
	let lines: string[];
	let client: {
		putByPath: ReturnType<typeof vi.fn>;
		getByPath: ReturnType<typeof vi.fn>;
	};
	const io = {
		out: (l: string) => lines.push(l),
		err: (l: string) => lines.push(l),
	};
	const push = (file: string, extra: Partial<PushOptions> = {}) =>
		runPush(
			{
				projectId: "p1",
				file,
				lock: false,
				force: false,
				dryRun: false,
				client: "paca-cli",
				...extra,
			},
			client,
			io,
			{ cwd: root },
		);
	const write = (rel: string, text: string) => {
		fs.mkdirSync(path.dirname(path.join(root, rel)), { recursive: true });
		fs.writeFileSync(path.join(root, rel), text);
	};
	const state = () =>
		JSON.parse(fs.readFileSync(path.join(root, ".paca/push.json"), "utf8"));
	const body = (n = 0) => client.putByPath.mock.calls[n][1];
	const putOpts = (n = 0) => client.putByPath.mock.calls[n][2];

	beforeEach(() => {
		root = fs.realpathSync(
			fs.mkdtempSync(path.join(os.tmpdir(), "paca-push-")),
		);
		lines = [];
		client = { putByPath: vi.fn(), getByPath: vi.fn() };
		git(root, "init", "-q");
		git(root, "remote", "add", "origin", "git@github.com:Acme/widgets.git");
		write("docs/plan.md", "# Plan\n\nfirst\n");
		git(root, "add", ".");
		git(root, "commit", "-q", "-m", "Add the plan");
	});
	afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

	it("first push sends source and no If-Match", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		expect(
			await push("docs/plan.md", {
				lock: true,
				task: "PACA-88",
				client: "cc/s1",
			}),
		).toBe(0);
		expect(client.putByPath).toHaveBeenCalledTimes(1);
		expect(client.putByPath.mock.calls[0][0]).toBe("p1");
		expect(body()).toMatchObject({
			path: "Plans/plan",
			source: {
				repository: "Acme/widgets",
				path: "docs/plan.md",
				commit: git(root, "rev-parse", "HEAD"),
			},
			lock: true,
			task: "PACA-88",
			client: "cc/s1",
		});
		expect(blocknoteToMarkdown(body().content)).toBe(
			blocknoteToMarkdown(markdownToBlocknote("# Plan\n\nfirst\n")),
		);
		expect(putOpts()).toEqual({});
		expect(lines).toContain("created Plans/plan");
		expect(state()["docs/plan.md"]).toMatchObject({
			doc_path: "Plans/plan",
			doc_id: DOC_ID,
			etag: '"e1-0000"',
		});
	});

	it("unchanged file makes no request", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		lines = [];
		expect(await push("docs/plan.md")).toBe(0);
		expect(client.putByPath).toHaveBeenCalledTimes(1);
		expect(lines).toEqual(["unchanged"]);
	});

	it("second push sends If-Match", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		write("docs/plan.md", "# Plan\n\nsecond\n");
		client.putByPath.mockResolvedValue(written({ etag: '"e2-0000"' }));
		expect(await push("docs/plan.md")).toBe(0);
		expect(putOpts(1)).toEqual({ ifMatch: '"e1-0000"' });
		expect(lines).toContain("updated Plans/plan");
		expect(state()["docs/plan.md"].etag).toBe('"e2-0000"');
	});

	it("--force sends no If-Match and pushes an unchanged file", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		expect(await push("docs/plan.md", { force: true })).toBe(0);
		expect(client.putByPath).toHaveBeenCalledTimes(2);
		expect(putOpts(1)).toEqual({});
	});

	it("412 prints the other writer's note and a diff, exits 2 and keeps state", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		const before = fs.readFileSync(path.join(root, ".paca/push.json"), "utf8");
		write("docs/plan.md", "# Plan\n\nmine\n");
		client.putByPath.mockRejectedValue(
			new PacaAPIError("version conflict", 412, "VERSION_CONFLICT", {
				content_hash: "other",
				updated_at: "2026-11-14T10:00:00Z",
				updated_by: "user-7",
				note: "fixed the intro",
				client: "web",
				task: "PACA-90",
			}),
		);
		client.getByPath.mockResolvedValue({
			status: "found",
			doc: {
				content: markdownToBlocknote("# Plan\n\nfirst, edited in Paca\n"),
			},
		});
		lines = [];
		expect(await push("docs/plan.md")).toBe(2);
		const out = lines.join("\n");
		expect(out).toContain(
			"conflict: the Paca copy changed since the last push",
		);
		expect(out).toContain("user-7");
		expect(out).toContain("2026-11-14T10:00:00Z");
		expect(out).toContain("note: fixed the intro");
		expect(out).toContain("client: web");
		expect(out).toContain("task: PACA-90");
		expect(out).toContain("--- last push");
		expect(out).toContain("+++ Paca copy");
		expect(out).toContain("-first");
		expect(out).toContain("+first, edited in Paca");
		expect(out).toContain(
			"merge that change into docs/plan.md and push again, or use --force to overwrite",
		);
		expect(client.getByPath).toHaveBeenCalledWith("p1", "Plans/plan");
		expect(fs.readFileSync(path.join(root, ".paca/push.json"), "utf8")).toBe(
			before,
		);
	});

	it("412 without details says the document is gone and exits 2", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		write("docs/plan.md", "changed\n");
		client.putByPath.mockRejectedValue(
			new PacaAPIError("version conflict", 412, "VERSION_CONFLICT"),
		);
		lines = [];
		expect(await push("docs/plan.md")).toBe(2);
		const out = lines.join("\n");
		expect(out).toContain("Plans/plan is gone or was moved");
		expect(out).toContain("use --force to create it again");
	});

	it("note defaults to the last commit subject", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		expect(body().note).toBe("Add the plan");
	});

	it("--note wins over the commit subject", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md", { note: "mine" });
		expect(body().note).toBe("mine");
	});

	it("dirty file omits commit and the default note", async () => {
		write("docs/plan.md", "# Plan\n\nuncommitted\n");
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		expect(body().source).toEqual({
			repository: "Acme/widgets",
			path: "docs/plan.md",
		});
		expect(body().note).toBeUndefined();
	});

	it("an untracked file omits commit", async () => {
		write("docs/new.md", "new\n");
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/new.md");
		expect(body().source.commit).toBeUndefined();
	});

	it("server says unchanged", async () => {
		client.putByPath.mockResolvedValue(written({ changed: false }));
		expect(await push("docs/plan.md")).toBe(0);
		expect(lines).toContain("unchanged");
		expect(state()["docs/plan.md"].file_sha256).toMatch(/^[0-9a-f]{64}$/);
	});

	it("--dry-run sends nothing", async () => {
		expect(await push("docs/plan.md", { dryRun: true, lock: true })).toBe(0);
		expect(client.putByPath).not.toHaveBeenCalled();
		expect(fs.existsSync(path.join(root, ".paca"))).toBe(false);
		const out = lines.join("\n");
		expect(out).toContain("Plans/plan");
		expect(out).toContain("Acme/widgets");
	});

	it("uses Specifications/ under a specs folder and reuses the recorded path", async () => {
		write("docs/specs/api.md", "spec\n");
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/specs/api.md");
		expect(body().path).toBe("Specifications/api");
		const s = state();
		s["docs/specs/api.md"].doc_path = "Elsewhere/API";
		fs.writeFileSync(path.join(root, ".paca/push.json"), JSON.stringify(s));
		write("docs/specs/api.md", "spec v2\n");
		await push("docs/specs/api.md");
		expect(body(1).path).toBe("Elsewhere/API");
	});

	it("--path overrides the recorded path and sends no If-Match there", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		write("docs/plan.md", "v2\n");
		await push("docs/plan.md", { path: "Other/Plan" });
		expect(body(1).path).toBe("Other/Plan");
		expect(putOpts(1)).toEqual({});
		expect(state()["docs/plan.md"].doc_path).toBe("Other/Plan");
	});

	it("409 DOC_PATH_CHANGED is retried once", async () => {
		client.putByPath
			.mockRejectedValueOnce(
				new PacaAPIError("path changed", 409, "DOC_PATH_CHANGED"),
			)
			.mockResolvedValueOnce(written({ created: true }));
		expect(await push("docs/plan.md")).toBe(0);
		expect(client.putByPath).toHaveBeenCalledTimes(2);

		client.putByPath.mockReset();
		client.putByPath.mockRejectedValue(
			new PacaAPIError("path changed", 409, "DOC_PATH_CHANGED"),
		);
		write("docs/plan.md", "v2\n");
		lines = [];
		expect(await push("docs/plan.md")).toBe(1);
		expect(client.putByPath).toHaveBeenCalledTimes(2);
	});

	it("409 DOC_LOCKED names the repository the copy is locked to", async () => {
		client.putByPath.mockRejectedValue(
			new PacaAPIError("locked", 409, "DOC_LOCKED"),
		);
		client.getByPath.mockResolvedValue({
			status: "found",
			doc: {
				source: { repository: "Other/repo", path: "plan.md" },
				source_locked: true,
			},
		});
		expect(await push("docs/plan.md")).toBe(1);
		expect(lines.join("\n")).toContain("locked to Other/repo (plan.md)");
	});

	it("a server without by-path writes exits 1", async () => {
		client.putByPath.mockResolvedValue({ status: "unsupported" });
		expect(await push("docs/plan.md")).toBe(1);
		expect(lines.join("\n")).toContain(
			"this Paca server is too old for pull/push",
		);
	});

	it("an unreachable server records nothing", async () => {
		client.putByPath.mockRejectedValue(new TypeError("fetch failed"));
		await expect(push("docs/plan.md")).rejects.toBeInstanceOf(UnreachableError);
		expect(fs.existsSync(path.join(root, ".paca/push.json"))).toBe(false);
	});

	it("warns when the document pushed before is gone and a new one was made", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		write("docs/plan.md", "v2\n");
		client.putByPath.mockResolvedValue(
			written({ created: true, id: "22222222-aaaa-4bbb-8ccc-000000000002" }),
		);
		lines = [];
		expect(await push("docs/plan.md", { force: true })).toBe(0);
		expect(lines.join("\n")).toContain(
			`the document pushed before (${DOC_ID}) is gone`,
		);
	});

	it("does not warn when a new --path makes a new document", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		write("docs/plan.md", "v2\n");
		client.putByPath.mockResolvedValue(
			written({ created: true, id: "22222222-aaaa-4bbb-8ccc-000000000002" }),
		);
		lines = [];
		expect(await push("docs/plan.md", { path: "Other/Plan" })).toBe(0);
		expect(lines.join("\n")).not.toContain("is gone");
	});

	it("says when a push only locked the document", async () => {
		const res = written({ changed: false });
		(res.doc as any).source_locked = true;
		client.putByPath.mockResolvedValue(res);
		expect(await push("docs/plan.md", { lock: true })).toBe(0);
		expect(lines).toContain("unchanged content; Plans/plan is locked");
	});

	it("an entry recorded for another project is ignored", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		expect(await push("docs/plan.md", { projectId: "p2" })).toBe(0);
		expect(client.putByPath).toHaveBeenCalledTimes(2);
		expect(client.putByPath.mock.calls[1][0]).toBe("p2");
		expect(putOpts(1)).toEqual({});
		expect(state()["docs/plan.md"]).toMatchObject({ project: "p2" });
	});

	it("an entry recorded for another server is ignored", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md", { server: "http://a.test" });
		expect(await push("docs/plan.md", { server: "http://b.test" })).toBe(0);
		expect(client.putByPath).toHaveBeenCalledTimes(2);
		expect(putOpts(1)).toEqual({});
		expect(state()["docs/plan.md"]).toMatchObject({
			project: "p1",
			server: "http://b.test",
		});
	});

	it("--path on an unchanged file pushes it", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		expect(await push("docs/plan.md", { path: "Other/X" })).toBe(0);
		expect(client.putByPath).toHaveBeenCalledTimes(2);
		expect(body(1).path).toBe("Other/X");
	});

	it("--lock on an unchanged file pushes it", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		expect(await push("docs/plan.md", { lock: true })).toBe(0);
		expect(client.putByPath).toHaveBeenCalledTimes(2);
		expect(body(1).lock).toBe(true);
	});

	it("the conflict diff ignores Markdown the converter rewrites", async () => {
		write("docs/plan.md", "# T\n\n__bold__ text\n\n1) x\n2) y\n");
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		write("docs/plan.md", "# T\n\n__bold__ text\n\n1) x\n2) y\n\nmine\n");
		client.putByPath.mockRejectedValue(
			new PacaAPIError("version conflict", 412, "VERSION_CONFLICT", {
				content_hash: "other",
				updated_at: "t",
			}),
		);
		client.getByPath.mockResolvedValue({
			status: "found",
			doc: {
				content: markdownToBlocknote(
					"# T\n\n__bold__ text, edited\n\n1) x\n2) y\n",
				),
			},
		});
		lines = [];
		expect(await push("docs/plan.md")).toBe(2);
		const changed = lines.filter(
			(l) => /^[-+]/.test(l) && !l.startsWith("---") && !l.startsWith("+++"),
		);
		expect(changed).toHaveLength(2);
		expect(changed[0]).toMatch(/^-.*bold.* text$/);
		expect(changed[1]).toMatch(/^\+.*bold.* text, edited$/);
	});

	it("a network failure reading the Paca copy after a 412 still exits 2", async () => {
		client.putByPath.mockResolvedValue(written({ created: true }));
		await push("docs/plan.md");
		write("docs/plan.md", "mine\n");
		client.putByPath.mockRejectedValue(
			new PacaAPIError("version conflict", 412, "VERSION_CONFLICT", {
				content_hash: "other",
				updated_at: "t",
			}),
		);
		client.getByPath.mockRejectedValue(new TypeError("fetch failed"));
		lines = [];
		expect(await push("docs/plan.md")).toBe(2);
		const out = lines.join("\n");
		expect(out).toContain(
			"conflict: the Paca copy changed since the last push",
		);
		expect(out).toContain("could not read the Paca copy");
	});

	it("accepts a file whose name starts with two dots", async () => {
		write("..draft.md", "draft\n");
		client.putByPath.mockResolvedValue(written({ created: true }));
		expect(await push("..draft.md")).toBe(0);
		expect(body().source.path).toBe("..draft.md");
	});

	it("outside git, uses the folder name and paths from the working folder", async () => {
		const plain = fs.realpathSync(
			fs.mkdtempSync(path.join(os.tmpdir(), "paca-plain-")),
		);
		try {
			fs.writeFileSync(path.join(plain, "notes.md"), "n\n");
			client.putByPath.mockResolvedValue(written({ created: true }));
			const code = await runPush(
				{
					projectId: "p1",
					file: "notes.md",
					lock: false,
					force: false,
					dryRun: false,
					client: "paca-cli",
				},
				client,
				io,
				{ cwd: plain },
			);
			expect(code).toBe(0);
			expect(body().source).toEqual({
				repository: path.basename(plain),
				path: "notes.md",
			});
			expect(fs.existsSync(path.join(plain, ".paca/push.json"))).toBe(true);
		} finally {
			fs.rmSync(plain, { recursive: true, force: true });
		}
	});

	describe("a document path another file already uses", () => {
		beforeEach(async () => {
			write("docs/api/plan.md", "# API plan\n");
			write("docs/ui/plan.md", "# UI plan\n");
			client.putByPath.mockResolvedValue(written({ created: true }));
			expect(await push("docs/api/plan.md")).toBe(0);
			client.putByPath.mockClear();
			lines = [];
		});

		it("is refused, naming the other file", async () => {
			expect(await push("docs/ui/plan.md")).toBe(1);
			expect(client.putByPath).not.toHaveBeenCalled();
			expect(lines.join("\n")).toContain(
				"Plans/plan is already pushed from docs/api/plan.md",
			);
			expect(lines.join("\n")).toContain("--path");
			expect(state()["docs/ui/plan.md"]).toBeUndefined();
			expect(state()["docs/api/plan.md"]).toMatchObject({
				doc_path: "Plans/plan",
			});
		});

		it("is refused for --path too", async () => {
			write("notes/other.md", "x\n");
			expect(await push("notes/other.md", { path: "Plans/plan" })).toBe(1);
			expect(client.putByPath).not.toHaveBeenCalled();
		});

		it("is refused with --force", async () => {
			expect(await push("docs/ui/plan.md", { force: true })).toBe(1);
			expect(client.putByPath).not.toHaveBeenCalled();
		});

		it("is refused with --dry-run", async () => {
			expect(await push("docs/ui/plan.md", { dryRun: true })).toBe(1);
			expect(lines.join("\n")).toContain(
				"Plans/plan is already pushed from docs/api/plan.md",
			);
		});

		const TAKEOVER =
			"docs/b/plan.md takes over Plans/plan from docs/api/plan.md, which no longer exists";
		const baseFiles = () =>
			fs.readdirSync(path.join(root, ".paca/push-base")).sort();
		const moveApi = () => {
			fs.mkdirSync(path.join(root, "docs/b"), { recursive: true });
			fs.renameSync(
				path.join(root, "docs/api/plan.md"),
				path.join(root, "docs/b/plan.md"),
			);
		};

		it("is taken over when the other file is gone, keeping If-Match", async () => {
			const basesBefore = baseFiles();
			moveApi();
			client.putByPath.mockResolvedValue(
				written({ changed: false, etag: '"e2-0000"' }),
			);
			expect(await push("docs/b/plan.md")).toBe(0);
			// Same content, new source path: still sent.
			expect(client.putByPath).toHaveBeenCalledTimes(1);
			expect(putOpts()).toEqual({ ifMatch: '"e1-0000"' });
			expect(body()).toMatchObject({
				path: "Plans/plan",
				source: { path: "docs/b/plan.md" },
			});
			expect(lines).toContain(TAKEOVER);
			const s = state();
			expect(s["docs/api/plan.md"]).toBeUndefined();
			expect(s["docs/b/plan.md"]).toMatchObject({
				doc_path: "Plans/plan",
				doc_id: DOC_ID,
				etag: '"e2-0000"',
			});
			// The base copy follows the entry.
			expect(baseFiles()).toHaveLength(basesBefore.length);
			expect(baseFiles()).not.toEqual(basesBefore);
		});

		it("a takeover records the new entry before it removes the old base copy", async () => {
			moveApi();
			// The old base copy cannot be removed (a folder is in its place):
			// the new entry must be recorded anyway.
			const oldBase = path.join(
				root,
				".paca/push-base",
				`${createHash("sha256").update("\np1\ndocs/api/plan.md").digest("hex")}.md`,
			);
			fs.rmSync(oldBase);
			fs.mkdirSync(path.join(oldBase, "x"), { recursive: true });
			client.putByPath.mockResolvedValue(written({ etag: '"e2-0000"' }));
			await push("docs/b/plan.md").catch(() => {});
			expect(state()["docs/api/plan.md"]).toBeUndefined();
			expect(state()["docs/b/plan.md"]).toMatchObject({ etag: '"e2-0000"' });
		});

		it("a 412 after a takeover shows the diff from the old base", async () => {
			moveApi();
			write("docs/b/plan.md", "# API plan\n\nmine\n");
			client.putByPath.mockRejectedValue(
				new PacaAPIError("version conflict", 412, "VERSION_CONFLICT", {
					content_hash: "other",
					updated_by: "user-7",
				}),
			);
			client.getByPath.mockResolvedValue({
				status: "found",
				doc: { content: markdownToBlocknote("# API plan\n\nfrom Paca\n") },
			});
			const before = fs.readFileSync(
				path.join(root, ".paca/push.json"),
				"utf8",
			);
			expect(await push("docs/b/plan.md")).toBe(2);
			expect(putOpts()).toEqual({ ifMatch: '"e1-0000"' });
			const out = lines.join("\n");
			expect(out).toContain("--- last push");
			expect(out).toContain("+from Paca");
			expect(out).not.toContain("is not kept here");
			// Nothing recorded on a conflict: the next push takes over again.
			expect(fs.readFileSync(path.join(root, ".paca/push.json"), "utf8")).toBe(
				before,
			);
		});

		it("--dry-run reports a takeover and changes nothing", async () => {
			moveApi();
			const before = fs.readFileSync(
				path.join(root, ".paca/push.json"),
				"utf8",
			);
			const basesBefore = baseFiles();
			expect(await push("docs/b/plan.md", { dryRun: true })).toBe(0);
			expect(client.putByPath).not.toHaveBeenCalled();
			expect(lines).toContain(TAKEOVER);
			expect(lines.join("\n")).toContain('If-Match: "e1-0000"');
			expect(fs.readFileSync(path.join(root, ".paca/push.json"), "utf8")).toBe(
				before,
			);
			expect(baseFiles()).toEqual(basesBefore);
		});

		it("is taken over after a move that changes only case", async () => {
			// On a case-insensitive file system (macOS, Windows) the old name
			// still opens the moved file; on a case-sensitive one it is gone.
			// Either way this push takes over the entry.
			fs.renameSync(path.join(root, "docs/api"), path.join(root, "docs/tmp"));
			fs.renameSync(path.join(root, "docs/tmp"), path.join(root, "docs/API"));
			client.putByPath.mockResolvedValue(written({ etag: '"e2-0000"' }));
			expect(await push("docs/API/plan.md")).toBe(0);
			expect(putOpts()).toEqual({ ifMatch: '"e1-0000"' });
			expect(lines.join("\n")).toContain(
				"docs/API/plan.md takes over Plans/plan from docs/api/plan.md",
			);
			expect(state()["docs/api/plan.md"]).toBeUndefined();
			expect(state()["docs/API/plan.md"]).toMatchObject({ etag: '"e2-0000"' });
		});

		it("is allowed with another --path", async () => {
			client.putByPath.mockResolvedValue(written({ id: "other-doc" }));
			expect(await push("docs/ui/plan.md", { path: "Plans/ui-plan" })).toBe(0);
			expect(body()).toMatchObject({ path: "Plans/ui-plan" });
		});

		it.each([
			["a case variant", "docs/ui/Plan.md", undefined],
			["spaces around segments", "docs/ui/plan.md", " Plans / plan "],
			["doubled slashes", "docs/ui/plan.md", "Plans//plan/"],
		])("is refused for %s of the path", async (_why, file, docPath) => {
			if (file !== "docs/ui/plan.md") {
				fs.rmSync(path.join(root, "docs/ui/plan.md"));
				write(file, "# UI plan\n");
			}
			expect(await push(file, docPath ? { path: docPath } : {})).toBe(1);
			expect(client.putByPath).not.toHaveBeenCalled();
			expect(lines.join("\n")).toContain(
				"is already pushed from docs/api/plan.md",
			);
		});

		it("--path that differs from the recorded path only in case and spaces keeps If-Match", async () => {
			write("docs/api/plan.md", "# API plan\n\nmore\n");
			client.putByPath.mockResolvedValue(written());
			expect(await push("docs/api/plan.md", { path: " plans / PLAN " })).toBe(
				0,
			);
			expect(putOpts()).toEqual({ ifMatch: '"e1-0000"' });
			// Unchanged file, same document under another spelling: unchanged.
			client.putByPath.mockClear();
			expect(await push("docs/api/plan.md", { path: "PLANS/plan" })).toBe(0);
			expect(client.putByPath).not.toHaveBeenCalled();
		});

		describe("when two entries already claim one document", () => {
			const addEntry = (file: string, etag: string) => {
				const st = state();
				st[file] = { ...st["docs/api/plan.md"], doc_path: "plans/Plan", etag };
				fs.writeFileSync(
					path.join(root, ".paca/push.json"),
					JSON.stringify(st),
				);
			};

			it("refuses while either file exists", async () => {
				addEntry("docs/old/plan.md", '"e0-0000"'); // gone
				expect(await push("docs/ui/plan.md")).toBe(1);
				expect(lines.join("\n")).toContain(
					"is already pushed from docs/api/plan.md",
				);
				expect(client.putByPath).not.toHaveBeenCalled();
			});

			it("takes over when all are gone and drops every stale entry", async () => {
				addEntry("docs/old/plan.md", '"e0-0000"');
				fs.rmSync(path.join(root, "docs/api/plan.md"));
				client.putByPath.mockResolvedValue(written({ etag: '"e2-0000"' }));
				expect(await push("docs/ui/plan.md")).toBe(0);
				// The first stale entry by file name gives If-Match: an older one
				// can only end in a conflict, never in an overwrite.
				expect(putOpts()).toEqual({ ifMatch: '"e1-0000"' });
				const out = lines.join("\n");
				expect(out).toContain("from docs/api/plan.md");
				expect(out).toContain("docs/old/plan.md");
				expect(Object.keys(state()).sort()).toEqual(["docs/ui/plan.md"]);
			});
		});

		it("refuses to record a document another file already records", async () => {
			// The server can match names that compare differently here (a few
			// non-ASCII characters lower-case differently in JavaScript and
			// Postgres): the response's document id gives it away.
			client.putByPath.mockResolvedValue(written());
			expect(await push("docs/ui/plan.md", { path: "Plans/other" })).toBe(1);
			expect(lines.join("\n")).toContain("docs/api/plan.md");
			expect(state()["docs/ui/plan.md"]).toBeUndefined();
		});

		it("is allowed when the other entry is for another project or server", async () => {
			expect(await push("docs/ui/plan.md", { projectId: "p2" })).toBe(0);
			expect(
				await push("docs/ui/plan.md", { server: "http://other.test" }),
			).toBe(0);
			expect(client.putByPath).toHaveBeenCalledTimes(2);
		});
	});

	it("refuses a file outside the repository", async () => {
		const outside = path.join(os.tmpdir(), `paca-outside-${process.pid}.md`);
		fs.writeFileSync(outside, "x");
		try {
			expect(await push(outside)).toBe(1);
			expect(client.putByPath).not.toHaveBeenCalled();
		} finally {
			fs.rmSync(outside, { force: true });
		}
	});
});
