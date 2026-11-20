import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/i18n";
import type { InboxItem, MessageRef } from "@/lib/chat-api";

const mocks = vi.hoisted(() => ({
	get: vi.fn(),
	post: vi.fn(),
	navigate: vi.fn(),
	path: "/chat",
}));

vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get: mocks.get, post: mocks.post } },
}));
vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => mocks.navigate,
	useRouterState: ({ select }: { select: (s: unknown) => unknown }) =>
		select({ location: { pathname: mocks.path } }),
	Link: ({
		children,
		to,
		params,
		search,
		className,
	}: Record<string, unknown> & { children?: ReactNode }) => (
		<a
			href={String(to)
				.replace(
					"$projectId",
					(params as { projectId?: string })?.projectId ?? "",
				)
				.replace(
					"$channelId",
					(params as { channelId?: string })?.channelId ?? "",
				)}
			data-search={JSON.stringify(search ?? {})}
			className={className as string}
		>
			{children}
		</a>
	),
}));
vi.mock("@/hooks/use-chat-permissions", () => ({
	useChatPermissions: () => ({ can: () => true, isLoading: false }),
}));
vi.mock("@/hooks/use-project-permissions", () => ({
	useProjectPermissions: () => ({
		hasProjectPermission: () => true,
		isLoading: false,
	}),
}));

import { SidebarProvider } from "@/components/ui/sidebar";
import { InboxPage } from "./inbox-page";
import { NewMessageDialog } from "./new-message-dialog";

const item = (o: Partial<InboxItem>): InboxItem => ({
	project_id: "p1",
	project_name: "Apollo",
	channel_id: "c1",
	channel_name: "release",
	kind: "public",
	last_seq: 5,
	last_read_seq: 3,
	unread_count: 2,
	last_message_at: new Date(Date.now() - 4 * 60_000).toISOString(),
	last_author: "Mina Park",
	peer: null,
	mention_count: 0,
	starred: false,
	preview: "the changelog needs sign-off",
	...o,
});

function wrap(ui: ReactNode) {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<SidebarProvider>{ui}</SidebarProvider>
		</QueryClientProvider>,
	);
}

function serve(routes: Record<string, unknown>) {
	mocks.get.mockImplementation(
		async (url: string, cfg?: { params?: Record<string, unknown> }) => {
			const hit = routes[url];
			const data =
				typeof hit === "function" ? (hit as (c: unknown) => unknown)(cfg) : hit;
			return { data: { data } };
		},
	);
}

beforeEach(() => {
	vi.clearAllMocks();
	mocks.post.mockResolvedValue({ data: {} });
	mocks.path = "/chat";
});

describe("InboxPage", () => {
	it("lists followed channels from every project with a project chip, author and preview", async () => {
		serve({
			"/users/me/chat/inbox": [
				item({}),
				item({
					project_id: "p2",
					project_name: "Borealis",
					channel_id: "c2",
					channel_name: "launch",
					unread_count: 0,
					last_read_seq: 5,
				}),
			],
		});
		wrap(<InboxPage tab="all" />);
		const rows = await screen.findAllByRole("listitem");
		expect(rows).toHaveLength(2);
		expect(
			within(rows[0] as HTMLElement).getByText("Apollo"),
		).toBeInTheDocument();
		expect(
			within(rows[0] as HTMLElement).getByText("release"),
		).toBeInTheDocument();
		expect(
			within(rows[0] as HTMLElement).getByText(/changelog needs sign-off/),
		).toBeInTheDocument();
		expect(
			within(rows[0] as HTMLElement).getByRole("status"),
		).toHaveTextContent("2");
		expect(
			within(rows[1] as HTMLElement).getByText("Borealis"),
		).toBeInTheDocument();
		expect(within(rows[1] as HTMLElement).queryByRole("status")).toBeNull();
		expect(within(rows[0] as HTMLElement).getByRole("link")).toHaveAttribute(
			"href",
			"/projects/p1/chat/c1",
		);
	});

	it("marks one channel read at its newest message, and everything read", async () => {
		serve({ "/users/me/chat/inbox": [item({})] });
		const user = userEvent.setup();
		wrap(<InboxPage tab="all" />);
		await user.click(await screen.findByRole("button", { name: "Mark read" }));
		await waitFor(() =>
			expect(mocks.post).toHaveBeenCalledWith("/projects/p1/channels/c1/read", {
				seq: 5,
			}),
		);
		await user.click(screen.getByRole("button", { name: "Mark all read" }));
		await waitFor(() =>
			expect(mocks.post).toHaveBeenCalledWith("/users/me/chat/read-all", {
				project_id: null,
			}),
		);
	});

	it("limits mark-all-read to the filtered project and asks for unread only on that tab", async () => {
		serve({ "/users/me/chat/inbox": [item({})] });
		const user = userEvent.setup();
		wrap(<InboxPage tab="unread" projectId="p1" />);
		await screen.findAllByRole("listitem");
		expect(mocks.get).toHaveBeenCalledWith("/users/me/chat/inbox", {
			params: { unread: "true", project_id: "p1" },
		});
		await user.click(screen.getByRole("button", { name: "Mark all read" }));
		await waitFor(() =>
			expect(mocks.post).toHaveBeenCalledWith("/users/me/chat/read-all", {
				project_id: "p1",
			}),
		);
	});

	it("switches tab and project through the URL", async () => {
		serve({ "/users/me/chat/inbox": [item({})] });
		const user = userEvent.setup();
		wrap(<InboxPage tab="all" />);
		await screen.findAllByRole("listitem");
		await user.click(screen.getByRole("tab", { name: "Unread" }));
		expect(mocks.navigate).toHaveBeenCalledWith(
			expect.objectContaining({
				search: { project: undefined, tab: "unread" },
			}),
		);
		await user.selectOptions(screen.getByLabelText("Project"), "p1");
		expect(mocks.navigate).toHaveBeenLastCalledWith(
			expect.objectContaining({ search: { project: "p1", tab: "all" } }),
		);
	});

	it("says you are caught up when nothing is unread", async () => {
		serve({ "/users/me/chat/inbox": [] });
		wrap(<InboxPage tab="unread" />);
		expect(await screen.findByText("You're all caught up")).toBeInTheDocument();
	});
});

