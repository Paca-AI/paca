import type {
	Attachment,
	BulkMoveTasksInput,
	CreateCustomFieldInput,
	CreateViewInput,
	CustomFieldDefinition,
	PacaConfig,
	ReorderViewsInput,
	SuccessEnvelope,
	TaskPosition,
	UpdateCustomFieldInput,
	UpdateViewInput,
	View,
} from "../types/index.js";
import { formatApiRequestError } from "../utils/index.js";

/**
 * Extended API client for Views, Custom Fields, and Attachments.
 */
export class PacaAPIViewsClient {
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
		if (this.config.agentId) {
			headers["X-Agent-ID"] = this.config.agentId;
		}

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

	private async put(path: string, body: any): Promise<any> {
		return this.request("PUT", path, body);
	}

	// ==================== Views ====================

	async listViews(
		projectId: string,
		context?: string,
		sprintId?: string | null,
	): Promise<View[]> {
		const params: string[] = [];
		if (context) params.push(`context=${context}`);
		if (sprintId !== undefined) params.push(`sprint_id=${sprintId}`);
		const queryString = params.length > 0 ? `?${params.join("&")}` : "";

		const response = await this.get(
			`/api/v1/projects/${projectId}/views${queryString}`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.views || response.data || [];
	}

	async createView(
		projectId: string,
		input: CreateViewInput,
		context?: string,
		sprintId?: string | null,
	): Promise<View> {
		const params: string[] = [];
		if (context) params.push(`context=${context}`);
		if (sprintId !== undefined) params.push(`sprint_id=${sprintId}`);
		const queryString = params.length > 0 ? `?${params.join("&")}` : "";

		return this.post(
			`/api/v1/projects/${projectId}/views${queryString}`,
			input,
		);
	}

	async reorderViews(
		projectId: string,
		input: ReorderViewsInput,
		context?: string,
		sprintId?: string | null,
	): Promise<void> {
		const params: string[] = [];
		if (context) params.push(`context=${context}`);
		if (sprintId !== undefined) params.push(`sprint_id=${sprintId}`);
		const queryString = params.length > 0 ? `?${params.join("&")}` : "";

		await this.put(
			`/api/v1/projects/${projectId}/views/positions${queryString}`,
			{ view_ids: input.view_ids },
		);
	}

	async getView(projectId: string, viewId: string): Promise<View> {
		return this.get(`/api/v1/projects/${projectId}/views/${viewId}`);
	}

	async updateView(
		projectId: string,
		viewId: string,
		input: UpdateViewInput,
	): Promise<View> {
		return this.patch(`/api/v1/projects/${projectId}/views/${viewId}`, input);
	}

	async deleteView(projectId: string, viewId: string): Promise<void> {
		await this.delete(`/api/v1/projects/${projectId}/views/${viewId}`);
	}

	async listTaskPositions(
		projectId: string,
		viewId: string,
	): Promise<TaskPosition[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/views/${viewId}/task-positions`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return (
			response.items ||
			response.taskPositions ||
			response.positions ||
			response.data ||
			[]
		);
	}

	async bulkMoveViewTaskPositions(
		projectId: string,
		viewId: string,
		items: Array<{
			task_id: string;
			position: number;
			group_key?: string | null;
		}>,
	): Promise<void> {
		await this.put(
			`/api/v1/projects/${projectId}/views/${viewId}/task-positions`,
			{ items },
		);
	}

	async bulkMoveTasks(
		projectId: string,
		viewId: string,
		input: BulkMoveTasksInput,
	): Promise<void> {
		await this.put(
			`/api/v1/projects/${projectId}/views/${viewId}/task-positions/${input.task_id}`,
			{
				target_view_id: input.target_view_id,
				target_status_id: input.target_status_id,
				target_position: input.target_position,
			},
		);
	}

	// ==================== Custom Fields ====================

	async listCustomFieldDefinitions(
		projectId: string,
	): Promise<CustomFieldDefinition[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/custom-fields`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return (
			response.items ||
			response.customFields ||
			response.fields ||
			response.data ||
			[]
		);
	}

	async getCustomFieldDefinition(
		projectId: string,
		fieldId: string,
	): Promise<CustomFieldDefinition> {
		return this.get(`/api/v1/projects/${projectId}/custom-fields/${fieldId}`);
	}

	async createCustomFieldDefinition(
		projectId: string,
		input: CreateCustomFieldInput,
	): Promise<CustomFieldDefinition> {
		return this.post(`/api/v1/projects/${projectId}/custom-fields`, input);
	}

	async updateCustomFieldDefinition(
		projectId: string,
		fieldId: string,
		input: UpdateCustomFieldInput,
	): Promise<CustomFieldDefinition> {
		return this.patch(
			`/api/v1/projects/${projectId}/custom-fields/${fieldId}`,
			input,
		);
	}

	async deleteCustomFieldDefinition(
		projectId: string,
		fieldId: string,
	): Promise<void> {
		await this.delete(`/api/v1/projects/${projectId}/custom-fields/${fieldId}`);
	}

	// ==================== Attachments ====================

	async listTaskAttachments(
		projectId: string,
		taskId: string,
	): Promise<Attachment[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/tasks/${taskId}/attachments`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.attachments || response.data || [];
	}

	async getAttachmentDownloadURL(
		projectId: string,
		taskId: string,
		attachmentId: string,
	): Promise<string> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/tasks/${taskId}/attachments/${attachmentId}/download-url`,
		);
		return response.url || response.downloadUrl || "";
	}

