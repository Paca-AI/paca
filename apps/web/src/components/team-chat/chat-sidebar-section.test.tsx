import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/i18n";
import type { InboxItem } from "@/lib/chat-api";

const mocks = vi.hoisted(() => ({
	get: vi.fn(),
	put: vi.fn(),
	del: vi.fn(),
	post: vi.fn(),
	path: "/chat",
}));

vi.mock("@/lib/api-client", () => ({
	apiClient: {
		instance: {
			get: mocks.get,
			put: mocks.put,
			delete: mocks.del,
			post: mocks.post,
		},
	},
}));
vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => vi.fn(),
	useRouterState: ({ select }: { select: (s: unknown) => unknown }) =>
		select({ location: { pathname: mocks.path } }),
	Link: ({
		children,
		to,
		params,
		className,
		...rest
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
			className={className as string}
			aria-label={rest["aria-label"] as string | undefined}
			title={rest.title as string | undefined}
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
import { SIDEBAR_STORAGE_KEY } from "@/lib/chat-sidebar";
import { ChatSidebarSection } from "./chat-sidebar-section";

const item = (o: Partial<InboxItem>): InboxItem => ({
	project_id: "p1",
	project_name: "Apollo",
	peer: null,
	channel_id: `${o.project_id ?? "p1"}-${o.channel_name ?? "general"}`,
	channel_name: "general",
	kind: "public",
	last_seq: 0,
	last_read_seq: 0,
	unread_count: 0,
	last_message_at: null,
	last_author: "",
	preview: "",
	mention_count: 0,
	starred: false,
	...o,
});

const DMS = { project_id: null, project_name: "" } as const;

const rows: InboxItem[] = [
	item({ channel_name: "general", unread_count: 3 }),
	item({ channel_name: "release", mention_count: 1, unread_count: 1 }),
	item({ channel_name: "design", kind: "private" }),
	item({
		project_id: "p2",
		project_name: "Borealis",
		channel_name: "general",
		unread_count: 4,
	}),
	item({ project_id: "p2", project_name: "Borealis", channel_name: "launch" }),
	item({ project_id: "p3", project_name: "Cirrus", channel_name: "infra" }),
	item({
		project_id: "p3",
		project_name: "Cirrus",
		channel_name: "alerts",
		starred: true,
	}),
	item({
		...DMS,
		channel_id: "d1",
		channel_name: "",
		unread_count: 2,
		peer: { account_id: "u1", name: "Mina Park", kind: "user" },
	}),
	item({
		...DMS,
		channel_id: "d2",
		channel_name: "",
		peer: { account_id: "a1", name: "Atlas", kind: "agent" },
	}),
];

function setup(data: InboxItem[] = rows, projectId?: string) {
	mocks.get.mockImplementation(async (url: string) => ({
		data: {
			data:
				url === "/users/me/chat/inbox"
					? data
					: [
							{
								project_id: "p1",
								project_name: "Apollo",
								unread: 99,
								channels: 3,
							},
						],
		},
	}));
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<SidebarProvider>
				<ChatSidebarSection projectId={projectId} />
			</SidebarProvider>
		</QueryClientProvider>,
	);
}

const link = (name: RegExp) => screen.findByRole("link", { name });
// A name two projects share (#general) needs the plural query.
const links = (name: RegExp) => screen.findAllByRole("link", { name });

beforeEach(() => {
	vi.clearAllMocks();
	localStorage.clear();
	mocks.put.mockResolvedValue({});
	mocks.del.mockResolvedValue({});
	mocks.path = "/chat";
});

describe("the channel list", () => {
	it("groups channels by project, mentions and unread first inside each", async () => {
		setup();
		await links(/general/);
		const names = screen.getAllByRole("link").map((a) => a.textContent ?? "");
		const idx = (s: string) => names.findIndex((n) => n.includes(s));
		expect(idx("release")).toBeLessThan(idx("general"));
		expect(idx("general")).toBeLessThan(idx("design"));
		// The project is named once, on its group, not on every row.
		expect(screen.getAllByText("Borealis")).toHaveLength(1);
		expect(screen.getAllByText("Apollo")).toHaveLength(1);
		const release = (await link(/release/)).closest("li") as HTMLElement;
		expect(within(release).getByLabelText("Mentions")).toHaveTextContent("@1");
		const gen = screen
			.getAllByRole("link")
			.find(
				(a) =>
					a.textContent?.includes("general") &&
					a.getAttribute("href")?.includes("p2"),
			) as HTMLElement;
		expect(
			within(gen.closest("li") as HTMLElement).getByLabelText(
				"Unread messages",
			),
		).toHaveTextContent("4");
	});

	it("has no flat or unread-only switches", async () => {
		setup();
		await links(/general/);
		expect(screen.queryByRole("button", { name: "Flat" })).toBeNull();
		expect(screen.queryByRole("button", { name: "By project" })).toBeNull();
		expect(screen.queryByRole("button", { name: "Unread" })).toBeNull();
	});

	it("keeps starred channels and direct messages in their own section, and agents tagged", async () => {
		setup();
		const alerts = await link(/alerts/);
		expect(alerts.getAttribute("href")).toBe("/projects/p3/chat/p3-alerts");
		expect(
			screen.getByRole("button", { name: /Collapse Starred/ }),
		).toBeInTheDocument();
		const atlas = screen.getByRole("link", { name: /Atlas/ });
		expect(atlas).toHaveTextContent("agent");
		expect(atlas.getAttribute("href")).toBe("/chat/dm/d2");
		// Not repeated in its project group.
		expect(screen.getAllByRole("link", { name: /alerts/ })).toHaveLength(1);
	});

	it("shows the Inbox with the mention count, or the unread total when nothing mentions you", async () => {
		const first = setup();
		const inbox = await link(/Inbox/);
		await waitFor(() => expect(inbox.closest("li")).toHaveTextContent("@1"));
		first.unmount();
		setup(rows.map((r) => ({ ...r, mention_count: 0 })));
		const again = await link(/Inbox/);
		await waitFor(() => expect(again.closest("li")).toHaveTextContent("99"));
	});

	it("opens a direct message under the current project, lists that project first, and offers create only there", async () => {
		setup(rows, "p2");
		expect((await link(/Mina Park/)).getAttribute("href")).toBe(
			"/projects/p2/dm/d1",
		);
		const headers = screen
			.getAllByRole("button", { name: /^Collapse (Apollo|Borealis|Cirrus)/ })
			.map((b) => b.textContent);
		expect(headers[0]).toBe("Borealis");
		// Browse for every project; create for the current one only.
		expect(
			screen.getAllByRole("link", { name: "Browse channels" }),
		).toHaveLength(3);
		expect(screen.getAllByRole("button", { name: "New channel" })).toHaveLength(
			1,
		);
	});

	it("offers browse and create for the current project even when it has no followed channels", async () => {
		setup(rows, "p9");
		await links(/general/);
		expect(
			screen.getAllByRole("link", { name: "Browse channels" }).length,
		).toBeGreaterThan(3);
		expect(
			screen.getByRole("button", { name: "New channel" }),
		).toBeInTheDocument();
	});

	it("has no create in the workspace context", async () => {
		setup();
		await links(/general/);
		expect(screen.queryByRole("button", { name: "New channel" })).toBeNull();
	});

	it("trims a long list of read direct messages behind Show more", async () => {
		const user = userEvent.setup();
		const dms = Array.from({ length: 14 }, (_, i) =>
			item({
				...DMS,
				channel_id: `d${i}`,
				channel_name: "",
				peer: {
					account_id: `u${i}`,
					name: `Person${String(i).padStart(2, "0")}`,
					kind: "user",
				},
			}),
		);
		setup(dms);
		await link(/Person00/);
		expect(screen.queryByRole("link", { name: /Person13/ })).toBeNull();
		await user.click(screen.getByRole("button", { name: "Show 6 more" }));
		expect(screen.getByRole("link", { name: /Person13/ })).toBeInTheDocument();
		await user.click(screen.getByRole("button", { name: "Show less" }));
		expect(screen.queryByRole("link", { name: /Person13/ })).toBeNull();
	});
});

describe("collapsing", () => {
	it("a closed project group keeps its unread channels and shows its unread count", async () => {
		const user = userEvent.setup();
		setup();
		await link(/release/);
		await user.click(screen.getByRole("button", { name: /Collapse Borealis/ }));
		expect(
			screen.getByRole("button", { name: /Expand Borealis/ }),
		).toHaveTextContent("4");
		expect(screen.queryByRole("link", { name: /launch/ })).toBeNull();
		// The unread #general of the closed group stays, next to Apollo's.
		expect(screen.getAllByRole("link", { name: /general/ })).toHaveLength(2);
		expect(
			JSON.parse(localStorage.getItem(SIDEBAR_STORAGE_KEY) ?? "{}"),
		).toMatchObject({ closedProjects: { p2: true } });
	});

	it("starts with a group collapsed when it was left collapsed", async () => {
		localStorage.setItem(
			SIDEBAR_STORAGE_KEY,
			JSON.stringify({ closedProjects: { p1: true } }),
		);
		setup();
		await screen.findByRole("button", { name: /Expand Apollo/ });
		expect(screen.queryByRole("link", { name: /design/ })).toBeNull();
	});

	it("closing Chat folds everything into one label that still shows what is unread", async () => {
		const user = userEvent.setup();
		setup();
		await link(/Inbox/);
		await user.click(screen.getByRole("button", { name: /Collapse Chat/ }));
		expect(screen.queryByRole("link")).toBeNull();
		const header = screen.getByRole("button", { name: /Expand Chat/ });
		await waitFor(() => expect(header).toHaveTextContent("@1"));
		await user.click(header);
		expect(await link(/Inbox/)).toBeInTheDocument();
	});
});

describe("starring", () => {
	it("stars a channel and a direct message through their own routes", async () => {
		const user = userEvent.setup();
		setup();
		const release = (await link(/release/)).closest("li") as HTMLElement;
		await user.click(within(release).getByRole("button", { name: "Star" }));
		await waitFor(() =>
			expect(mocks.put).toHaveBeenCalledWith(
				"/projects/p1/channels/p1-release/star",
			),
		);
		const mina = screen
			.getByRole("link", { name: /Mina Park/ })
			.closest("li") as HTMLElement;
		await user.click(within(mina).getByRole("button", { name: "Star" }));
		await waitFor(() =>
			expect(mocks.put).toHaveBeenCalledWith("/chat/dms/d1/star"),
		);
	});

	it("unstars a starred one", async () => {
		const user = userEvent.setup();
		setup();
		const alerts = (await link(/alerts/)).closest("li") as HTMLElement;
		await user.click(within(alerts).getByRole("button", { name: "Unstar" }));
		await waitFor(() =>
			expect(mocks.del).toHaveBeenCalledWith(
				"/projects/p3/channels/p3-alerts/star",
			),
		);
	});
});
