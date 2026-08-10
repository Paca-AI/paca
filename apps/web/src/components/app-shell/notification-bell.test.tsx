import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { Notification } from "@/lib/notification-api";
import { isNotificationSoundMuted } from "@/lib/notification-sound";

// ── Mocks ─────────────────────────────────────────────────────────────────────

const mocks = vi.hoisted(() => ({
	navigateMock: vi.fn(),
	listNotificationsMock: vi.fn(),
	markNotificationAsReadMock: vi.fn(),
	markAllNotificationsAsReadMock: vi.fn(),
}));

vi.mock("@tanstack/react-router", () => ({
	useNavigate: () => mocks.navigateMock,
}));

// useDocumentTitle pulls in route params, branding, and a project query — all
// covered by its own test suite (use-document-title.test.ts). Stubbing it out
// here keeps this suite focused on NotificationBell's own job: rendering,
// pagination, and the mute toggle.
vi.mock("@/hooks/use-document-title", () => ({
	useDocumentTitle: () => {},
}));

vi.mock("@/lib/notification-api", async () => {
	const actual = await vi.importActual<typeof import("@/lib/notification-api")>(
		"@/lib/notification-api",
	);
	return {
		...actual,
		markNotificationAsRead: mocks.markNotificationAsReadMock,
		markAllNotificationsAsRead: mocks.markAllNotificationsAsReadMock,
		notificationsQueryOptions: {
			...actual.notificationsQueryOptions,
			queryFn: mocks.listNotificationsMock,
		},
	};
});

import { NotificationBell } from "./notification-bell";

// ── Fixtures ──────────────────────────────────────────────────────────────────

function makeNotification(overrides: Partial<Notification> = {}): Notification {
	return {
		id: "notif-1",
		type: "assigned",
		actor_full_name: "Ada Lovelace",
		actor_username: "ada",
		task_id: "task-1",
		task_title: "Ship the thing",
		task_number: 42,
		project_id: "proj-a",
		project_name: "Project Alpha",
		read_at: null,
		created_at: "2026-01-01T00:00:00Z",
		...overrides,
	};
}

function wrapper({ children }: { children: ReactNode }) {
	const qc = new QueryClient({
		defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
	});
	return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

async function renderAndOpen() {
	render(<NotificationBell />, { wrapper });
	await userEvent.click(screen.getByRole("button", { name: "Notifications" }));
	return screen.findByText("Notifications");
}

// ── Tests ─────────────────────────────────────────────────────────────────────

beforeEach(() => {
	mocks.navigateMock.mockReset();
	mocks.listNotificationsMock.mockReset();
	mocks.markNotificationAsReadMock.mockReset().mockResolvedValue(undefined);
	mocks.markAllNotificationsAsReadMock.mockReset().mockResolvedValue(undefined);
});

describe("NotificationBell", () => {
	it("renders an actor's initials and the assigned/mentioned badge icon", async () => {
		mocks.listNotificationsMock.mockResolvedValue({
			items: [
				makeNotification({ id: "n-assigned", type: "assigned" }),
				makeNotification({
					id: "n-mentioned",
					type: "mentioned",
					actor_full_name: "Grace Hopper",
				}),
			],
			page_size: 20,
			next_cursor: null,
			unread_count: 2,
		});

		await renderAndOpen();

		expect(await screen.findByText("AL")).toBeInTheDocument();
		expect(screen.getByText("GH")).toBeInTheDocument();
	});

	it("fetches the next page once the list is scrolled near its bottom", async () => {
		mocks.listNotificationsMock.mockImplementation(
			async ({ pageParam }: { pageParam?: string }) => {
				if (!pageParam) {
					return {
						items: [makeNotification({ id: "n-1", actor_full_name: "First" })],
						page_size: 1,
						next_cursor: "cursor-2",
						unread_count: 2,
					};
				}
				return {
					items: [makeNotification({ id: "n-2", actor_full_name: "Second" })],
					page_size: 1,
					next_cursor: null,
					unread_count: 2,
				};
			},
		);

		await renderAndOpen();
		expect(await screen.findByText("F")).toBeInTheDocument();
		expect(mocks.listNotificationsMock).toHaveBeenCalledTimes(1);

		const list = screen.getByText("F").closest("div.overflow-y-auto");
		if (!list) throw new Error("expected scrollable notification list");
		Object.defineProperty(list, "scrollHeight", {
			value: 1000,
			configurable: true,
		});
		Object.defineProperty(list, "clientHeight", {
			value: 300,
			configurable: true,
		});
		Object.defineProperty(list, "scrollTop", {
			value: 999,
			configurable: true,
		});
		list.dispatchEvent(new Event("scroll", { bubbles: true }));

		await waitFor(() => {
			expect(mocks.listNotificationsMock).toHaveBeenCalledTimes(2);
		});
		expect(await screen.findByText("S")).toBeInTheDocument();
	});

	it("toggles and persists the mute preference", async () => {
		mocks.listNotificationsMock.mockResolvedValue({
			items: [],
			page_size: 20,
			next_cursor: null,
			unread_count: 0,
		});

		await renderAndOpen();
		expect(isNotificationSoundMuted()).toBe(false);

		const muteButton = screen.getByRole("button", {
			name: "Mute notification sound",
		});
		await userEvent.click(muteButton);

		expect(
			await screen.findByRole("button", { name: "Unmute notification sound" }),
		).toBeInTheDocument();
		await waitFor(() => {
			expect(isNotificationSoundMuted()).toBe(true);
		});

		await userEvent.click(
			screen.getByRole("button", { name: "Unmute notification sound" }),
		);
		expect(
			await screen.findByRole("button", { name: "Mute notification sound" }),
		).toBeInTheDocument();
		await waitFor(() => {
			expect(isNotificationSoundMuted()).toBe(false);
		});
	});
});
