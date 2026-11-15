// spec: features/docs/etag-through-gateway.feature
// seed: tests/seed.spec.ts
//
// A document ETag sent back through the gateway.
//
// The gateway compresses responses (Caddy `encode gzip`) and appends the
// content coding to a strong ETag: "<hash>-<meta>" becomes
// "<hash>-<meta>-gzip". A client that sends the tag it received back in
// If-Match must get its write through, not a 400.

import { type APIRequestContext, expect, test } from "@playwright/test";
import {
	API_URL,
	authRequest,
	createProject,
	newRunId,
} from "../helpers/e2e-api";

const PREFIX = "E2E_ETAG_GZIP_";
const RUN_ID = newRunId();

// Enough text that the gateway compresses the response (Caddy skips bodies
// under 512 bytes).
const paragraphs = (label: string) =>
	Array.from({ length: 60 }, (_, i) => ({
		type: "paragraph",
		content: [
			{
				type: "text",
				text: `${label} paragraph ${i}: the quick brown fox jumps over the lazy dog.`,
				styles: {},
			},
		],
	}));

let ownProjects: string[] = [];

async function newProject(request: APIRequestContext): Promise<string> {
	const id = await createProject(
		request,
		`${PREFIX}${RUN_ID}_${test.info().repeatEachIndex}`,
	);
	ownProjects.push(id);
	return id;
}

test.beforeEach(async ({ request }) => {
	ownProjects = [];
	await authRequest(request);
});

test.afterEach(async ({ request }) => {
	await authRequest(request);
	await Promise.all(
		ownProjects.map((id) => request.delete(`${API_URL}/projects/${id}`)),
	);
});

test("the ETag the gateway returns works as If-Match on a content write", async ({
	request,
}) => {
	const projectId = await newProject(request);
	const docs = `${API_URL}/projects/${projectId}/docs`;

	const created = await request.post(docs, {
		data: { title: "Gzip ETag", content: paragraphs("first") },
	});
	expect(created.status()).toBe(201);
	const docId = (await created.json()).data.id as string;

	const read = await request.get(`${docs}/${docId}`, {
		headers: { "Accept-Encoding": "gzip" },
	});
	expect(read.status()).toBe(200);
	const etag = read.headers().etag;
	expect(etag).toBeTruthy();
	const before = (await read.json()).data.content_hash as string;

	// Round-trip the tag exactly as the gateway returned it.
	const write = await request.patch(`${docs}/${docId}`, {
		headers: { "If-Match": etag },
		data: { content: paragraphs("second") },
	});
	const body = await write.text();
	expect(write.status(), body).toBe(200);
	expect(JSON.parse(body).data.content_hash).not.toBe(before);
});
