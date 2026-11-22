import type { Bootstrap } from "./paca.ts";

export interface ToolDef {
	name: string;
	description: string;
	/** JSON Schema of the arguments. */
	parameters: Record<string, unknown>;
}

export interface ToolCall {
	name: string;
	args: Record<string, unknown>;
}

export interface Completion {
	text: string;
	toolCalls: ToolCall[];
}

export interface Chat {
	complete(o: { system: string; user: string; tools?: ToolDef[]; maxTokens?: number }): Promise<Completion>;
}

type Fetch = typeof fetch;

const OPENAI_COMPATIBLE: Record<string, string> = {
	openai: "https://api.openai.com/v1",
	groq: "https://api.groq.com/openai/v1",
	deepseek: "https://api.deepseek.com/v1",
	gemini: "https://generativelanguage.googleapis.com/v1beta/openai",
	fireworks_ai: "https://api.fireworks.ai/inference/v1",
	cerebras: "https://api.cerebras.ai/v1",
};

function parseArgs(raw: unknown): Record<string, unknown> {
	if (raw && typeof raw === "object") return raw as Record<string, unknown>;
	if (typeof raw !== "string" || raw.trim() === "") return {};
	try {
		const v = JSON.parse(raw);
		return v && typeof v === "object" ? (v as Record<string, unknown>) : {};
	} catch {
		return {};
	}
}

export class OpenAIChat implements Chat {
	constructor(
		private readonly o: { baseUrl: string; apiKey: string; model: string },
		private readonly doFetch: Fetch = fetch,
	) {}

	async complete(o: { system: string; user: string; tools?: ToolDef[]; maxTokens?: number }): Promise<Completion> {
		const res = await this.doFetch(`${this.o.baseUrl.replace(/\/+$/, "")}/chat/completions`, {
			method: "POST",
			headers: { "Content-Type": "application/json", Authorization: `Bearer ${this.o.apiKey}` },
			body: JSON.stringify({
				model: this.o.model,
				max_tokens: o.maxTokens ?? 1500,
				messages: [
					{ role: "system", content: o.system },
					{ role: "user", content: o.user },
				],
				...(o.tools?.length
					? {
							tools: o.tools.map((t) => ({
								type: "function",
								function: { name: t.name, description: t.description, parameters: t.parameters },
							})),
						}
					: {}),
			}),
			signal: AbortSignal.timeout(90_000),
		});
		if (!res.ok) throw new Error(`llm: ${res.status}: ${(await res.text()).slice(0, 200)}`);
		const body = (await res.json()) as {
			choices?: {
				message?: {
					content?: string | null;
					tool_calls?: { function?: { name?: string; arguments?: string } }[];
				};
			}[];
		};
		const msg = body.choices?.[0]?.message;
		return {
			text: (msg?.content ?? "").trim(),
			toolCalls: (msg?.tool_calls ?? []).flatMap((c) =>
				c.function?.name ? [{ name: c.function.name, args: parseArgs(c.function.arguments) }] : [],
			),
		};
	}
}

export class AnthropicChat implements Chat {
	constructor(
		private readonly o: { baseUrl: string; apiKey: string; model: string },
		private readonly doFetch: Fetch = fetch,
	) {}

	async complete(o: { system: string; user: string; tools?: ToolDef[]; maxTokens?: number }): Promise<Completion> {
		const res = await this.doFetch(
			`${(this.o.baseUrl || "https://api.anthropic.com").replace(/\/+$/, "")}/v1/messages`,
			{
				method: "POST",
				headers: { "Content-Type": "application/json", "x-api-key": this.o.apiKey, "anthropic-version": "2023-06-01" },
				body: JSON.stringify({
					model: this.o.model,
					max_tokens: o.maxTokens ?? 1500,
					system: o.system,
					messages: [{ role: "user", content: o.user }],
					...(o.tools?.length
						? { tools: o.tools.map((t) => ({ name: t.name, description: t.description, input_schema: t.parameters })) }
						: {}),
				}),
				signal: AbortSignal.timeout(90_000),
			},
		);
		if (!res.ok) throw new Error(`llm: ${res.status}: ${(await res.text()).slice(0, 200)}`);
		const body = (await res.json()) as {
			content?: { type: string; text?: string; name?: string; input?: unknown }[];
		};
		const blocks = body.content ?? [];
		return {
			text: blocks
				.filter((b) => b.type === "text")
				.map((b) => b.text ?? "")
				.join("")
				.trim(),
			toolCalls: blocks.flatMap((b) =>
				b.type === "tool_use" && b.name ? [{ name: b.name, args: parseArgs(b.input) }] : [],
			),
		};
	}
}

export function makeChat(llm: Bootstrap["llm"], doFetch: Fetch = fetch): Chat {
	const o = { baseUrl: llm.base_url, apiKey: llm.api_key, model: llm.model };
	if (llm.provider === "anthropic") return new AnthropicChat(o, doFetch);
	const base = llm.base_url || OPENAI_COMPATIBLE[llm.provider];
	if (!base) throw new Error(`llm: provider "${llm.provider}" needs a base URL`);
	return new OpenAIChat({ ...o, baseUrl: base }, doFetch);
}
