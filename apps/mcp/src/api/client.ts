import type {
	CompleteSprintInput,
	CreateDocumentInput,
	CreateProjectInput,
	CreateSprintInput,
	CreateTaskInput,
	Document,
	GitCredential,
	GitRepository,
	PacaConfig,
	Project,
	Sprint,
	SuccessEnvelope,
	Task,
	UpdateDocumentInput,
	UpdateProjectInput,
	UpdateSprintInput,
	UpdateTaskInput,
} from "../types/index.js";
import { formatApiRequestError, markdownToBlocknote } from "../utils/index.js";
import {
	LIST_PAGE_SIZE,
	type Page,
	type PageOptions,
	toPage,
	withPaging,
} from "./paginate.js";

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
		// Lets core attribute what the agent posts to this conversation; core
		// ignores it unless the calling agent owns the conversation.
		if (this.config.conversationId) {
			headers["X-Paca-Conversation-Id"] = this.config.conversationId;
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

	/** One page of the projects the caller can access. Projects page by
	 *  number, so the cursor here is the next page number. */
	async listProjects(opts: PageOptions = {}): Promise<Page<Project>> {
		const pageSize = opts.pageSize ?? LIST_PAGE_SIZE;
		const pageNumber = Math.max(
			1,
			Number.parseInt(opts.cursor ?? "1", 10) || 1,
		);
		const response = await this.get(
			`/api/v1/projects?page=${pageNumber}&page_size=${pageSize}`,
		);
		if (Array.isArray(response)) return { items: response, nextCursor: null };
		const items: Project[] =
			response.items || response.projects || response.data || [];
		const total =
			typeof response.total === "number" ? response.total : undefined;
		const more = total !== undefined && pageNumber * pageSize < total;
		return {
			items,
			nextCursor: more ? String(pageNumber + 1) : null,
			...(total !== undefined ? { total } : {}),
		};
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
			search?: string;
			searchContent?: boolean;
			pageSize?: number;
			cursor?: string;
		} = {},
	): Promise<{
		items: Task[];
		nextCursor?: string | null;
		total?: number;
	}> {
		const params: string[] = [];
		if (options.pageSize !== undefined)
			params.push(`page_size=${options.pageSize}`);
		if (options.cursor)
			params.push(`cursor=${encodeURIComponent(options.cursor)}`);
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
		if (options.search) {
			params.push(`search=${encodeURIComponent(options.search)}`);
			if (options.searchContent) params.push("search_content=true");
		}

		const queryString = params.length > 0 ? `?${params.join("&")}` : "";
		const response = await this.get(
			`/api/v1/projects/${projectId}/tasks${queryString}`,
		);
		if (Array.isArray(response)) {
			return { items: response, nextCursor: null };
		}
		const total = response.total_count ?? response.total;
		return {
			items: response.items || response.tasks || response.data || [],
			nextCursor: response.next_cursor ?? null,
			...(typeof total === "number" ? { total } : {}),
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

	async listSprints(
		projectId: string,
		opts: PageOptions & { status?: string; search?: string } = {},
	): Promise<Page<Sprint>> {
		const filters = new URLSearchParams();
		if (opts.status) filters.set("status", opts.status);
		if (opts.search) filters.set("search", opts.search);
		const qs = filters.toString();
		return toPage<Sprint>(
			await this.get(
				withPaging(
					`/api/v1/projects/${projectId}/sprints${qs ? `?${qs}` : ""}`,
					opts,
				),
			),
		);
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

	/** One page of documents; `folderId` narrows to one folder ("null" for
	 *  the top level), `viewId` lists the notes of a note view instead, and
	 *  `title` narrows to an exact, case-insensitive title. */
	async listDocuments(
		projectId: string,
		opts: PageOptions & {
			folderId?: string;
			viewId?: string;
			title?: string;
		} = {},
	): Promise<Page<Document>> {
		const filters = new URLSearchParams();
		if (opts.viewId) filters.set("view_id", opts.viewId);
		else if (opts.folderId) filters.set("folder_id", opts.folderId);
		if (opts.title) filters.set("title", opts.title);
		const qs = filters.toString();
		return toPage<Document>(
			await this.get(
				withPaging(
					`/api/v1/projects/${projectId}/docs${qs ? `?${qs}` : ""}`,
					opts,
				),
			),
		);
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
		if (input.view_id !== undefined) body.view_id = input.view_id;
		if (input.position !== undefined) body.position = input.position;
		if (input.note) body.note = input.note;
		if (input.client) body.client = input.client;
		if (input.task) body.task = input.task;

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

	// ==================== Git Methods ====================
	// Core's platform-neutral Git API (services/api git handler); the provider
	// plugin behind a repository is never called directly.

	async listGitRepositories(projectId: string): Promise<GitRepository[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/git/repositories`,
		);
		return Array.isArray(response) ? response : [];
	}

	/**
	 * Calls one of core's per-repository Git endpoints
	 * (`/projects/{id}/git/repositories/{repoId}/…`): change requests, reviews,
	 * comments, issues and CI. Core resolves the provider and the identity.
	 */
	async gitRepo(
		method: "GET" | "POST" | "PATCH" | "PUT",
		projectId: string,
		repoId: string,
		path: string,
		opts: {
			query?: Record<string, string | number | undefined>;
			body?: any;
		} = {},
	): Promise<any> {
		const params = new URLSearchParams();
		for (const [k, v] of Object.entries(opts.query ?? {})) {
			if (v !== undefined && v !== "") params.set(k, String(v));
		}
		const qs = params.toString();
		const url = `/api/v1/projects/${encodeURIComponent(projectId)}/git/repositories/${encodeURIComponent(repoId)}${path}${qs ? `?${qs}` : ""}`;
		return this.request(
			method,
			url,
			method === "GET" ? undefined : (opts.body ?? {}),
		);
	}

	/** Mints a short-lived credential for ONE git command; a write credential needs git:write. */
	async getGitCredential(
		projectId: string,
		repoId: string,
		access: "read" | "write",
	): Promise<GitCredential | undefined> {
		return this.post(
			`/api/v1/projects/${projectId}/git/repositories/${repoId}/credential/${access}`,
			{},
		);
	}
}
