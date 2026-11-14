import type {
	CreateFolderInput,
	DocSource,
	Document,
	DocumentActivity,
	DocumentFolder,
	DocumentSearchHit,
	DocumentSnapshot,
	PacaConfig,
	SuccessEnvelope,
	UpdateFolderInput,
} from "../types/index.js";
import { formatApiRequestError, markdownToBlocknote } from "../utils/index.js";
import { type Page, type PageOptions, toPage, withPaging } from "./paginate.js";

/** An error answer from the Paca API, with its status, code and details. */
export class PacaAPIError extends Error {
	constructor(
		message: string,
		readonly status: number,
		readonly code?: string,
		readonly details?: Record<string, string>,
	) {
		super(message);
		this.name = "PacaAPIError";
	}
}

/** A write by path: the document at `path` is created or updated. */
export interface PutByPathInput {
	/** "Folder/Sub folder/Title"; missing folders are created. */
	path: string;
	/** BlockNote blocks. */
	content: unknown[];
	/** The repository file this document is a managed copy of. */
	source?: DocSource;
	/** Lock the managed copy (needs `source`). */
	lock?: boolean;
}

/** What GET /docs/by-path answered. `unsupported`: the server is older and
 *  has no by-path routes, so the caller walks the folders instead. */
export type GetByPathResult =
	| { status: "found"; doc: Document; etag?: string }
	| { status: "not_found" }
	| { status: "folder" }
	| { status: "unsupported" };

/** What PUT /docs/by-path answered (`unsupported` as for GetByPathResult). */
export type PutByPathResult =
	| {
			status: "written";
			doc: Document;
			created: boolean;
			/** false when the content, title, folder and position are as they were. */
			changed: boolean;
			etag?: string;
	  }
	| { status: "unsupported" };

/**
 * Reports whether an error answer means the server has no by-path route (a
 * Paca from before it). Such a server reads GET /docs/by-path as a document
 * whose id is "by-path" (400 "invalid doc id") and has no PUT on it (405). A
 * 404 without an API error code is a route the server (or a proxy) does not
 * know. A 404 DOC_NOT_FOUND is a real answer from a newer server.
 */
function isMissingByPathRoute(err: PacaAPIError): boolean {
	if (err.status === 405) return true;
	if (err.status === 404) return !err.code;
	return (
		err.status === 400 &&
		err.code === "BAD_REQUEST" &&
		/invalid doc id/i.test(err.message)
	);
}

/**
 * Extended API client for Document features.
 */
export class PacaAPIDocClient {
	private config: PacaConfig;

	constructor(config: PacaConfig) {
		this.config = config;
	}

	private async request(
		method: string,
		path: string,
		body?: any,
	): Promise<any> {
		const url = `${this.config.baseURL}${path}`;
		const headers: Record<string, string> = {
			"Content-Type": "application/json",
			"X-API-Key": this.config.apiKey,
		};

		const options: RequestInit = {
			method,
			headers,
		};

		if (body) {
			options.body = JSON.stringify(body);
		}

		const response = await fetch(url, options);

		if (!response.ok) {
			const errorText = await response.text();
			throw new Error(
				formatApiRequestError(response.status, response.statusText, errorText),
			);
		}

		if (response.status === 204) {
			return undefined;
		}

		const jsonResponse = await response.json();

		// Handle SuccessEnvelope wrapper
		if (
			jsonResponse &&
			typeof jsonResponse === "object" &&
			"success" in jsonResponse
		) {
			const envelope = jsonResponse as SuccessEnvelope<any>;
			if (envelope.success) {
				return envelope.data;
			}
		}

		// Fallback for responses not wrapped in SuccessEnvelope
		return jsonResponse;
	}

