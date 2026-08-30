import type {
	AgentConversation,
	ConversationEventPage,
	ConversationEventWindow,
	PacaConfig,
	SuccessEnvelope,
} from "../types/index.js";
import { formatApiRequestError } from "../utils/index.js";

/**
 * Read-only API client for another conversation's transcript. Backs the
 * read_conversation MCP tool, which lets an agent pull in a Conversation
 * attached as chat context (or otherwise referenced by ID) the same way
 * get_task/read_doc already do for a Task/Document.
 *
 * Calls GET /agents/me/conversations/:id(/events) — an agent-authenticated
 * self-service path, deliberately distinct from the human-facing
 * GET /projects/:projectId/conversations/:id and GET /agents/conversations/:id
 * endpoints the frontend uses. Both of those require a human member/actor
 * identity (project_members.id or actor_user_id) to authorize against; a
 * bare agent calling via its own X-Agent-ID has neither (agents aren't
 * project members, and a plain project-scoped chat trigger never carries an
 * actor_user_id), so calling them here always failed — with "member not
 * found" (404) on the project-scoped path, or a mis-scoped lookup on the
 * global path, regardless of which one this tool guessed at based on
 * whether the caller happened to pass a projectId. The /agents/me path
 * sidesteps this entirely: it authorizes by the server's own verified
 * X-Agent-ID against the conversation's own agent_id (see
 * agentdom.Service.GetConversationForAgent's doc comment for the exact
 * rule), so there's no project/global split — and no projectId parameter —
 * for the caller to get wrong.
 *
 * Only covers GET .../conversations/:id and GET .../conversations/:id/events
 * — every other conversation endpoint (send message, stop, pause,
 * heartbeat) belongs to the conversation the calling agent is actually
 * running inside, not one it's reading for context, so there's no need to
 * mirror those here.
 */
export class PacaAPIConversationClient {
	private config: PacaConfig;

	constructor(config: PacaConfig) {
		this.config = config;
	}

	private async request(method: string, path: string): Promise<any> {
		const url = `${this.config.baseURL}${path}`;
		const headers: Record<string, string> = {
			"Content-Type": "application/json",
			"X-API-Key": this.config.apiKey,
		};
		if (this.config.agentId) {
			headers["X-Agent-ID"] = this.config.agentId;
		}
		// Only ever meaningful alongside X-Agent-ID (see PacaConfig.actorUserId's
		// doc comment: "unset for a personal API key"). Gating on agentId here
		// too — not just trusting actorUserId's own truthiness — means a stray
		// or misconfigured PACA_ACTOR_USER_ID can never reach the server
		// without an agent identity attached to it. Mirrors client.ts's
		// PacaAPIClient exactly. Unused by the /agents/me path itself (it
		// authorizes on X-Agent-ID alone), kept for parity with the other
		// clients and in case a future agents/me route needs it.
		if (this.config.agentId && this.config.actorUserId) {
			headers["X-Actor-User-ID"] = this.config.actorUserId;
		}

		const response = await fetch(url, { method, headers });

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

	/** Builds the shared after/before/limit query string for an events window. */
	private buildEventWindowQuery(window: ConversationEventWindow): string {
		const params: string[] = [];
		if (window.after) params.push(`after=${encodeURIComponent(window.after)}`);
		if (window.before)
			params.push(`before=${encodeURIComponent(window.before)}`);
		if (window.limit !== undefined) params.push(`limit=${window.limit}`);
		return params.length > 0 ? `?${params.join("&")}` : "";
	}

	/** Normalizes the {items, total, next_cursor, prev_cursor} envelope shared
	 * by both events endpoints (see writeConversationEventWindowResponse). */
	private toEventPage(response: any): ConversationEventPage {
		return {
			items: response?.items ?? [],
			total: response?.total ?? 0,
			next_cursor: response?.next_cursor ?? null,
			prev_cursor: response?.prev_cursor ?? null,
		};
	}

	async getConversation(conversationId: string): Promise<AgentConversation> {
		return this.get(`/api/v1/agents/me/conversations/${conversationId}`);
	}

	async listConversationEvents(
		conversationId: string,
		window: ConversationEventWindow = {},
	): Promise<ConversationEventPage> {
		const query = this.buildEventWindowQuery(window);
		const response = await this.get(
			`/api/v1/agents/me/conversations/${conversationId}/events${query}`,
		);
		return this.toEventPage(response);
	}
}
