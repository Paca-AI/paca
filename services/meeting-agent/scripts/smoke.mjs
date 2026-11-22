// A real-LiveKit smoke test of the worker with the Paca API, the transcription
// service and the LLM replaced by local fakes. It proves the LiveKit half:
// dispatch, job start, per-participant audio, captions on the data channel, the
// RPC that shows a proposal, the "Suggest" RPC, transcript flushes and the
// notes summary. It does NOT prove a real STT/LLM provider.
//
// Needs a LiveKit server and a running worker:
//   LIVEKIT_URL=ws://localhost:3000/livekit LIVEKIT_API_KEY=... LIVEKIT_API_SECRET=... \
//   PACA_API_URL=http://localhost:18080/api/v1 MEETING_AGENT_TOKEN=smoke node dist/index.js start
//   node scripts/smoke.mjs        (same LIVEKIT_* env)
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
import { AccessToken } from "livekit-server-sdk";

const url = process.env.LIVEKIT_URL ?? "ws://localhost:3000/livekit";
const key = process.env.LIVEKIT_API_KEY ?? "devkey";
const secret = process.env.LIVEKIT_API_SECRET ?? "devsecret-devsecret-devsecret-0123456789";
const apiPort = Number(process.env.FAKE_API_PORT ?? 18080);
const room = `smoke-${Date.now()}`;
const session = crypto.randomUUID();
const human = "22222222-2222-4222-8222-222222222222";

const seen = { transcript: [], proposals: [], posted: [], finished: 0, bootstrap: 0 };
const json = (res, status, body) => {
	res.writeHead(status, { "Content-Type": "application/json" });
	res.end(JSON.stringify(body));
};
const readBody = (req) =>
	new Promise((r) => {
		let b = "";
		req.on("data", (c) => (b += c));
		req.on("end", () => r(b));
	});

const fake = http.createServer(async (req, res) => {
	const body = await readBody(req);
	const path = req.url ?? "";
	// A job left over from an earlier run talks to this server too: ignore it.
	if (path.includes("/sessions/") && !path.includes(session))
		return json(res, 410, { error: { code: "STALE", message: "other session" } });
	if (path.endsWith("/bootstrap")) {
		seen.bootstrap++;
		return json(res, 200, {
			data: {
				session_id: session,
				project_id: "p1",
				channel_id: room,
				call_id: "k1",
				agent_id: "agent-1",
				agent_name: "Atlas",
				requester_id: human,
				host_id: human,
				instructions: "",
				api_key: "paca_fake",
				expires_at: "",
				llm: { provider: "openai", model: "fake", api_key: "x", base_url: `http://localhost:${apiPort}/llm` },
				stt: {
					provider: "openai",
					model: "fake",
					api_key: "x",
					base_url: `http://localhost:${apiPort}/stt`,
					languages: [],
				},
				behavior: {
					mode: "on_demand",
					suggest_interval_seconds: 60,
					proposal_kinds: ["task.create"],
					max_proposals_per_call: 5,
					store_transcript: true,
					announcement: "",
					summary_target: "channel_message",
				},
			},
		});
	}
	if (path.endsWith("/stt/audio/transcriptions")) return json(res, 200, { text: "we should fix the login page" });
	if (path.endsWith("/transcript") && req.method === "POST") {
		seen.transcript.push(...JSON.parse(body).segments);
		return json(res, 204, {});
	}
	if (path.endsWith("/finish")) {
		seen.finished++;
		return json(res, 204, {});
	}
	if (path.endsWith("/proposals")) {
		const b = JSON.parse(body);
		seen.proposals.push(b);
		return json(res, 200, {
			data: { id: "prop-1", status: "pending", target_account_id: b.target_id ?? human, kind: b.kind },
		});
	}
	if (path.endsWith("/messages")) {
		seen.posted.push(JSON.parse(body));
		return json(res, 200, { data: {} });
	}
	if (path.endsWith("/stt/audio/transcriptions")) return json(res, 200, { text: "we should fix the login page" });
	if (path.endsWith("/llm/chat/completions")) {
		const b = JSON.parse(body);
		if (b.tools)
			return json(res, 200, {
				choices: [
					{
						message: {
							content: null,
							tool_calls: [
								{ function: { name: "propose_task", arguments: JSON.stringify({ title: "Fix the login page" }) } },
							],
						},
					},
				],
			});
		return json(res, 200, {
			choices: [{ message: { content: "We discussed the login page.\nDecisions: none\nAction items: fix login" } }],
		});
	}
	json(res, 404, { error: { code: "NOT_FOUND", message: path } });
});
await new Promise((r) => fake.listen(apiPort, r));

