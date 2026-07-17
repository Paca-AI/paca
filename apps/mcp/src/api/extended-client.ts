import type {
	AddMemberInput,
	CreateCustomFieldInput,
	CreateRoleInput,
	CreateTaskStatusInput,
	CreateTaskTypeInput,
	CustomFieldDefinition,
	PacaConfig,
	ProjectMember,
	ProjectRole,
	SuccessEnvelope,
	TaskStatus,
	TaskType,
	UpdateCustomFieldInput,
	UpdateMemberRoleInput,
	UpdateRoleInput,
	UpdateTaskStatusInput,
	UpdateTaskTypeInput,
} from "../types/index.js";
import { buildAuthHeaders, registerAuthResponse } from "../auth.js";

/**
 * Extended API client methods for additional Paca endpoints.
 * This extends the base PacaAPIClient with additional functionality.
 */
export class PacaAPIExtendedClient {
	private config: PacaConfig;

	constructor(config: PacaConfig) {
		this.config = config;
	}

	/**
	 * Makes an HTTP request to the Paca API.
	 * Handles SuccessEnvelope wrapper by extracting data.data.
	 */
	private async request(
		method: string,
		path: string,
		body?: any,
	): Promise<any> {
		const url = `${this.config.baseURL}${path}`;
		const headers: Record<string, string> = {
			"Content-Type": "application/json",
			...buildAuthHeaders(this.config),
		};

		const options: RequestInit = {
			method,
			headers,
		};

		if (body) {
			options.body = JSON.stringify(body);
		}

		const response = await fetch(url, options);
		registerAuthResponse(response.status, this.config);

		if (!response.ok) {
			const errorText = await response.text();
			throw new Error(
				`API request failed: ${response.status} ${response.statusText} - ${errorText}`,
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

	// ==================== Project Members ====================

	async listProjectMembers(projectId: string): Promise<ProjectMember[]> {
		const response = await this.get(`/api/v1/projects/${projectId}/members`);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.members || response.data || [];
	}

	async addProjectMember(
		projectId: string,
		input: AddMemberInput,
	): Promise<ProjectMember> {
		return this.post(`/api/v1/projects/${projectId}/members`, input);
	}

	async getMyProjectPermissions(
		projectId: string,
	): Promise<Record<string, boolean>> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/members/me/permissions`,
		);
		return response.permissions || {};
	}

	async updateProjectMemberRole(
		projectId: string,
		userId: string,
		input: UpdateMemberRoleInput,
	): Promise<ProjectMember> {
		return this.patch(`/api/v1/projects/${projectId}/members/${userId}`, input);
	}

	async removeProjectMember(projectId: string, userId: string): Promise<void> {
		await this.delete(`/api/v1/projects/${projectId}/members/${userId}`);
	}

	// ==================== Project Roles ====================

	async listProjectRoles(projectId: string): Promise<ProjectRole[]> {
		const response = await this.get(`/api/v1/projects/${projectId}/roles`);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.roles || response.data || [];
	}

	async createProjectRole(
		projectId: string,
		input: CreateRoleInput,
	): Promise<ProjectRole> {
		return this.post(`/api/v1/projects/${projectId}/roles`, input);
	}

	async updateProjectRole(
		projectId: string,
		roleId: string,
		input: UpdateRoleInput,
	): Promise<ProjectRole> {
		return this.patch(`/api/v1/projects/${projectId}/roles/${roleId}`, input);
	}

	async deleteProjectRole(projectId: string, roleId: string): Promise<void> {
		await this.delete(`/api/v1/projects/${projectId}/roles/${roleId}`);
	}

	// ==================== Task Types ====================

	async listTaskTypes(projectId: string): Promise<TaskType[]> {
		const response = await this.get(`/api/v1/projects/${projectId}/task-types`);
		if (Array.isArray(response)) {
			return response;
		}
		return (
			response.items ||
			response.taskTypes ||
			response.types ||
			response.data ||
			[]
		);
	}

	async createTaskType(
		projectId: string,
		input: CreateTaskTypeInput,
	): Promise<TaskType> {
		return this.post(`/api/v1/projects/${projectId}/task-types`, input);
	}

	async updateTaskType(
		projectId: string,
		typeId: string,
		input: UpdateTaskTypeInput,
	): Promise<TaskType> {
		return this.patch(
			`/api/v1/projects/${projectId}/task-types/${typeId}`,
			input,
		);
	}

	async deleteTaskType(projectId: string, typeId: string): Promise<void> {
		await this.delete(`/api/v1/projects/${projectId}/task-types/${typeId}`);
	}

	async setDefaultTaskType(
		projectId: string,
		typeId: string,
	): Promise<TaskType> {
		return this.put(
			`/api/v1/projects/${projectId}/task-types/${typeId}/set-default`,
			{},
		);
	}

	// ==================== Task Statuses ====================

	async listTaskStatuses(projectId: string): Promise<TaskStatus[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/task-statuses`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return (
			response.items ||
			response.taskStatuses ||
			response.statuses ||
			response.data ||
			[]
		);
	}

	async createTaskStatus(
		projectId: string,
		input: CreateTaskStatusInput,
	): Promise<TaskStatus> {
		return this.post(`/api/v1/projects/${projectId}/task-statuses`, input);
	}

	async updateTaskStatus(
		projectId: string,
		statusId: string,
		input: UpdateTaskStatusInput,
	): Promise<TaskStatus> {
		return this.patch(
			`/api/v1/projects/${projectId}/task-statuses/${statusId}`,
			input,
		);
	}

	async deleteTaskStatus(projectId: string, statusId: string): Promise<void> {
		await this.delete(
			`/api/v1/projects/${projectId}/task-statuses/${statusId}`,
		);
	}

	async setDefaultTaskStatus(
		projectId: string,
		statusId: string,
	): Promise<TaskStatus> {
		return this.put(
			`/api/v1/projects/${projectId}/task-statuses/${statusId}/set-default`,
			{},
		);
	}

	// ==================== Custom Field Definitions ====================

	async listCustomFieldDefinitions(
		projectId: string,
	): Promise<CustomFieldDefinition[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/custom-fields`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.customFields || response.data || [];
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
}
