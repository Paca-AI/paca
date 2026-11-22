// Settings the worker reads from its environment. Nothing here is a person's
// secret: the LiveKit pair is infrastructure (the worker can join any room), and
// MEETING_AGENT_TOKEN only lets it ask the API for the settings of a session it
// was dispatched to. Per-agent keys (LLM, transcription) arrive per job from the
// API and live in memory only.

export interface Config {
	/** LiveKit signalling URL the worker connects to (ws://livekit:7880). */
	livekitUrl: string;
	livekitApiKey: string;
	livekitApiSecret: string;
	/** Base URL of the Paca API including /api/v1 (http://api:8080/api/v1). */
	pacaApiUrl: string;
	workerToken: string;
	/** Name the worker registers under; the API dispatches to it. */
	agentName: string;
}

function need(env: NodeJS.ProcessEnv, key: string): string {
	const v = env[key]?.trim();
	if (!v) throw new Error(`${key} is required`);
	return v;
}

/** The agents library builds its endpoint as `${url}agent`, so the signalling URL
 * must end in a slash (ws://livekit:7880/, or a path such as .../livekit/). */
export function withTrailingSlash(url: string): string {
	return url.endsWith("/") ? url : `${url}/`;
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env): Config {
	return {
		livekitUrl: withTrailingSlash(env.LIVEKIT_INTERNAL_WS_URL?.trim() || need(env, "LIVEKIT_URL")),
		livekitApiKey: need(env, "LIVEKIT_API_KEY"),
		livekitApiSecret: need(env, "LIVEKIT_API_SECRET"),
		pacaApiUrl: need(env, "PACA_API_URL").replace(/\/+$/, ""),
		workerToken: need(env, "MEETING_AGENT_TOKEN"),
		agentName: env.MEETING_AGENT_NAME?.trim() || "paca-meeting",
	};
}
