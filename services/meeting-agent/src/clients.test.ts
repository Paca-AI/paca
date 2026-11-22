import assert from "node:assert/strict";
import { test } from "node:test";
import { makeChat } from "./llm.ts";
import { PacaClient, PacaError } from "./paca.ts";
import { makeTranscriber, pickLanguage } from "./stt.ts";

type Call = { url: string; init: RequestInit };
function fakeFetch(respond: (c: Call) => Response) {
	const calls: Call[] = [];
	const f = (async (url: string | URL | Request, init?: RequestInit) => {
		const c = { url: String(url), init: init ?? {} };
		calls.push(c);
		return respond(c);
	}) as typeof fetch;
	return { f, calls };
}
const hdr = (c: Call | undefined): Record<string, string> => (c?.init.headers ?? {}) as Record<string, string>;
const json = (body: unknown, status = 200) =>
	new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

test("openai transcriber posts multipart with a bearer key and language", async () => {
	const { f, calls } = fakeFetch(() => json({ text: "  hello there " }));
	const stt = makeTranscriber(
		{ provider: "openai", model: "whisper-1", api_key: "sk-1", base_url: "http://whisper:8000/v1/", languages: [] },
		f,
	);
	assert.equal(await stt.transcribe(Buffer.from("RIFF"), "en"), "hello there");
	const c = calls[0];
	assert.equal(c?.url, "http://whisper:8000/v1/audio/transcriptions");
	assert.equal(hdr(c).Authorization, "Bearer sk-1");
	const form = c?.init.body as FormData;
	assert.equal(form.get("model"), "whisper-1");
	assert.equal(form.get("language"), "en");
});

test("openai transcriber without a key sends no auth header (self-hosted)", async () => {
	const { f, calls } = fakeFetch(() => json({ text: "x" }));
	const stt = makeTranscriber(
		{ provider: "openai", model: "m", api_key: "", base_url: "http://w/v1", languages: [] },
		f,
	);
	await stt.transcribe(Buffer.from("x"));
	assert.deepEqual(calls[0]?.init.headers, {});
});

test("deepgram transcriber reads the first alternative", async () => {
	const { f, calls } = fakeFetch(() =>
		json({ results: { channels: [{ alternatives: [{ transcript: "xin chào" }] }] } }),
	);
	const stt = makeTranscriber({ provider: "deepgram", model: "nova-3", api_key: "dg", base_url: "", languages: [] }, f);
	assert.equal(await stt.transcribe(Buffer.from("x")), "xin chào");
	assert.match(calls[0]?.url ?? "", /^https:\/\/api\.deepgram\.com\/v1\/listen\?.*detect_language=true/);
	assert.equal(hdr(calls[0]).Authorization, "Token dg");
});

test("a failing provider surfaces an error", async () => {
	const { f } = fakeFetch(() => new Response("nope", { status: 401 }));
	const stt = makeTranscriber({ provider: "openai", model: "m", api_key: "k", base_url: "", languages: [] }, f);
	await assert.rejects(stt.transcribe(Buffer.from("x")), /openai 401/);
	assert.throws(
		() => makeTranscriber({ provider: "who", model: "", api_key: "", base_url: "", languages: [] }),
		/unsupported/,
	);
});

test("pickLanguage only pins a single language", () => {
	assert.equal(pickLanguage([]), undefined);
	assert.equal(pickLanguage(["en", "vi"]), undefined);
	assert.equal(pickLanguage(["vi"]), "vi");
});

test("openai chat sends tools and parses tool calls", async () => {
	const { f, calls } = fakeFetch(() =>
		json({
			choices: [
				{
					message: {
						content: null,
						tool_calls: [{ function: { name: "propose_task", arguments: '{"title":"Fix login"}' } }],
					},
				},
			],
		}),
	);
	const chat = makeChat({ provider: "openai", model: "gpt", api_key: "k", base_url: "" }, f);
	const res = await chat.complete({
		system: "s",
		user: "u",
		tools: [{ name: "propose_task", description: "d", parameters: { type: "object" } }],
	});
	assert.deepEqual(res.toolCalls, [{ name: "propose_task", args: { title: "Fix login" } }]);
	assert.equal(calls[0]?.url, "https://api.openai.com/v1/chat/completions");
	const body = JSON.parse(String(calls[0]?.init.body));
	assert.equal(body.tools[0].function.name, "propose_task");
});

test("malformed tool arguments become empty args, not a crash", async () => {
	const { f } = fakeFetch(() =>
		json({ choices: [{ message: { tool_calls: [{ function: { name: "propose_task", arguments: "{oops" } }] } }] }),
	);
	const res = await makeChat({ provider: "groq", model: "m", api_key: "k", base_url: "" }, f).complete({
		system: "s",
		user: "u",
	});
	assert.deepEqual(res.toolCalls, [{ name: "propose_task", args: {} }]);
});

test("anthropic chat uses the messages API", async () => {
	const { f, calls } = fakeFetch(() =>
		json({
			content: [
				{ type: "text", text: "ok " },
				{ type: "tool_use", name: "propose_doc", input: { title: "T", markdown: "m" } },
			],
		}),
	);
	const res = await makeChat({ provider: "anthropic", model: "claude", api_key: "ak", base_url: "" }, f).complete({
		system: "s",
		user: "u",
	});
	assert.equal(res.text, "ok");
	assert.deepEqual(res.toolCalls, [{ name: "propose_doc", args: { title: "T", markdown: "m" } }]);
	assert.equal(calls[0]?.url, "https://api.anthropic.com/v1/messages");
	assert.equal(hdr(calls[0])["x-api-key"], "ak");
});

test("an unknown provider needs a base url", () => {
	assert.throws(() => makeChat({ provider: "weird", model: "m", api_key: "k", base_url: "" }), /base URL/);
	assert.doesNotThrow(() => makeChat({ provider: "weird", model: "m", api_key: "k", base_url: "http://x/v1" }));
});

test("PacaClient: bootstrap stores the agent key and later calls use it", async () => {
	const { f, calls } = fakeFetch((c) => {
		if (c.url.endsWith("/bootstrap")) return json({ data: { session_id: "s1", api_key: "paca_abc" } });
		return json({ data: { id: "p1", status: "pending", target_account_id: "t", kind: "task.create" } });
	});
	const paca = new PacaClient({ pacaApiUrl: "http://api/api/v1", workerToken: "tok" }, f);
	await assert.rejects(
		async () => paca.createProposal("proj", { channel_id: "c", kind: "task.create", payload: {} }),
		/bootstrap first/,
	);
	await paca.bootstrap("s1");
	assert.equal(hdr(calls[0])["X-Meeting-Worker-Token"], "tok");
	const chip = await paca.createProposal("proj", { channel_id: "c", kind: "task.create", payload: { title: "x" } });
	assert.equal(chip.id, "p1");
	assert.equal(calls[1]?.url, "http://api/api/v1/projects/proj/proposals");
	assert.equal(hdr(calls[1])["X-API-Key"], "paca_abc");
});

test("PacaClient surfaces API error codes", async () => {
	const { f } = fakeFetch(() => json({ success: false, error_code: "PROPOSAL_LIMIT", error: "too many" }, 409));
	const paca = new PacaClient({ pacaApiUrl: "http://api/api/v1", workerToken: "tok" }, f);
	await assert.rejects(
		paca.finish("s"),
		(e: unknown) =>
			e instanceof PacaError && e.code === "PROPOSAL_LIMIT" && e.status === 409 && e.message === "too many",
	);
});
