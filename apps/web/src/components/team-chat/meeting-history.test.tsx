import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/i18n";

const mocks = vi.hoisted(() => ({ get: vi.fn(), download: vi.fn() }));

vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get: mocks.get } },
}));
vi.mock("@/lib/meeting-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/meeting-api")>(
			"@/lib/meeting-api",
		);
	return { ...actual, downloadTranscript: mocks.download };
});

import { MeetingHistoryButton } from "./meeting-history";

const meeting = (over: Record<string, unknown> = {}) => ({
	call_id: "k1",
	started_at: "2026-01-01T10:00:00Z",
	ended_at: "2026-01-01T10:30:00Z",
	segments: 2,
	speakers: ["Alice", "Bob"],
	...over,
});

function serve(o: {
	enabled?: boolean;
	recording?: boolean;
	meetings?: unknown[];
	recordings?: unknown[];
}) {
	mocks.get.mockImplementation(async (url: string) => {
		const data = (v: unknown) => ({ data: { data: v } });
		if (url === "/chat/meeting/config")
			return data({ enabled: o.enabled ?? true });
		if (url === "/chat/calls/config")
			return data({ enabled: true, recording: o.recording ?? true });
		if (url.endsWith("/meetings")) return data(o.meetings ?? []);
		if (url.endsWith("/recordings")) return data(o.recordings ?? []);
		if (url.endsWith("/url"))
			return data({ url: "https://files.example/rec.mp4?sig=1" });
		if (url.endsWith("/transcript")) {
			const first = (o.meetings ?? [])[0] as { segments?: number } | undefined;
			if (first?.segments === 0) return data([]);
			return data([
				{
					speaker_id: "a",
					speaker_name: "Alice",
					text: "ship it friday",
					spoken_at: "2026-01-01T10:01:00Z",
				},
				{
					speaker_id: "b",
					speaker_name: "Bob",
					text: "agreed",
					spoken_at: "2026-01-01T10:02:00Z",
				},
			]);
		}
		throw new Error(`unexpected GET ${url}`);
	});
}

function renderButton() {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<MeetingHistoryButton projectId="p1" channelId="c1" />
		</QueryClientProvider>,
	);
}

beforeEach(() => vi.resetAllMocks());

describe("MeetingHistoryButton", () => {
	it("is not there when assistants are off or nothing was transcribed", async () => {
		serve({ enabled: false });
		const off = renderButton();
		await waitFor(() => expect(mocks.get).toHaveBeenCalled());
		expect(off.container).toBeEmptyDOMElement();
		off.unmount();

		serve({ meetings: [] });
		const none = renderButton();
		await waitFor(() =>
			expect(mocks.get).toHaveBeenCalledWith(
				"/projects/p1/channels/c1/meetings",
			),
		);
		expect(none.container).toBeEmptyDOMElement();
	});

	it("lists the calls and shows who said what", async () => {
		serve({
			meetings: [
				meeting(),
				meeting({
					call_id: "k2",
					started_at: "2025-12-31T09:00:00Z",
					segments: 5,
					speakers: [],
				}),
			],
		});
		renderButton();
		await userEvent.click(
			await screen.findByRole("button", { name: "Meeting history" }),
		);
		expect(await screen.findByText("Alice:")).toBeInTheDocument();
		expect(screen.getByText("ship it friday")).toBeInTheDocument();
		expect(screen.getByText("Bob:")).toBeInTheDocument();
		expect(screen.getByText("Alice, Bob")).toBeInTheDocument();
		expect(mocks.get).toHaveBeenCalledWith(
			"/projects/p1/channels/c1/meetings/k1/transcript",
		);
		// Another call can be opened.
		await userEvent.click(screen.getByText("Lines: 5"));
		await waitFor(() =>
			expect(mocks.get).toHaveBeenCalledWith(
				"/projects/p1/channels/c1/meetings/k2/transcript",
			),
		);
	});

	it("marks a call that is still going", async () => {
		serve({ meetings: [meeting({ ended_at: null })] });
		renderButton();
		await userEvent.click(
			await screen.findByRole("button", { name: "Meeting history" }),
		);
		expect(await screen.findByText("In progress")).toBeInTheDocument();
	});

	it("saves a transcript as a file", async () => {
		serve({ meetings: [meeting()] });
		mocks.download.mockResolvedValue(undefined);
		renderButton();
		await userEvent.click(
			await screen.findByRole("button", { name: "Meeting history" }),
		);
		await userEvent.click(
			await screen.findByRole("button", { name: "Download transcript" }),
		);
		expect(mocks.download).toHaveBeenCalledWith("p1", "c1", "k1");
	});

	const rec = (over: Record<string, unknown> = {}) => ({
		id: "r1",
		call_id: "k1",
		status: "complete",
		started_by: "a",
		started_at: "2026-01-01T10:00:00Z",
		ended_at: "2026-01-01T10:30:00Z",
		duration_ms: 125000,
		size_bytes: 1000,
		error: "",
		...over,
	});

	it("plays a finished recording through a link fetched on demand", async () => {
		serve({ meetings: [meeting()], recordings: [rec()] });
		const { container } = renderButton();
		await userEvent.click(
			await screen.findByRole("button", { name: "Meeting history" }),
		);
		// Nothing is loaded until it is asked for.
		expect(mocks.get).not.toHaveBeenCalledWith(
			"/projects/p1/channels/c1/recordings/r1/url",
			expect.anything(),
		);
		await userEvent.click(
			await screen.findByRole("button", { name: "Play recording (2 min)" }),
		);
		await waitFor(() =>
			expect(document.querySelector("video")?.getAttribute("src")).toBe(
				"https://files.example/rec.mp4?sig=1",
			),
		);
		expect(container).toBeDefined();
	});

	it("says when a recording is still processing or failed", async () => {
		serve({
			meetings: [meeting()],
			recordings: [
				rec({ id: "r2", status: "stopping" }),
				rec({ id: "r3", status: "failed" }),
			],
		});
		renderButton();
		await userEvent.click(
			await screen.findByRole("button", { name: "Meeting history" }),
		);
		expect(
			await screen.findByText("The recording is being processed."),
		).toBeInTheDocument();
		expect(screen.getByText("The recording failed.")).toBeInTheDocument();
	});

	it("lists a call that was only recorded", async () => {
		serve({
			meetings: [meeting({ segments: 0, speakers: [] })],
			recordings: [rec()],
		});
		renderButton();
		await userEvent.click(
			await screen.findByRole("button", { name: "Meeting history" }),
		);
		expect(
			await screen.findByRole("button", { name: "Play recording (2 min)" }),
		).toBeInTheDocument();
		expect(
			await screen.findByText("Nothing has been said yet."),
		).toBeInTheDocument();
	});

	it("still plays a recording when the assistant is off", async () => {
		serve({ enabled: false, meetings: [], recordings: [rec()] });
		renderButton();
		await userEvent.click(
			await screen.findByRole("button", { name: "Meeting history" }),
		);
		expect(
			await screen.findByRole("button", { name: "Play recording (2 min)" }),
		).toBeInTheDocument();
		expect(screen.queryByText("Nothing has been said yet.")).toBeNull();
	});
});
