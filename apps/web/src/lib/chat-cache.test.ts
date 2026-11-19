import { describe, expect, it } from "vitest";
import type { ChatMessage } from "./chat-api";
import {
	EMPTY_WINDOW,
	firstSeq,
	lastSeq,
	mergeMessages,
	replaceMessage,
	toggleReaction,
	withNewest,
	withOlder,
} from "./chat-cache";

const msg = (seq: number, over: Partial<ChatMessage> = {}): ChatMessage => ({
	id: `m${seq}`,
	channel_id: "c",
	seq,
	thread_root_id: null,
	author_id: "u",
	author_name: "U",
	content: [],
	edited_at: null,
	deleted_at: null,
	created_at: "2026-01-01T00:00:00Z",
	reply_count: 0,
	reactions: [],
	saved: false,
	following: false,
	...over,
});

describe("mergeMessages", () => {
	it("keeps messages in seq order and lets the newer copy of an id win", () => {
		const merged = mergeMessages(
			[msg(1), msg(3)],
			[msg(2), msg(3, { edited_at: "x" })],
		);
		expect(merged.map((m) => m.seq)).toEqual([1, 2, 3]);
		expect(merged[2]?.edited_at).toBe("x");
	});

	it("returns the same array when there is nothing to merge", () => {
		const a = [msg(1)];
		expect(mergeMessages(a, [])).toBe(a);
	});
});

describe("windows", () => {
	it("replaces on a fresh load and appends on a catch-up", () => {
		const first = withNewest(
			undefined,
			{ items: [msg(5), msg(6)], has_more: true },
			true,
		);
		expect(first).toEqual({ items: [msg(5), msg(6)], hasOlder: true });
		const caught = withNewest(
			first,
			{ items: [msg(7)], has_more: false },
			false,
		);
		expect(caught.items.map((m) => m.seq)).toEqual([5, 6, 7]);
		// A catch-up page says nothing about older messages.
		expect(caught.hasOlder).toBe(true);
	});

	it("takes hasOlder from the older page", () => {
		const w = withOlder(
			{ items: [msg(5)], hasOlder: true },
			{ items: [msg(3), msg(4)], has_more: false },
		);
		expect(w.items.map((m) => m.seq)).toEqual([3, 4, 5]);
		expect(w.hasOlder).toBe(false);
	});

	it("reports the ends, 0 when empty", () => {
		expect(lastSeq(undefined)).toBe(0);
		expect(firstSeq(EMPTY_WINDOW)).toBe(0);
		const w = { items: [msg(2), msg(9)], hasOlder: false };
		expect([firstSeq(w), lastSeq(w)]).toEqual([2, 9]);
	});

	it("replaces a message only if it is already held", () => {
		const w = { items: [msg(1)], hasOlder: false };
		expect(
			replaceMessage(w, msg(1, { reply_count: 2 }))?.items[0]?.reply_count,
		).toBe(2);
		expect(replaceMessage(w, msg(8))).toBe(w);
		expect(replaceMessage(undefined, msg(1))).toBeUndefined();
	});
});

describe("toggleReaction", () => {
	it("adds, counts and removes my reaction", () => {
		let m = msg(1);
		m = toggleReaction(m, "👍", true);
		expect(m.reactions).toEqual([{ emoji: "👍", count: 1, mine: true }]);
		m = toggleReaction(m, "👍", false);
		expect(m.reactions).toEqual([]);
	});

	it("joins someone else's reaction and leaves it when I take mine back", () => {
		const base = msg(1, {
			reactions: [{ emoji: "🎉", count: 1, mine: false }],
		});
		const on = toggleReaction(base, "🎉", true);
		expect(on.reactions[0]).toEqual({ emoji: "🎉", count: 2, mine: true });
		expect(toggleReaction(on, "🎉", false).reactions[0]).toEqual({
			emoji: "🎉",
			count: 1,
			mine: false,
		});
	});

	it("does nothing when the state already matches", () => {
		const m = msg(1, { reactions: [{ emoji: "👀", count: 1, mine: true }] });
		expect(toggleReaction(m, "👀", true)).toBe(m);
	});
});
