import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithQueries } from "@/test/render-with-queries";
import { SessionsCard } from "./SessionsCard";

const http = vi.hoisted(() => ({ get: vi.fn(), delete: vi.fn() }));
vi.mock("@/lib/api-client", () => ({ apiClient: { instance: http } }));

const now = new Date().toISOString();
const here = {
	id: "f-here",
	current: true,
	browser: "Chrome",
	os: "Linux",
	ip: "203.0.113.4",
	remember_me: true,
	created_at: "2026-10-01T10:00:00Z",
	last_active_at: now,
};
const phone = {
	...here,
	id: "f-phone",
	current: false,
	browser: "Safari",
	os: "iOS",
	ip: "198.51.100.8",
};

function serve(sessions = [here, phone]) {
	http.get.mockResolvedValue({ data: { data: sessions } });
}

beforeEach(() => vi.clearAllMocks());

describe("SessionsCard", () => {
	it("lists devices and marks the current one without a sign-out button", async () => {
		serve();
		renderWithQueries(<SessionsCard />);

		expect(await screen.findByText("Chrome on Linux")).toBeInTheDocument();
		expect(screen.getByText("Safari on iOS")).toBeInTheDocument();
		expect(screen.getByText("This device")).toBeInTheDocument();
		expect(
			screen.queryByRole("button", { name: "Sign out Chrome on Linux" }),
		).not.toBeInTheDocument();
		expect(
			screen.getByRole("button", { name: "Sign out Safari on iOS" }),
		).toBeInTheDocument();
	});

	it("signs one device out after confirming", async () => {
		serve();
		http.delete.mockResolvedValue({ data: { data: {} } });
		const user = userEvent.setup();
		renderWithQueries(<SessionsCard />);

		await user.click(
			await screen.findByRole("button", { name: "Sign out Safari on iOS" }),
		);
		const dialog = await screen.findByRole("dialog");
		expect(http.delete).not.toHaveBeenCalled();
		await user.click(within(dialog).getByRole("button", { name: "Sign out" }));

		await waitFor(() =>
			expect(http.delete).toHaveBeenCalledWith("/users/me/sessions/f-phone"),
		);
	});

	it("signs out every other device in one go", async () => {
		serve();
		http.delete.mockResolvedValue({ data: { data: { revoked: 1 } } });
		const user = userEvent.setup();
		renderWithQueries(<SessionsCard />);

		await user.click(
			await screen.findByRole("button", {
				name: "Sign out all other devices",
			}),
		);
		const dialog = await screen.findByRole("dialog");
		await user.click(within(dialog).getByRole("button", { name: "Sign out" }));

		await waitFor(() =>
			expect(http.delete).toHaveBeenCalledWith("/users/me/sessions"),
		);
	});

	it("offers no bulk sign-out when this is the only device", async () => {
		serve([here]);
		renderWithQueries(<SessionsCard />);
		await screen.findByText("Chrome on Linux");
		expect(
			screen.queryByRole("button", { name: "Sign out all other devices" }),
		).not.toBeInTheDocument();
	});

	it("shows an error when the sign-out fails and keeps the dialog open", async () => {
		serve();
		http.delete.mockRejectedValue(new Error("boom"));
		const user = userEvent.setup();
		renderWithQueries(<SessionsCard />);

		await user.click(
			await screen.findByRole("button", { name: "Sign out Safari on iOS" }),
		);
		const dialog = await screen.findByRole("dialog");
		await user.click(within(dialog).getByRole("button", { name: "Sign out" }));

		expect(
			await within(dialog).findByText(/something went wrong/i),
		).toBeInTheDocument();
	});
});
