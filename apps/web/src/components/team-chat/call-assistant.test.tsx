import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/i18n";

const mocks = vi.hoisted(() => ({
	get: vi.fn(),
	post: vi.fn(),
	del: vi.fn(),
	executeProposal: vi.fn(),
	suggest: vi.fn(),
	reportResolved: vi.fn(),
}));

vi.mock("@/lib/api-client", () => ({
	apiClient: {
		instance: { get: mocks.get, post: mocks.post, delete: mocks.del },
	},
}));
vi.mock("@/lib/proposal-actions", () => ({
	executeProposal: mocks.executeProposal,
}));
vi.mock("./call-provider", () => ({
	useCall: () => ({
		captions: [{ id: 1, speakerId: "u1", text: "ship it friday", at: "" }],
		suggest: mocks.suggest,
		reportResolved: mocks.reportResolved,
	}),
}));

import {
	AssistantSection,
	CaptionsStrip,
	ProposalsSection,
} from "./call-assistant";

const chip = (over: Record<string, unknown> = {}) => ({
	id: "pr1",
	channel_id: "c1",
	agent_id: "a1",
	agent_name: "Atlas",
	target_account_id: "me",
	kind: "task.create",
	status: "pending",
	expires_at: "2026-01-01T01:00:00Z",
	created_at: "2026-01-01T00:00:00Z",
	...over,
});

const assistant = (over: Record<string, unknown> = {}) => ({
	agent_id: "a1",
	name: "Atlas",
	status: "active",
	invited_by: "me",
	joined_at: "2026-01-01T00:00:00Z",
	mode: "on_demand",
	store_transcript: true,
	transcript_retention_days: 30,
	announcement: "I transcribe this call.",
	...over,
});

function serve(o: {
	enabled?: boolean;
	assistants?: unknown[];
	agents?: unknown[];
	chips?: unknown[];
}) {
	mocks.get.mockImplementation(async (url: string) => {
		const data = (v: unknown) => ({ data: { data: v } });
		if (url === "/chat/meeting/config")
			return data({ enabled: o.enabled ?? true });
		if (url.endsWith("/call/assistants")) return data(o.assistants ?? []);
		if (url.endsWith("/meeting-agents")) return data(o.agents ?? []);
		if (url.endsWith("/proposals")) return data(o.chips ?? []);
		if (url.endsWith("/proposals/pr1")) {
			return data({
				...chip(),
				call_session_id: "k1",
				payload: { title: "Fix login", description: "Safari" },
				result: {},
				resolved_by: null,
			});
		}
		if (url.includes("/auth/me") || url.includes("/users/me"))
			return data({ id: "me" });
		throw new Error(`unexpected GET ${url}`);
	});
}

function wrap(node: ReactNode) {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

beforeEach(() => vi.resetAllMocks());

describe("AssistantSection", () => {
	it("renders nothing when assistants are not enabled", async () => {
		serve({ enabled: false });
		const { container } = wrap(
			<AssistantSection
				projectId="p1"
				channelId="c1"
				canInvite
				canModerate={false}
			/>,
		);
		await waitFor(() => expect(mocks.get).toHaveBeenCalled());
		expect(container).toBeEmptyDOMElement();
	});

	it("shows the consent indicator with what is kept", async () => {
		serve({ assistants: [assistant()] });
		wrap(
			<AssistantSection
				projectId="p1"
				channelId="c1"
				canInvite={false}
				canModerate={false}
			/>,
		);
		expect(
			await screen.findByText("Atlas is listening and transcribing"),
		).toBeInTheDocument();
		expect(
			screen.getByText("Transcript kept for 30 days."),
		).toBeInTheDocument();
		expect(screen.getByText("I transcribe this call.")).toBeInTheDocument();
	});

	it("says when nothing is stored", async () => {
		serve({ assistants: [assistant({ store_transcript: false })] });
		wrap(
			<AssistantSection
				projectId="p1"
				channelId="c1"
				canInvite={false}
				canModerate={false}
			/>,
		);
		expect(await screen.findByText("Nothing is stored.")).toBeInTheDocument();
	});

	it("offers Remove only to moderators", async () => {
		serve({ assistants: [assistant()] });
		const { unmount } = wrap(
			<AssistantSection
				projectId="p1"
				channelId="c1"
				canInvite={false}
				canModerate={false}
			/>,
		);
		await screen.findByText("Atlas is listening and transcribing");
		expect(screen.queryByRole("button", { name: /Remove Atlas/ })).toBeNull();
		unmount();
		wrap(
			<AssistantSection
				projectId="p1"
				channelId="c1"
				canInvite={false}
				canModerate
			/>,
		);
		await userEvent.click(
			await screen.findByRole("button", { name: /Remove Atlas/ }),
		);
		await waitFor(() =>
			expect(mocks.del).toHaveBeenCalledWith(
				"/projects/p1/channels/c1/call/assistants/a1",
			),
		);
	});

	it("hides the whole section from someone who cannot invite when nobody is there", async () => {
		serve({ assistants: [] });
		const { container } = wrap(
			<AssistantSection
				projectId="p1"
				channelId="c1"
				canInvite={false}
				canModerate={false}
			/>,
		);
		await waitFor(() => expect(mocks.get).toHaveBeenCalled());
		expect(container).toBeEmptyDOMElement();
	});

	it("invites the chosen assistant, showing its privacy summary first", async () => {
		serve({
			assistants: [],
			agents: [
				{
					id: "a9",
					name: "Scribe",
					handle: "scribe",
					description: "",
					mode: "notes_only",
					store_transcript: true,
					transcript_retention_days: 7,
				},
			],
		});
		mocks.post.mockResolvedValue({
			data: { data: assistant({ agent_id: "a9", name: "Scribe" }) },
		});
		wrap(
			<AssistantSection
				projectId="p1"
				channelId="c1"
				canInvite
				canModerate={false}
			/>,
		);
		await userEvent.click(
			await screen.findByRole("button", { name: "Add assistant" }),
		);
		expect(
			await screen.findByText(
				/Transcribes and writes notes · transcript kept 7 days/,
			),
		).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: /Scribe/ }));
		await waitFor(() =>
			expect(mocks.post).toHaveBeenCalledWith(
				"/projects/p1/channels/c1/call/assistants/a9",
			),
		);
	});

	it("asks for suggestions only when an active assistant can make them", async () => {
		serve({ assistants: [assistant({ mode: "notes_only" })] });
		const { unmount } = wrap(
			<AssistantSection
				projectId="p1"
				channelId="c1"
				canInvite={false}
				canModerate={false}
			/>,
		);
		await screen.findByText("Atlas is listening and taking notes");
		expect(screen.queryByRole("button", { name: "Suggest tasks" })).toBeNull();
		unmount();

		serve({ assistants: [assistant()] });
		mocks.suggest.mockResolvedValue(2);
		wrap(
			<AssistantSection
				projectId="p1"
				channelId="c1"
				canInvite={false}
				canModerate={false}
			/>,
		);
		await userEvent.click(
			await screen.findByRole("button", { name: "Suggest tasks" }),
		);
		expect(
			await screen.findByText("Suggestions ready for you below: 2."),
		).toBeInTheDocument();
	});
});