	/** Sends a request and returns its status, headers and unwrapped data; an
	 *  error answer is thrown as a PacaAPIError. */
	private async send(
		method: string,
		path: string,
		body?: unknown,
		extraHeaders: Record<string, string> = {},
	): Promise<{ status: number; headers: Headers | undefined; data: any }> {
		const headers: Record<string, string> = {
			"Content-Type": "application/json",
			"X-API-Key": this.config.apiKey,
			...extraHeaders,
		};
		const response = await fetch(`${this.config.baseURL}${path}`, {
			method,
			headers,
			body: body === undefined ? undefined : JSON.stringify(body),
		});
		if (!response.ok) {
			const text = await response.text();
			let code: string | undefined;
			let details: Record<string, string> | undefined;
			try {
				const parsed = JSON.parse(text);
				if (parsed && typeof parsed === "object") {
					if (typeof parsed.error_code === "string") code = parsed.error_code;
					if (parsed.error_details && typeof parsed.error_details === "object")
						details = parsed.error_details;
				}
			} catch {
				// Not the JSON error envelope.
			}
			throw new PacaAPIError(
				formatApiRequestError(response.status, response.statusText, text),
				response.status,
				code,
				details,
			);
		}
		const text = await response.text();
		const json = text ? JSON.parse(text) : undefined;
		const data =
			json && typeof json === "object" && "success" in json ? json.data : json;
		return { status: response.status, headers: response.headers, data };
	}

	private async get(path: string): Promise<any> {
		return this.request("GET", path);
	}

	private async post(path: string, body: any): Promise<any> {
		return this.request("POST", path, body);
	}

	private async patch(path: string, body: any): Promise<any> {
		return this.request("PATCH", path, body);
	}

	private async delete(path: string): Promise<any> {
		return this.request("DELETE", path);
	}

	// ==================== Documents by path ====================

	/** Reads the document at `path` ("Folder/Title") in one request. */
	async getByPath(projectId: string, path: string): Promise<GetByPathResult> {
		try {
			const res = await this.send(
				"GET",
				`/api/v1/projects/${projectId}/docs/by-path?path=${encodeURIComponent(path)}`,
			);
			return {
				status: "found",
				doc: res.data,
				etag: res.headers?.get("ETag") ?? undefined,
			};
		} catch (err) {
			if (!(err instanceof PacaAPIError)) throw err;
			if (isMissingByPathRoute(err)) return { status: "unsupported" };
			if (err.code === "DOC_NOT_FOUND") return { status: "not_found" };
			if (err.code === "DOC_PATH_IS_FOLDER") return { status: "folder" };
			throw err;
		}
	}

	/** Creates or updates the document at `input.path` (and any missing
	 *  folder) in one request. `ifMatch` is a content hash or ETag the
	 *  stored content must still have; a mismatch throws a PacaAPIError with
	 *  status 412 and the current version in `details`. */
	async putByPath(
		projectId: string,
		input: PutByPathInput,
		opts: { ifMatch?: string } = {},
	): Promise<PutByPathResult> {
		const headers: Record<string, string> = {};
		if (opts.ifMatch) headers["If-Match"] = opts.ifMatch;
		try {
			const res = await this.send(
				"PUT",
				`/api/v1/projects/${projectId}/docs/by-path`,
				input,
				headers,
			);
			return {
				status: "written",
				doc: res.data,
				created: res.status === 201,
				changed: res.data?.changed !== false,
				etag: res.headers?.get("ETag") ?? undefined,
			};
		} catch (err) {
			if (err instanceof PacaAPIError && isMissingByPathRoute(err))
				return { status: "unsupported" };
			throw err;
		}
	}

	// ==================== Document Folders ====================

	/** One page of the folders directly inside `parentId` (the top level when
	 *  omitted). `name` narrows to an exact, case-insensitive folder name. */
	async listFolders(
		projectId: string,
		opts: PageOptions & { parentId?: string | null; name?: string } = {},
	): Promise<Page<DocumentFolder>> {
		const filters = new URLSearchParams();
		filters.set("parent_id", opts.parentId ?? "null");
		if (opts.name) filters.set("name", opts.name);
		return toPage<DocumentFolder>(
			await this.get(
				withPaging(
					`/api/v1/projects/${projectId}/docs/folders?${filters.toString()}`,
					opts,
				),
			),
		);
	}

