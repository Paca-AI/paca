import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	fireEvent,
	render,
	screen,
	waitFor,
	within,
} from "@testing-library/react";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/lib/mention-api", () => ({
	searchMentionableMembers: vi.fn(async (_p: string, q: string) =>
		[
			{
				id: "u-mina",
				name: "Mina Park",
				username: "mina",
				avatar: {},
				avatarUrl: null,
			},
			{
				id: "u-miles",
				name: "Miles",
				username: "miles",
				avatar: {},
				avatarUrl: null,
			},
		].filter((m) => m.name.toLowerCase().includes(q.toLowerCase())),
	),
}));
import "@/i18n";
import type { ChatMessage } from "@/lib/chat-api";
import { activeMention, Composer } from "./composer";
import { MessageContent } from "./message-content";
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

// The composer searches mentions with a query, so it needs a client.
function renderWithClient(ui: ReactElement) {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	const wrap = (el: ReactElement) => (
		<QueryClientProvider client={qc}>{el}</QueryClientProvider>
	);
	const r = render(wrap(ui));
	return { ...r, rerender: (el: ReactElement) => r.rerender(wrap(el)) };
}

describe("MessageContent", () => {
	it("never renders an unsafe link as a link", () => {
		const { container } = render(
			<MessageContent
				blocks={[
					{
						type: "paragraph",
						content: [
							{
								type: "link",
								href: "javascript:alert(1)",
								content: [{ type: "text", text: "click" }],
							},
							{
								type: "link",
								href: "https://ok.test",
								content: [{ type: "text", text: "fine" }],
							},
						],
					},
				]}
			/>,
		);
		const links = [...container.querySelectorAll("a")];
		expect(links.map((a) => a.getAttribute("href"))).toEqual([
			"https://ok.test",
		]);
		expect(links[0]?.getAttribute("rel")).toContain("noopener");
		expect(container.textContent).toContain("click");
	});

	it("renders markup in text as text", () => {
		const { container } = render(
			<MessageContent
				blocks={[
					{
						type: "paragraph",
						content: [{ type: "text", text: "<img src=x onerror=alert(1)>" }],
					},
				]}
			/>,
		);
		expect(container.querySelector("img")).toBeNull();
		expect(container.textContent).toContain("<img");
	});
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

describe("Composer", () => {
	it("sends on Enter, keeps Shift+Enter for a new line, and clears after success", async () => {
		const onSend = vi.fn().mockResolvedValue(undefined);
		renderWithClient(
			<Composer draftId="t1" placeholder="Message" onSend={onSend} />,
		);
		const box = screen.getByLabelText("Message");
		fireEvent.change(box, { target: { value: "hello\nworld" } });
		fireEvent.keyDown(box, { key: "Enter", shiftKey: true });
		expect(onSend).not.toHaveBeenCalled();
		fireEvent.keyDown(box, { key: "Enter" });
		await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1));
		expect(onSend.mock.calls[0]?.[0]).toHaveLength(2);
		await waitFor(() => expect((box as HTMLTextAreaElement).value).toBe(""));
	});

	it("keeps the text when sending fails and shows the error", async () => {
		const onSend = vi.fn().mockRejectedValue(new Error("boom"));
		renderWithClient(
			<Composer draftId="t2" placeholder="Message" onSend={onSend} />,
		);
		const box = screen.getByLabelText("Message");
		fireEvent.change(box, { target: { value: "keep me" } });
		fireEvent.keyDown(box, { key: "Enter" });
		await screen.findByRole("alert");
		expect((box as HTMLTextAreaElement).value).toBe("keep me");
	});

	it("does not send blank text and explains why it is read-only", () => {
		const onSend = vi.fn();
		const { rerender } = renderWithClient(
			<Composer draftId="t3" placeholder="Message" onSend={onSend} />,
		);
		fireEvent.keyDown(screen.getByLabelText("Message"), { key: "Enter" });
		expect(onSend).not.toHaveBeenCalled();
		rerender(
			<Composer
				draftId="t3"
				placeholder="Message"
				onSend={onSend}
				disabledReason="Read only"
			/>,
		);
		expect(screen.getByText("Read only")).toBeTruthy();
		expect(screen.queryByLabelText("Message")).toBeNull();
	});
});

