// spec: Paca-AI/paca#747 PR 8 — a document edited as a file while a person
// edits it in the web editor.
//
// The CLI's pull and push (apps/mcp/build: run `bun install` and `bun run
// build` in apps/mcp first) mirror the document to a file with an API key.
// A person edits paragraph 1 in the browser while the mirror file's
// paragraph 3 is edited: the first push is refused (the document changed),
// pull merges, the second push sends the file. Both changes end up in the
// document, and the blocks neither side touched are stored as before.
//
// The build is loaded with a native import(), as in mcp-edit-doc.spec.ts.

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	BASE_URL,
	createProject,
	newRunId,
	signIn,
} from "../helpers/e2e-api";

const PREFIX = "E2E_MCPMIRROR_";
const MCP_BUILD = path.resolve(__dirname, "../../../mcp/build");

// Kept out of Playwright's transform, which would turn import() into require().
const nativeImport = new Function("p", "return import(p)") as (
	p: string,
	// biome-ignore lint/suspicious/noExplicitAny: an untyped built module.
) => Promise<any>;
const loadMcp = async (file: string) => {
	const full = path.join(MCP_BUILD, file);
	if (!fs.existsSync(full)) {
		throw new Error(
			`${full} is missing: run "bun install --frozen-lockfile && bun run build" in apps/mcp first.`,
		);
	}
	return nativeImport(pathToFileURL(full).href);
};

const props = {
	textColor: "default",
	backgroundColor: "default",
	textAlignment: "left",
};
const para = (id: string, t: string, extra: Record<string, string> = {}) => ({
	id,
	type: "paragraph",
	props: { ...props, ...extra },
	content: [{ type: "text", text: t, styles: {} }],
	children: [],
});

const IDS = Array.from({ length: 6 }, (_, i) => `mcp-mirror-b${i}`);

function blocks() {
	return [
		{
			id: IDS[0],
			type: "heading",
			props: { ...props, level: 1, isToggleable: false },
			content: [{ type: "text", text: "Mirror plan", styles: {} }],
			children: [],
		},
		para(IDS[1], "Paragraph one."),
		para(IDS[2], "Paragraph two."),
		para(IDS[3], "Paragraph three."),
		// Formatting Markdown cannot show, untouched by both sides.
		para(IDS[4], "Paragraph four, in red.", { textColor: "red" }),
		para(IDS[5], "Paragraph five."),
	];
}