const ref = (o: Partial<MessageRef>): MessageRef => ({
	project_id: "p1",
	project_name: "Apollo",
	peer: null,
	channel_id: "c1",
	channel_name: "release",
	kind: "public",
	message_id: "m1",
	seq: 5,
	thread_root_id: null,
	root_seq: null,
	author_id: "u1",
	author_name: "Mina Park",
	preview: "@Bob can you sign off?",
	root_preview: "",
	created_at: "2026-01-01T10:00:00Z",
	activity_at: new Date(Date.now() - 60_000).toISOString(),
	unread: true,
	unread_count: 0,
	reply_count: 0,
	...o,
});

describe("Inbox Mentions, Threads and Saved tabs", () => {
	it("lists mentions with the project, author and an unread dot; a reply opens its thread", async () => {
		serve({
			"/users/me/chat/mentions": [
				ref({}),
				ref({
					message_id: "m2",
					thread_root_id: "t9",
					root_seq: 3,
					unread: false,
					preview: "in a thread",
				}),
			],
			"/users/me/chat/threads": [],
		});
		wrap(<InboxPage tab="mentions" />);
		const rows = await screen.findAllByRole("listitem");
		expect(rows).toHaveLength(2);
		expect(
			within(rows[0] as HTMLElement).getByText("Apollo"),
		).toBeInTheDocument();
		expect(
			within(rows[0] as HTMLElement).getByText(/can you sign off/),
		).toBeInTheDocument();
		expect(
			within(rows[0] as HTMLElement).getByLabelText("Unread mention"),
		).toBeInTheDocument();
		expect(
			within(rows[1] as HTMLElement).queryByLabelText("Unread mention"),
		).toBeNull();
		expect(within(rows[0] as HTMLElement).getByRole("link")).toHaveAttribute(
			"data-search",
			"{}",
		);
		expect(
			JSON.parse(
				within(rows[1] as HTMLElement)
					.getByRole("link")
					.getAttribute("data-search") ?? "{}",
			),
		).toEqual({ thread: "t9", seq: 3 });
		// The tab carries its unread count.
		expect(screen.getByRole("tab", { name: /Mentions/ })).toHaveTextContent(
			"1",
		);
	});

	it("lists followed threads with the root, the latest reply and unread replies, opening the thread", async () => {
		serve({
			"/users/me/chat/mentions": [],
			"/users/me/chat/threads": [
				ref({
					message_id: "r1",
					seq: 4,
					root_seq: 4,
					root_preview: "let's discuss the plan",
					preview: "I agree",
					reply_count: 3,
					unread_count: 2,
				}),
			],
		});
		wrap(<InboxPage tab="threads" />);
		const row = (await screen.findAllByRole("listitem"))[0] as HTMLElement;
		expect(within(row).getByText("let's discuss the plan")).toBeInTheDocument();
		expect(within(row).getByText(/I agree/)).toBeInTheDocument();
		expect(within(row).getByText("Replies: 3")).toBeInTheDocument();
		expect(within(row).getByRole("status")).toHaveTextContent("2");
		expect(
			JSON.parse(
				within(row).getByRole("link").getAttribute("data-search") ?? "{}",
			),
		).toEqual({ thread: "r1", seq: 4 });
	});

	it("lists saved messages, and filters every tab by the chosen project", async () => {
		serve({
			"/users/me/chat/mentions": [],
			"/users/me/chat/threads": [],
			"/users/me/chat/saved": [
				ref({ unread: false }),
				ref({
					message_id: "m3",
					project_id: "p2",
					project_name: "Borealis",
					unread: false,
				}),
			],
		});
		wrap(<InboxPage tab="saved" projectId="p2" />);
		const rows = await screen.findAllByRole("listitem");
		expect(rows).toHaveLength(1);
		expect(
			within(rows[0] as HTMLElement).getByText("Borealis"),
		).toBeInTheDocument();
	});

	it("explains an empty tab", async () => {
		serve({ "/users/me/chat/mentions": [], "/users/me/chat/threads": [] });
		wrap(<InboxPage tab="mentions" />);
		expect(await screen.findByText("No mentions")).toBeInTheDocument();
	});
});

