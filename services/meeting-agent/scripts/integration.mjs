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

	const noListening = await admin.req("POST", `/projects/${pid}/agents`, {
		name: `NoStt ${run}`,
		handle: `nostt${run}`,
		agent_type: "meeting",
		llm_provider: "openai",
		llm_model: "m",
		llm_api_key: "k",
		llm_base_url: "http://x",
		role_ids: [roleId("Meeting assistant")],
	});
	check(
		"a meeting agent cannot be created without its transcription settings",
		noListening.status === 400,
		`${noListening.status}`,
	);
	const halfListening = await admin.req("POST", `/projects/${pid}/agents`, {
		name: `Half ${run}`,
		handle: `half${run}`,
		agent_type: "meeting",
		llm_provider: "openai",
		llm_model: "m",
		llm_api_key: "k",
		llm_base_url: "http://x",
		role_ids: [roleId("Meeting assistant")],
		meeting_settings: { stt_provider: "openai", stt_model: "whisper-1" },
	});
	check("a provider and model alone are not enough", halfListening.status === 400, `${halfListening.status}`);
	// Transcription alone needs no model at all.
	const minimal = await admin.req("POST", `/projects/${pid}/agents`, {
		name: `Mini ${run}`,
		handle: `mini${run}`,
		agent_type: "meeting",
		role_ids: [roleId("Meeting assistant")],
		meeting_settings: { stt_provider: "openai", stt_model: "whisper-1", stt_api_key: "k" },
	});
	check(
		"a transcription-only agent is created without any model settings",
		minimal.status < 300 && minimal.data?.agent_type === "meeting",
		`${minimal.status} ${minimal.text.slice(0, 160)}`,
	);
	const mset = must(
		await admin.req("GET", `/projects/${pid}/agents/${minimal.data?.id}/meeting-settings`),
		"minimal settings",
	);
	check("it transcribes only by default", mset.mode === "notes_only" && mset.summary_target === "none");
	for (const [what, patch] of [
		["suggestions", { mode: "on_demand" }],
		["notes", { summary_target: "channel_message" }],
	]) {
		const r = await admin.req("PATCH", `/projects/${pid}/agents/${minimal.data?.id}/meeting-settings`, patch);
		check(
			`turning on ${what} needs the model first`,
			r.status === 400 && r.error?.code === "MEETING_MODEL_REQUIRED",
			`${r.status} ${r.text.slice(0, 120)}`,
		);
	}
	const needsModel = await admin.req("POST", `/projects/${pid}/agents`, {
		name: `NeedsModel ${run}`,
		handle: `needs${run}`,
		agent_type: "meeting",
		role_ids: [roleId("Meeting assistant")],
		meeting_settings: {
			stt_provider: "openai",
			stt_model: "whisper-1",
			stt_api_key: "k",
			summary_target: "channel_message",
		},
	});
	check("notes at creation need the model", needsModel.status === 400, `${needsModel.status}`);
	const leftovers = must(await admin.req("GET", `/projects/${pid}/agents`), "agents");
	check(
		"the refused requests left no agent behind",
		(leftovers.items ?? leftovers).every((a) => !/^(NoStt|Half|NeedsModel) /.test(a.name)),
	);

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
			// How it transcribes is part of creating it.
			meeting_settings: {
				stt_provider: "openai",
				stt_model: "fake-stt",
				stt_api_key: "stt-secret-key",
				stt_base_url: `http://localhost:${FAKE_PORT}`,
				stt_languages: ["en"],
				// Suggestions and notes are options that use the model; this run turns both on.
				mode: "on_demand",
				summary_target: "channel_message",
			},
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
	check(
		"it listens as configured at creation, with defaults for the rest",
		st.mode === "on_demand" && st.has_stt_key === true && st.stt_provider === "openai" && st.stt_model === "fake-stt",
	);
	const notReady = await admin.req("GET", `/projects/${pid}/channels`);
	const channel = must(
		await admin.req("POST", `/projects/${pid}/channels`, { kind: "voice", name: `lounge${run}`.toLowerCase() }),
		"channel",
	);
	void notReady;

	// ---- the caller joins -------------------------------------------------------
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

	const patched = must(
		await admin.req("PATCH", `/projects/${pid}/agents/${agent.id}/meeting-settings`, {
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
	const unlisten = await admin.req("PATCH", `/projects/${pid}/agents/${agent.id}/meeting-settings`, { stt_model: "" });
	check("it cannot be edited into one that cannot listen", unlisten.status === 400, `${unlisten.status}`);
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

	// ---- recording -------------------------------------------------------------------
	const features = must(await admin.req("GET", "/chat/calls/config"), "call config");
	if (features.recording) {
		const base = `/projects/${pid}/channels/${channel.id}`;
		const denied = await peer.req("POST", `${base}/call/recording`);
		check("an Editor without calls:record cannot start a recording", denied.status === 403, `${denied.status}`);
		const started = must(await admin.req("POST", `${base}/call/recording`), "start recording");
		check("the admin starts a recording", started.status === "starting" || started.status === "recording");
		const dup = await admin.req("POST", `${base}/call/recording`);
		check(
			"a second recording is refused",
			dup.status === 409 && dup.error?.code === "RECORDING_ACTIVE",
			`${dup.status} ${dup.text.slice(0, 100)}`,
		);
		const during = must(await admin.req("GET", `${base}/call`), "call");
		check("everyone in the call can see it is being recorded", during.recording?.id === started.id);
		await until(
			"the egress to be active",
			async () =>
				must(await admin.req("GET", `${base}/recordings`), "recordings").find((r) => r.id === started.id)?.status ===
				"recording",
			60000,
		);
		await speak(3000);
		await hush(1000);
		const none = await peer.req("DELETE", `${base}/call/recording`);
		check("an Editor cannot stop it either", none.status === 403, `${none.status}`);
		const stopped = await admin.req("DELETE", `${base}/call/recording`);
		check("the admin stops it", stopped.status === 204, `${stopped.status}`);
		const done = await until(
			"the recording to complete",
			async () => {
				const r = must(await admin.req("GET", `${base}/recordings`), "recordings").find((x) => x.id === started.id);
				return r?.status === "complete" || r?.status === "failed" ? r : null;
			},
			120000,
		);
		check(
			"the recording completes with a file",
			done.status === "complete" && done.size_bytes > 0 && done.duration_ms > 0,
			JSON.stringify(done),
		);
		const link = must(await admin.req("GET", `${base}/recordings/${started.id}/url`), "recording url");
		const file = await fetch(link.url);
		const bytes = Buffer.from(await file.arrayBuffer());
		check(
			"the link serves an MP4",
			file.ok && bytes.subarray(4, 8).toString() === "ftyp",
			`${file.status} ${bytes.length}`,
		);
		const dl = must(await admin.req("GET", `${base}/recordings/${started.id}/url?download=1`), "download url");
		check(
			"a download link forces a save",
			(await fetch(dl.url, { headers: { Range: "bytes=0-1" } })).headers
				.get("content-disposition")
				?.includes("attachment") === true,
		);
		const outsider = await stranger.req("GET", `${base}/recordings/${started.id}/url`);
		check("someone outside the project cannot get it", outsider.status === 403, `${outsider.status}`);
		const hist = must(await admin.req("GET", `${base}/meetings`), "meetings");
		check(
			"the call is in the meeting history",
			hist.some((m) => m.call_id === done.call_id),
		);
	} else {
		console.log("SKIP  recording (the API says it is not enabled)");
	}

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
	// ---- transcripts people can read ---------------------------------------------------
	const tbase = `/projects/${pid}/channels/${channel.id}`;
	const meetings = must(await admin.req("GET", `${tbase}/meetings`), "meetings");
	check(
		"the call is listed with its transcript",
		meetings.length >= 1 && meetings[0].segments >= 3 && meetings[0].speakers.length >= 1,
		JSON.stringify(meetings),
	);
	const lines = must(await admin.req("GET", `${tbase}/meetings/${meetings[0].call_id}/transcript`), "transcript");
	check(
		"the transcript names who said what",
		lines.length >= 3 && lines[0].speaker_name !== "" && lines[0].text.includes("login page"),
		JSON.stringify(lines[0]),
	);
	const txt = await fetch(`${API}${tbase}/meetings/${meetings[0].call_id}/transcript?format=txt`, {
		headers: { Cookie: [...admin.cookies].map(([k, v]) => `${k}=${v}`).join("; ") },
	});
	const body = await txt.text();
	check(
		"it can be saved as a text file",
		txt.headers.get("content-disposition")?.includes("attachment") && body.includes("login page"),
		body.slice(0, 80),
	);
	check(
		"a stranger cannot read it",
		(await stranger.req("GET", `${tbase}/meetings/${meetings[0].call_id}/transcript`)).status === 403,
	);
	check(
		"another channel's call id is not found",
		(await admin.req("GET", `${tbase}/meetings/${crypto.randomUUID()}/transcript`)).status === 404,
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
