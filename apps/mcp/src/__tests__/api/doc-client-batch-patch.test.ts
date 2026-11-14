import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PacaAPIDocClient, PacaAPIError } from "../../api/doc-client.js";

const CONFIG = { baseURL: "https://api.example.com", apiKey: "key123" };
const DOC = {
	id: "d1",
	title: "Doc",
	content: [],
	position: 0,
	created_at: "",
	updated_at: "",
};

function respond(
	status: number,
	body: unknown,
	headers: Record<string, string> = {},
) {
	const text = typeof body === "string" ? body : JSON.stringify(body);
	return {
		ok: status >= 200 && status < 300,
		status,
		statusText: String(status),
		headers: new Headers(headers),
		text: async () => text,
		json: async () => JSON.parse(text),
	};
}

const envelope = (data: unknown) => ({ success: true, data });
const apiError = (
	code: string,
	error: string,
	details?: Record<string, string>,
) => ({
	success: false,
	error_code: code,
	error,
	...(details ? { error_details: details } : {}),
});

describe("PacaAPIDocClient batchGet and patchDocument", () => {
	let fetchMock: ReturnType<typeof vi.fn>;
	let client: PacaAPIDocClient;

	beforeEach(() => {
		fetchMock = vi.fn();
		vi.stubGlobal("fetch", fetchMock);
		client = new PacaAPIDocClient(CONFIG);
	});
	afterEach(() => vi.unstubAllGlobals());

	it("batchGet posts the ids and returns the documents", async () => {
		fetchMock.mockResolvedValue(respond(200, envelope({ items: [DOC] })));
		expect(await client.batchGet("p1", ["d1", "d2"])).toEqual({
			status: "ok",
			items: [DOC],
		});
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe(
			"https://api.example.com/api/v1/projects/p1/docs/batch-get",
		);
		expect(init.method).toBe("POST");
		expect(JSON.parse(init.body)).toEqual({ ids: ["d1", "d2"] });
	});

	it("batchGet reports a server without the route", async () => {
		// An older server has no POST on /docs/{docId}.
		fetchMock.mockResolvedValueOnce(respond(405, ""));
		expect(await client.batchGet("p1", ["d1"])).toEqual({
			status: "unsupported",
		});
		fetchMock.mockResolvedValueOnce(respond(404, "404 page not found"));
		expect(await client.batchGet("p1", ["d1"])).toEqual({
			status: "unsupported",
		});
		fetchMock.mockResolvedValueOnce(
			respond(403, apiError("FORBIDDEN", "forbidden")),
		);
		await expect(client.batchGet("p1", ["d1"])).rejects.toBeInstanceOf(
			PacaAPIError,
		);
	});

	it("patchDocument sends blocks, the note and If-Match and returns the ETag", async () => {
		fetchMock.mockResolvedValue(respond(200, envelope(DOC), { ETag: '"h-2"' }));
		const res = await client.patchDocument(
			"p1",
			"d1",
			{
				content: [{ type: "paragraph" }],
				note: "fix",
				client: "c",
				task: "PACA-1",
			},
			{ ifMatch: "h1" },
		);
		expect(res).toEqual({ doc: DOC, etag: '"h-2"' });
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe("https://api.example.com/api/v1/projects/p1/docs/d1");
		expect(init.method).toBe("PATCH");
		expect(init.headers["If-Match"]).toBe("h1");
		expect(JSON.parse(init.body)).toEqual({
			content: [{ type: "paragraph" }],
			note: "fix",
			client: "c",
			task: "PACA-1",
		});
	});

	it("patchDocument throws 412 and 409 with their code and details", async () => {
		fetchMock.mockResolvedValueOnce(
			respond(
				412,
				apiError("VERSION_CONFLICT", "version conflict", {
					content_hash: "h9",
					updated_by: "u2",
				}),
			),
		);
		const err = await client
			.patchDocument("p1", "d1", { content: [] }, { ifMatch: "h1" })
			.catch((e) => e);
		expect(err).toBeInstanceOf(PacaAPIError);
		expect(err.status).toBe(412);
		expect(err.details).toEqual({ content_hash: "h9", updated_by: "u2" });

		fetchMock.mockResolvedValueOnce(
			respond(409, apiError("DOC_LOCKED", "document is locked")),
		);
		const locked = await client
			.patchDocument("p1", "d1", { content: [] })
			.catch((e) => e);
		expect(locked.code).toBe("DOC_LOCKED");
		expect(fetchMock.mock.calls[1][1].headers["If-Match"]).toBeUndefined();
	});
});