	async deleteTaskAttachment(
		projectId: string,
		taskId: string,
		attachmentId: string,
	): Promise<void> {
		await this.delete(
			`/api/v1/projects/${projectId}/tasks/${taskId}/attachments/${attachmentId}`,
		);
	}

	/**
	 * Downloads the raw bytes of an attachment through the Paca API itself,
	 * rather than via a presigned object-store URL. Presigned URLs are
	 * rewritten to a browser-facing public host (see the backend's
	 * STORAGE_PUBLIC_URL), which server-side callers like this MCP server
	 * often can't reach — e.g. when running inside an agent sandbox
	 * container that can resolve `api`/`gateway` by their internal Docker
	 * hostname but not the public one the presigned URL points at. Going
	 * through config.baseURL instead reuses the same connectivity every
	 * other tool call in this client already depends on.
	 */
	async downloadAttachmentContent(
		projectId: string,
		taskId: string,
		attachmentId: string,
	): Promise<{ buffer: ArrayBuffer; contentType?: string }> {
		const url = `${this.config.baseURL}/api/v1/projects/${projectId}/tasks/${taskId}/attachments/${attachmentId}/content`;
		const headers: Record<string, string> = {
			"X-API-Key": this.config.apiKey,
		};
		if (this.config.agentId) {
			headers["X-Agent-ID"] = this.config.agentId;
		}

		const response = await fetch(url, { headers });
		if (!response.ok) {
			const errorText = await response.text().catch(() => "");
			throw new Error(
				formatApiRequestError(response.status, response.statusText, errorText),
			);
		}
		const buffer = await response.arrayBuffer();
		return {
			buffer,
			contentType: response.headers.get("content-type") ?? undefined,
		};
	}

	/**
	 * Uploads a file as a task attachment via Paca's three-step flow:
	 * initiate-upload (reserve a File row + presigned PUT URL[s]) → PUT the
	 * bytes straight to object storage → complete-upload (mark uploaded +
	 * create the task-attachment link). Handles both the single-part path
	 * (< 5 MiB) and the multipart path the backend switches to for larger
	 * files.
	 *
	 * The PUT goes to a presigned URL, which the backend rewrites to its
	 * browser-facing public host (STORAGE_PUBLIC_URL) — the same host the web
	 * client uploads through. An MCP server that can reach the Paca API's
	 * public base URL (the usual setup for an external agent) can reach it
	 * too; one confined to an internal Docker network that only resolves
	 * `api`/`gateway` may not, and should surface the PUT error rather than
	 * silently failing. (This mirrors downloadAttachmentContent's note, which
	 * is why *reads* go through the API proxy instead — there is no equivalent
	 * upload proxy endpoint to use here.)
	 */
	async uploadTaskAttachment(
		projectId: string,
		taskId: string,
		input: { fileName: string; contentType: string; data: Uint8Array },
	): Promise<Attachment> {
		const { fileName, contentType, data } = input;
		const session = await this.post(
			`/api/v1/projects/${projectId}/tasks/${taskId}/attachments/initiate-upload`,
			{
				file_name: fileName,
				content_type: contentType,
				file_size: data.byteLength,
			},
		);

		if (session.is_multipart && session.multipart) {
			const parts = await this.putMultipart(
				session.multipart.parts,
				contentType,
				data,
			);
			return this.post(
				`/api/v1/projects/${projectId}/tasks/${taskId}/attachments/complete-upload`,
				{
					file_id: session.file_id,
					upload_id: session.multipart.upload_id,
					parts,
				},
			);
		}

		await this.putBytes(session.upload_url, contentType, data);
		return this.post(
			`/api/v1/projects/${projectId}/tasks/${taskId}/attachments/complete-upload`,
			{ file_id: session.file_id },
		);
	}

	/** PUTs raw bytes to a presigned object-store URL, returning its ETag. */
	private async putBytes(
		url: string,
		contentType: string,
		body: Uint8Array,
	): Promise<string> {
		const response = await fetch(url, {
			method: "PUT",
			headers: { "Content-Type": contentType },
			// A Uint8Array is a valid fetch body at runtime (undici honors the
			// view's byteOffset/byteLength, so a multipart subarray sends only its
			// own bytes) but isn't in the DOM lib's BodyInit type — hence the cast.
			body: body as unknown as BodyInit,
		});
		if (!response.ok) {
			const errorText = await response.text().catch(() => "");
			throw new Error(
				`Uploading file bytes to object storage failed: ${formatApiRequestError(
					response.status,
					response.statusText,
					errorText,
				)}`,
			);
		}
		return response.headers.get("etag") ?? "";
	}

	/**
	 * Uploads each 5 MiB part to its presigned URL and returns the
	 * {part_number, etag} list complete-upload needs to reassemble the object.
	 */
	private async putMultipart(
		partURLs: Array<{ part_number: number; upload_url: string }>,
		contentType: string,
		data: Uint8Array,
	): Promise<Array<{ part_number: number; etag: string }>> {
		const PART_SIZE = 5 * 1024 * 1024; // matches the backend's DefaultPartSize
		const ordered = [...partURLs].sort((a, b) => a.part_number - b.part_number);
		const parts: Array<{ part_number: number; etag: string }> = [];
		for (let i = 0; i < ordered.length; i++) {
			const part = ordered[i];
			const chunk = data.subarray(i * PART_SIZE, (i + 1) * PART_SIZE);
			const etag = await this.putBytes(part.upload_url, contentType, chunk);
			parts.push({ part_number: part.part_number, etag });
		}
		return parts;
	}
}
