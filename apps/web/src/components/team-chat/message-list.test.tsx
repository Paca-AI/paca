import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import "@/i18n";
import type { ChatMessage } from "@/lib/chat-api";

vi.mock("@tanstack/react-router", () => ({ Link: () => null }));

import { MessageList } from "./message-list";

const at = (day: number, h: number, m = 0) =>
	new Date(2026, 10, day, h, m).toISOString();

const msg = (o: Partial<ChatMessage>): ChatMessage => ({
	id: "m",
	channel_id: "c",
	seq: 1,
	thread_root_id: null,
	author_id: "other",
	author_name: "Other",
	content: [{ type: "paragraph", content: [{ type: "text", text: "x" }] }],
	edited_at: null,
	deleted_at: null,
	created_at: at(10, 9),
	reply_count: 0,
	reactions: [],
	saved: false,
	following: false,
	...o,
});

function list(items: ChatMessage[], readSeq?: number) {
	const qc = new QueryClient();
	return render(
		<QueryClientProvider client={qc}>
			<MessageList
				items={items}
				hasOlder={false}
				loadingOlder={false}
				onLoadOlder={() => undefined}
				currentUserId="me"
				canReact
				canModerate={false}
				canReply
				actions={{ react: vi.fn(), edit: vi.fn(), remove: vi.fn() }}
				readSeq={readSeq}
			/>
		</QueryClientProvider>,
	);
}

describe("MessageList dividers", () => {
	it("puts a divider at each new day", () => {
		list([
			msg({ id: "a", seq: 1, created_at: at(8, 9) }),
			msg({ id: "b", seq: 2, created_at: at(8, 9, 1) }),
			msg({ id: "c", seq: 3, created_at: at(9, 9) }),
		]);
		expect(screen.getAllByText(/\d{4}/)).toHaveLength(2);
	});

	it("marks where the unread begin, skipping the reader's own messages", () => {
		list(
			[
				msg({ id: "a", seq: 1 }),
				msg({ id: "mine", seq: 2, author_id: "me", author_name: "Me" }),
				msg({ id: "b", seq: 3, created_at: at(10, 9, 1) }),
			],
			1,
		);
		const divider = screen.getByText("New");
		const next = divider.parentElement?.nextElementSibling as HTMLElement;
		expect(next.getAttribute("data-message-id")).toBe("b");
	});

	it("shows no divider for a reader who had read everything", () => {
		list([msg({ id: "a", seq: 1 }), msg({ id: "b", seq: 2 })], 2);
		expect(screen.queryByText("New")).toBeNull();
	});
});
