import { describe, expect, it, vi } from "vitest";

vi.mock("../../utils/index.js", () => ({
	formatList: vi.fn((items: any[], fn: any) => items.map(fn).join("---")),
	formatFileSize: vi.fn((bytes: number) => `${bytes} bytes`),
}));

import {
	getAttachmentTools,
	handleAttachmentTool,
} from "../../tools/attachment-tools.js";

const attachment = {
	id: "att-1",
	file: {
		file_name: "photo.png",
		file_size: 1024,
		content_type: "image/png",
	},
	created_by: "user-1",
	created_at: "2024-01-01T00:00:00Z",
};

function makeClient(overrides: Record<string, any> = {}) {
	return {
		listTaskAttachments: vi.fn().mockResolvedValue([attachment]),
		getAttachmentDownloadURL: vi
			.fn()
			.mockResolvedValue("https://cdn.example.com/photo.png"),
		downloadAttachmentContent: vi.fn().mockResolvedValue({
			buffer: new TextEncoder().encode("hello world").buffer,
			contentType: "image/png",
		}),
		deleteTaskAttachment: vi.fn().mockResolvedValue(undefined),
		...overrides,
	} as any;
}

// ---------------------------------------------------------------------------
// getAttachmentTools
// ---------------------------------------------------------------------------

describe("getAttachmentTools", () => {
	it("returns 4 tools", () => {
		expect(getAttachmentTools()).toHaveLength(4);
	});

	it("includes list_task_attachments, get_attachment_download_url, read_task_attachment, delete_task_attachment", () => {
		const names = getAttachmentTools().map((t) => t.name);
		expect(names).toContain("list_task_attachments");
		expect(names).toContain("get_attachment_download_url");
		expect(names).toContain("read_task_attachment");
		expect(names).toContain("delete_task_attachment");
	});
});

// ---------------------------------------------------------------------------
// list_task_attachments
// ---------------------------------------------------------------------------

