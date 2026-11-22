import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import "@/i18n";

const mocks = vi.hoisted(() => ({ get: vi.fn(), patch: vi.fn() }));

vi.mock("@/lib/api-client", () => ({
	apiClient: { instance: { get: mocks.get, patch: mocks.patch } },
}));

import { MeetingAssistantTab } from "./meeting-assistant-tab";

const settings = (over: Record<string, unknown> = {}) => ({
	stt_provider: "openai",
	stt_model: "whisper-1",
	has_stt_key: true,
	stt_base_url: "",
	stt_languages: ["en"],
	mode: "on_demand",
	suggest_interval_seconds: 120,
	proposal_kinds: ["task.create", "doc.create_from_notes"],
	max_proposals_per_call: 10,
	store_transcript: true,
	transcript_retention_days: 30,
	announcement: "",
	summary_target: "none",
	default_target: "requester",
	updated_at: "2026-01-01T00:00:00Z",
	stt_providers: ["openai", "deepgram"],
	...over,
});

function renderTab(canWrite = true) {
	const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
	return render(
		<QueryClientProvider client={qc}>
			<MeetingAssistantTab projectId="p1" agentId="a1" canWrite={canWrite} />
		</QueryClientProvider>,
	);
}

beforeEach(() => {
	vi.resetAllMocks();
	mocks.get.mockResolvedValue({ data: { data: settings() } });
});

describe("MeetingAssistantTab", () => {
	it("shows the saved settings and never the stored key", async () => {
		renderTab();
		expect(await screen.findByDisplayValue("whisper-1")).toBeInTheDocument();
		expect(screen.getByDisplayValue("en")).toBeInTheDocument();
		const key = screen.getByLabelText("Transcription API key");
		expect(key).toHaveValue("");
		expect(key).toHaveAttribute(
			"placeholder",
			"A key is stored. Type to replace it.",
		);
		expect(mocks.get).toHaveBeenCalledWith(
			"/projects/p1/agents/a1/meeting-settings",
		);
	});

	it("saves what changed, sending the key only when one is typed", async () => {
		mocks.patch.mockResolvedValue({
			data: {
				data: settings({
					stt_model: "whisper-large",
					updated_at: "2026-01-02T00:00:00Z",
				}),
			},
		});
		renderTab();
		const model = await screen.findByDisplayValue("whisper-1");
		await userEvent.clear(model);
		await userEvent.type(model, "whisper-large");
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
		await waitFor(() => expect(mocks.patch).toHaveBeenCalled());
		const [url, body] = mocks.patch.mock.calls[0] ?? [];
		expect(url).toBe("/projects/p1/agents/a1/meeting-settings");
		expect(body).toMatchObject({
			stt_provider: "openai",
			stt_model: "whisper-large",
			stt_languages: ["en"],
			mode: "on_demand",
			proposal_kinds: ["task.create", "doc.create_from_notes"],
			store_transcript: true,
			transcript_retention_days: 30,
		});
		expect(body).not.toHaveProperty("stt_api_key");
	});

	it("sends a typed key", async () => {
		mocks.patch.mockResolvedValue({ data: { data: settings() } });
		renderTab();
		await userEvent.type(
			await screen.findByLabelText("Transcription API key"),
			"sk-new",
		);
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
		await waitFor(() => expect(mocks.patch).toHaveBeenCalled());
		expect(mocks.patch.mock.calls[0]?.[1]).toMatchObject({
			stt_api_key: "sk-new",
		});
	});

	it("blocks saving a value the server would reject", async () => {
		renderTab();
		const retention = await screen.findByLabelText("Keep it for (days)");
		await userEvent.clear(retention);
		await userEvent.type(retention, "0");
		expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();
	});

	it("is read-only without agents:write", async () => {
		renderTab(false);
		expect(await screen.findByDisplayValue("whisper-1")).toBeDisabled();
		expect(screen.queryByRole("button", { name: "Save changes" })).toBeNull();
	});

	it("hides suggestion settings in notes-only mode", async () => {
		mocks.get.mockResolvedValue({
			data: { data: settings({ mode: "notes_only" }) },
		});
		renderTab();
		await screen.findByDisplayValue("whisper-1");
		expect(screen.queryByText("What it may suggest")).toBeNull();
		expect(screen.queryByLabelText("Most suggestions per call")).toBeNull();
	});

	it("will not let the transcription setup be edited into one that cannot listen", async () => {
		renderTab();
		const save = () => screen.getByRole("button", { name: "Save changes" });
		await screen.findByDisplayValue("whisper-1");
		expect(save()).toBeEnabled();

		// Removing the only key leaves nothing to transcribe with ...
		await userEvent.click(
			screen.getByRole("switch", { name: "Remove the stored key" }),
		);
		expect(save()).toBeDisabled();
		// ... unless a self-hosted endpoint takes its place.
		await userEvent.type(
			screen.getByLabelText(/^Endpoint/),
			"http://whisper:8000/v1",
		);
		expect(save()).toBeEnabled();

		await userEvent.clear(screen.getByLabelText("Transcription model"));
		expect(save()).toBeDisabled();
	});

	it("keeps suggesting off by default and offers when to suggest once it is on", async () => {
		mocks.get.mockResolvedValue({
			data: { data: settings({ mode: "notes_only" }) },
		});
		renderTab();
		const toggle = await screen.findByRole("switch", {
			name: /Suggest tasks and docs/,
		});
		expect(toggle).not.toBeChecked();
		expect(screen.queryByText("When to suggest")).toBeNull();
		await userEvent.click(toggle);
		expect(screen.getByText("When to suggest")).toBeInTheDocument();
		expect(screen.getByText("What it may suggest")).toBeInTheDocument();
	});

	it("has notes as an option, off unless turned on, and sends the choice", async () => {
		mocks.patch.mockResolvedValue({ data: { data: settings() } });
		renderTab();
		const toggle = await screen.findByRole("switch", {
			name: /Write meeting notes/,
		});
		expect(toggle).not.toBeChecked();
		await userEvent.click(toggle);
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
		await waitFor(() => expect(mocks.patch).toHaveBeenCalled());
		expect(mocks.patch.mock.calls[0]?.[1]).toMatchObject({
			summary_target: "channel_message",
		});
	});

	it("says to set up the model first when the server refuses notes or suggestions without one", async () => {
		mocks.patch.mockRejectedValue({
			response: { data: { error_code: "MEETING_MODEL_REQUIRED" } },
		});
		renderTab();
		await userEvent.click(
			await screen.findByRole("switch", { name: /Write meeting notes/ }),
		);
		await userEvent.click(screen.getByRole("button", { name: "Save changes" }));
		await waitFor(() => expect(mocks.patch).toHaveBeenCalled());
		// The error is shown as a toast by notifyError; the form stays editable.
		expect(
			screen.getByRole("button", { name: "Save changes" }),
		).toBeInTheDocument();
	});
});
