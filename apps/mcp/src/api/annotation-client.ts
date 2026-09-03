import type {
	AnnotationListResult,
	PacaConfig,
	PageAnnotation,
	SuccessEnvelope,
} from "../types/index.js";
import { formatApiRequestError } from "../utils/index.js";

/**
 * Read-only API client for page annotations ("comments" pinned to elements
 * of a page via the browser extension, apps/extension) — backs
 * list_annotations/get_annotation. Both hit the project-wide annotation
 * routes (not the ones nested under one specific port forward, which exist
 * for the extension's own per-page hot path and the web app's per-port-
 * forward Comments tab) — see
 * services/api/internal/transport/http/router/router.go's
 * `r.Route("/annotations", ...)` block under `/projects/{projectId}`.
 */
export class PacaAPIAnnotationClient {
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

		const jsonResponse = await response.json();

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

		return jsonResponse;
	}

	private async get(path: string): Promise<any> {
		return this.request("GET", path);
	}

	async listAnnotations(
		projectId: string,
		opts: {
			search?: string;
			environmentId?: string;
			portForwardId?: string;
			status?: "open" | "resolved";
			cursor?: string;
			pageSize?: number;
		} = {},
	): Promise<AnnotationListResult> {
		const params: string[] = [];
		if (opts.search) params.push(`search=${encodeURIComponent(opts.search)}`);
		if (opts.environmentId)
			params.push(`environment_id=${encodeURIComponent(opts.environmentId)}`);
		if (opts.portForwardId)
			params.push(`port_forward_id=${encodeURIComponent(opts.portForwardId)}`);
		if (opts.status) params.push(`status=${opts.status}`);
		if (opts.cursor) params.push(`cursor=${encodeURIComponent(opts.cursor)}`);
		// Always sent, defaulted here too (not just in the tool's Zod
		// schema): the backend only applies a LIMIT when page_size is
		// present on the query string at all, so any other caller of this
		// client that forgets to pass one would otherwise fetch every
		// annotation in the project unbounded.
		params.push(`page_size=${opts.pageSize ?? 20}`);
		const query = params.length > 0 ? `?${params.join("&")}` : "";
		return this.get(`/api/v1/projects/${projectId}/annotations${query}`);
	}

	async getAnnotation(
		projectId: string,
		annotationId: string,
	): Promise<PageAnnotation> {
		return this.get(
			`/api/v1/projects/${projectId}/annotations/${annotationId}`,
		);
	}
}
