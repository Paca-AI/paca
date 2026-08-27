import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { PacaAPIViewsClient } from "../../api/views-client.js";

const CONFIG = { baseURL: "https://api.example.com", apiKey: "key123" };
const CONFIG_WITH_AGENT = { ...CONFIG, agentId: "agent-1" };

function okEnvelope(data: any) {
	return {
		ok: true,
		json: async () => ({ success: true, data }),
		text: async () => "",
	};
}

function rawOk(data: any) {
	return { ok: true, json: async () => data, text: async () => "" };
}

function errorResponse(status = 400, body = "Bad Request") {
	return {
		ok: false,
		status,
		statusText: body,
		text: async () => body,
		json: async () => ({}),
	};
}

function noContentResponse() {
	return {
		ok: true,
		status: 204,
		text: async () => "",
		json: async () => {
			throw new SyntaxError("Unexpected end of JSON input");
		},
	};
}

describe("PacaAPIViewsClient", () => {
	let fetchMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		fetchMock = vi.fn().mockResolvedValue(okEnvelope([]));
		vi.stubGlobal("fetch", fetchMock);
	});

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	// ---------------------------------------------------------------------------
	// Headers / error handling
	// ---------------------------------------------------------------------------

	it("includes X-API-Key header", async () => {
		const client = new PacaAPIViewsClient(CONFIG);
		await client.listViews("p1");
		expect(fetchMock.mock.calls[0][1].headers["X-API-Key"]).toBe("key123");
	});

	it("includes X-Agent-ID when agentId set", async () => {
		const client = new PacaAPIViewsClient(CONFIG_WITH_AGENT);
		await client.listViews("p1");
		expect(fetchMock.mock.calls[0][1].headers["X-Agent-ID"]).toBe("agent-1");
	});

	it("omits X-Agent-ID when agentId absent", async () => {
		const client = new PacaAPIViewsClient(CONFIG);
		await client.listViews("p1");
		expect(fetchMock.mock.calls[0][1].headers["X-Agent-ID"]).toBeUndefined();
	});

	it("throws on non-OK response", async () => {
		fetchMock.mockResolvedValue(errorResponse(404, "Not Found"));
		const client = new PacaAPIViewsClient(CONFIG);
		await expect(client.listViews("p1")).rejects.toThrow("404");
	});

	it("resolves on 204 No Content without parsing JSON", async () => {
		fetchMock.mockResolvedValue(noContentResponse());
		const client = new PacaAPIViewsClient(CONFIG);
		await expect(
			client.deleteTaskAttachment("p1", "t1", "att1"),
		).resolves.toBeUndefined();
	});

	it("returns raw JSON when not a SuccessEnvelope", async () => {
		fetchMock.mockResolvedValue(rawOk([{ id: "v1" }]));
		const client = new PacaAPIViewsClient(CONFIG);
		const result = await client.listViews("p1");
		expect(result).toEqual([{ id: "v1" }]);
	});

	// ---------------------------------------------------------------------------
	// Views
	// ---------------------------------------------------------------------------

	describe("listViews", () => {
		it("calls GET /api/v1/projects/:id/views with no params when none provided", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "v1" }]));
			await client.listViews("p1");
			expect(fetchMock.mock.calls[0][0]).toBe(
				"https://api.example.com/api/v1/projects/p1/views",
			);
		});

		it("appends context query param when provided", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([]));
			await client.listViews("p1", "sprint");
			expect(fetchMock.mock.calls[0][0]).toContain("context=sprint");
		});

		it("appends sprint_id query param when provided", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([]));
			await client.listViews("p1", "sprint", "s1");
			expect(fetchMock.mock.calls[0][0]).toContain("sprint_id=s1");
		});

		it("extracts .items when response is an object", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ items: [{ id: "v1" }] }));
			const result = await client.listViews("p1");
			expect(result).toEqual([{ id: "v1" }]);
		});
	});

	describe("createView", () => {
		it("calls POST /api/v1/projects/:id/views", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "v2" }));
			await client.createView("p1", {
				name: "Board",
				context: "sprint",
				view_type: "board",
				sprint_id: null,
			});
			expect(fetchMock.mock.calls[0][1].method).toBe("POST");
		});
	});

	describe("reorderViews", () => {
		it("calls PUT /api/v1/projects/:id/views/positions", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.reorderViews("p1", { view_ids: ["v2", "v1"] });
			expect(fetchMock.mock.calls[0][0]).toContain("/views/positions");
			expect(fetchMock.mock.calls[0][1].method).toBe("PUT");
		});
	});

	describe("getView", () => {
		it("calls GET /api/v1/projects/:id/views/:viewId", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "v1" }));
			const result = await client.getView("p1", "v1");
			expect(fetchMock.mock.calls[0][0]).toContain("/views/v1");
			expect(result).toEqual({ id: "v1" });
		});
	});

	describe("updateView", () => {
		it("calls PATCH /api/v1/projects/:id/views/:viewId", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "v1", name: "Updated" }));
			await client.updateView("p1", "v1", { name: "Updated" });
			expect(fetchMock.mock.calls[0][0]).toContain("/views/v1");
			expect(fetchMock.mock.calls[0][1].method).toBe("PATCH");
		});
	});

	describe("deleteView", () => {
		it("calls DELETE /api/v1/projects/:id/views/:viewId", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.deleteView("p1", "v1");
			expect(fetchMock.mock.calls[0][0]).toContain("/views/v1");
			expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
		});
	});

	// ---------------------------------------------------------------------------
	// Task Positions
	// ---------------------------------------------------------------------------

	describe("listTaskPositions", () => {
		it("calls GET /api/v1/projects/:id/views/:viewId/task-positions", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ task_id: "t1", position: 0 }]));
			const result = await client.listTaskPositions("p1", "v1");
			expect(fetchMock.mock.calls[0][0]).toContain("/views/v1/task-positions");
			expect(result).toEqual([{ task_id: "t1", position: 0 }]);
		});

		it("extracts .items from object response", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(
				okEnvelope({ items: [{ task_id: "t2", position: 1 }] }),
			);
			const result = await client.listTaskPositions("p1", "v1");
			expect(result).toEqual([{ task_id: "t2", position: 1 }]);
		});
	});

	describe("bulkMoveViewTaskPositions", () => {
		it("calls PUT /api/v1/projects/:id/views/:viewId/task-positions with items", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.bulkMoveViewTaskPositions("p1", "v1", [
				{ task_id: "t1", position: 2 },
			]);
			expect(fetchMock.mock.calls[0][0]).toContain("/views/v1/task-positions");
			expect(fetchMock.mock.calls[0][1].method).toBe("PUT");
			const body = JSON.parse(fetchMock.mock.calls[0][1].body);
			expect(body.items).toEqual([{ task_id: "t1", position: 2 }]);
		});
	});

	describe("bulkMoveTasks", () => {
		it("calls PUT /api/v1/projects/:id/views/:viewId/task-positions/:taskId", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.bulkMoveTasks("p1", "v1", {
				task_id: "t1",
				target_view_id: "v2",
				target_status_id: null,
				target_position: 0,
			});
			expect(fetchMock.mock.calls[0][0]).toContain("/task-positions/t1");
			expect(fetchMock.mock.calls[0][1].method).toBe("PUT");
		});
	});

	// ---------------------------------------------------------------------------
	// Custom Fields
	// ---------------------------------------------------------------------------

	describe("listCustomFieldDefinitions", () => {
		it("calls GET /api/v1/projects/:id/custom-fields", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "cf1" }]));
			const result = await client.listCustomFieldDefinitions("p1");
			expect(fetchMock.mock.calls[0][0]).toContain("/custom-fields");
			expect(result).toEqual([{ id: "cf1" }]);
		});

		it("extracts .fields from object response", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ fields: [{ id: "cf1" }] }));
			const result = await client.listCustomFieldDefinitions("p1");
			expect(result).toEqual([{ id: "cf1" }]);
		});
	});

	describe("getCustomFieldDefinition", () => {
		it("calls GET /api/v1/projects/:id/custom-fields/:fieldId", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "cf1" }));
			const result = await client.getCustomFieldDefinition("p1", "cf1");
			expect(fetchMock.mock.calls[0][0]).toContain("/custom-fields/cf1");
			expect(result).toEqual({ id: "cf1" });
		});
	});

	describe("createCustomFieldDefinition", () => {
		it("calls POST /api/v1/projects/:id/custom-fields", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "cf2" }));
			await client.createCustomFieldDefinition("p1", {
				field_key: "status",
				display_name: "Status",
				field_type: "text",
			});
			expect(fetchMock.mock.calls[0][1].method).toBe("POST");
		});
	});

	describe("updateCustomFieldDefinition", () => {
		it("calls PATCH /api/v1/projects/:id/custom-fields/:fieldId", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({ id: "cf1" }));
			await client.updateCustomFieldDefinition("p1", "cf1", {
				display_name: "Status V2",
			});
			expect(fetchMock.mock.calls[0][0]).toContain("/custom-fields/cf1");
			expect(fetchMock.mock.calls[0][1].method).toBe("PATCH");
		});
	});

	describe("deleteCustomFieldDefinition", () => {
		it("calls DELETE /api/v1/projects/:id/custom-fields/:fieldId", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.deleteCustomFieldDefinition("p1", "cf1");
			expect(fetchMock.mock.calls[0][0]).toContain("/custom-fields/cf1");
			expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
		});
	});

	// ---------------------------------------------------------------------------
	// Attachments
	// ---------------------------------------------------------------------------

	describe("listTaskAttachments", () => {
		it("calls GET /api/v1/projects/:id/tasks/:taskId/attachments", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope([{ id: "att1" }]));
			const result = await client.listTaskAttachments("p1", "t1");
			expect(fetchMock.mock.calls[0][0]).toContain("/tasks/t1/attachments");
			expect(result).toEqual([{ id: "att1" }]);
		});

		it("extracts .attachments from object response", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(
				okEnvelope({ attachments: [{ id: "att1" }] }),
			);
			const result = await client.listTaskAttachments("p1", "t1");
			expect(result).toEqual([{ id: "att1" }]);
		});
	});

	describe("getAttachmentDownloadURL", () => {
		it("returns .url from response", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(
				okEnvelope({ url: "https://cdn.example.com/file.pdf" }),
			);
			const result = await client.getAttachmentDownloadURL("p1", "t1", "att1");
			expect(result).toBe("https://cdn.example.com/file.pdf");
		});

		it("returns .downloadUrl as fallback", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(
				okEnvelope({ downloadUrl: "https://cdn.example.com/file2.pdf" }),
			);
			const result = await client.getAttachmentDownloadURL("p1", "t1", "att1");
			expect(result).toBe("https://cdn.example.com/file2.pdf");
		});

		it("returns empty string when no url fields present", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope({}));
			const result = await client.getAttachmentDownloadURL("p1", "t1", "att1");
			expect(result).toBe("");
		});
	});

	describe("deleteTaskAttachment", () => {
		it("calls DELETE /api/v1/projects/:id/tasks/:taskId/attachments/:attId", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue(okEnvelope(null));
			await client.deleteTaskAttachment("p1", "t1", "att1");
			expect(fetchMock.mock.calls[0][0]).toContain("/attachments/att1");
			expect(fetchMock.mock.calls[0][1].method).toBe("DELETE");
		});
	});

	describe("downloadAttachmentContent", () => {
		it("fetches the content endpoint on baseURL (not a presigned URL) and returns bytes + content type", async () => {
			const client = new PacaAPIViewsClient(CONFIG_WITH_AGENT);
			fetchMock.mockResolvedValue({
				ok: true,
				headers: new Map([["content-type", "text/plain; charset=utf-8"]]),
				arrayBuffer: async () => new TextEncoder().encode("hello").buffer,
				text: async () => "",
			});

			const result = await client.downloadAttachmentContent("p1", "t1", "att1");

			expect(fetchMock).toHaveBeenCalledTimes(1);
			const [url, options] = fetchMock.mock.calls[0];
			expect(url).toBe(
				"https://api.example.com/api/v1/projects/p1/tasks/t1/attachments/att1/content",
			);
			expect(options.headers["X-API-Key"]).toBe("key123");
			expect(options.headers["X-Agent-ID"]).toBe("agent-1");
			expect(new TextDecoder().decode(result.buffer)).toBe("hello");
			expect(result.contentType).toBe("text/plain; charset=utf-8");
		});

		it("throws when the content fetch fails", async () => {
			const client = new PacaAPIViewsClient(CONFIG);
			fetchMock.mockResolvedValue({
				ok: false,
				status: 413,
				statusText: "Request Entity Too Large",
				text: async () => "too large",
			});

			await expect(
				client.downloadAttachmentContent("p1", "t1", "att1"),
			).rejects.toThrow("413");
		});
	});

	describe("uploadTaskAttachment", () => {
		// A presigned-PUT response carrying (or omitting) an ETag header.
		function putOk(etag: string | null) {
			return {
				ok: true,
				status: 200,
				headers: { get: (k: string) => (k.toLowerCase() === "etag" ? etag : null) },
				text: async () => "",
				json: async () => ({}),
			};
		}

		it("single-part: PUTs the bytes, then completes with only the file_id", async () => {
			const attachment = { id: "att1", file: { file_name: "a.txt" } };
			const puts: string[] = [];
			let completeBody: any;
			fetchMock.mockImplementation((url: string, init: any) => {
				if (url.endsWith("/initiate-upload")) {
					return Promise.resolve(
						okEnvelope({
							file_id: "f1",
							is_multipart: false,
							upload_url: "https://store/put1",
						}),
					);
				}
				if (init?.method === "PUT") {
					puts.push(url);
					return Promise.resolve(putOk('"etag1"'));
				}
				if (url.endsWith("/complete-upload")) {
					completeBody = JSON.parse(init.body);
					return Promise.resolve(okEnvelope(attachment));
				}
				throw new Error(`unexpected fetch: ${url}`);
			});

			const client = new PacaAPIViewsClient(CONFIG);
			const result = await client.uploadTaskAttachment("p1", "t1", {
				fileName: "a.txt",
				contentType: "text/plain",
				data: new Uint8Array([1, 2, 3]),
			});

			expect(puts).toEqual(["https://store/put1"]);
			expect(completeBody).toEqual({ file_id: "f1" });
			expect(result).toEqual(attachment);
		});

		it("multipart: splits into 5 MiB parts, collects ETags, completes with upload_id + parts", async () => {
			const attachment = { id: "att2" };
			const puts: Array<{ url: string; size: number }> = [];
			let completeBody: any;
			fetchMock.mockImplementation((url: string, init: any) => {
				if (url.endsWith("/initiate-upload")) {
					return Promise.resolve(
						okEnvelope({
							file_id: "f2",
							is_multipart: true,
							multipart: {
								upload_id: "u9",
								parts: [
									{ part_number: 1, upload_url: "https://store/p1" },
									{ part_number: 2, upload_url: "https://store/p2" },
								],
							},
						}),
					);
				}
				if (init?.method === "PUT") {
					puts.push({ url, size: (init.body as Uint8Array).byteLength });
					return Promise.resolve(putOk(url.endsWith("/p1") ? '"e1"' : '"e2"'));
				}
				if (url.endsWith("/complete-upload")) {
					completeBody = JSON.parse(init.body);
					return Promise.resolve(okEnvelope(attachment));
				}
				throw new Error(`unexpected fetch: ${url}`);
			});

			const client = new PacaAPIViewsClient(CONFIG);
			const FIVE_MB = 5 * 1024 * 1024;
			const data = new Uint8Array(FIVE_MB + 100); // → 5 MiB + a 100-byte tail
			const result = await client.uploadTaskAttachment("p1", "t1", {
				fileName: "big.bin",
				contentType: "application/octet-stream",
				data,
			});

			expect(puts.map((p) => p.url)).toEqual([
				"https://store/p1",
				"https://store/p2",
			]);
			// Byte slicing: first part is exactly one PART_SIZE, second is the tail.
			expect(puts.map((p) => p.size)).toEqual([FIVE_MB, 100]);
			expect(completeBody).toEqual({
				file_id: "f2",
				upload_id: "u9",
				parts: [
					{ part_number: 1, etag: '"e1"' },
					{ part_number: 2, etag: '"e2"' },
				],
			});
			expect(result).toEqual(attachment);
		});

		it("fails fast (and never completes) when a part upload returns no ETag", async () => {
			let completed = false;
			fetchMock.mockImplementation((url: string, init: any) => {
				if (url.endsWith("/initiate-upload")) {
					return Promise.resolve(
						okEnvelope({
							file_id: "f3",
							is_multipart: true,
							multipart: {
								upload_id: "u",
								parts: [{ part_number: 1, upload_url: "https://store/x" }],
							},
						}),
					);
				}
				if (init?.method === "PUT") return Promise.resolve(putOk(null));
				if (url.endsWith("/complete-upload")) {
					completed = true;
					return Promise.resolve(okEnvelope({}));
				}
				throw new Error(`unexpected fetch: ${url}`);
			});

			const client = new PacaAPIViewsClient(CONFIG);
			await expect(
				client.uploadTaskAttachment("p1", "t1", {
					fileName: "x.bin",
					contentType: "application/octet-stream",
					data: new Uint8Array(6 * 1024 * 1024),
				}),
			).rejects.toThrow(/no ETag/);
			expect(completed).toBe(false);
		});
	});
});