	/** The folders from the top level down to (and including) `folderId`. */
	async getFolderPath(
		projectId: string,
		folderId: string,
	): Promise<DocumentFolder[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/docs/folders/${folderId}/path`,
		);
		return Array.isArray(response) ? response : (response?.items ?? []);
	}

	async createFolder(
		projectId: string,
		input: CreateFolderInput,
	): Promise<DocumentFolder> {
		return this.post(`/api/v1/projects/${projectId}/docs/folders`, input);
	}

	async updateFolder(
		projectId: string,
		folderId: string,
		input: UpdateFolderInput,
	): Promise<DocumentFolder> {
		return this.patch(
			`/api/v1/projects/${projectId}/docs/folders/${folderId}`,
			input,
		);
	}

	async deleteFolder(projectId: string, folderId: string): Promise<void> {
		await this.delete(`/api/v1/projects/${projectId}/docs/folders/${folderId}`);
	}

	// ==================== Document Snapshots ====================

	async listSnapshots(
		projectId: string,
		docId: string,
		opts: PageOptions = {},
	): Promise<Page<DocumentSnapshot>> {
		// One page, newest first; list items carry no body (getSnapshot returns it).
		return toPage<DocumentSnapshot>(
			await this.get(
				withPaging(
					`/api/v1/projects/${projectId}/docs/${docId}/snapshots`,
					opts,
				),
			),
		);
	}

	async getSnapshot(
		projectId: string,
		docId: string,
		snapshotId: string,
	): Promise<DocumentSnapshot> {
		return this.get(
			`/api/v1/projects/${projectId}/docs/${docId}/snapshots/${snapshotId}`,
		);
	}

	// ==================== Document Activities ====================

	async listDocumentActivities(
		projectId: string,
		docId: string,
		opts: PageOptions = {},
	): Promise<Page<DocumentActivity>> {
		// One page, newest first; next pages reach further back in time.
		return toPage<DocumentActivity>(
			await this.get(
				withPaging(
					`/api/v1/projects/${projectId}/docs/${docId}/activities`,
					opts,
				),
			),
		);
	}

	// ==================== Document Comments ====================

	async addDocumentComment(
		projectId: string,
		docId: string,
		content: string,
	): Promise<DocumentActivity> {
		const contentBlocks = content ? markdownToBlocknote(content) : null;
		return this.post(`/api/v1/projects/${projectId}/docs/${docId}/comments`, {
			content: contentBlocks,
		});
	}

	async updateDocumentComment(
		projectId: string,
		docId: string,
		commentId: string,
		content: string,
	): Promise<DocumentActivity> {
		const contentBlocks = content ? markdownToBlocknote(content) : null;
		return this.patch(
			`/api/v1/projects/${projectId}/docs/${docId}/comments/${commentId}`,
			{ content: contentBlocks },
		);
	}

	async deleteDocumentComment(
		projectId: string,
		docId: string,
		commentId: string,
	): Promise<void> {
		await this.delete(
			`/api/v1/projects/${projectId}/docs/${docId}/comments/${commentId}`,
		);
	}

	// ==================== Document Files ====================

	async getDocFileDownloadURL(
		projectId: string,
		docId: string,
		fileId: string,
	): Promise<string> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/docs/${docId}/files/${fileId}/download-url`,
		);
		return response.url || response.downloadUrl || "";
	}

	async deleteDocFile(
		projectId: string,
		docId: string,
		fileId: string,
	): Promise<void> {
		await this.delete(
			`/api/v1/projects/${projectId}/docs/${docId}/files/${fileId}`,
		);
	}

	/**
	 * Searches a project's documents by title or body text
	 * (GET /docs/search) — the list endpoint only matches titles.
	 */
	async searchDocuments(
		projectId: string,
		query: string,
		limit = 20,
	): Promise<DocumentSearchHit[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/docs/search?q=${encodeURIComponent(query)}&limit=${limit}`,
		);
		if (Array.isArray(response)) return response;
		return response?.items ?? [];
	}
}
