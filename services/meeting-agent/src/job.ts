import type { JobContext } from "@livekit/agents";
import {
	AudioStream,
	ParticipantKind,
	type RemoteParticipant,
	type RemoteTrack,
	RoomEvent,
	TrackKind,
} from "@livekit/rtc-node";
import { Assistant, type Logger, type Transport } from "./assistant.ts";
import { Segmenter } from "./audio.ts";
import type { Config } from "./config.ts";
import { makeChat } from "./llm.ts";
import { PacaClient } from "./paca.ts";
import { makeTranscriber } from "./stt.ts";

const SAMPLE_RATE = 16_000;

// The wire protocol with the browser (see docs/architecture/chat-and-meetings.md).
export const TOPIC_CAPTION = "paca.caption";
export const RPC_SHOW = "paca.proposal.show";
export const RPC_RESOLVED = "paca.proposal.resolved";
export const RPC_SUGGEST = "paca.assistant.suggest";

const enc = new TextEncoder();

function logger(session: string): Logger {
	const line = (level: string, msg: string, extra?: object) =>
		console.log(JSON.stringify({ level, msg, session, ...extra }));
	return { info: (m, e) => line("info", m, e), warn: (m, e) => line("warn", m, e) };
}

/** Runs one job: the assistant's stay in one call. The dispatch metadata holds
 * the id of the call_agent_sessions row and nothing else; everything the job
 * needs, secrets included, comes from the API for that id. */
export async function runJob(ctx: JobContext, cfg: Config): Promise<void> {
	let meta: { v?: number; session?: string } = {};
	try {
		meta = JSON.parse(ctx.job.metadata || "{}");
	} catch {
		// handled below
	}
	if (!meta.session) {
		console.log(JSON.stringify({ level: "warn", msg: "dispatch without a session id; leaving" }));
		return;
	}
	const log = logger(meta.session);
	const paca = new PacaClient(cfg);
	const boot = await paca.bootstrap(meta.session);

	await ctx.connect();
	const room = ctx.room;
	const me = room.localParticipant;
	if (!me) throw new Error("not connected");
	// The call UI tells assistants apart by participant kind (set by the LiveKit
	// server for dispatched agents, so a person cannot fake it); this is for display.
	await me.updateMetadata(JSON.stringify({ kind: "agent", agent_id: boot.agent_id, name: boot.agent_name }));
	await me.updateName(boot.agent_name);

	const names = new Map<string, string>();
	const remember = (p: RemoteParticipant) => names.set(p.identity, p.name || "");
	for (const p of room.remoteParticipants.values()) remember(p);

	const transport: Transport = {
		nameOf: (id) => names.get(id) ?? "",
		caption: (c) => me.publishData(enc.encode(JSON.stringify(c)), { reliable: true, topic: TOPIC_CAPTION }),
		showProposal: async (target, id, summary) => {
			await me.performRpc({
				destinationIdentity: target,
				method: RPC_SHOW,
				payload: JSON.stringify({ proposal_id: id, summary }),
				responseTimeout: 10_000,
			});
		},
	};

	// The language model is for notes and suggestions only; transcription alone
	// needs just the transcription provider.
	const usesModel = boot.behavior.mode !== "notes_only" || boot.behavior.summary_target !== "none";
	const assistant = new Assistant(
		boot,
		paca,
		usesModel ? makeChat(boot.llm) : null,
		makeTranscriber(boot.stt),
		transport,
		log,
	);
	assistant.start();

	// A person asks for suggestions ("Suggest tasks" in the call view).
	me.registerRpcMethod(RPC_SUGGEST, async (data) => {
		const made = await assistant.suggest(data.callerIdentity);
		return JSON.stringify({ made });
	});
	// The reviewer's browser reports the outcome. The API row is the source of
	// truth; this only lets the log say what happened.
	me.registerRpcMethod(RPC_RESOLVED, async (data) => {
		log.info("proposal resolved", { by: data.callerIdentity, payload: data.payload.slice(0, 200) });
		return "ok";
	});

	const listening = new Map<string, AbortController>();
	const listen = (track: RemoteTrack, p: RemoteParticipant) => {
		if (track.kind !== TrackKind.KIND_AUDIO || listening.has(p.identity)) return;
		remember(p);
		const stop = new AbortController();
		listening.set(p.identity, stop);
		void (async () => {
			const seg = new Segmenter({ sampleRate: SAMPLE_RATE });
			const reader = new AudioStream(track, SAMPLE_RATE, 1).getReader();
			try {
				while (!stop.signal.aborted) {
					const { value, done } = await reader.read();
					if (done) break;
					const u = seg.push(value.data);
					if (u) void assistant.hear(p.identity, u);
				}
			} catch (err) {
				log.warn("audio stream ended with an error", { speaker: p.identity, err: String(err) });
			} finally {
				const u = seg.flush();
				if (u) await assistant.hear(p.identity, u);
				reader.releaseLock();
				listening.delete(p.identity);
			}
		})();
	};
	room.on(RoomEvent.TrackSubscribed, (track, _pub, p) => listen(track, p));
	room.on(RoomEvent.ParticipantConnected, remember);
	// Tracks that were already published, and subscribed, before this handler
	// existed raise no event: pick them up now.
	for (const p of room.remoteParticipants.values()) {
		for (const pub of p.trackPublications.values()) if (pub.track) listen(pub.track, p);
	}
	room.on(RoomEvent.TrackUnsubscribed, (track, _pub, p) => {
		if (track.kind === TrackKind.KIND_AUDIO) listening.get(p.identity)?.abort();
	});

	// The assistant has nothing to do in a call with nobody in it: leave when the
	// last person does (LiveKit would otherwise keep the room open for it).
	let done: () => void = () => undefined;
	const left = new Promise<void>((resolve) => {
		done = resolve;
	});
	room.on(RoomEvent.ParticipantDisconnected, () => {
		const people = [...room.remoteParticipants.values()].filter((p) => p.kind !== ParticipantKind.AGENT);
		if (people.length === 0) done();
	});
	room.on(RoomEvent.Disconnected, () => done());
	ctx.addShutdownCallback(async () => {
		for (const s of listening.values()) s.abort();
		await assistant.finish();
		done();
	});
	log.info("assistant listening", { channel: boot.channel_id, mode: boot.behavior.mode });
	await left;
	// The entrypoint must return before LiveKit runs shutdown callbacks, so the
	// wrap-up happens here; the callback above is only a backstop.
	for (const s of listening.values()) s.abort();
	await assistant.finish();
	ctx.shutdown("the call is over");
}
