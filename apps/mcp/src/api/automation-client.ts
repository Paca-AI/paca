import type {
	AddAutomationEdgeInput,
	AddAutomationNodeInput,
	Automation,
	AutomationEdge,
	AutomationGraph,
	AutomationNode,
	AutomationRun,
	AutomationRunStep,
	AutomationStatus,
	CreateAutomationInput,
	PacaConfig,
	SuccessEnvelope,
	UpdateAutomationInput,
	UpdateAutomationNodeInput,
} from "../types/index.js";

/**
 * API client for automation-graph endpoints.
 */
export class PacaAPIAutomationClient {
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

	private async post(path: string, body?: any): Promise<any> {
		return this.request("POST", path, body);
	}

	private async patch(path: string, body: any): Promise<any> {
		return this.request("PATCH", path, body);
	}

	private async delete(path: string): Promise<any> {
		return this.request("DELETE", path);
	}

	// ==================== Automations ====================

	async listAutomations(
		projectId: string,
		status?: AutomationStatus,
	): Promise<Automation[]> {
		const queryString = status ? `?status=${status}` : "";
		const response = await this.get(
			`/api/v1/projects/${projectId}/automations${queryString}`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.data || [];
	}

	async getAutomation(
		projectId: string,
		automationId: string,
	): Promise<AutomationGraph> {
		return this.get(
			`/api/v1/projects/${projectId}/automations/${automationId}`,
		);
	}

	async createAutomation(
		projectId: string,
		input: CreateAutomationInput,
	): Promise<Automation> {
		return this.post(`/api/v1/projects/${projectId}/automations`, input);
	}

	async updateAutomation(
		projectId: string,
		automationId: string,
		input: UpdateAutomationInput,
	): Promise<Automation> {
		return this.patch(
			`/api/v1/projects/${projectId}/automations/${automationId}`,
			input,
		);
	}

	async deleteAutomation(
		projectId: string,
		automationId: string,
	): Promise<void> {
		await this.delete(
			`/api/v1/projects/${projectId}/automations/${automationId}`,
		);
	}

	async activateAutomation(
		projectId: string,
		automationId: string,
	): Promise<Automation> {
		return this.post(
			`/api/v1/projects/${projectId}/automations/${automationId}/activate`,
		);
	}

	async archiveAutomation(
		projectId: string,
		automationId: string,
	): Promise<Automation> {
		return this.post(
			`/api/v1/projects/${projectId}/automations/${automationId}/archive`,
		);
	}

	async revertAutomationToDraft(
		projectId: string,
		automationId: string,
	): Promise<Automation> {
		return this.post(
			`/api/v1/projects/${projectId}/automations/${automationId}/revert-to-draft`,
		);
	}

	// ==================== Automation Nodes ====================

	async addAutomationNode(
		projectId: string,
		automationId: string,
		input: AddAutomationNodeInput,
	): Promise<AutomationNode> {
		return this.post(
			`/api/v1/projects/${projectId}/automations/${automationId}/nodes`,
			input,
		);
	}

	async updateAutomationNode(
		projectId: string,
		automationId: string,
		nodeId: string,
		input: UpdateAutomationNodeInput,
	): Promise<AutomationNode> {
		return this.patch(
			`/api/v1/projects/${projectId}/automations/${automationId}/nodes/${nodeId}`,
			input,
		);
	}

	async removeAutomationNode(
		projectId: string,
		automationId: string,
		nodeId: string,
	): Promise<void> {
		await this.delete(
			`/api/v1/projects/${projectId}/automations/${automationId}/nodes/${nodeId}`,
		);
	}

	// ==================== Automation Edges ====================

	async addAutomationEdge(
		projectId: string,
		automationId: string,
		input: AddAutomationEdgeInput,
	): Promise<AutomationEdge> {
		return this.post(
			`/api/v1/projects/${projectId}/automations/${automationId}/edges`,
			input,
		);
	}

	async removeAutomationEdge(
		projectId: string,
		automationId: string,
		edgeId: string,
	): Promise<void> {
		await this.delete(
			`/api/v1/projects/${projectId}/automations/${automationId}/edges/${edgeId}`,
		);
	}

	// ==================== Runs ====================

	async listAutomationRuns(
		projectId: string,
		automationId: string,
	): Promise<AutomationRun[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/automations/${automationId}/runs`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.data || [];
	}

	async listAutomationRunSteps(
		projectId: string,
		automationId: string,
		runId: string,
	): Promise<AutomationRunStep[]> {
		const response = await this.get(
			`/api/v1/projects/${projectId}/automations/${automationId}/runs/${runId}/steps`,
		);
		if (Array.isArray(response)) {
			return response;
		}
		return response.items || response.data || [];
	}
}
