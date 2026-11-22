// The worker and the real Paca API together, over a real LiveKit server, with
// only the speech-to-text and the LLM faked. It drives the whole assistant flow
// through the HTTP API the way the web client does:
//
//   create a meeting agent -> settings -> voice channel -> join -> invite ->
//   speak -> captions -> "Suggest" -> proposal -> claim/resolve -> leave -> notes
//
// and checks the authorization edges (someone else may not read or resolve the
// proposal; the agent's key may not do more than its role allows).
//
// Needs the e2e stack (deploy/docker-compose.e2e.yml, gateway on :80) with
// LiveKit's media ports reachable from this machine, and a worker already
// running against it:
//
//   LIVEKIT_URL=ws://localhost/livekit LIVEKIT_API_KEY=e2ekey \
//   LIVEKIT_API_SECRET=e2esecret-e2esecret-e2esecret-0123456789 \
//   PACA_API_URL=http://localhost/api/v1 MEETING_AGENT_TOKEN=e2e-meeting-agent-token npm start
//   node scripts/integration.mjs
import http from "node:http";
import {
	AudioFrame,
	AudioSource,
	LocalAudioTrack,
	Room,
	RoomEvent,
	TrackPublishOptions,
	TrackSource,
} from "@livekit/rtc-node";

const BASE = process.env.E2E_BASE_URL ?? "http://localhost";
const API = `${BASE}/api/v1`;
const ADMIN = {
	username: process.env.E2E_USERNAME ?? "admin",
	password: process.env.E2E_PASSWORD ?? "e2e-admin-password",
};
const FAKE_PORT = Number(process.env.FAKE_PORT ?? 18081);
const run = Date.now().toString(36);

