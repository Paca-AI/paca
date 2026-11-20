import { beforeEach, describe, expect, it, vi } from "vitest";

type Handler = (...args: unknown[]) => void;

const socket = vi.hoisted(() => {
	const handlers = new Map<string, Handler>();
	const emitted: Array<[string, unknown]> = [];
	let joinReply: unknown;
	return {
		handlers,
		emitted,
		connected: true,
		setJoinReply(v: unknown) {
			joinReply = v;
		},
		on(ev: string, fn: Handler) {
			handlers.set(ev, fn);
		},
		emit(ev: string, payload: unknown) {
			emitted.push([ev, payload]);
		},
		timeout() {
			return {
				emit: (ev: string, payload: unknown, cb: Handler) => {
					emitted.push([ev, payload]);
					cb(null, joinReply);
				},
			};
		},
	};
});

vi.mock("./socket-client", () => ({ getSocket: () => socket }));

import {
	getLiveState,
	joinLive,
	resetLivePresence,
	setLiveTyping,
	setTypingBroadcastEnabled,
	TYPING_REFRESH_MS,
} from "./live-presence";

const ref = { kind: "task" as const, projectId: "p", id: "t" };

beforeEach(() => {
	resetLivePresence();
	socket.emitted.length = 0;
	socket.handlers.clear();
	socket.connected = true;
	setTypingBroadcastEnabled(true);
	socket.setJoinReply({
		ok: true,
		snapshot: { viewers: ["u1"], typing: [] },
		canType: true,
	});
});

describe("live presence", () => {
	it("joins once per room, applies pushes, and leaves with the last user", () => {
		const leaveA = joinLive(ref);
		const leaveB = joinLive(ref);
		expect(socket.emitted.filter(([e]) => e === "live:join")).toHaveLength(1);
		expect(getLiveState(ref)).toMatchObject({ viewers: ["u1"], joined: true });

		socket.handlers.get("live:presence")?.({
			kind: "task",
			id: "t",
			viewers: ["u1", "u2"],
			typing: ["u2"],
		});
		expect(getLiveState(ref).typing).toEqual(["u2"]);

		leaveA();
		expect(socket.emitted.some(([e]) => e === "live:leave")).toBe(false);
		leaveB();
		expect(socket.emitted.some(([e]) => e === "live:leave")).toBe(true);
	});

	it("throttles typing and sends a stop", () => {
		joinLive(ref);
		setLiveTyping(ref, true, 0);
		setLiveTyping(ref, true, 1000);
		setLiveTyping(ref, true, TYPING_REFRESH_MS + 1);
		setLiveTyping(ref, false);
		setLiveTyping(ref, false);
		const typing = socket.emitted.filter(([e]) => e === "live:typing");
		expect(typing.map(([, p]) => (p as { typing: boolean }).typing)).toEqual([
			true,
			true,
			false,
		]);
	});

	it("sends nothing for a read-only viewer or when broadcast is off", () => {
		socket.setJoinReply({
			ok: true,
			snapshot: { viewers: [], typing: [] },
			canType: false,
		});
		joinLive(ref);
		setLiveTyping(ref, true, 0);
		expect(socket.emitted.some(([e]) => e === "live:typing")).toBe(false);

		resetLivePresence();
		socket.setJoinReply({
			ok: true,
			snapshot: { viewers: [], typing: [] },
			canType: true,
		});
		joinLive(ref);
		setTypingBroadcastEnabled(false);
		setLiveTyping(ref, true, 0);
		expect(socket.emitted.some(([e]) => e === "live:typing")).toBe(false);
	});
});
