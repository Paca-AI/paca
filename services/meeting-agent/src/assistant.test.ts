import assert from "node:assert/strict";
import { test } from "node:test";
import { Assistant, describeProposal, type Transport, toProposal } from "./assistant.ts";
import type { Utterance } from "./audio.ts";
import type { Chat, Completion } from "./llm.ts";
import type { Bootstrap, PacaClient } from "./paca.ts";
import type { Transcriber } from "./stt.ts";

function boot(over: Partial<Bootstrap["behavior"]> = {}): Bootstrap {
	return {
		session_id: "s1",
		project_id: "p1",
		channel_id: "c1",
		call_id: "k1",
		agent_id: "a1",
		agent_name: "Atlas",
		requester_id: "alice",
		host_id: "alice",
		instructions: "Be brief.",
		llm: { provider: "openai", model: "m", api_key: "k", base_url: "" },
		stt: { provider: "openai", model: "w", api_key: "k", base_url: "", languages: [] },
		behavior: {
			mode: "on_demand",
			suggest_interval_seconds: 60,
			proposal_kinds: ["task.create", "doc.create_from_notes"],
			max_proposals_per_call: 2,
			store_transcript: true,
			announcement: "",
			summary_target: "channel_message",
			...over,
		},
		api_key: "paca_x",
		expires_at: "",
	};
}

const utt = (t = 0): Utterance => ({ pcm: new Int16Array(160), sampleRate: 16_000, startedAt: t, durationMs: 10 });

function setup(over: Partial<Bootstrap["behavior"]> = {}, completions: Completion[] = []) {
	const log = {
		appended: [] as unknown[][],
		proposals: [] as { kind: string; payload: unknown; target_id?: string }[],
		posted: [] as unknown[][],
		finished: 0,
	};
	let failAppend = 0;
	const paca = {
		appendTranscript: async (_s: string, segs: unknown[]) => {
			if (failAppend > 0) {
				failAppend--;
				throw new Error("down");
			}
			log.appended.push(segs);
		},
		createProposal: async (_p: string, b: { kind: string; payload: unknown; target_id?: string }) => {
			log.proposals.push(b);
			return {
				id: `prop${log.proposals.length}`,
				status: "pending",
				target_account_id: b.target_id ?? "alice",
				kind: b.kind,
			};
		},
		postMessage: async (_p: string, _c: string, content: unknown[]) => {
			log.posted.push(content);
		},
		finish: async () => {
			log.finished++;
		},
	} as unknown as PacaClient;
	const prompts: string[] = [];
	const chat: Chat = {
		complete: async (o) => {
			prompts.push(o.user);
			return completions.shift() ?? { text: "", toolCalls: [] };
		},
	};
	const said: string[] = [];
	const stt: Transcriber = { transcribe: async () => said.shift() ?? "" };
	const shown: { target: string; id: string; summary: string }[] = [];
	const captions: { text: string }[] = [];
	const out: Transport = {
		nameOf: (id) => ({ alice: "Alice", bob: "Bob" })[id] ?? "",
		caption: async (c) => {
			captions.push(c);
		},
		showProposal: async (target, id, summary) => {
			shown.push({ target, id, summary });
		},
	};
	const warn: string[] = [];
	const a = new Assistant(boot(over), paca, chat, stt, out, { info: () => undefined, warn: (m) => warn.push(m) });
	return {
		a,
		log,
		said,
		shown,
		captions,
		prompts,
		warn,
		failNextAppend: (n: number) => {
			failAppend = n;
		},
	};
}

test("hearing speech records, captions and flushes it with the speaker", async () => {
	const s = setup();
	s.said.push("we should ship friday");
	await s.a.hear("alice", utt(1000));
	assert.equal(s.captions[0]?.text, "we should ship friday");
	await s.a.flush();
	assert.deepEqual(
		(s.log.appended[0] as { speaker_id: string; text: string }[]).map((x) => [x.speaker_id, x.text]),
		[["alice", "we should ship friday"]],
	);
});

test("silence the recogniser returns nothing for is not recorded", async () => {
	const s = setup();
	s.said.push("");
	await s.a.hear("alice", utt());
	await s.a.flush();
	assert.equal(s.log.appended.length, 0);
});

test("a failed flush is retried with the next one, in order", async () => {
	const s = setup();
	s.said.push("one", "two");
	await s.a.hear("alice", utt(1));
	s.failNextAppend(1);
	await s.a.flush();
	assert.equal(s.log.appended.length, 0);
	await s.a.hear("alice", utt(2));
	await s.a.flush();
	assert.deepEqual(
		(s.log.appended[0] as { text: string }[]).map((x) => x.text),
		["one", "two"],
	);
});

