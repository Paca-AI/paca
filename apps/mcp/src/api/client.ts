import type {
	CompleteSprintInput,
	CreateDocumentInput,
	CreateProjectInput,
	CreateSprintInput,
	CreateTaskInput,
	Document,
	PacaConfig,
	Project,
	RepositoryCloneInfo,
	RepositoryListItem,
	Sprint,
	SuccessEnvelope,
	Task,
	UpdateDocumentInput,
	UpdateProjectInput,
	UpdateSprintInput,
	UpdateTaskInput,
} from "../types/index.js";
import { formatApiRequestError, markdownToBlocknote } from "../utils/index.js";

/**
 * Paca API client for interacting with the Paca backend.
 * Handles authentication, HTTP requests, and format conversions.
 */
export class PacaAPIClient {
	private config: PacaConfig;

	constructor(config: PacaConfig) {
		this.config = config;
	}

	/**
	 * Makes an HTTP request to the Paca API.
	 * Handles SuccessEnvelope wrapper by extracting data.data.
	 * @param method - HTTP method
	 * @param path - API path
	 * @param body - Request body (optional)
	 * @returns Response data (extracted from SuccessEnvelope)
	 */
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
		if (this.config.agentTurnId) {
			headers["X-Agent-Turn-ID"] = this.config.agentTurnId;
		}
		// Only ever meaningful alongside X-Agent-ID (see PacaConfig.actorUserId's
		// doc comment: "unset for a personal API key"). Gating on agentId here
		// too — not just trusting actorUserId's own truthiness — means a stray
		// or misconfigured PACA_ACTOR_USER_ID can never reach the server
		// without an agent identity attached to it.
		if (this.config.agentId && this.config.actorUserId) {
			headers["X-Actor-User-ID"] = this.config.actorUserId;
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

	// ==================== Project Methods ====================

	async listProjects(
		page: number = 1,
		pageSize: number = 50,
	): Promise<Project[]> {
		const response = await this.get(
			`/api/v1/projects?page=${page}&page_size=${pageSize}`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.projects || response.data || [];
	}

	async getProject(projectId: string): Promise<Project> {
		return this.get(`/api/v1/projects/${projectId}`);
	}

	async createProject(input: CreateProjectInput): Promise<Project> {
		return this.post("/api/v1/projects", input);
	}

	async updateProject(
		projectId: string,
		input: UpdateProjectInput,
	): Promise<Project> {
		return this.patch(`/api/v1/projects/${projectId}`, input);
	}

	async deleteProject(projectId: string): Promise<void> {
		await this.delete(`/api/v1/projects/${projectId}`);
	}

	// ==================== Task Methods ====================

	async listTasks(
		projectId: string,
		options: {
			sprintId?: string | null;
			statusId?: string;
			assigneeId?: string;
			taskTypeIds?: string[];
			parentTaskId?: string;
			pageSize?: number;
			cursor?: string;
		} = {},
	): Promise<{ items: Task[]; nextCursor?: string | null }> {
		const params: string[] = [];
		if (options.pageSize !== undefined)
			params.push(`page_size=${options.pageSize}`);
		if (options.cursor) params.push(`cursor=${options.cursor}`);
		if (options.sprintId !== undefined)
			params.push(`sprint_id=${options.sprintId}`);
		if (options.statusId !== undefined)
			params.push(`status_id=${options.statusId}`);
		if (options.assigneeId !== undefined)
			params.push(`assignee_id=${options.assigneeId}`);
		if (options.taskTypeIds && options.taskTypeIds.length > 0)
			params.push(`task_type_ids=${options.taskTypeIds.join(",")}`);
		if (options.parentTaskId !== undefined)
			params.push(`parent_task_id=${options.parentTaskId}`);

		const queryString = params.length > 0 ? `?${params.join("&")}` : "";
		const response = await this.get(
			`/api/v1/projects/${projectId}/tasks${queryString}`,
		);
		if (Array.isArray(response)) {
			return { items: response, nextCursor: null };
		}
		return {
			items: response.items || response.tasks || response.data || [],
			nextCursor: response.next_cursor ?? null,
		};
	}

	async getTask(projectId: string, taskId: string): Promise<Task> {
		return this.get(`/api/v1/projects/${projectId}/tasks/${taskId}`);
	}

	async getTaskByNumber(projectId: string, taskNumber: number): Promise<Task> {
		return this.get(
			`/api/v1/projects/${projectId}/tasks/by-number/${taskNumber}`,
		);
	}

	async createTask(input: CreateTaskInput): Promise<Task> {
		const descriptionBlocks = input.description
			? markdownToBlocknote(input.description)
			: null;

		const body: any = {
			project_id: input.project_id,
			title: input.title,
			description: descriptionBlocks,
		};

		if (input.status_id !== undefined) body.status_id = input.status_id;
		if (input.task_type_id !== undefined)
			body.task_type_id = input.task_type_id;
		if (input.sprint_id !== undefined) body.sprint_id = input.sprint_id;
		if (input.assignee_id !== undefined)
			body.assignee_ids = input.assignee_id ? [input.assignee_id] : [];
		if (input.parent_task_id !== undefined)
			body.parent_task_id = input.parent_task_id;
		if (input.importance !== undefined) body.importance = input.importance;
		if (input.story_points !== undefined)
			body.story_points = input.story_points;
		if (input.tags) body.tags = input.tags;
		if (input.start_date !== undefined) body.start_date = input.start_date;
		if (input.due_date !== undefined) body.due_date = input.due_date;

		return this.post(`/api/v1/projects/${input.project_id}/tasks`, body);
	}

	async updateTask(
		projectId: string,
		taskId: string,
		input: UpdateTaskInput,
	): Promise<Task> {
		const body: any = {};

		if (input.title !== undefined) body.title = input.title;
		if (input.description !== undefined) {
			body.description = input.description
				? markdownToBlocknote(input.description)
				: null;
		}
		if (input.status_id !== undefined) body.status_id = input.status_id;
		if (input.task_type_id !== undefined)
			body.task_type_id = input.task_type_id;
		if (input.sprint_id !== undefined) body.sprint_id = input.sprint_id;
		if (input.assignee_id !== undefined)
			body.assignee_ids = input.assignee_id ? [input.assignee_id] : [];
		if (input.reporter_id !== undefined) body.reporter_id = input.reporter_id;
		if (input.parent_task_id !== undefined)
			body.parent_task_id = input.parent_task_id;
		if (input.importance !== undefined) body.importance = input.importance;
		if (input.story_points !== undefined)
			body.story_points = input.story_points;
		if (input.tags !== undefined) body.tags = input.tags;
		if (input.start_date !== undefined) body.start_date = input.start_date;
		if (input.due_date !== undefined) body.due_date = input.due_date;
		if (input.custom_fields !== undefined)
			body.custom_fields = input.custom_fields;

		return this.patch(`/api/v1/projects/${projectId}/tasks/${taskId}`, body);
	}

	async deleteTask(projectId: string, taskId: string): Promise<void> {
		await this.delete(`/api/v1/projects/${projectId}/tasks/${taskId}`);
	}

	// ==================== Sprint Methods ====================

	async listSprints(projectId: string): Promise<Sprint[]> {
		const response = await this.get(`/api/v1/projects/${projectId}/sprints`);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.sprints || response.data || [];
	}

	async getSprint(projectId: string, sprintId: string): Promise<Sprint> {
		return this.get(`/api/v1/projects/${projectId}/sprints/${sprintId}`);
	}

	async createSprint(input: CreateSprintInput): Promise<Sprint> {
		const body: any = {
			project_id: input.project_id,
			name: input.name,
		};

		if (input.start_date !== undefined) body.start_date = input.start_date;
		if (input.end_date !== undefined) body.end_date = input.end_date;
		if (input.goal !== undefined) body.goal = input.goal;
		if (input.status !== undefined) body.status = input.status;

		return this.post(`/api/v1/projects/${input.project_id}/sprints`, body);
	}

	async updateSprint(
		projectId: string,
		sprintId: string,
		input: UpdateSprintInput,
	): Promise<Sprint> {
		const body: any = {};
		if (input.name !== undefined) body.name = input.name;
		if (input.start_date !== undefined) body.start_date = input.start_date;
		if (input.end_date !== undefined) body.end_date = input.end_date;
		if (input.goal !== undefined) body.goal = input.goal;
		if (input.status !== undefined) body.status = input.status;

		return this.patch(
			`/api/v1/projects/${projectId}/sprints/${sprintId}`,
			body,
		);
	}

	async deleteSprint(projectId: string, sprintId: string): Promise<void> {
		await this.delete(`/api/v1/projects/${projectId}/sprints/${sprintId}`);
	}

	async completeSprint(
		projectId: string,
		sprintId: string,
		input: CompleteSprintInput = {},
	): Promise<Sprint> {
		return this.post(
			`/api/v1/projects/${projectId}/sprints/${sprintId}/complete`,
			input,
		);
	}

	// ==================== Document Methods ====================

	async listDocuments(
		projectId: string,
		folderId?: string,
	): Promise<Document[]> {
		const queryString = folderId ? `?folder_id=${folderId}` : "";
		const response = await this.get(
			`/api/v1/projects/${projectId}/docs${queryString}`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.documents || response.data || [];
	}

	async getDocument(projectId: string, docId: string): Promise<Document> {
		return this.get(`/api/v1/projects/${projectId}/docs/${docId}`);
	}

	async createDocument(input: CreateDocumentInput): Promise<Document> {
		const contentBlocks = input.content
			? markdownToBlocknote(input.content)
			: null;

		const body: any = {
			project_id: input.project_id,
			title: input.title,
			content: contentBlocks,
		};

		if (input.folder_id !== undefined) body.folder_id = input.folder_id;
		if (input.position !== undefined) body.position = input.position;

		return this.post(`/api/v1/projects/${input.project_id}/docs`, body);
	}

	async updateDocument(
		projectId: string,
		docId: string,
		input: UpdateDocumentInput,
	): Promise<Document> {
		const body: any = {};

		if (input.title !== undefined) body.title = input.title;
		if (input.content !== undefined) {
			body.content = input.content ? markdownToBlocknote(input.content) : null;
		}
		if (input.folder_id !== undefined) body.folder_id = input.folder_id;
		if (input.position !== undefined) body.position = input.position;

		return this.patch(`/api/v1/projects/${projectId}/docs/${docId}`, body);
	}

	async deleteDocument(projectId: string, docId: string): Promise<void> {
		await this.delete(`/api/v1/projects/${projectId}/docs/${docId}`);
	}

	// ==================== Repository Methods ====================
	// These hit a repository plugin's own proxied backend routes (mounted at
	// /api/v1/plugins/:pluginId/* — see PluginHandler.ProxyRequest in
	// services/api), not a core-API endpoint. Matches whatever a repository
	// plugin (e.g. paca-plugin-github) actually implements — ported from the
	// equivalent request shapes in the now-removed services/ai-agent's
	// repo_tools.py (ListRepositoriesExecutor / CloneRepositoryExecutor /
	// PushBranchExecutor).

	async listPluginRepositories(
		pluginId: string,
		projectId: string,
	): Promise<RepositoryListItem[]> {
		const response = await this.get(
			`/api/v1/plugins/${pluginId}/projects/${projectId}/repositories`,
		);
		return Array.isArray(response) ? response : [];
	}

	async getRepositoryCloneInfo(
		pluginId: string,
		projectId: string,
		repoId: string,
	): Promise<RepositoryCloneInfo | undefined> {
		return this.get(
			`/api/v1/plugins/${pluginId}/projects/${projectId}/repositories/${repoId}/clone-info`,
		);
	}
}