describe("InboxPage with direct messages", () => {
	it("shows the person instead of a project, links to the conversation and marks it read in the direct-message scope", async () => {
		serve({
			"/users/me/chat/inbox": [
				item({
					project_id: null,
					project_name: "",
					channel_id: "d9",
					channel_name: "",
					kind: "dm",
					peer: { account_id: "u1", name: "Mina Park", kind: "user" },
					preview: "can you look at this?",
				}),
			],
		});
		const user = userEvent.setup();
		wrap(<InboxPage tab="all" />);
		const row = (await screen.findAllByRole("listitem"))[0] as HTMLElement;
		expect(within(row).getAllByText(/Mina Park/)[0]).toBeInTheDocument();
		expect(within(row).getByText("Direct message")).toBeInTheDocument();
		expect(within(row).getByRole("link")).toHaveAttribute(
			"href",
			"/chat/dm/d9",
		);
		await user.click(within(row).getByRole("button", { name: "Mark read" }));
		await waitFor(() =>
			expect(mocks.post).toHaveBeenCalledWith("/chat/dms/d9/read", { seq: 5 }),
		);
		// Direct messages are not a project filter choice.
		expect(screen.queryByRole("option", { name: "Direct message" })).toBeNull();
	});
});

describe("NewMessageDialog", () => {
	const people = [
		{ account_id: "u1", name: "Mina Park", kind: "user", description: "" },
		{ account_id: "u2", name: "Dev Patel", kind: "user", description: "" },
	];
	const agents = [
		{
			account_id: "a1",
			name: "Atlas",
			kind: "agent",
			description: "Project agent",
		},
	];

	it("searches the directory by kind, and opens the conversation with the chosen account", async () => {
		serve({
			"/chat/directory": (cfg: unknown) =>
				(cfg as { params: { kind?: string } }).params.kind === "agent"
					? agents
					: people,
		});
		mocks.post.mockResolvedValue({
			data: {
				data: {
					id: "d5",
					peer: { account_id: "u1", name: "Mina Park", kind: "user" },
				},
			},
		});
		const onOpenChange = vi.fn();
		const user = userEvent.setup();
		wrap(<NewMessageDialog open onOpenChange={onOpenChange} />);
		expect(await screen.findByText("Dev Patel")).toBeInTheDocument();

		await user.click(screen.getByRole("tab", { name: "Agents" }));
		expect(await screen.findByText("Atlas")).toBeInTheDocument();
		expect(screen.getByText("Project agent")).toBeInTheDocument();
		expect(
			screen.getByText(/Replies in direct messages are not available yet/),
		).toBeInTheDocument();

		await user.click(screen.getByRole("tab", { name: "People" }));
		await user.click(await screen.findByRole("button", { name: /Mina Park/ }));
		await waitFor(() =>
			expect(mocks.post).toHaveBeenCalledWith("/chat/dms", {
				account_id: "u1",
			}),
		);
		await waitFor(() =>
			expect(mocks.navigate).toHaveBeenCalledWith({
				to: "/chat/dm/$channelId",
				params: { channelId: "d5" },
			}),
		);
		expect(onOpenChange).toHaveBeenCalledWith(false);
	});

	it("explains a refusal in the dialog and stays open", async () => {
		serve({ "/chat/directory": people });
		mocks.post.mockRejectedValue({
			isAxiosError: true,
			response: { status: 404, data: { error_code: "DM_PEER_NOT_FOUND" } },
		});
		const user = userEvent.setup();
		wrap(<NewMessageDialog open onOpenChange={() => undefined} />);
		await user.click(await screen.findByRole("button", { name: /Mina Park/ }));
		expect(await screen.findByRole("alert")).toHaveTextContent(
			"That account no longer exists.",
		);
		expect(mocks.navigate).not.toHaveBeenCalled();
	});

	it("says so when no one matches", async () => {
		serve({ "/chat/directory": [] });
		wrap(<NewMessageDialog open onOpenChange={() => undefined} />);
		expect(
			await screen.findByText("There is no one to message yet."),
		).toBeInTheDocument();
	});
});
