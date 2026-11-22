import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/i18n";

const mocks = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), del: vi.fn() }));

vi.mock("@/lib/api-client", () => ({
	apiClient: {
		instance: { get: mocks.get, post: mocks.post, delete: mocks.del },
	},
}));

import { RecordButton, RecordingNotice } from "./call-recording";

const recording = (status: string) => ({
	id: "r1",
	call_id: "k1",
	status,
	started_by: "me",
	started_at: "2026-01-01T00:00:00Z",
	ended_at: null,
	duration_ms: 0,
	size_bytes: 0,
	error: "",
});

function serve(o: {
	recording?: boolean;
	rec?: ReturnType<typeof recording> | null;
}) {
	mocks.get.mockImplementation(async (url: string) => {
		const data = (v: unknown) => ({ data: { data: v } });
		if (url === "/chat/calls/config")
			return data({ enabled: true, recording: o.recording ?? true });
		if (url.endsWith("/call")) {
			return data({
				channel_id: "c1",
				started_at: null,
				participants: [],
				recording: o.rec ?? null,
			});
		}
		throw new Error(`unexpected GET ${url}`);
	});
}

function wrap(node: React.ReactNode) {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

beforeEach(() => vi.resetAllMocks());

describe("RecordButton", () => {
	it("is not offered when this deployment cannot record", async () => {
		serve({ recording: false });
		const { container } = wrap(<RecordButton projectId="p1" channelId="c1" />);
		await waitFor(() => expect(mocks.get).toHaveBeenCalled());
		expect(container).toBeEmptyDOMElement();
	});

	it("asks before it starts, telling everyone is notified", async () => {
		serve({});
		mocks.post.mockResolvedValue({ data: { data: recording("starting") } });
		wrap(<RecordButton projectId="p1" channelId="c1" />);
		await userEvent.click(
			await screen.findByRole("button", { name: "Start recording" }),
		);
		expect(screen.getByText("Record this call?")).toBeInTheDocument();
		expect(mocks.post).not.toHaveBeenCalled();

		await userEvent.click(
			screen
				.getAllByRole("button", { name: "Start recording" })
				.at(-1) as HTMLElement,
		);
		await waitFor(() =>
			expect(mocks.post).toHaveBeenCalledWith(
				"/projects/p1/channels/c1/call/recording",
			),
		);
	});

	it("can be cancelled without recording", async () => {
		serve({});
		wrap(<RecordButton projectId="p1" channelId="c1" />);
		await userEvent.click(
			await screen.findByRole("button", { name: "Start recording" }),
		);
		await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
		expect(mocks.post).not.toHaveBeenCalled();
	});

	it("stops a running recording", async () => {
		serve({ rec: recording("recording") });
		mocks.del.mockResolvedValue({});
		wrap(<RecordButton projectId="p1" channelId="c1" />);
		await userEvent.click(
			await screen.findByRole("button", { name: "Stop recording" }),
		);
		await waitFor(() =>
			expect(mocks.del).toHaveBeenCalledWith(
				"/projects/p1/channels/c1/call/recording",
			),
		);
	});

	it("shows why starting failed", async () => {
		serve({});
		mocks.post.mockRejectedValue({
			response: { data: { error_code: "RECORDING_ACTIVE" } },
		});
		wrap(<RecordButton projectId="p1" channelId="c1" />);
		await userEvent.click(
			await screen.findByRole("button", { name: "Start recording" }),
		);
		await userEvent.click(
			screen
				.getAllByRole("button", { name: "Start recording" })
				.at(-1) as HTMLElement,
		);
		expect(await screen.findByRole("alert")).toHaveTextContent(
			"already being recorded",
		);
	});
});

describe("RecordingNotice", () => {
	it("tells everyone in the call while it is recorded", async () => {
		serve({ rec: recording("recording") });
		wrap(<RecordingNotice projectId="p1" channelId="c1" />);
		expect(await screen.findByRole("status")).toHaveTextContent(
			"This call is being recorded.",
		);
	});

	it("says it is finishing, then goes away once the recording is done", async () => {
		serve({ rec: recording("stopping") });
		const first = wrap(<RecordingNotice projectId="p1" channelId="c1" />);
		expect(await screen.findByRole("status")).toHaveTextContent(
			"Finishing the recording",
		);
		first.unmount();

		serve({ rec: recording("complete") });
		const done = wrap(<RecordingNotice projectId="p1" channelId="c1" />);
		await waitFor(() => expect(mocks.get).toHaveBeenCalled());
		expect(done.container).toBeEmptyDOMElement();
	});

	it("shows nothing when the call is not recorded", async () => {
		serve({ rec: null });
		const { container } = wrap(
			<RecordingNotice projectId="p1" channelId="c1" />,
		);
		await waitFor(() => expect(mocks.get).toHaveBeenCalled());
		expect(container).toBeEmptyDOMElement();
	});
});