describe("MessageContent mentions", () => {
	const blocks = [
		{
			type: "paragraph",
			content: [
				{ type: "teamMention", props: { id: "me", name: "Bob" } },
				{ type: "text", text: " and " },
				{ type: "teamMention", props: { id: "other", name: "Mina" } },
			],
		},
	];
	it("highlights a mention of the reader and not of others, as text", () => {
		const { container } = render(
			<MessageContent blocks={blocks} currentUserId="me" />,
		);
		const chips = [...container.querySelectorAll("[data-mention]")];
		expect(
			chips.map((c) => [c.getAttribute("data-mention"), c.textContent]),
		).toEqual([
			["me", "@Bob"],
			["other", "@Mina"],
		]);
	});
});

describe("activeMention", () => {
	it("finds the @ being typed at the caret", () => {
		expect(activeMention("hello @mi", 9)).toEqual({ start: 6, query: "mi" });
		expect(activeMention("@", 1)).toEqual({ start: 0, query: "" });
		expect(activeMention("ask @Mina now", 9)).toEqual({
			start: 4,
			query: "Mina",
		});
	});
	it("ignores an email address, a finished word and a caret elsewhere", () => {
		expect(activeMention("mail me@x.test", 14)).toBeNull();
		expect(activeMention("@Mina done", 10)).toBeNull();
		expect(activeMention("no at sign", 5)).toBeNull();
	});
});

describe("Composer mentions", () => {
	it("suggests members after @, inserts the chosen one and sends it as a mention", async () => {
		const onSend = vi.fn().mockResolvedValue(undefined);
		renderWithClient(
			<Composer
				draftId="m1"
				placeholder="Message"
				onSend={onSend}
				mentionProjectId="p1"
			/>,
		);
		const box = screen.getByLabelText("Message") as HTMLTextAreaElement;
		fireEvent.change(box, { target: { value: "hi @mi" } });
		const list = await screen.findByRole("listbox");
		expect(within(list).getAllByRole("option")).toHaveLength(2);

		// Arrow to the second, Enter picks it instead of sending.
		fireEvent.keyDown(box, { key: "ArrowDown" });
		fireEvent.keyDown(box, { key: "Enter" });
		expect(onSend).not.toHaveBeenCalled();
		await waitFor(() => expect(box.value).toBe("hi @Miles "));
		expect(screen.queryByRole("listbox")).toBeNull();

		fireEvent.change(box, { target: { value: "hi @Miles ok" } });
		fireEvent.keyDown(box, { key: "Enter" });
		await waitFor(() => expect(onSend).toHaveBeenCalledTimes(1));
		const [blocks] = onSend.mock.calls[0] as [
			Array<{ content: Array<{ type: string; props?: { id: string } }> }>,
		];
		expect(
			blocks[0]?.content
				.filter((n) => n.type === "teamMention")
				.map((n) => n.props?.id),
		).toEqual(["u-miles"]);
	});

	it("does not send, or pick from an older list, while the search catches up with the text", async () => {
		const onSend = vi.fn().mockResolvedValue(undefined);
		renderWithClient(
			<Composer
				draftId="m4"
				placeholder="Message"
				onSend={onSend}
				mentionProjectId="p1"
			/>,
		);
		const box = screen.getByLabelText("Message") as HTMLTextAreaElement;
		fireEvent.change(box, { target: { value: "@mi" } });
		// Enter straight away: nothing is sent, nothing is inserted.
		fireEvent.keyDown(box, { key: "Enter" });
		expect(onSend).not.toHaveBeenCalled();
		expect(box.value).toBe("@mi");
		// Once the results for "mi" are in, Enter picks the first of them.
		await screen.findByRole("listbox");
		fireEvent.keyDown(box, { key: "Enter" });
		await waitFor(() => expect(box.value).toBe("@Mina Park "));
	});

	it("closes the list on Escape and offers nothing without a project", async () => {
		renderWithClient(
			<Composer
				draftId="m2"
				placeholder="Message"
				onSend={vi.fn()}
				mentionProjectId="p1"
			/>,
		);
		const box = screen.getByLabelText("Message");
		fireEvent.change(box, { target: { value: "@m" } });
		await screen.findByRole("listbox");
		fireEvent.keyDown(box, { key: "Escape" });
		expect(screen.queryByRole("listbox")).toBeNull();

		renderWithClient(
			<Composer draftId="m3" placeholder="Direct" onSend={vi.fn()} />,
		);
		fireEvent.change(screen.getByLabelText("Direct"), {
			target: { value: "@m" },
		});
		await new Promise((r) => setTimeout(r, 250));
		expect(screen.queryByRole("listbox")).toBeNull();
	});
});
