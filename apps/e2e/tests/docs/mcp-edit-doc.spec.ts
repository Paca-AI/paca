// spec: Paca-AI/paca#747 PR 6 — MCP edit_doc against the real API.
//
// API only (the `request` fixture, no page): runs the MCP server's own
// edit_doc code with an API key, then checks that the nine blocks the edit
// did not touch are stored exactly as before.
//
// The MCP code is loaded from its build (apps/mcp/build: run `bun install`
// and `bun run build` in apps/mcp first) with a native import(): Playwright
// compiles specs to CommonJS, and BlockNote's CommonJS build cannot load
// its ESM-only dependencies, so importing apps/mcp/src does not work.

import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { expect, test } from "@playwright/test";
import {
	API_URL,
	BASE_URL,
	authRequest,
	createProject,
	newRunId,
} from "../helpers/e2e-api";

const PREFIX = "E2E_MCPEDIT_";
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

const text = (t: string, styles: Record<string, boolean> = {}) => ({
	type: "text",
	text: t,
	styles,
});
const props = {
	textColor: "default",
	backgroundColor: "default",
	textAlignment: "left",
};

/** Ten blocks of the kinds people write: headings, styled text, lists, code. */
function tenBlocks() {
	return [
		{
			type: "heading",
			props: { ...props, level: 1, isToggleable: false },
			content: [text("Release plan")],
			children: [],
		},
		{
			type: "paragraph",
			props: { ...props, textColor: "red" },
			content: [
				text("Intro with "),
				text("bold", { bold: true }),
				text(" text."),
			],
			children: [],
		},
		{
			type: "heading",
			props: { ...props, level: 2, isToggleable: false },
			content: [text("Steps")],
			children: [],
		},
		{
			type: "numberedListItem",
			props,
			content: [text("Build the image")],
			children: [],
		},
		{
			type: "numberedListItem",
			props,
			content: [text("Push the image")],
			children: [],
		},
		{
			type: "checkListItem",
			props: { ...props, checked: true },
			content: [text("tag release")],
			children: [],
		},
		{
			type: "codeBlock",
			props: { language: "bash" },
			content: [text("# not a heading\nmake deploy")],
			children: [],
		},
		{
			type: "quote",
			props: { textColor: "default", backgroundColor: "default" },
			content: [text("Careful with prod.", { italic: true })],
			children: [],
		},
		{
			type: "heading",
			props: { ...props, level: 2, isToggleable: false },
			content: [text("Rollback")],
			children: [],
		},
		{
			type: "paragraph",
			props,
			content: [text("Run the previous tag.")],
			children: [],
		},
	];
}

test.describe("MCP edit_doc", () => {
	test("changing one line leaves the nine other stored blocks identical", async ({
		request,
	}) => {
		const { PacaAPIClient, PacaAPIDocClient, PacaAPIExtendedClient } =
			await loadMcp("api/index.js");
		const { handleFilesystemDocTool } = await loadMcp(
			"tools/filesystem-doc-tools.js",
		);
		const run = newRunId();
		await authRequest(request);
		const projectId = await createProject(request, `${PREFIX}${run}`);
		const keyRes = await request.post(`${API_URL}/users/me/api-keys`, {
			data: { name: `${PREFIX}${run}` },
		});
		expect(keyRes.ok()).toBeTruthy();
		const key = (await keyRes.json()).data as { id: string; key: string };

		try {
			const created = await request.post(
				`${API_URL}/projects/${projectId}/docs`,
				{
					data: { title: `${PREFIX}${run}`, content: tenBlocks() },
				},
			);
			expect(created.ok()).toBeTruthy();
			const docId = (await created.json()).data.id as string;
			const readBlocks = async () => {
				const res = await request.get(
					`${API_URL}/projects/${projectId}/docs/${docId}`,
				);
				expect(res.ok()).toBeTruthy();
				return ((await res.json()).data.content as unknown[]).map((b) =>
					JSON.stringify(b),
				);
			};
			const before = await readBlocks();
			expect(before).toHaveLength(10);

			const config = { baseURL: BASE_URL, apiKey: key.key };
			const result = await handleFilesystemDocTool(
				"edit_doc",
				{
					projectId,
					docId,
					old_string: "Push the image",
					new_string: "Push the tag",
					note: "e2e: one-line edit",
				},
				new PacaAPIClient(config),
				new PacaAPIDocClient(config),
				new PacaAPIExtendedClient(config),
			);
			expect(result.isError, result.content?.[0]?.text).toBeFalsy();

			const after = await readBlocks();
			expect(after).toHaveLength(10);
			for (let i = 0; i < 10; i++) {
				if (i === 4) {
					expect(after[i]).toContain("Push the tag");
					continue;
				}
				expect(after[i], `block ${i}`).toBe(before[i]);
			}
		} finally {
			await request.delete(`${API_URL}/users/me/api-keys/${key.id}`);
			await request.delete(`${API_URL}/projects/${projectId}`);
		}
	});
});
