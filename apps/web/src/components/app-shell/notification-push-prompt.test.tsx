import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { renderWithQueries } from "@/test/render-with-queries";
import { NotificationPushPrompt } from "./notification-push-prompt";

const push = vi.hoisted(() => ({
	supported: true,
	permission: "default" as NotificationPermission,
	subscribed: false,
	optedOut: false,
	enable: vi.fn(),
	setOptedOut: vi.fn(),
	settings: vi.fn(),
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
		pushSettingsQueryOptions: {
			...actual.pushSettingsQueryOptions,
			queryFn: push.settings,
		},
	};
});

beforeEach(() => {
	vi.clearAllMocks();
	window.localStorage.clear();
	push.supported = true;
	push.permission = "default";
	push.subscribed = false;
	push.optedOut = false;
	push.settings.mockResolvedValue({
		public_key: "KEY",
		subscription_count: 0,
		preferences: [],
	});
});

describe("NotificationPushPrompt", () => {
	it("offers to turn push on, and does it with the server's key on a click", async () => {
		push.enable.mockResolvedValue("enabled");
		renderWithQueries(<NotificationPushPrompt />);
		await userEvent.click(
			await screen.findByRole("button", { name: "Turn on" }),
		);
		await waitFor(() => expect(push.enable).toHaveBeenCalledWith("KEY"));
		await waitFor(() =>
			expect(
				screen.queryByText("Get notified even when Paca is closed."),
			).not.toBeInTheDocument(),
		);
	});

	it("can be waved away, and stays away", async () => {
		const first = renderWithQueries(<NotificationPushPrompt />);
		await userEvent.click(
			await screen.findByRole("button", { name: "Not now" }),
		);
		expect(
			screen.queryByRole("button", { name: "Turn on" }),
		).not.toBeInTheDocument();
		first.unmount();

		renderWithQueries(<NotificationPushPrompt />);
		await waitFor(() => expect(push.settings).toHaveBeenCalled());
		expect(
			screen.queryByRole("button", { name: "Turn on" }),
		).not.toBeInTheDocument();
	});

	it.each([
		[
			"the browser has no push support",
			() => {
				push.supported = false;
			},
		],
		[
			"notifications are blocked",
			() => {
				push.permission = "denied";
			},
		],
		[
			"notifications are already allowed",
			() => {
				push.permission = "granted";
			},
		],
		[
			"this browser is already subscribed",
			() => {
				push.subscribed = true;
			},
		],
		[
			"the user switched push off",
			() => {
				push.optedOut = true;
			},
		],
	])("stays out of the way when %s", async (_name, arrange) => {
		arrange();
		renderWithQueries(<NotificationPushPrompt />);
		await new Promise((r) => setTimeout(r, 30));
		expect(
			screen.queryByRole("button", { name: "Turn on" }),
		).not.toBeInTheDocument();
	});
});
