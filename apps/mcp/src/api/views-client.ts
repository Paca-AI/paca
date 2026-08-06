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
}