test.describe("MCP mirror", () => {
	test("a browser edit and a mirror file edit both reach the document; other blocks stay as stored", async ({
		page,
		request,
	}) => {
		const { PacaAPIDocClient } = await loadMcp("api/index.js");
		const { runPull } = await loadMcp("cli/pull.js");
		const { runPush } = await loadMcp("cli/push.js");
		const run = newRunId();
		const title = `${PREFIX}${run}`;
		await authRequest(request);
		const projectId = await createProject(request, title);
		const keyRes = await request.post(`${API_URL}/users/me/api-keys`, {
			data: { name: title },
		});
		expect(keyRes.ok()).toBeTruthy();
		const key = (await keyRes.json()).data as { id: string; key: string };
		const work = fs.mkdtempSync(path.join(os.tmpdir(), "paca-e2e-mirror-"));

		try {
			const created = await request.post(
				`${API_URL}/projects/${projectId}/docs`,
				{ data: { title, content: blocks() } },
			);
			expect(created.ok()).toBeTruthy();
			const docId = (await created.json()).data.id as string;
			const readBlocks = async () => {
				const res = await request.get(
					`${API_URL}/projects/${projectId}/docs/${docId}`,
				);
				expect(res.ok()).toBeTruthy();
				return (await res.json()).data.content as Array<{ id: string }>;
			};

			const client = new PacaAPIDocClient({
				baseURL: BASE_URL,
				apiKey: key.key,
			});
			const said: string[] = [];
			const io = {
				out: (l: string) => said.push(l),
				err: (l: string) => said.push(l),
			};
			const pull = () =>
				runPull(
					{ projectId, dir: path.join(work, "docs"), force: false },
					client,
					io,
				);
			const push = () =>
				runPush(
					{
						projectId,
						file: `docs/${title}.md`,
						lock: false,
						force: false,
						dryRun: false,
						client: "e2e-mirror",
						note: "e2e: mirror edit of paragraph three",
					},
					client,
					io,
					{ cwd: work },
				);

			// 1. Pull, and edit paragraph 3 in the mirror file.
			expect(await pull(), said.join("\n")).toBe(0);
			const file = path.join(work, "docs", `${title}.md`);
			const pulled = fs.readFileSync(file, "utf8");
			expect(pulled).toContain("Paragraph three.");
			fs.writeFileSync(
				file,
				pulled.replace(
					"Paragraph three.",
					"Paragraph three, edited as a file.",
				),
			);

			// 2. A person edits paragraph 1 in the web editor and saves.
			await signIn(page);
			await page.goto(`${BASE_URL}/projects/${projectId}/docs/${docId}`);
			const editor = page.locator('[contenteditable="true"]').last();
			await expect(editor).toBeVisible({ timeout: 10_000 });
			await editor.getByText("Paragraph one.", { exact: true }).click();
			await page.keyboard.press("End");
			await page.keyboard.type(" Edited in the browser.");
			await page.keyboard.press("Control+s");
			await expect
				.poll(async () => JSON.stringify((await readBlocks())[1]), {
					timeout: 10_000,
				})
				.toContain("Edited in the browser.");
			// Leave the editor, so no later autosave changes the document
			// after the reference below is read.
			await page.goto(`${BASE_URL}/projects/${projectId}`);
			// A save on leaving may still land: wait until two reads agree.
			let seen = "";
			await expect
				.poll(
					async () => {
						const now = JSON.stringify(await readBlocks());
						const same = now === seen;
						seen = now;
						return same;
					},
					{ timeout: 10_000, intervals: [500] },
				)
				.toBe(true);
			// What the browser's save stored: the reference for the blocks the
			// mirror edit must leave alone.
			const browserBlocks = await readBlocks();
			const browser = browserBlocks.map((b) => JSON.stringify(b));

			// 3. Push is refused: the document changed since the pull.
			said.length = 0;
			expect(await push(), said.join("\n")).toBe(2);
			expect(said.join("\n")).toContain("the document changed in Paca");
			expect(JSON.stringify(await readBlocks())).toBe(
				JSON.stringify(browserBlocks),
			);

			// 4. Pull merges the browser's edit into the file.
			said.length = 0;
			expect(await pull(), said.join("\n")).toBe(0);
			expect(said.join("\n")).toContain("merged");
			const merged = fs.readFileSync(file, "utf8");
			expect(merged).toContain("Paragraph one. Edited in the browser.");
			expect(merged).toContain("Paragraph three, edited as a file.");

			// 5. Push sends the file's edit.
			said.length = 0;
			expect(await push(), said.join("\n")).toBe(0);
			expect(said.join("\n")).toMatch(/updated .* \(1 block changed\)/);

			const after = (await readBlocks()).map((b) => JSON.stringify(b));
			expect(after).toHaveLength(browser.length);
			expect(after[1]).toContain("Paragraph one. Edited in the browser.");
			expect(after[3]).toContain("Paragraph three, edited as a file.");
			// The edited block keeps the id the browser's save stored (if any).
			expect(JSON.parse(after[3]).id).toBe(JSON.parse(browser[3]).id);
			for (const i of [0, 1, 2, 4, 5]) {
				expect(after[i], `block ${i}`).toBe(browser[i]);
			}
			expect(after[4]).toContain('"textColor":"red"');

			// 6. The mirror is up to date.
			said.length = 0;
			expect(await pull(), said.join("\n")).toBe(0);
			expect(said).toEqual(["up to date"]);
		} finally {
			fs.rmSync(work, { recursive: true, force: true });
			await request.delete(`${API_URL}/users/me/api-keys/${key.id}`);
			await request.delete(`${API_URL}/projects/${projectId}`);
		}
	});
});