describe("CaptionsStrip", () => {
	it("names the speaker", () => {
		wrap(<CaptionsStrip names={new Map([["u1", "Mina"]])} />);
		expect(
			screen.getByRole("log", { name: "Live captions" }),
		).toHaveTextContent("Mina: ship it friday");
	});
});

describe("ProposalsSection", () => {
	it("shows a card for me and a read-only line for someone else", async () => {
		serve({
			chips: [
				chip(),
				chip({
					id: "pr2",
					target_account_id: "u2",
					kind: "doc.create_from_notes",
				}),
				chip({ id: "pr3", status: "resolved" }),
			],
		});
		wrap(
			<ProposalsSection
				projectId="p1"
				channelId="c1"
				names={new Map([["u2", "Mina"]])}
			/>,
		);
		expect(await screen.findByDisplayValue("Fix login")).toBeInTheDocument();
		expect(
			screen.getByText(
				"Atlas suggested: Save notes as a doc. Mina is reviewing.",
			),
		).toBeInTheDocument();
		expect(screen.getAllByRole("button", { name: "Dismiss" })).toHaveLength(1);
	});

	it("claims, writes what was edited, then resolves, and tells the assistant", async () => {
		serve({ chips: [chip()] });
		mocks.post.mockResolvedValue({ data: { data: {} } });
		mocks.executeProposal.mockResolvedValue({ task_id: "t1", task_number: 5 });
		wrap(<ProposalsSection projectId="p1" channelId="c1" names={new Map()} />);
		const title = await screen.findByDisplayValue("Fix login");
		await userEvent.clear(title);
		await userEvent.type(title, "Fix the login page");
		await userEvent.click(screen.getByRole("button", { name: "Create task" }));
		await waitFor(() =>
			expect(mocks.reportResolved).toHaveBeenCalledWith("pr1", "resolved"),
		);
		expect(mocks.post.mock.calls.map((c) => c[0])).toEqual([
			"/projects/p1/proposals/pr1/claim",
			"/projects/p1/proposals/pr1/resolve",
		]);
		expect(mocks.post.mock.calls[1]?.[1]).toEqual({
			status: "resolved",
			result: { task_id: "t1", task_number: 5 },
		});
		expect(mocks.executeProposal).toHaveBeenCalledWith(
			"p1",
			"task.create",
			expect.objectContaining({ title: "Fix the login page" }),
		);
	});

	it("records a failed write and shows an error", async () => {
		serve({ chips: [chip()] });
		mocks.post.mockResolvedValue({ data: { data: {} } });
		mocks.executeProposal.mockRejectedValue(new Error("nope"));
		wrap(<ProposalsSection projectId="p1" channelId="c1" names={new Map()} />);
		await userEvent.click(
			await screen.findByRole("button", { name: "Create task" }),
		);
		await waitFor(() =>
			expect(mocks.reportResolved).toHaveBeenCalledWith("pr1", "failed"),
		);
		expect(mocks.post.mock.calls.at(-1)?.[1]).toEqual({
			status: "failed",
			result: { error: "write failed" },
		});
		expect(await screen.findByRole("alert")).toBeInTheDocument();
	});

	it("dismisses without writing", async () => {
		serve({ chips: [chip()] });
		mocks.post.mockResolvedValue({ data: { data: {} } });
		wrap(<ProposalsSection projectId="p1" channelId="c1" names={new Map()} />);
		await userEvent.click(
			await screen.findByRole("button", { name: "Dismiss" }),
		);
		await waitFor(() =>
			expect(mocks.reportResolved).toHaveBeenCalledWith("pr1", "rejected"),
		);
		expect(mocks.executeProposal).not.toHaveBeenCalled();
		expect(mocks.post).toHaveBeenCalledWith(
			"/projects/p1/proposals/pr1/resolve",
			{ status: "rejected", result: undefined },
		);
	});

	it("will not create a task with an empty title", async () => {
		serve({ chips: [chip()] });
		wrap(<ProposalsSection projectId="p1" channelId="c1" names={new Map()} />);
		await userEvent.clear(await screen.findByDisplayValue("Fix login"));
		expect(screen.getByRole("button", { name: "Create task" })).toBeDisabled();
	});
});