describe("handleAttachmentTool – list_task_attachments", () => {
	it("calls viewsClient.listTaskAttachments with projectId and taskId", async () => {
		const client = makeClient();
		await handleAttachmentTool(
			"list_task_attachments",
			{ projectId: "p1", taskId: "t1" },
			client,
		);
		expect(client.listTaskAttachments).toHaveBeenCalledWith("p1", "t1");
	});

	it("includes 'Attachments:' header in the response", async () => {
		const result = await handleAttachmentTool(
			"list_task_attachments",
			{ projectId: "p1", taskId: "t1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("Attachments:");
	});

	it("includes attachment file name in the formatted output", async () => {
		const result = await handleAttachmentTool(
			"list_task_attachments",
			{ projectId: "p1", taskId: "t1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("photo.png");
	});

	it("throws ZodError when projectId is missing", async () => {
		await expect(
			handleAttachmentTool(
				"list_task_attachments",
				{ taskId: "t1" },
				makeClient(),
			),
		).rejects.toThrow();
	});
});

// ---------------------------------------------------------------------------
// get_attachment_download_url
// ---------------------------------------------------------------------------

describe("handleAttachmentTool – get_attachment_download_url", () => {
	it("calls viewsClient.getAttachmentDownloadURL with all three IDs", async () => {
		const client = makeClient();
		await handleAttachmentTool(
			"get_attachment_download_url",
			{ projectId: "p1", taskId: "t1", attachmentId: "att-1" },
			client,
		);
		expect(client.getAttachmentDownloadURL).toHaveBeenCalledWith(
			"p1",
			"t1",
			"att-1",
		);
	});

	it("returns the download URL in the response text", async () => {
		const result = await handleAttachmentTool(
			"get_attachment_download_url",
			{ projectId: "p1", taskId: "t1", attachmentId: "att-1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain(
			"https://cdn.example.com/photo.png",
		);
	});
});

// ---------------------------------------------------------------------------
// read_task_attachment
// ---------------------------------------------------------------------------

describe("handleAttachmentTool – read_task_attachment", () => {
	it("returns an image content block for an image attachment", async () => {
		const client = makeClient();
		const result = await handleAttachmentTool(
			"read_task_attachment",
			{ projectId: "p1", taskId: "t1", attachmentId: "att-1" },
			client,
		);
		expect(client.downloadAttachmentContent).toHaveBeenCalledWith(
			"p1",
			"t1",
			"att-1",
		);
		expect(result.content[0].type).toBe("image");
		expect(result.content[0].mimeType).toBe("image/png");
		expect(result.content[0].data).toBe(
			Buffer.from("hello world").toString("base64"),
		);
	});

	it("returns a text content block for a text attachment", async () => {
		const textAttachment = {
			id: "att-2",
			file: {
				file_name: "notes.md",
				file_size: 11,
				content_type: "text/markdown",
			},
			created_by: "user-1",
			created_at: "2024-01-01T00:00:00Z",
		};
		const client = makeClient({
			listTaskAttachments: vi.fn().mockResolvedValue([textAttachment]),
			downloadAttachmentContent: vi.fn().mockResolvedValue({
				buffer: new TextEncoder().encode("hello world").buffer,
				contentType: "text/markdown",
			}),
		});
		const result = await handleAttachmentTool(
			"read_task_attachment",
			{ projectId: "p1", taskId: "t1", attachmentId: "att-2" },
			client,
		);
		expect(result.content[0].type).toBe("text");
		expect(result.content[0].text).toContain("notes.md");
		expect(result.content[0].text).toContain("hello world");
	});

	it("classifies a generic content-type by file extension", async () => {
		const codeAttachment = {
			id: "att-3",
			file: {
				file_name: "script.py",
				file_size: 11,
				content_type: "application/octet-stream",
			},
			created_by: "user-1",
			created_at: "2024-01-01T00:00:00Z",
		};
		const client = makeClient({
			listTaskAttachments: vi.fn().mockResolvedValue([codeAttachment]),
			downloadAttachmentContent: vi.fn().mockResolvedValue({
				buffer: new TextEncoder().encode("print('hi')").buffer,
				contentType: "application/octet-stream",
			}),
		});
		const result = await handleAttachmentTool(
			"read_task_attachment",
			{ projectId: "p1", taskId: "t1", attachmentId: "att-3" },
			client,
		);
		expect(result.content[0].type).toBe("text");
		expect(result.content[0].text).toContain("print('hi')");
	});

	it("classifies a generic content-type by conventional extensionless filename", async () => {
		const dockerfileAttachment = {
			id: "att-3b",
			file: {
				file_name: "Dockerfile",
				file_size: 11,
				content_type: "application/octet-stream",
			},
			created_by: "user-1",
			created_at: "2024-01-01T00:00:00Z",
		};
		const client = makeClient({
			listTaskAttachments: vi.fn().mockResolvedValue([dockerfileAttachment]),
			downloadAttachmentContent: vi.fn().mockResolvedValue({
				buffer: new TextEncoder().encode("FROM node:20").buffer,
				contentType: "application/octet-stream",
			}),
		});
		const result = await handleAttachmentTool(
			"read_task_attachment",
			{ projectId: "p1", taskId: "t1", attachmentId: "att-3b" },
			client,
		);
		expect(result.content[0].type).toBe("text");
		expect(result.content[0].text).toContain("FROM node:20");
	});

	it("returns an error for a binary attachment it can't read", async () => {
		const pdfAttachment = {
			id: "att-4",
			file: {
				file_name: "report.pdf",
				file_size: 1024,
				content_type: "application/pdf",
			},
			created_by: "user-1",
			created_at: "2024-01-01T00:00:00Z",
		};
		const client = makeClient({
			listTaskAttachments: vi.fn().mockResolvedValue([pdfAttachment]),
		});
		const result = await handleAttachmentTool(
			"read_task_attachment",
			{ projectId: "p1", taskId: "t1", attachmentId: "att-4" },
			client,
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("get_attachment_download_url");
		expect(client.downloadAttachmentContent).not.toHaveBeenCalled();
	});

	it("returns an error when the attachment exceeds the size limit", async () => {
		const bigAttachment = {
			id: "att-5",
			file: {
				file_name: "huge.txt",
				file_size: 3 * 1024 * 1024,
				content_type: "text/plain",
			},
			created_by: "user-1",
			created_at: "2024-01-01T00:00:00Z",
		};
		const client = makeClient({
			listTaskAttachments: vi.fn().mockResolvedValue([bigAttachment]),
		});
		const result = await handleAttachmentTool(
			"read_task_attachment",
			{ projectId: "p1", taskId: "t1", attachmentId: "att-5" },
			client,
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("exceeds");
		expect(client.downloadAttachmentContent).not.toHaveBeenCalled();
	});

	it("returns an error when the attachment isn't found", async () => {
		const client = makeClient();
		const result = await handleAttachmentTool(
			"read_task_attachment",
			{ projectId: "p1", taskId: "t1", attachmentId: "does-not-exist" },
			client,
		);
		expect(result.isError).toBe(true);
		expect(result.content[0].text).toContain("not found");
	});
});

// ---------------------------------------------------------------------------
// delete_task_attachment
// ---------------------------------------------------------------------------

describe("handleAttachmentTool – delete_task_attachment", () => {
	it("calls viewsClient.deleteTaskAttachment with all three IDs", async () => {
		const client = makeClient();
		await handleAttachmentTool(
			"delete_task_attachment",
			{ projectId: "p1", taskId: "t1", attachmentId: "att-1" },
			client,
		);
		expect(client.deleteTaskAttachment).toHaveBeenCalledWith(
			"p1",
			"t1",
			"att-1",
		);
	});

	it("includes 'deleted successfully' in the response", async () => {
		const result = await handleAttachmentTool(
			"delete_task_attachment",
			{ projectId: "p1", taskId: "t1", attachmentId: "att-1" },
			makeClient(),
		);
		expect(result.content[0].text).toContain("deleted successfully");
		expect(result.content[0].text).toContain("att-1");
	});
});

// ---------------------------------------------------------------------------
// unknown tool
// ---------------------------------------------------------------------------

describe("handleAttachmentTool – unknown tool", () => {
	it("throws for an unknown tool name", async () => {
		await expect(
			handleAttachmentTool("bad_tool", {}, makeClient()),
		).rejects.toThrow("Unknown attachment tool");
	});
});