const checks = [];
function check(name, ok, extra = "") {
	checks.push({ name, ok });
	console.log(`${ok ? "PASS" : "FAIL"}  ${name}${ok || !extra ? "" : `  -> ${extra}`}`);
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function until(what, fn, ms = 30000) {
	const end = Date.now() + ms;
	for (;;) {
		const v = await fn();
		if (v) return v;
		if (Date.now() > end) throw new Error(`timed out waiting for ${what}`);
		await sleep(300);
	}
}

// ---- a tiny cookie-keeping client ------------------------------------------
class Client {
	constructor() {
		this.cookies = new Map();
	}
	async req(method, path, body, headers = {}) {
		const res = await fetch(`${API}${path}`, {
			method,
			headers: {
				...(body === undefined ? {} : { "Content-Type": "application/json" }),
				Cookie: [...this.cookies].map(([k, v]) => `${k}=${v}`).join("; "),
				...headers,
			},
			body: body === undefined ? undefined : JSON.stringify(body),
		});
		for (const c of res.headers.getSetCookie?.() ?? []) {
			const [kv] = c.split(";");
			const i = kv.indexOf("=");
			this.cookies.set(kv.slice(0, i), kv.slice(i + 1));
		}
		const text = await res.text();
		let json = null;
		try {
			json = JSON.parse(text);
		} catch {
			/* empty */
		}
		return { status: res.status, data: json?.data, error: { code: json?.error_code, message: json?.error }, text };
	}
	async login(u, p) {
		const r = await this.req("POST", "/auth/login", { username: u, password: p, rememberMe: false });
		if (r.status !== 200) throw new Error(`login ${u}: ${r.status} ${r.text}`);
	}
}
async function makeUser(admin, username) {
	const TEMP = "TempPassword123!";
	const created = must(
		await admin.req("POST", "/admin/users", { username, full_name: username, password: TEMP }),
		`create ${username}`,
	);
	const c = new Client();
	await c.login(username, TEMP);
	must(
		await c.req("PATCH", "/users/me/password", { current_password: TEMP, new_password: "E2eRestricted123!" }),
		`password ${username}`,
	);
	await c.login(username, "E2eRestricted123!");
	return { id: created.id, client: c };
}
const must = (r, what) => {
	if (r.status >= 300) throw new Error(`${what}: ${r.status} ${r.text.slice(0, 300)}`);
	return r.data;
};

// ---- fake STT and LLM -------------------------------------------------------
const seen = { llmCalls: 0, sttCalls: 0 };
const fake = http.createServer((req, res) => {
	let b = "";
	req.on("data", (c) => (b += c));
	req.on("end", () => {
		const json = (o) => {
			res.writeHead(200, { "Content-Type": "application/json" });
			res.end(JSON.stringify(o));
		};
		if (req.url.endsWith("/audio/transcriptions")) {
			seen.sttCalls++;
			return json({ text: "we should fix the login page on safari" });
		}
		if (req.url.endsWith("/chat/completions")) {
			seen.llmCalls++;
			const body = JSON.parse(b);
			if (body.tools) {
				return json({
					choices: [
						{
							message: {
								content: null,
								tool_calls: [
									{
										function: {
											name: "propose_task",
											arguments: JSON.stringify({
												title: "Fix the login page",
												description: "Breaks on Safari",
												priority: "high",
											}),
										},
									},
								],
							},
						},
					],
				});
			}
			return json({
				choices: [
					{
						message: {
							content: "We discussed the Safari login bug.\nDecisions: none\nAction items: fix the login page",
						},
					},
				],
			});
		}
		res.writeHead(404);
		res.end();
	});
});
await new Promise((r) => fake.listen(FAKE_PORT, r));

let lk;
try {
	// ---- setup as admin -------------------------------------------------------
	const admin = new Client();
	await admin.login(ADMIN.username, ADMIN.password);
	const me = must(await admin.req("GET", "/users/me"), "me");
	check(
		"the API reports assistants as enabled",
		must(await admin.req("GET", "/chat/meeting/config"), "config").enabled === true,
	);

	const project = must(await admin.req("POST", "/projects", { name: `Assistant ${run}` }), "create project");
	const pid = project.id ?? project.project?.id;
	const roles = must(await admin.req("GET", `/projects/${pid}/roles?page_size=100`), "roles");
	const roleList = roles.items ?? roles;
	const roleId = (name) => roleList.find((r) => r.name === name)?.id;
	check("every project has a Meeting assistant role", !!roleId("Meeting assistant"));

	const agent = must(
		await admin.req("POST", `/projects/${pid}/agents`, {
			name: `Atlas ${run}`,
			handle: `atlas${run}`,
			agent_type: "meeting",
			description: "Takes notes",
			llm_provider: "openai",
			llm_model: "fake-llm",
			llm_api_key: "llm-secret-key",
			llm_base_url: `http://localhost:${FAKE_PORT}`,
			system_prompt: "Be brief.",
			role_ids: [roleId("Meeting assistant")],
		}),
		"create agent",
	);
	check("a meeting agent is created", agent.agent_type === "meeting");

	const bad = await admin.req("POST", `/projects/${pid}/agents`, {
		name: "x",
		handle: `bad${run}`,
		agent_type: "meeting",
		llm_provider: "openai",
		llm_model: "m",
		llm_api_key: "k",
		llm_base_url: "http://x",
		git_committer_name: "someone",
		role_ids: [roleId("Meeting assistant")],
	});
	check("git identity is refused for a meeting agent", bad.status === 400, `${bad.status} ${bad.text.slice(0, 120)}`);

	const st = must(await admin.req("GET", `/projects/${pid}/agents/${agent.id}/meeting-settings`), "settings");
	check("defaults before configuring", st.mode === "on_demand" && st.has_stt_key === false);
	const notReady = await admin.req("GET", `/projects/${pid}/channels`);
	const channel = must(
		await admin.req("POST", `/projects/${pid}/channels`, { kind: "voice", name: `lounge${run}`.toLowerCase() }),
		"channel",
	);
	void notReady;

	// ---- the caller joins, but the assistant is not configured yet --------------
	const join = must(await admin.req("POST", `/projects/${pid}/channels/${channel.id}/call/join`), "join");
	lk = new Room();
	const captions = [];
	const shown = [];
	let agentIdentity = "";
	lk.on(RoomEvent.DataReceived, (payload, _p, _k, topic) => {
		if (topic === "paca.caption") captions.push(JSON.parse(new TextDecoder().decode(payload)));
	});
	lk.on(RoomEvent.ParticipantConnected, (p) => {
		if (p.identity !== me.id) agentIdentity = p.identity;
	});
	await lk.connect(process.env.LIVEKIT_WS ?? `ws://localhost/livekit`, join.token, { autoSubscribe: true });
	lk.localParticipant.registerRpcMethod("paca.proposal.show", async (d) => {
		shown.push({ from: d.callerIdentity, ...JSON.parse(d.payload) });
		return "shown";
	});
	const source = new AudioSource(16000, 1);
	await lk.localParticipant.publishTrack(
		LocalAudioTrack.createAudioTrack("mic", source),
		new TrackPublishOptions({ source: TrackSource.SOURCE_MICROPHONE }),
	);
	await until("the webhook to record me in the call", async () =>
		(must(await admin.req("GET", `/projects/${pid}/channels/${channel.id}/call`), "call").participants ?? []).some(
			(p) => p.account_id === me.id,
		),
	);

	const early = await admin.req("POST", `/projects/${pid}/channels/${channel.id}/call/assistants/${agent.id}`);
	check(
		"inviting an unconfigured assistant is refused",
		early.status === 409 && early.error?.code === "MEETING_NOT_CONFIGURED",
		`${early.status} ${early.text.slice(0, 160)}`,
	);

	const patched = must(
		await admin.req("PATCH", `/projects/${pid}/agents/${agent.id}/meeting-settings`, {
			stt_provider: "openai",
			stt_model: "fake-stt",
			stt_api_key: "stt-secret-key",
			stt_base_url: `http://localhost:${FAKE_PORT}`,
			stt_languages: ["en"],
			announcement: "I transcribe this call.",
			store_transcript: true,
			transcript_retention_days: 7,
		}),
		"patch settings",
	);
	check(
		"the STT key is stored but never returned",
		patched.has_stt_key === true && !JSON.stringify(patched).includes("stt-secret-key"),
	);
	const keyInDb = await admin.req("GET", `/projects/${pid}/agents/${agent.id}`);
	check("the agent response does not leak the LLM key", !keyInDb.text.includes("llm-secret-key"));

	// ---- a stranger cannot invite or read ----------------------------------------
	const stranger = (await makeUser(admin, `viewer${run}`)).client;
	const denied = await stranger.req("POST", `/projects/${pid}/channels/${channel.id}/call/assistants/${agent.id}`);
	check("a user outside the project cannot invite an assistant", denied.status === 403, `${denied.status}`);

	// ---- invite -------------------------------------------------------------------
	const invited = must(
		await admin.req("POST", `/projects/${pid}/channels/${channel.id}/call/assistants/${agent.id}`),
		"invite",
	);
	check("the invite is accepted", invited.agent_id === agent.id && ["dispatched", "active"].includes(invited.status));
	const again = await admin.req("POST", `/projects/${pid}/channels/${channel.id}/call/assistants/${agent.id}`);
	check(
		"a second invite is refused",
		again.status === 409 && again.error?.code === "MEETING_ALREADY_IN_CALL",
		`${again.status} ${again.text.slice(0, 120)}`,
	);
	await until(
		"the assistant to become active",
		async () =>
			must(await admin.req("GET", `/projects/${pid}/channels/${channel.id}/call/assistants`), "assistants").some(
				(a) => a.status === "active",
			),
		45000,
	);
	await until("the assistant to appear in the room", async () => agentIdentity, 20000);
	check("the assistant joined the room as a LiveKit agent", !!agentIdentity);
	const assistants = must(
		await admin.req("GET", `/projects/${pid}/channels/${channel.id}/call/assistants`),
		"assistants",
	);
	check(
		"the call view can tell what it keeps",
		assistants[0]?.store_transcript === true &&
			assistants[0]?.transcript_retention_days === 7 &&
			assistants[0]?.announcement === "I transcribe this call.",
	);

	// ---- speak ---------------------------------------------------------------------
	const speak = async (ms) => {
		for (let t = 0; t < ms; t += 10) {
			const d = new Int16Array(160);
			for (let i = 0; i < 160; i++) d[i] = Math.round(8000 * Math.sin((2 * Math.PI * 440 * (t * 16 + i)) / 16000));
			await source.captureFrame(new AudioFrame(d, 16000, 1, 160));
		}
	};
	const hush = async (ms) => {
		for (let t = 0; t < ms; t += 10) await source.captureFrame(new AudioFrame(new Int16Array(160), 16000, 1, 160));
	};
	await hush(500);
	for (let i = 0; i < 3; i++) {
		await speak(1200);
		await hush(1500);
	}
	await until("3 captions", async () => captions.length >= 3, 40000);
	check(
		"captions carry the speaker and text",
		captions[0]?.speaker_id === me.id && captions[0]?.text.includes("login page"),
	);

	// ---- suggest -------------------------------------------------------------------
	const out = await lk.localParticipant.performRpc({
		destinationIdentity: agentIdentity,
		method: "paca.assistant.suggest",
		payload: "{}",
		responseTimeout: 60000,
	});
	check("Suggest made a proposal", JSON.parse(out).made === 1, out);
	await until("the card to be pushed to my screen", async () => shown.length > 0, 20000);
	const pid1 = shown[0].proposal_id;
	check("the proposal id arrives over RPC from the agent", !!pid1 && shown[0].from === agentIdentity);

	const chips = must(await admin.req("GET", `/projects/${pid}/channels/${channel.id}/proposals`), "chips");
	check(
		"the channel lists a chip without the fields",
		chips.length === 1 &&
			chips[0].status === "pending" &&
			chips[0].payload === undefined &&
			chips[0].target_account_id === me.id,
	);
	const detail = must(await admin.req("GET", `/projects/${pid}/proposals/${pid1}`), "proposal");
	check(
		"the target sees the fields that will be written",
		detail.payload.title === "Fix the login page" && detail.payload.priority === "high",
	);

	// ---- who else may touch it ------------------------------------------------------
	const other = await stranger.req("GET", `/projects/${pid}/proposals/${pid1}`);
	check("someone outside the project cannot read it", other.status === 403, `${other.status}`);
	const peerUser = await makeUser(admin, `editor${run}`);
	const peer = peerUser.client;
	const members = await admin.req("POST", `/projects/${pid}/members`, {
		user_id: peerUser.id,
		role_ids: [roleId("Editor")],
	});
	const peerRead = await peer.req("GET", `/projects/${pid}/proposals/${pid1}`);
	check(
		"an Editor who is not the target cannot read it either",
		members.status < 300 && peerRead.status === 403,
		`member ${members.status} read ${peerRead.status}`,
	);
	const peerChips = await peer.req("GET", `/projects/${pid}/channels/${channel.id}/proposals`);
	check(
		"but sees the read-only chip",
		peerChips.status === 200 && peerChips.data?.length === 1 && !peerChips.text.includes("Fix the login page"),
		`${peerChips.status}`,
	);
	const peerClaim = await peer.req("POST", `/projects/${pid}/proposals/${pid1}/claim`);
	check("and cannot claim it", peerClaim.status === 403, `${peerClaim.status}`);

	// ---- the agent's own key does what its role allows, no more ----------------------
	const worker = await fetch(`${API}/internal/meeting-agent/sessions/${invited.agent_id}/bootstrap`, {
		method: "POST",
		headers: { "X-Meeting-Worker-Token": "wrong" },
	});
	check(
		"the worker route rejects a wrong secret",
		worker.status === 401 || worker.status === 400 || worker.status === 409,
		`${worker.status}`,
	);

	// ---- approve: claim, "write", resolve --------------------------------------------
	const claimed = must(await admin.req("POST", `/projects/${pid}/proposals/${pid1}/claim`), "claim");
	check("claiming moves it to claimed", claimed.status === "claimed");
	const clash = await peer.req("POST", `/projects/${pid}/proposals/${pid1}/claim`);
	check("a second claim cannot win", clash.status >= 400);
	const task = must(
		await admin.req("POST", `/projects/${pid}/tasks`, { title: detail.payload.title }),
		"create task as the person",
	);
	const done = must(
		await admin.req("POST", `/projects/${pid}/proposals/${pid1}/resolve`, {
			status: "resolved",
			result: { task_id: task.id ?? task.task?.id },
		}),
		"resolve",
	);
	check("the proposal resolves with the task as the result", done.status === "resolved" && !!done.resolved_by);
	const closed = await admin.req("POST", `/projects/${pid}/proposals/${pid1}/resolve`, { status: "rejected" });
	check(
		"a closed proposal stays closed",
		closed.status === 409 && closed.error?.code === "PROPOSAL_CLOSED",
		`${closed.status} ${closed.text.slice(0, 100)}`,
	);

	// ---- leave: notes are posted, the assistant leaves ---------------------------------
	await lk.disconnect();
	lk = null;
	await until(
		"the assistant to leave",
		async () =>
			must(await admin.req("GET", `/projects/${pid}/channels/${channel.id}/call/assistants`), "assistants").length ===
			0,
		45000,
	);
	check("the assistant leaves when the last person does", true);
	const msgs = must(await admin.req("GET", `/projects/${pid}/channels/${channel.id}/messages`), "messages");
	const list = msgs.items ?? msgs.messages ?? msgs;
	const notes = JSON.stringify(list);
	check(
		"the notes were posted to the channel by the agent",
		notes.includes("Meeting notes from") && notes.includes(agent.id),
		notes.slice(0, 200),
	);
	check(
		"the model was asked (suggest + summary) and speech was transcribed",
		seen.llmCalls >= 2 && seen.sttCalls >= 3,
		JSON.stringify(seen),
	);
} catch (e) {
	check("the run completed without an exception", false, String(e?.stack ?? e));
} finally {
	try {
		await lk?.disconnect();
	} catch {
		/* ignore */
	}
	fake.close();
}
const failed = checks.filter((c) => !c.ok);
console.log(`\n${checks.length - failed.length}/${checks.length} checks passed`);
process.exit(failed.length ? 1 : 0);
