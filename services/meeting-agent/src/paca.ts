import type { Config } from "./config.ts";

export interface Bootstrap {
	session_id: string;
	project_id: string;
	channel_id: string;
	call_id: string;
	agent_id: string;
	agent_name: string;
	requester_id: string | null;
	host_id: string | null;
	instructions: string;
	llm: { provider: string; model: string; api_key: string; base_url: string };
	stt: {
		provider: string;
		model: string;
		api_key: string;
		base_url: string;
		languages: string[];
	};
	behavior: {
		mode: "on_demand" | "auto_suggest" | "notes_only";
		suggest_interval_seconds: number;
		proposal_kinds: string[];
		max_proposals_per_call: number;
		store_transcript: boolean;
		announcement: string;
		summary_target: "none" | "channel_message";
	};
	api_key: string;
	expires_at: string;
}

export interface Segment {
	speaker_id: string | null;
	text: string;
	spoken_at: string;
}

export interface ProposalChip {
	id: string;
	status: string;
	target_account_id: string;
	kind: string;
}

export class PacaError extends Error {
	constructor(
		readonly status: number,
		readonly code: string,
		message: string,
	) {
		super(message);
	}
}

type Fetch = typeof fetch;

/** The API as the worker sees it: the worker routes (worker secret) and, once
 * bootstrapped, ordinary routes under the agent's own key. */
export class PacaClient {
	private agentKey = "";

	constructor(
		private readonly cfg: Pick<Config, "pacaApiUrl" | "workerToken">,
		private readonly doFetch: Fetch = fetch,
	) {}

	private async call<T>(method: string, path: string, headers: Record<string, string>, body?: unknown): Promise<T> {
		const res = await this.doFetch(this.cfg.pacaApiUrl + path, {
			method,
			headers: {
				...(body === undefined ? {} : { "Content-Type": "application/json" }),
				...headers,
			},
			body: body === undefined ? undefined : JSON.stringify(body),
			signal: AbortSignal.timeout(20_000),
		});
		if (res.status === 204) return undefined as T;
		const text = await res.text();
		// The API's envelope: {success, data} or {success:false, error_code, error}.
		let parsed: { data?: T; error_code?: string; error?: string } = {};
		try {
			parsed = JSON.parse(text);
		} catch {
			// not JSON: keep the raw text for the message
		}
		if (!res.ok) {
			throw new PacaError(res.status, parsed.error_code ?? "", parsed.error ?? (text.slice(0, 200) || res.statusText));
		}
		return (parsed.data ?? (parsed as unknown)) as T;
	}

	private worker<T>(method: string, session: string, tail: string, body?: unknown) {
		return this.call<T>(
			method,
			`/internal/meeting-agent/sessions/${session}/${tail}`,
			{ "X-Meeting-Worker-Token": this.cfg.workerToken },
			body,
		);
	}

	private asAgent<T>(method: string, path: string, body?: unknown) {
		if (!this.agentKey) throw new Error("bootstrap first");
		return this.call<T>(method, path, { "X-API-Key": this.agentKey }, body);
	}

	async bootstrap(session: string): Promise<Bootstrap> {
		const b = await this.worker<Bootstrap>("POST", session, "bootstrap");
		this.agentKey = b.api_key;
		return b;
	}

	appendTranscript(session: string, segments: { speaker_id: string; text: string; spoken_at: string }[]) {
		return this.worker<void>("POST", session, "transcript", { segments });
	}

	transcript(session: string, limit = 300) {
		return this.worker<Segment[]>("GET", session, `transcript?limit=${limit}`);
	}

	finish(session: string) {
		return this.worker<void>("POST", session, "finish");
	}

	createProposal(project: string, body: { channel_id: string; kind: string; payload: unknown; target_id?: string }) {
		return this.asAgent<ProposalChip>("POST", `/projects/${project}/proposals`, body);
	}

	/** Posts a message the agent's roles allow it to post. */
	postMessage(project: string, channel: string, content: unknown[]) {
		return this.asAgent<unknown>("POST", `/projects/${project}/channels/${channel}/messages`, { content });
	}
}
