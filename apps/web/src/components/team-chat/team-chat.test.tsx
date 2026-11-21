import { describe, expect, it } from "vitest";

import "@/i18n";
import type { ChatMessage } from "@/lib/chat-api";
import { isFollowUp } from "./message-list";

const msg = (over: Partial<ChatMessage>): ChatMessage => ({
	id: "m",
	channel_id: "c",
	seq: 1,
	thread_root_id: null,
	author_id: "a",
	author_name: "A",
	content: [],
	edited_at: null,
	deleted_at: null,
	created_at: "2026-01-01T10:00:00Z",
	reply_count: 0,
	reactions: [],
	saved: false,
	following: false,
	...over,
});

describe("isFollowUp", () => {
	const first = msg({});
	it("groups the same author within five minutes", () => {
		expect(
			isFollowUp(first, msg({ id: "n", created_at: "2026-01-01T10:04:00Z" })),
		).toBe(true);
	});
	it("does not group another author, a long gap, the first message or after a deleted one", () => {
		expect(
			isFollowUp(
				first,
				msg({ author_id: "b", created_at: "2026-01-01T10:01:00Z" }),
			),
		).toBe(false);
		expect(isFollowUp(first, msg({ created_at: "2026-01-01T10:06:00Z" }))).toBe(
			false,
		);
		expect(isFollowUp(undefined, first)).toBe(false);
		expect(
			isFollowUp(
				msg({ deleted_at: "x" }),
				msg({ created_at: "2026-01-01T10:01:00Z" }),
			),
		).toBe(false);
	});
});