const fail = (msg) => {
	console.error("SMOKE FAILED:", msg, JSON.stringify(seen));
	process.exit(1);
};
const timer = setTimeout(() => fail("timed out"), 90_000);

const token = await new AccessToken(key, secret, { identity: human, name: "Alice" });
token.addGrant({ roomJoin: true, room, canPublish: true, canSubscribe: true, canPublishData: true });
const lk = new Room();
const captions = [];
const shown = [];
let agentIdentity = "";
lk.on(RoomEvent.DataReceived, (payload, _p, _k, topic) => {
	if (topic === "paca.caption") captions.push(JSON.parse(new TextDecoder().decode(payload)));
});
lk.on(RoomEvent.ParticipantConnected, (p) => {
	if (p.identity !== human) agentIdentity = p.identity;
});
await lk.connect(url, await token.toJwt(), { autoSubscribe: true, dynacast: false });
lk.localParticipant.registerRpcMethod("paca.proposal.show", async (d) => {
	shown.push({ from: d.callerIdentity, ...JSON.parse(d.payload) });
	return "shown";
});

const source = new AudioSource(16000, 1);
const track = LocalAudioTrack.createAudioTrack("mic", source);
await lk.localParticipant.publishTrack(track, new TrackPublishOptions({ source: TrackSource.SOURCE_MICROPHONE }));

// The API does this with the same call (internal/platform/livekit DispatchAgent).
const admin = new AccessToken(key, secret, { ttl: 60 });
admin.addGrant({ roomAdmin: true, room });
const dres = await fetch(`${url.replace(/^ws/, "http")}/twirp/livekit.AgentDispatchService/CreateDispatch`, {
	method: "POST",
	headers: { "Content-Type": "application/json", Authorization: `Bearer ${await admin.toJwt()}` },
	body: JSON.stringify({ room, agent_name: "paca-meeting", metadata: JSON.stringify({ v: 1, session }) }),
});
if (!dres.ok) fail(`dispatch ${dres.status} ${await dres.text()}`);
console.log("dispatched; waiting for the assistant to join");

for (let i = 0; i < 60 && !agentIdentity; i++) await new Promise((r) => setTimeout(r, 500));
if (!agentIdentity) fail("the assistant never joined");
const agent = [...lk.remoteParticipants.values()].find((p) => p.identity === agentIdentity);
console.log(
	"assistant joined as",
	agentIdentity,
	"kind",
	agent?.kind,
	"name",
	agent?.name,
	"metadata",
	agent?.metadata,
);

async function speak(ms) {
	const n = 160; // 10 ms
	for (let t = 0; t < ms; t += 10) {
		const d = new Int16Array(n);
		for (let i = 0; i < n; i++) d[i] = Math.round(8000 * Math.sin((2 * Math.PI * 440 * (t * 16 + i)) / 16000));
		await source.captureFrame(new AudioFrame(d, 16000, 1, n));
	}
}
async function silence(ms) {
	for (let t = 0; t < ms; t += 10) await source.captureFrame(new AudioFrame(new Int16Array(160), 16000, 1, 160));
}
await silence(500);
for (let turn = 0; turn < 3; turn++) {
	await speak(1200);
	await silence(1500);
}
for (let i = 0; i < 60 && captions.length < 3; i++) await new Promise((r) => setTimeout(r, 250));
if (captions.length < 3) fail(`expected 3 captions, got ${captions.length}`);
console.log("caption:", captions[0]);

const res = await lk.localParticipant.performRpc({
	destinationIdentity: agentIdentity,
	method: "paca.assistant.suggest",
	payload: "{}",
	responseTimeout: 30000,
});
console.log("suggest ->", res);
for (let i = 0; i < 40 && shown.length === 0; i++) await new Promise((r) => setTimeout(r, 250));
if (shown.length === 0) fail("the proposal was never shown to the reviewer");
console.log("proposal shown:", shown[0]);

await lk.disconnect();
for (let i = 0; i < 60 && seen.finished === 0; i++) await new Promise((r) => setTimeout(r, 500));
clearTimeout(timer);
if (seen.finished === 0) fail("the worker never reported the end of the session");
if (seen.transcript.length === 0 || seen.transcript[0].speaker_id !== human)
	fail("transcript not flushed for the speaker");
if (seen.proposals[0]?.target_id !== human || seen.proposals[0]?.kind !== "task.create")
	fail("proposal not filed for the asker");
if (seen.posted.length === 0) fail("no notes summary posted");
console.log(
	"SMOKE OK",
	JSON.stringify({ transcript: seen.transcript.length, proposals: seen.proposals.length, posted: seen.posted.length }),
);
fake.close();
process.exit(0);
