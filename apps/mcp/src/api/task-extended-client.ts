import type {
	CreateCommentInput,
	CreateTaskLinkInput,
	PacaConfig,
	SuccessEnvelope,
	TaskActivity,
	TaskLink,
	UpdateCommentInput,
} from "../types/index.js";
import {
	formatApiRequestError,
	linkTeamMentions,
	type MentionableMember,
	markdownToBlocknote,
} from "../utils/index.js";
import { type Page, type PageOptions, toPage, withPaging } from "./paginate.js";

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
		opts: PageOptions = {},
	): Promise<Page<TaskActivity>> {
		// One page, newest first; next pages reach further back in time.
		return toPage<TaskActivity>(
			await this.get(
				withPaging(
					`/api/v1/projects/${projectId}/tasks/${taskId}/activities`,
					opts,
				),
			),
		);
	}

	// ==================== Task Comments ====================

	/**
	 * Turns `@username` / `@Name (id: ...)` text into real mention chips for
	 * project members. Best-effort: on any failure the comment is posted with
	 * the text as written rather than blocked.
	 */
	private async linkMentions(projectId: string, blocks: any[]): Promise<any[]> {
		if (!JSON.stringify(blocks).includes("@")) return blocks;
		try {
			const members: MentionableMember[] = [];
			let cursor: string | undefined;
			// Bounded: a project has no fixed upper bound on members.
			for (let i = 0; i < 10; i++) {
				const page = toPage<MentionableMember>(
					await this.get(
						withPaging(`/api/v1/projects/${projectId}/members`, {
							cursor,
							pageSize: 100,
						}),
					),
				);
				members.push(...page.items);
				if (!page.nextCursor) break;
				cursor = page.nextCursor;
			}
			return linkTeamMentions(blocks, members);
		} catch {
			return blocks;
		}
	}

	async addTaskComment(
		projectId: string,
		taskId: string,
		input: CreateCommentInput,
	): Promise<TaskActivity> {
		const contentBlocks = input.content
			? await this.linkMentions(projectId, markdownToBlocknote(input.content))
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
			? await this.linkMentions(projectId, markdownToBlocknote(input.content))
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

	async listSubtasks(
		projectId: string,
		parentTaskId: string,
		opts: PageOptions = {},
	): Promise<Page<any>> {
		return toPage(
			await this.get(
				withPaging(
					`/api/v1/projects/${projectId}/tasks?parent_task_id=${parentTaskId}`,
					opts,
				),
			),
		);
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

	async listProjectMembers(
		projectId: string,
		opts: PageOptions & { search?: string; ids?: string[] } = {},
	): Promise<Page<any>> {
		const filters = new URLSearchParams();
		if (opts.search) filters.set("search", opts.search);
		if (opts.ids?.length) filters.set("ids", opts.ids.join(","));
		const qs = filters.toString();
		return toPage(
			await this.get(
				withPaging(
					`/api/v1/projects/${projectId}/members${qs ? `?${qs}` : ""}`,
					opts,
				),
			),
		);
	}

	// ==================== Task Links ====================

	async listTaskLinks(
		projectId: string,
		taskId: string,
		opts: PageOptions = {},
	): Promise<Page<TaskLink>> {
		return toPage<TaskLink>(
			await this.get(
				withPaging(`/api/v1/projects/${projectId}/tasks/${taskId}/links`, opts),
			),
		);
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
