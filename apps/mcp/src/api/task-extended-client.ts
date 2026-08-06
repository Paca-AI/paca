import type {
	CreateCommentInput,
	CreateTaskLinkInput,
	PacaConfig,
	SuccessEnvelope,
	TaskActivity,
	TaskLink,
	UpdateCommentInput,
} from "../types/index.js";
import { formatApiRequestError, markdownToBlocknote } from "../utils/index.js";

/**
 * Extended API client for task activities and comments.
 */
export class PacaAPITaskExtendedClient {
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

	// ==================== Task Activities ====================

	async listTaskActivities(
		projectId: string,
		taskId: string,
	): Promise<TaskActivity[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/tasks/${taskId}/activities`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.activities || response.data || [];
	}

	// ==================== Task Comments ====================

	async addTaskComment(
		projectId: string,
		taskId: string,
		input: CreateCommentInput,
	): Promise<TaskActivity> {
		const contentBlocks = input.content
			? markdownToBlocknote(input.content)
			: null;

		const body: any = {
			content: contentBlocks,
		};

		return this.post(
			`/api/v1/projects/${projectId}/tasks/${taskId}/activities/comments`,
			body,
		);
	}

	async updateTaskComment(
		projectId: string,
		taskId: string,
		commentId: string,
		input: UpdateCommentInput,
	): Promise<TaskActivity> {
		const contentBlocks = input.content
			? markdownToBlocknote(input.content)
			: null;

		const body: any = {
			content: contentBlocks,
		};

		return this.patch(
			`/api/v1/projects/${projectId}/tasks/${taskId}/activities/comments/${commentId}`,
			body,
		);
	}

	async deleteTaskComment(
		projectId: string,
		taskId: string,
		commentId: string,
	): Promise<void> {
		await this.delete(
			`/api/v1/projects/${projectId}/tasks/${taskId}/activities/comments/${commentId}`,
		);
	}

	async listSubtasks(projectId: string, parentTaskId: string): Promise<any[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/tasks?parent_task_id=${parentTaskId}`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.tasks || response.data || [];
	}

	async listTaskStatuses(projectId: string): Promise<any[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/task-statuses`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.statuses || response.data || [];
	}

	async listTaskTypes(projectId: string): Promise<any[]> {
		const response = await this.get(`/api/v1/projects/${projectId}/task-types`);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.types || response.data || [];
	}

	async listProjectMembers(projectId: string): Promise<any[]> {
		const response = await this.get(`/api/v1/projects/${projectId}/members`);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.members || response.data || [];
	}

	// ==================== Task Links ====================

	async listTaskLinks(projectId: string, taskId: string): Promise<TaskLink[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/tasks/${taskId}/links`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.links || response.data || [];
	}

	async createTaskLink(
		projectId: string,
		taskId: string,
		input: CreateTaskLinkInput,
	): Promise<TaskLink> {
		return this.post(
			`/api/v1/projects/${projectId}/tasks/${taskId}/links`,
			input,
		);
	}

	async deleteTaskLink(
		projectId: string,
		taskId: string,
		linkId: string,
	): Promise<void> {
		await this.delete(
			`/api/v1/projects/${projectId}/tasks/${taskId}/links/${linkId}`,
		);
	}
}
