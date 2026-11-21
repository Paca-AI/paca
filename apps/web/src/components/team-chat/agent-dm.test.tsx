import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/i18n";
import type { AgentRun, AgentRuns, Channel, ChatMessage } from "@/lib/chat-api";

const mocks = vi.hoisted(() => ({ get: vi.fn() }));

vi.mock("@/lib/api-client", () => ({
	apiClient: {
		instance: { get: mocks.get, put: vi.fn(), delete: vi.fn(), post: vi.fn() },
	},
}));
vi.mock("@/lib/auth-api", () => ({
	currentUserQueryOptions: {
		queryKey: ["me"],
		queryFn: async () => ({ id: "me" }),
	},
}));
vi.mock("@/lib/mention-api", () => ({
	searchMentionableMembers: vi.fn(async () => []),
	searchMentionableTasks: vi.fn(async () => []),
	searchMentionableDocuments: vi.fn(async () => []),
}));
vi.mock("@/hooks/use-chat-realtime", () => ({
	useChatRealtime: () => undefined,
}));
vi.mock("@/hooks/use-chat-permissions", () => ({
	useChatPermissions: () => ({ can: () => true, isLoading: false }),
}));
vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => vi.fn(),
	Link: ({
		children,
		to,
		params,
		...rest
	}: Record<string, unknown> & { children?: ReactNode }) => (
		<a
			href={String(to).replace(
				"$conversationId",
				(params as { conversationId?: string })?.conversationId ?? "",
			)}
			aria-label={rest["aria-label"] as string | undefined}
			className={rest.className as string | undefined}
		>
			{children}
		</a>
	),
}));

import { ChannelView } from "./channel-view";

const channel = (kind: "agent" | "user" = "agent"): Channel =>
	({
		id: "ch1",
		scope: "workspace",
		project_id: null,
		kind: "dm",
		name: "",
		topic: "",
		last_seq: 3,
		archived_at: null,
		peer: { account_id: "atlas", name: "Atlas", kind },
		mention_count: 0,
		starred: false,
		is_member: true,
		last_read_seq: 1,
		unread_count: 2,
	}) as unknown as Channel;

const msg = (o: Partial<ChatMessage>): ChatMessage => ({
	id: "m",
	channel_id: "ch1",
	seq: 1,
	thread_root_id: null,
	author_id: "me",
	author_name: "Me",
	content: [{ type: "paragraph", content: [{ type: "text", text: "hi" }] }],
	edited_at: null,
	deleted_at: null,
	created_at: "2026-11-10T10:00:00Z",
	reply_count: 0,
	reactions: [],
	saved: false,
	following: false,
	...o,
});

const run = (o: Partial<AgentRun>): AgentRun => ({
	id: "r1",
	title: "Weekly report",
	status: "finished",
	created_at: "2026-11-10T10:00:00Z",
	updated_at: "2026-11-10T10:05:00Z",
	...o,
});

function setup(opts: {
	peerKind?: "agent" | "user";
	agent?: AgentRuns;
	messages?: ChatMessage[];
}) {
	mocks.get.mockImplementation(async (url: string) => {
		if (url === "/chat/dms")
			return { data: { data: [channel(opts.peerKind)] } };
		if (url.endsWith("/messages"))
			return {
				data: { data: { items: opts.messages ?? [], has_more: false } },
			};
		if (url.endsWith("/agent"))
			return {
				data: {
					data: opts.agent ?? { replies: true, working: false, runs: [] },
				},
			};
		return { data: { data: [] } };
	});
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<ChannelView projectId="dm" channelId="ch1" />
		</QueryClientProvider>,
	);
}

beforeEach(() => vi.clearAllMocks());

describe("a direct message with an agent", () => {
	it("shows the agent's reply as the agent, with a link to the run behind it", async () => {
		setup({
			messages: [
				msg({}),
				msg({
					id: "r",
					seq: 2,
					author_id: "atlas",
					author_name: "Atlas",
					run_id: "r1",
					content: [
						{
							type: "paragraph",
							content: [{ type: "text", text: "All done." }],
						},
					],
				}),
			],
		});
		const reply = (await screen.findByText("All done.")).closest(
			"[data-message-id]",
		) as HTMLElement;
		expect(within(reply).getByText("agent")).toBeInTheDocument();
		expect(
			within(reply).getByRole("link", { name: /View run/ }),
		).toHaveAttribute("href", "/conversations/r1");
		// Your own message has neither.
		const mine = screen
			.getByText("hi")
			.closest("[data-message-id]") as HTMLElement;
		expect(within(mine).queryByRole("link")).toBeNull();
	});

	it("says the agent is working, in the header and under the last message", async () => {
		setup({
			messages: [msg({})],
			agent: {
				replies: true,
				working: true,
				runs: [run({ status: "running" })],
			},
		});
		// The header names who is working; the line under the messages is theirs.
		expect(await screen.findByText("Atlas is working…")).toBeInTheDocument();
		expect(await screen.findByText("is working…")).toBeInTheDocument();
	});

	it("lists the runs, searchable, each opening the full conversation", async () => {
		const user = userEvent.setup();
		setup({
			agent: {
				replies: true,
				working: false,
				runs: [
					run({}),
					run({ id: "r2", title: "Fix flaky test", status: "failed" }),
				],
			},
		});
		await user.click(await screen.findByRole("button", { name: "Runs" }));
		const link = await screen.findByRole("link", { name: /Weekly report/ });
		expect(link).toHaveAttribute("href", "/conversations/r1");
		await user.type(
			screen.getByRole("textbox", { name: "Search runs" }),
			"flaky",
		);
		expect(screen.queryByRole("link", { name: /Weekly report/ })).toBeNull();
		expect(
			screen.getByRole("link", { name: /Fix flaky test/ }),
		).toBeInTheDocument();
		await user.clear(screen.getByRole("textbox", { name: "Search runs" }));
		await user.click(screen.getByRole("button", { name: "Failed" }));
		expect(screen.queryByRole("link", { name: /Weekly report/ })).toBeNull();
	});

	it("tells the person when the last run failed, and when the agent cannot answer here", async () => {
		const first = setup({
			agent: {
				replies: true,
				working: false,
				runs: [run({ status: "failed" })],
			},
		});
		expect(await screen.findByText(/last run failed/)).toBeInTheDocument();
		first.unmount();
		setup({ agent: { replies: false, working: false, runs: [] } });
		expect(
			await screen.findByText(/can't answer in direct messages/),
		).toBeInTheDocument();
	});

	it("asks nothing of the agent endpoint for a person", async () => {
		setup({ peerKind: "user" });
		await screen.findByText(/Direct message|Person/i).catch(() => undefined);
		await waitFor(() => expect(mocks.get).toHaveBeenCalledWith("/chat/dms"));
		expect(
			mocks.get.mock.calls.some(([u]) => String(u).endsWith("/agent")),
		).toBe(false);
	});
});
