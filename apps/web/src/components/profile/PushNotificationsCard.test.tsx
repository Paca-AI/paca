import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithQueries } from "@/test/render-with-queries";
import { PushNotificationsCard } from "./PushNotificationsCard";

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }));
vi.mock("@/lib/api-client", () => ({ apiClient: { instance: http } }));

const push = vi.hoisted(() => ({
	supported: true,
	permission: "default" as NotificationPermission,
	subscribed: false,
	optedOut: false,
	setOptedOut: vi.fn(),
	enable: vi.fn(),
	disable: vi.fn(),
}));
vi.mock("@/lib/push-api", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@/lib/push-api")>();
	return {
		...actual,
		isPushSupported: () => push.supported,
		pushPermission: () => push.permission,
		isPushOptedOut: () => push.optedOut,
		setPushOptedOut: push.setOptedOut,
		currentPushSubscription: async () => (push.subscribed ? {} : null),
		enablePush: push.enable,
		disablePush: push.disable,
	};
});

const settings = {
	public_key: "KEY",
	subscription_count: 0,
	preferences: [
		{ type: "assigned", enabled: true },
		{ type: "mentioned", enabled: false },
		{ type: "question", enabled: true },
		{ type: "automation", enabled: true },
	],
};

beforeEach(() => {
	vi.clearAllMocks();
	push.supported = true;
	push.permission = "default";
	push.subscribed = false;
	push.optedOut = false;
	http.get.mockResolvedValue({ data: { data: settings } });
});

describe("PushNotificationsCard", () => {
	it("says so when the browser has no push support", async () => {
		push.supported = false;
		renderWithQueries(<PushNotificationsCard />);
		expect(
			await screen.findByText(
				"This browser doesn't support push notifications.",
			),
		).toBeInTheDocument();
	});

	it("shows each type's saved state", async () => {
		renderWithQueries(<PushNotificationsCard />);
		const mentions = await screen.findByRole("switch", { name: "Mentions" });
		expect(mentions).not.toBeChecked();
		expect(
			screen.getByRole("switch", { name: "Tasks assigned to me" }),
		).toBeChecked();
	});

	it("saves a type switched off", async () => {
		http.put.mockResolvedValue({});
		renderWithQueries(<PushNotificationsCard />);
		await userEvent.click(
			await screen.findByRole("switch", { name: "Tasks assigned to me" }),
		);
		await waitFor(() =>
			expect(http.put).toHaveBeenCalledWith("/users/me/push/preferences", {
				type: "assigned",
				enabled: false,
			}),
		);
	});

	it("turns push on for this browser with the server's key", async () => {
		push.optedOut = true; // not the automatic path: the user switches it on
		push.enable.mockResolvedValue("enabled");
		renderWithQueries(<PushNotificationsCard />);
		await userEvent.click(
			await screen.findByRole("switch", { name: "Notify this browser" }),
		);
		await waitFor(() => expect(push.enable).toHaveBeenCalledWith("KEY"));
	});

	it("explains a refused permission", async () => {
		push.optedOut = true;
		push.enable.mockResolvedValue("denied");
		renderWithQueries(<PushNotificationsCard />);
		await userEvent.click(
			await screen.findByRole("switch", { name: "Notify this browser" }),
		);
		expect(
			await screen.findByText(/Notifications are blocked for this site/),
		).toBeInTheDocument();
	});

	it("turns push off for a subscribed browser", async () => {
		push.subscribed = true;
		push.disable.mockResolvedValue(undefined);
		renderWithQueries(<PushNotificationsCard />);
		const sw = await screen.findByRole("switch", {
			name: "Notify this browser",
		});
		await waitFor(() => expect(sw).toBeChecked());
		await userEvent.click(sw);
		await waitFor(() => expect(push.disable).toHaveBeenCalled());
	});

	it("subscribes this browser by itself when it has not been switched off", async () => {
		push.enable.mockResolvedValue("enabled");
		renderWithQueries(<PushNotificationsCard />);
		await waitFor(() => expect(push.enable).toHaveBeenCalledWith("KEY"));
		expect(push.enable).toHaveBeenCalledTimes(1);
		// An automatic subscription is not the user's choice to remember.
		expect(push.setOptedOut).not.toHaveBeenCalled();
	});

	it("does not subscribe by itself after the user switched it off", async () => {
		push.optedOut = true;
		renderWithQueries(<PushNotificationsCard />);
		await screen.findByRole("switch", { name: "Notify this browser" });
		expect(push.enable).not.toHaveBeenCalled();
	});

	it("does not subscribe by itself when notifications are blocked", async () => {
		push.permission = "denied";
		renderWithQueries(<PushNotificationsCard />);
		await screen.findByRole("switch", { name: "Notify this browser" });
		expect(push.enable).not.toHaveBeenCalled();
	});

	it("does not subscribe by itself when already subscribed", async () => {
		push.subscribed = true;
		renderWithQueries(<PushNotificationsCard />);
		const sw = await screen.findByRole("switch", {
			name: "Notify this browser",
		});
		await waitFor(() => expect(sw).toBeChecked());
		expect(push.enable).not.toHaveBeenCalled();
	});

	it("shows no error when the automatic attempt cannot prompt", async () => {
		push.enable.mockRejectedValue(new Error("needs a user gesture"));
		renderWithQueries(<PushNotificationsCard />);
		await waitFor(() => expect(push.enable).toHaveBeenCalled());
		await screen.findByRole("switch", { name: "Notify this browser" });
		expect(
			screen.queryByText("Couldn't turn on notifications for this browser."),
		).not.toBeInTheDocument();
	});

	it("remembers when the user switches it off", async () => {
		push.subscribed = true;
		push.disable.mockResolvedValue(undefined);
		renderWithQueries(<PushNotificationsCard />);
		const sw = await screen.findByRole("switch", {
			name: "Notify this browser",
		});
		await waitFor(() => expect(sw).toBeChecked());
		await userEvent.click(sw);
		await waitFor(() => expect(push.setOptedOut).toHaveBeenCalledWith(true));
	});
});
