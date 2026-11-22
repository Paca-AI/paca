import type { Bootstrap } from "./paca.ts";

/** Turns one utterance (a WAV file) into text. Providers differ only in how the
 * request is shaped, so this is the whole interface. */
export interface Transcriber {
	transcribe(wav: Buffer, language?: string, signal?: AbortSignal): Promise<string>;
}

type Fetch = typeof fetch;

const OPENAI_URL = "https://api.openai.com/v1";

/** OpenAI's transcription API, and any server that speaks it (a self-hosted
 * Whisper behind stt_base_url). */
export class OpenAITranscriber implements Transcriber {
	constructor(
		private readonly o: { baseUrl: string; apiKey: string; model: string },
		private readonly doFetch: Fetch = fetch,
	) {}

	async transcribe(wav: Buffer, language?: string, signal?: AbortSignal): Promise<string> {
		const form = new FormData();
		form.set("file", new Blob([new Uint8Array(wav)], { type: "audio/wav" }), "audio.wav");
		form.set("model", this.o.model);
		form.set("response_format", "json");
		if (language) form.set("language", language);
		const res = await this.doFetch(`${(this.o.baseUrl || OPENAI_URL).replace(/\/+$/, "")}/audio/transcriptions`, {
			method: "POST",
			headers: this.o.apiKey ? { Authorization: `Bearer ${this.o.apiKey}` } : {},
			body: form,
			signal: signal ?? AbortSignal.timeout(60_000),
		});
		if (!res.ok) throw new Error(`stt: openai ${res.status}: ${(await res.text()).slice(0, 200)}`);
		const body = (await res.json()) as { text?: string };
		return (body.text ?? "").trim();
	}
}

/** Deepgram's pre-recorded API, one request per utterance. */
export class DeepgramTranscriber implements Transcriber {
	constructor(
		private readonly o: { baseUrl: string; apiKey: string; model: string },
		private readonly doFetch: Fetch = fetch,
	) {}

	async transcribe(wav: Buffer, language?: string, signal?: AbortSignal): Promise<string> {
		const q = new URLSearchParams({ model: this.o.model || "nova-3", smart_format: "true" });
		if (language) q.set("language", language);
		else q.set("detect_language", "true");
		const res = await this.doFetch(
			`${(this.o.baseUrl || "https://api.deepgram.com").replace(/\/+$/, "")}/v1/listen?${q}`,
			{
				method: "POST",
				headers: { Authorization: `Token ${this.o.apiKey}`, "Content-Type": "audio/wav" },
				body: new Uint8Array(wav),
				signal: signal ?? AbortSignal.timeout(60_000),
			},
		);
		if (!res.ok) throw new Error(`stt: deepgram ${res.status}: ${(await res.text()).slice(0, 200)}`);
		const body = (await res.json()) as {
			results?: { channels?: { alternatives?: { transcript?: string }[] }[] };
		};
		return (body.results?.channels?.[0]?.alternatives?.[0]?.transcript ?? "").trim();
	}
}

export function makeTranscriber(stt: Bootstrap["stt"], doFetch: Fetch = fetch): Transcriber {
	const o = { baseUrl: stt.base_url, apiKey: stt.api_key, model: stt.model };
	switch (stt.provider) {
		case "openai":
			return new OpenAITranscriber(o, doFetch);
		case "deepgram":
			return new DeepgramTranscriber(o, doFetch);
		default:
			throw new Error(`stt: unsupported provider "${stt.provider}"`);
	}
}

/** The first language of the agent's list, or undefined to auto-detect. With
 * several languages the provider's own detection is the better judge. */
export function pickLanguage(languages: string[]): string | undefined {
	return languages.length === 1 ? languages[0] : undefined;
}
