import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/i18n";
import type { Channel } from "@/lib/chat-api";

const mocks = vi.hoisted(() => ({ get: vi.fn(), open: vi.fn() }));

vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get: mocks.get } },
}));
vi.mock("@/lib/call-window", () => ({ openCallTab: mocks.open }));

import { CallBanner, CallHeaderButton, useChannelCall } from "./call-join";

const channel = (over: Partial<Channel> = {}): Channel => ({
	id: "c1",
	scope: "project",
	project_id: "p1",
	kind: "voice",
	name: "Lounge",
	topic: "",
	last_seq: 0,
	archived_at: null,
	created_by: null,
	created_at: "2026-01-01T00:00:00Z",
	is_member: true,
	last_read_seq: 0,
	unread_count: 0,
	peer: null,
	mention_count: 0,
	starred: false,
	...over,
});

function serve(opts: { enabled: boolean; names: string[] }) {
	mocks.get.mockImplementation(async (url: string) => {
		if (url.includes("/users/me") || url.includes("/auth/me")) {
			return { data: { data: { id: "me" } } };
		}
		if (url === "/chat/calls/config") {
			return { data: { data: { enabled: opts.enabled } } };
		}
		return {
			data: {
				data: {
					channel_id: "c1",
					started_at: null,
					participants: opts.names.map((name, i) => ({
						account_id: `a${i}`,
						name,
						kind: "user",
						joined_at: "2026-01-01T00:00:00Z",
					})),
				},
			},
		};
	});
}

function Harness({
	ch,
	canJoin = true,
}: {
	ch: Channel;
	canJoin?: boolean;
}): ReactNode {
	const call = useChannelCall("p1", ch, canJoin);
	return (
		<>
			<CallHeaderButton call={call} voice={ch.kind === "voice"} />
			<CallBanner call={call} voice={ch.kind === "voice"} />
		</>
	);
}

function renderHarness(ch: Channel, canJoin = true) {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<Harness ch={ch} canJoin={canJoin} />
		</QueryClientProvider>,
	);
}

describe("channel call join", () => {
	beforeEach(() => vi.clearAllMocks());

	it("lists who is in a voice channel and joins on click", async () => {
		serve({ enabled: true, names: ["Ada", "Bo"] });
		renderHarness(channel());
		expect(await screen.findByText(/Ada, Bo/)).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Join call" }));
		expect(mocks.open).toHaveBeenCalledWith("p1", "c1", "Lounge");
	});

	it("says an empty voice channel is empty", async () => {
		serve({ enabled: true, names: [] });
		renderHarness(channel());
		expect(await screen.findByText(/Nobody is here yet/)).toBeInTheDocument();
	});

	it("offers a huddle in a text channel, and no banner until one runs", async () => {
		serve({ enabled: true, names: [] });
		renderHarness(channel({ kind: "public", name: "general" }));
		expect(
			await screen.findByRole("button", { name: "Start huddle" }),
		).toBeInTheDocument();
		expect(screen.queryByText(/in call/)).not.toBeInTheDocument();
	});

	it("shows nothing when calls are off or the caller may not join", async () => {
		serve({ enabled: false, names: [] });
		renderHarness(channel());
		await vi.waitFor(() => expect(mocks.get).toHaveBeenCalled());
		expect(screen.queryByRole("button")).not.toBeInTheDocument();
	});

	it("shows nothing without calls:join", async () => {
		serve({ enabled: true, names: ["Ada"] });
		renderHarness(channel(), false);
		await new Promise((r) => setTimeout(r, 30));
		expect(screen.queryByRole("button")).not.toBeInTheDocument();
	});
});
