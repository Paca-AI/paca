import { screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { NotificationHealth } from "@/lib/notification-health-api";
import { renderWithQueries } from "@/test/render-with-queries";
import { NotificationHealthSettings } from "./NotificationHealthSettings";

const http = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("@/lib/api-client", () => ({ apiClient: { instance: http } }));

const healthy: NotificationHealth = {
	status: "ok",
	reasons: [],
	checked_at: "2026-11-23T12:00:00Z",
	channels: [
		{
			channel: "realtime",
			pending: 0,
			retrying: 0,
			overdue: 0,
			oldest_overdue: null,
			sent: 120,
			failed: 0,
			sent_last_hour: 14,
			failed_last_hour: 0,
			latency_p50_seconds: 0.3,
			latency_p95_seconds: 1.8,
			top_errors: [],
		},
	],
	streams: [
		{
			stream: "paca.plugin_events",
			group: "api.plugin_dispatcher",
			pending: 0,
			oldest_pending_idle_seconds: 0,
			dead_last_hour: 0,
			dead_total: 0,
		},
	],
};

function serve(h: NotificationHealth) {
	http.get.mockResolvedValue({ data: { data: h } });
}

beforeEach(() => vi.clearAllMocks());

describe("NotificationHealthSettings", () => {
	it("shows a healthy pipeline with its channel and stream numbers", async () => {
		serve(healthy);
		renderWithQueries(<NotificationHealthSettings />);
		expect(await screen.findByText("Healthy")).toBeInTheDocument();
		expect(
			screen.getByRole("rowheader", { name: "Realtime" }),
		).toBeInTheDocument();
		expect(screen.getByText("0.3 s")).toBeInTheDocument();
		expect(screen.getByText("1.8 s")).toBeInTheDocument();
		expect(screen.getByText("paca.plugin_events")).toBeInTheDocument();
		expect(http.get).toHaveBeenCalledWith("/admin/notifications/health");
	});

	it("lists why it is degraded, and the commonest failure reasons", async () => {
		serve({
			...healthy,
			status: "degraded",
			reasons: ["push: 3 deliveries failed in the last hour"],
			channels: [
				{
					...healthy.channels[0],
					channel: "push",
					failed_last_hour: 3,
					latency_p50_seconds: null,
					latency_p95_seconds: null,
					top_errors: [{ error: "push service returned 503", count: 3 }],
				},
			],
		});
		renderWithQueries(<NotificationHealthSettings />);
		expect(await screen.findByText("Degraded")).toBeInTheDocument();
		expect(
			screen.getByText("push: 3 deliveries failed in the last hour"),
		).toBeInTheDocument();
		expect(
			screen.getByText(/Push: 3 failed — push service returned 503/),
		).toBeInTheDocument();
		// Nothing was sent, so latency is unknown rather than zero.
		expect(screen.getAllByText("—").length).toBeGreaterThanOrEqual(2);
	});

	it("says so when delivery is failing", async () => {
		serve({
			...healthy,
			status: "failing",
			reasons: ["realtime: looks stalled"],
		});
		renderWithQueries(<NotificationHealthSettings />);
		expect(await screen.findByText("Failing")).toBeInTheDocument();
	});

	it("says so when nothing has been delivered yet", async () => {
		serve({ ...healthy, channels: [], streams: [] });
		renderWithQueries(<NotificationHealthSettings />);
		expect(await screen.findByText("No deliveries yet.")).toBeInTheDocument();
	});

	it("reports a load failure", async () => {
		http.get.mockRejectedValue(new Error("403"));
		renderWithQueries(<NotificationHealthSettings />);
		expect(
			await screen.findByText("Couldn't load notification health."),
		).toBeInTheDocument();
	});
});
