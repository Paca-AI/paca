import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PacaAPIDocClient, PacaAPIError } from "../../api/doc-client.js";

const CONFIG = { baseURL: "https://api.example.com", apiKey: "key123" };

function respond(status: number, body: unknown) {
	const text = typeof body === "string" ? body : JSON.stringify(body);
	return {
		ok: status >= 200 && status < 300,
		status,
		statusText: String(status),
		headers: new Headers(),
		text: async () => text,
		json: async () => JSON.parse(text),
	};
}

const envelope = (data: unknown) => ({ success: true, data });
const apiError = (code: string, error: string) => ({
	success: false,
	error_code: code,
	error,
});

describe("PacaAPIDocClient change feed and batch read", () => {
	let fetchMock: ReturnType<typeof vi.fn>;
	let client: PacaAPIDocClient;

	beforeEach(() => {
		fetchMock = vi.fn();
		vi.stubGlobal("fetch", fetchMock);
		client = new PacaAPIDocClient(CONFIG);
	});
	afterEach(() => vi.unstubAllGlobals());

	it("getChanges sends the cursor and returns the feed", async () => {
		const feed = {
			cursor: "c2",
			reset: false,
			docs: [
				{
					id: "d1",
					folder_id: null,
					title: "A",
					content_hash: "h1",
					updated_at: "t",
					deleted: false,
				},
			],
			folders: null,
		};
		fetchMock.mockResolvedValue(respond(200, envelope(feed)));
		const res = await client.getChanges("p1", "c 1");
		expect(res).toEqual({ status: "ok", ...feed });
		expect(fetchMock.mock.calls[0][0]).toBe(
			"https://api.example.com/api/v1/projects/p1/docs/changes?since=c%201",
		);
		expect(fetchMock.mock.calls[0][1].method).toBe("GET");
	});

	it("getChanges without a cursor sends no since", async () => {
		fetchMock.mockResolvedValue(
			respond(
				200,
				envelope({ cursor: "c", reset: true, docs: [], folders: [] }),
			),
		);
		const res = await client.getChanges("p1");
		expect(fetchMock.mock.calls[0][0]).toBe(
			"https://api.example.com/api/v1/projects/p1/docs/changes",
		);
		expect(res).toEqual({
			status: "ok",
			cursor: "c",
			reset: true,
			docs: [],
			folders: [],
		});
	});

	it("getChanges reports an older server as unsupported", async () => {
		// An old router reads "changes" as a document id.
		fetchMock.mockResolvedValueOnce(
			respond(400, apiError("BAD_REQUEST", "invalid document id")),
		);
		expect(await client.getChanges("p1")).toEqual({ status: "unsupported" });
		fetchMock.mockResolvedValueOnce(respond(404, "404 page not found"));
		expect(await client.getChanges("p1")).toEqual({ status: "unsupported" });
	});

	it("getChanges throws other errors", async () => {
		fetchMock.mockResolvedValueOnce(
			respond(403, apiError("FORBIDDEN", "permission denied")),
		);
		const err = await client.getChanges("p1").catch((e) => e);
		expect(err).toBeInstanceOf(PacaAPIError);
		expect(err.status).toBe(403);
	});

	it("batchGet posts the ids and returns the items", async () => {
		const items = [{ id: "d1", title: "A", content: [] }];
		fetchMock.mockResolvedValue(respond(200, envelope({ items })));
		const res = await client.batchGet("p1", ["d1", "d2"]);
		expect(res).toEqual({ status: "ok", items });
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe(
			"https://api.example.com/api/v1/projects/p1/docs/batch-get",
		);
		expect(init.method).toBe("POST");
		expect(JSON.parse(init.body)).toEqual({ ids: ["d1", "d2"] });
	});

	it("batchGet reports an older server as unsupported", async () => {
		fetchMock.mockResolvedValueOnce(respond(405, ""));
		expect(await client.batchGet("p1", ["d1"])).toEqual({
			status: "unsupported",
		});
	});
});
