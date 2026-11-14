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

describe("PacaAPIDocClient by-path", () => {
	let fetchMock: ReturnType<typeof vi.fn>;
	let client: PacaAPIDocClient;

	beforeEach(() => {
		fetchMock = vi.fn();
		vi.stubGlobal("fetch", fetchMock);
		client = new PacaAPIDocClient(CONFIG);
	});
	afterEach(() => vi.unstubAllGlobals());

	it("getByPath reads a document and its ETag in one request", async () => {
		fetchMock.mockResolvedValue(
			respond(200, envelope(DOC), { ETag: '"abc-1"' }),
		);
		const res = await client.getByPath("p1", "Plans/My plan");
		expect(res).toEqual({ status: "found", doc: DOC, etag: '"abc-1"' });
		expect(fetchMock).toHaveBeenCalledTimes(1);
		expect(fetchMock.mock.calls[0][0]).toBe(
			"https://api.example.com/api/v1/projects/p1/docs/by-path?path=Plans%2FMy%20plan",
		);
	});

	it("getByPath tells a missing doc, a folder and an older server apart", async () => {
		fetchMock.mockResolvedValueOnce(
			respond(404, apiError("DOC_NOT_FOUND", "document: not found")),
		);
		expect(await client.getByPath("p1", "x")).toEqual({ status: "not_found" });
		fetchMock.mockResolvedValueOnce(
			respond(
				409,
				apiError("DOC_PATH_IS_FOLDER", "document: path names a folder"),
			),
		);
		expect(await client.getByPath("p1", "x")).toEqual({ status: "folder" });
		// What a server without the route answers: "by-path" is taken for a doc id.
		fetchMock.mockResolvedValueOnce(
			respond(400, apiError("BAD_REQUEST", "invalid document id")),
		);
		expect(await client.getByPath("p1", "x")).toEqual({
			status: "unsupported",
		});
		fetchMock.mockResolvedValueOnce(respond(404, "404 page not found"));
		expect(await client.getByPath("p1", "x")).toEqual({
			status: "unsupported",
		});
	});

	it("getByPath throws other errors with their code", async () => {
		fetchMock.mockResolvedValueOnce(
			respond(
				400,
				apiError("DOC_PATH_INVALID", "document: path is empty or invalid"),
			),
		);
		const err = await client.getByPath("p1", "/").catch((e) => e);
		expect(err).toBeInstanceOf(PacaAPIError);
		expect(err.status).toBe(400);
		expect(err.code).toBe("DOC_PATH_INVALID");
	});

	it("putByPath sends the body and If-Match and reads created/changed", async () => {
		fetchMock.mockResolvedValue(
			respond(201, envelope({ ...DOC, changed: true }), { ETag: '"h-2"' }),
		);
		const res = await client.putByPath(
			"p1",
			{
				path: "Plans/Doc",
				content: [{ type: "paragraph" }],
				source: { repository: "paca", path: "x.md" },
				lock: true,
			},
			{ ifMatch: '"h-1"' },
		);
		expect(res).toMatchObject({
			status: "written",
			created: true,
			changed: true,
			etag: '"h-2"',
		});
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe("https://api.example.com/api/v1/projects/p1/docs/by-path");
		expect(init.method).toBe("PUT");
		expect(init.headers["If-Match"]).toBe('"h-1"');
		expect(JSON.parse(init.body)).toEqual({
			path: "Plans/Doc",
			content: [{ type: "paragraph" }],
			source: { repository: "paca", path: "x.md" },
			lock: true,
		});
	});

	it("putByPath reports an older server and throws 412 with its details", async () => {
		fetchMock.mockResolvedValueOnce(respond(405, ""));
		expect(await client.putByPath("p1", { path: "x", content: [] })).toEqual({
			status: "unsupported",
		});

		fetchMock.mockResolvedValueOnce(
			respond(
				412,
				apiError("VERSION_CONFLICT", "document: version conflict", {
					content_hash: "h9",
					note: "fixed typo",
				}),
			),
		);
		const err = await client
			.putByPath("p1", { path: "x", content: [] })
			.catch((e) => e);
		expect(err).toBeInstanceOf(PacaAPIError);
		expect(err.status).toBe(412);
		expect(err.details).toEqual({ content_hash: "h9", note: "fixed typo" });
		expect(err.message).toContain("version conflict");
	});
});
