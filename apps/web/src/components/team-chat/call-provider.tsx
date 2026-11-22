import { useQueryClient } from "@tanstack/react-query";
import { ParticipantKind, Room, RoomEvent } from "livekit-client";
import {
	createContext,
	type ReactNode,
	useCallback,
	useContext,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { callKeys, joinCall } from "@/lib/call-api";
import { DM_SCOPE } from "@/lib/chat-api";
import { type ChatErrorKey, chatErrorKey } from "@/lib/chat-errors";

/** The channel (or direct message) a call is in. */
export interface CallTarget {
	/** A project id, or DM_SCOPE for a direct message. */
	scope: string;
	channelId: string;
	channelName: string;
}

export type CallStatus = "idle" | "connecting" | "connected";

// The wire protocol with the meeting assistant's worker (services/meeting-agent).
export const TOPIC_CAPTION = "paca.caption";
export const RPC_SHOW = "paca.proposal.show";
export const RPC_RESOLVED = "paca.proposal.resolved";
export const RPC_SUGGEST = "paca.assistant.suggest";

/** A live caption: one finished turn of a participant's speech. */
export interface Caption {
	id: number;
	speakerId: string;
	text: string;
	at: string;
}

const MAX_CAPTIONS = 4;

/** The assistant in the room, if there is one. Only the LiveKit server marks a
 * participant as an agent (it is set from the token of a dispatched job), so a
 * person cannot pose as one. */
export function findAssistant(room: Room | null) {
	if (!room) return undefined;
	return Array.from(room.remoteParticipants.values()).find(
		(p) => p.kind === ParticipantKind.AGENT,
	);
}

interface CallContextValue {
	target: CallTarget | null;
	status: CallStatus;
	room: Room | null;
	/** An error key of the "chat" namespace's errors, from the last join. */
	error: ChatErrorKey | null;
	/** The browser refused the microphone or camera. */
	mediaBlocked: boolean;
	/** Bumps whenever the room's participants or tracks change, to re-render. */
	version: number;
	/** The assistant's latest live captions, newest last. */
	captions: Caption[];
	/** Ids of proposals an assistant asked this person to review, as they arrive. */
	arrivals: string[];
	/** Asks the assistant for suggestions from the discussion so far. */
	suggest: () => Promise<number>;
	/** Tells the assistant how a proposal ended (best effort; the API is the record). */
	reportResolved: (proposalId: string, status: string) => void;
	join: (target: CallTarget) => Promise<void>;
	leave: () => void;
	toggleMic: () => void;
	toggleCamera: () => void;
	toggleScreen: () => void;
}

const CallContext = createContext<CallContextValue | null>(null);

// What a hook gets outside a provider (a test, or a page rendered alone): no call.
const NO_CALL: CallContextValue = {
	target: null,
	status: "idle",
	room: null,
	error: null,
	mediaBlocked: false,
	version: 0,
	captions: [],
	arrivals: [],
	suggest: async () => 0,
	reportResolved: () => undefined,
	join: async () => undefined,
	leave: () => undefined,
	toggleMic: () => undefined,
	toggleCamera: () => undefined,
	toggleScreen: () => undefined,
};

export function useCall(): CallContextValue {
	return useContext(CallContext) ?? NO_CALL;
}

const ROOM_EVENTS = [
	RoomEvent.ParticipantConnected,
	RoomEvent.ParticipantDisconnected,
	RoomEvent.TrackSubscribed,
	RoomEvent.TrackUnsubscribed,
	RoomEvent.TrackMuted,
	RoomEvent.TrackUnmuted,
	RoomEvent.LocalTrackPublished,
	RoomEvent.LocalTrackUnpublished,
	RoomEvent.ActiveSpeakersChanged,
	RoomEvent.ParticipantNameChanged,
] as const;

/**
 * Holds the call of one call tab (routes/_authenticated/call). The call lives
 * in its own browser tab, so moving around the app in the main tab neither
 * ends it nor needs it to be kept somewhere above the routes.
 */
export function CallProvider({ children }: { children: ReactNode }) {
	const queryClient = useQueryClient();
	const [target, setTarget] = useState<CallTarget | null>(null);
	const [status, setStatus] = useState<CallStatus>("idle");
	const [error, setError] = useState<ChatErrorKey | null>(null);
	const [mediaBlocked, setMediaBlocked] = useState(false);
	const [version, setVersion] = useState(0);
	const [captions, setCaptions] = useState<Caption[]>([]);
	const [arrivals, setArrivals] = useState<string[]>([]);
	const captionId = useRef(0);
	const roomRef = useRef<Room | null>(null);
	const [room, setRoom] = useState<Room | null>(null);

	const refreshCalls = useCallback(
		(t: CallTarget) => {
			void queryClient.invalidateQueries({
				queryKey: callKeys.channel(t.scope, t.channelId),
			});
			if (t.scope !== DM_SCOPE) {
				void queryClient.invalidateQueries({
					queryKey: callKeys.project(t.scope),
				});
			}
		},
		[queryClient],
	);

	const teardown = useCallback(
		(t: CallTarget | null) => {
			const r = roomRef.current;
			roomRef.current = null;
			setRoom(null);
			setTarget(null);
			setStatus("idle");
			setMediaBlocked(false);
			setCaptions([]);
			setArrivals([]);
			if (r) void r.disconnect();
			if (t) refreshCalls(t);
		},
		[refreshCalls],
	);

	const targetRef = useRef<CallTarget | null>(null);
	targetRef.current = target;

	const join = useCallback(
		async (next: CallTarget) => {
			if (
				roomRef.current &&
				targetRef.current?.channelId === next.channelId &&
				targetRef.current.scope === next.scope
			) {
				return; // already in it
			}
			if (roomRef.current) teardown(targetRef.current);
			setError(null);
			setStatus("connecting");
			setTarget(next);
			const r = new Room({ adaptiveStream: true, dynacast: true });
			roomRef.current = r;
			const bump = () => setVersion((v) => v + 1);
			for (const ev of ROOM_EVENTS) r.on(ev, bump);
			r.on(RoomEvent.Disconnected, () => {
				// Ended by the server (removed, or the connection was lost for good).
				if (roomRef.current === r) teardown(next);
			});
			// Live captions from the assistant, on the data channel.
			r.on(RoomEvent.DataReceived, (payload, from, _kind, topic) => {
				if (topic !== TOPIC_CAPTION || from?.kind !== ParticipantKind.AGENT) {
					return;
				}
				try {
					const c = JSON.parse(new TextDecoder().decode(payload)) as {
						speaker_id?: string;
						text?: string;
						at?: string;
					};
					if (typeof c.text !== "string" || c.text === "") return;
					const next: Caption = {
						id: ++captionId.current,
						speakerId: String(c.speaker_id ?? ""),
						text: c.text.slice(0, 500),
						at: String(c.at ?? ""),
					};
					setCaptions((cur) => [...cur, next].slice(-MAX_CAPTIONS));
				} catch {
					// not a caption we understand
				}
			});
			try {
				const info = await joinCall(next.scope, next.channelId);
				await r.connect(info.url, info.token);
				// The assistant tells this browser a proposal is waiting. Anything
				// from a participant that is not an agent is ignored, and the
				// payload is only an id: the card is fetched from the API.
				r.localParticipant.registerRpcMethod(RPC_SHOW, async (data) => {
					const caller = r.remoteParticipants.get(data.callerIdentity);
					if (caller?.kind !== ParticipantKind.AGENT) return "ignored";
					try {
						const body = JSON.parse(data.payload) as { proposal_id?: string };
						const id = body.proposal_id;
						if (typeof id !== "string" || !/^[0-9a-f-]{36}$/i.test(id)) {
							return "ignored";
						}
						setArrivals((cur) => (cur.includes(id) ? cur : [...cur, id]));
						void queryClient.invalidateQueries({
							queryKey: ["meeting", next.scope],
						});
					} catch {
						return "ignored";
					}
					return "shown";
				});
				if (roomRef.current !== r) {
					void r.disconnect();
					return;
				}
				setRoom(r);
				setStatus("connected");
				refreshCalls(next);
				// Join with the microphone on, as in a voice channel. A refusal keeps
				// the person in the call, listening.
				try {
					await r.localParticipant.setMicrophoneEnabled(true);
				} catch {
					setMediaBlocked(true);
				}
				bump();
			} catch (e) {
				if (roomRef.current === r) {
					roomRef.current = null;
					setRoom(null);
					setTarget(null);
					setStatus("idle");
				}
				void r.disconnect();
				setError(chatErrorKey(e));
			}
		},
		[teardown, refreshCalls, queryClient],
	);

	const leave = useCallback(() => teardown(targetRef.current), [teardown]);

	const suggest = useCallback(async (): Promise<number> => {
		const r = roomRef.current;
		const agent = findAssistant(r);
		if (!r || !agent) return 0;
		const out = await r.localParticipant.performRpc({
			destinationIdentity: agent.identity,
			method: RPC_SUGGEST,
			payload: "{}",
			responseTimeout: 60_000,
		});
		try {
			return Number((JSON.parse(out) as { made?: number }).made ?? 0);
		} catch {
			return 0;
		}
	}, []);

	const reportResolved = useCallback((proposalId: string, status: string) => {
		const r = roomRef.current;
		const agent = findAssistant(r);
		if (!r || !agent) return;
		void r.localParticipant
			.performRpc({
				destinationIdentity: agent.identity,
				method: RPC_RESOLVED,
				payload: JSON.stringify({ proposal_id: proposalId, status }),
			})
			.catch(() => undefined);
	}, []);

	// Leaving the app (closing the tab) ends the call; LiveKit also notices.
	useEffect(() => {
		const onUnload = () => void roomRef.current?.disconnect();
		window.addEventListener("pagehide", onUnload);
		return () => {
			window.removeEventListener("pagehide", onUnload);
			void roomRef.current?.disconnect();
		};
	}, []);

	const toggle = useCallback((kind: "mic" | "camera" | "screen") => {
		const lp = roomRef.current?.localParticipant;
		if (!lp) return;
		const act =
			kind === "mic"
				? lp.setMicrophoneEnabled(!lp.isMicrophoneEnabled)
				: kind === "camera"
					? lp.setCameraEnabled(!lp.isCameraEnabled)
					: lp.setScreenShareEnabled(!lp.isScreenShareEnabled);
		act
			.then(() => setMediaBlocked(false))
			.catch(() => {
				// A cancelled screen-share picker is not a block.
				if (kind !== "screen") setMediaBlocked(true);
			})
			.finally(() => setVersion((v) => v + 1));
	}, []);

	const value = useMemo<CallContextValue>(
		() => ({
			target,
			status,
			room,
			error,
			mediaBlocked,
			version,
			captions,
			arrivals,
			suggest,
			reportResolved,
			join,
			leave,
			toggleMic: () => toggle("mic"),
			toggleCamera: () => toggle("camera"),
			toggleScreen: () => toggle("screen"),
		}),
		[
			target,
			status,
			room,
			error,
			mediaBlocked,
			version,
			captions,
			arrivals,
			suggest,
			reportResolved,
			join,
			leave,
			toggle,
		],
	);

	return <CallContext.Provider value={value}>{children}</CallContext.Provider>;
}