test("suggest files each tool call for the person who asked and shows it to them", async () => {
	const s = setup({}, [
		{
			text: "",
			toolCalls: [
				{ name: "propose_task", args: { title: "Fix login", description: "It breaks on Safari" } },
				{ name: "unknown_tool", args: {} },
			],
		},
	]);
	s.said.push("login is broken on safari, bob will fix it");
	await s.a.hear("alice", utt());
	const made = await s.a.suggest("bob");
	assert.equal(made, 1);
	assert.deepEqual(s.log.proposals, [
		{
			channel_id: "c1",
			kind: "task.create",
			payload: { title: "Fix login", description: "It breaks on Safari" },
			target_id: "bob",
		},
	]);
	assert.deepEqual(s.shown, [{ target: "bob", id: "prop1", summary: "task.create: Fix login" }]);
	assert.match(s.prompts[0] ?? "", /Alice: login is broken on safari/);
});

test("suggest does nothing without new speech, in notes_only, or with every kind off", async () => {
	const s = setup();
	assert.equal(await s.a.suggest(), 0);
	const n = setup({ mode: "notes_only" }, [{ text: "", toolCalls: [{ name: "propose_task", args: { title: "x" } }] }]);
	n.said.push("hi");
	await n.a.hear("alice", utt());
	assert.equal(await n.a.suggest(), 0);
	assert.equal(n.log.proposals.length, 0);
	const k = setup({ proposal_kinds: [] }, [{ text: "", toolCalls: [{ name: "propose_task", args: { title: "x" } }] }]);
	k.said.push("hi");
	await k.a.hear("alice", utt());
	assert.equal(await k.a.suggest(), 0);
});

test("only enabled kinds are offered to the model", async () => {
	const seen: string[][] = [];
	const s = setup({ proposal_kinds: ["doc.create_from_notes"] });
	(s.a as unknown as { chat: Chat }).chat = {
		complete: async (o) => {
			seen.push((o.tools ?? []).map((t) => t.name));
			return { text: "", toolCalls: [] };
		},
	};
	s.said.push("hi");
	await s.a.hear("alice", utt());
	await s.a.suggest();
	assert.deepEqual(seen, [["propose_doc"]]);
});

test("the worker stops at max_proposals_per_call", async () => {
	const call = { name: "propose_task", args: { title: "t" } };
	const s = setup({ max_proposals_per_call: 2 }, [{ text: "", toolCalls: [call, call, call] }]);
	s.said.push("lots to do");
	await s.a.hear("alice", utt());
	assert.equal(await s.a.suggest(), 2);
});

test("a proposal the API rejects does not stop the rest", async () => {
	const s = setup({}, [
		{
			text: "",
			toolCalls: [
				{ name: "propose_task", args: { title: "a" } },
				{ name: "propose_task", args: { title: "b" } },
			],
		},
	]);
	let n = 0;
	(s.a as unknown as { paca: { createProposal: unknown } }).paca.createProposal = async (
		_p: string,
		b: { kind: string },
	) => {
		if (n++ === 0) throw new Error("PROPOSAL_INVALID");
		return { id: "ok", status: "pending", target_account_id: "alice", kind: b.kind };
	};
	s.said.push("x");
	await s.a.hear("alice", utt());
	assert.equal(await s.a.suggest(), 1);
	assert.ok(s.warn.includes("proposal rejected"));
});

test("finish flushes, posts notes and tells the API once", async () => {
	const s = setup({}, [{ text: "Summary.\nDecisions: none\nAction items: Bob fixes login", toolCalls: [] }]);
	s.said.push("one", "two", "three");
	for (const t of [1, 2, 3]) await s.a.hear("alice", utt(t));
	await s.a.finish();
	await s.a.finish();
	assert.equal(s.log.appended.length, 1);
	assert.equal(s.log.finished, 1);
	const blocks = s.log.posted[0] as { content: { text: string }[] }[];
	assert.equal(blocks[0]?.content[0]?.text, "Meeting notes from Atlas");
	assert.equal(blocks.length, 4);
});

test("no summary for a call with almost no speech or when the target is none", async () => {
	const a = setup({}, [{ text: "Summary", toolCalls: [] }]);
	a.said.push("hi");
	await a.a.hear("alice", utt());
	await a.a.finish();
	assert.equal(a.log.posted.length, 0);
	assert.equal(a.log.finished, 1);
	const b = setup({ summary_target: "none" }, [{ text: "Summary", toolCalls: [] }]);
	b.said.push("1", "2", "3");
	for (const t of [1, 2, 3]) await b.a.hear("alice", utt(t));
	await b.a.finish();
	assert.equal(b.log.posted.length, 0);
});

test("toProposal maps only the tools the worker offers", () => {
	assert.deepEqual(toProposal({ name: "propose_task", args: { title: "T", priority: "high", extra: 1 } }), {
		kind: "task.create",
		payload: { title: "T", priority: "high" },
	});
	assert.deepEqual(toProposal({ name: "propose_doc", args: { title: "N", markdown: "- a" } }), {
		kind: "doc.create_from_notes",
		payload: { title: "N", markdown: "- a" },
	});
	assert.equal(toProposal({ name: "delete_everything", args: {} }), null);
	assert.equal(describeProposal("task.create", { title: "x".repeat(500) }).length, 200);
});
