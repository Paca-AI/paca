import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithQueries } from "@/test/render-with-queries";
import { DoNotDisturbCard } from "./DoNotDisturbCard";

const http = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }));
vi.mock("@/lib/api-client", () => ({ apiClient: { instance: http } }));

function serve(dnd: {
	active: boolean;
	until: string | null;
	indefinite: boolean;
}) {
	http.get.mockResolvedValue({
		data: {
			data: {
				public_key: "K",
				subscription_count: 0,
				preferences: [],
				do_not_disturb: dnd,
			},
		},
	});
}

beforeEach(() => vi.clearAllMocks());

describe("DoNotDisturbCard", () => {
	it("offers durations while it is off", async () => {
		serve({ active: false, until: null, indefinite: false });
		renderWithQueries(<DoNotDisturbCard />);
		for (const name of [
			"For 1 hour",
			"For 8 hours",
			"Until tomorrow morning",
			"Until I turn it off",
		]) {
			expect(await screen.findByRole("button", { name })).toBeInTheDocument();
		}
		expect(
			screen.queryByRole("button", { name: "Turn off" }),
		).not.toBeInTheDocument();
	});

	it("turns it on for an hour", async () => {
		serve({ active: false, until: null, indefinite: false });
		http.put.mockResolvedValue({ data: { data: {} } });
		renderWithQueries(<DoNotDisturbCard />);
		const before = Date.now();
		await userEvent.click(
			await screen.findByRole("button", { name: "For 1 hour" }),
		);
		await waitFor(() => expect(http.put).toHaveBeenCalled());
		const [path, body] = http.put.mock.calls[0];
		expect(path).toBe("/users/me/push/do-not-disturb");
		const until = new Date(body.until).getTime();
		expect(until).toBeGreaterThanOrEqual(before + 3_600_000 - 1000);
		expect(until).toBeLessThanOrEqual(Date.now() + 3_600_000 + 1000);
	});

	it("turns it on until switched off", async () => {
		serve({ active: false, until: null, indefinite: false });
		http.put.mockResolvedValue({ data: { data: {} } });
		renderWithQueries(<DoNotDisturbCard />);
		await userEvent.click(
			await screen.findByRole("button", { name: "Until I turn it off" }),
		);
		await waitFor(() =>
			expect(http.put).toHaveBeenCalledWith("/users/me/push/do-not-disturb", {
				indefinite: true,
			}),
		);
	});

	it("says when it ends and turns it off", async () => {
		serve({
			active: true,
			until: new Date(Date.now() + 3_600_000).toISOString(),
			indefinite: false,
		});
		http.put.mockResolvedValue({ data: { data: {} } });
		renderWithQueries(<DoNotDisturbCard />);
		expect(
			await screen.findByText(/Do Not Disturb is on until/),
		).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Turn off" }));
		await waitFor(() =>
			expect(http.put).toHaveBeenCalledWith("/users/me/push/do-not-disturb", {
				until: null,
			}),
		);
	});

	it("says so when it has no end", async () => {
		serve({ active: true, until: null, indefinite: true });
		renderWithQueries(<DoNotDisturbCard />);
		expect(
			await screen.findByText("Do Not Disturb is on until you turn it off."),
		).toBeInTheDocument();
	});

	it("treats an end time that has passed as off", async () => {
		serve({
			active: true,
			until: new Date(Date.now() - 60_000).toISOString(),
			indefinite: false,
		});
		renderWithQueries(<DoNotDisturbCard />);
		expect(
			await screen.findByRole("button", { name: "For 1 hour" }),
		).toBeInTheDocument();
	});
});
