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
	pushSettingsMock: vi.fn(),
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

// The bell reads Do Not Disturb from the push settings the layout keeps warm.
vi.mock("@/lib/push-api", async () => {
	const actual =
		await vi.importActual<typeof import("@/lib/push-api")>("@/lib/push-api");
	return {
		...actual,
		isPushSupported: () => false,
		pushSettingsQueryOptions: {
			...actual.pushSettingsQueryOptions,
			queryFn: mocks.pushSettingsMock,
		},
	};
});

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
		document_id: null,
		document_title: "",
		conversation_id: null,
		conversation_title: "",
		message: "",
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
	mocks.pushSettingsMock.mockReset().mockResolvedValue({
		public_key: "K",
		subscription_count: 0,
		preferences: [],
		do_not_disturb: { active: false, until: null, indefinite: false },
	});
});

describe("NotificationBell", () => {
	it("opens the document for a mention in a document comment", async () => {
		mocks.listNotificationsMock.mockResolvedValue({
			items: [
				makeNotification({
					type: "mentioned",
					actor_full_name: "Claude Agent",
					task_id: null,
					document_id: "doc-1",
					document_title: "Release Notes",
				}),
			],
			page_size: 20,
			next_cursor: null,
			unread_count: 1,
		});

		await renderAndOpen();

		await userEvent.click(
			await screen.findByText(/mentioned you in document Release Notes/),
		);
		expect(mocks.navigateMock).toHaveBeenLastCalledWith({
			to: "/projects/$projectId/docs/$docId",
			params: { projectId: "proj-a", docId: "doc-1" },
		});
	});

	it("opens the conversation for a question notification, global or project", async () => {
		mocks.listNotificationsMock.mockResolvedValue({
			items: [
				makeNotification({
					id: "n-global",
					type: "question",
					actor_full_name: "Claude Agent",
					task_id: null,
					project_id: null,
					project_name: "",
					conversation_id: "conv-g",
					conversation_title: "Plan the release",
				}),
				makeNotification({
					id: "n-project",
					type: "question",
					actor_full_name: "Claude Agent",
					task_id: null,
					conversation_id: "conv-p",
				}),
			],
			page_size: 20,
			next_cursor: null,
			unread_count: 2,
		});

		await renderAndOpen();

		await userEvent.click(
			await screen.findByText(/asked you a question in Plan the release/),
		);
		expect(mocks.navigateMock).toHaveBeenLastCalledWith({
			to: "/conversations/$conversationId",
			params: { conversationId: "conv-g" },
		});

		// Opening a notification closes the list; reopen it for the next one.
		await userEvent.click(
			screen.getByRole("button", { name: "Notifications" }),
		);
		await userEvent.click(
			await screen.findByText(/asked you a question in Untitled conversation/),
		);
		expect(mocks.navigateMock).toHaveBeenLastCalledWith({
			to: "/projects/$projectId/conversations/$conversationId",
			params: { projectId: "proj-a", conversationId: "conv-p" },
		});
	});

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

	it("groups notifications by day under Today, Yesterday and Earlier", async () => {
		const day = (daysAgo: number) => {
			const d = new Date();
			d.setHours(12, 0, 0, 0);
			d.setDate(d.getDate() - daysAgo);
			return d.toISOString();
		};
		mocks.listNotificationsMock.mockResolvedValue({
			items: [
				makeNotification({
					id: "a",
					task_title: "Fresh",
					created_at: new Date().toISOString(),
				}),
				makeNotification({ id: "b", task_title: "Yday", created_at: day(1) }),
				makeNotification({ id: "c", task_title: "Old", created_at: day(5) }),
			],
			page_size: 20,
			next_cursor: null,
			unread_count: 3,
		});
		await renderAndOpen();
		expect(
			await screen.findByRole("heading", { name: "Today" }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("heading", { name: "Yesterday" }),
		).toBeInTheDocument();
		expect(
			screen.getByRole("heading", { name: "Earlier" }),
		).toBeInTheDocument();
	});

	it("filters to unread and says so when you are caught up", async () => {
		mocks.listNotificationsMock.mockResolvedValue({
			items: [
				makeNotification({ id: "u", task_title: "Unread one" }),
				makeNotification({
					id: "r",
					task_title: "Read one",
					read_at: "2026-01-02T00:00:00Z",
				}),
			],
			page_size: 20,
			next_cursor: null,
			unread_count: 1,
		});
		await renderAndOpen();
		expect(await screen.findByText(/Read one/)).toBeInTheDocument();

		await userEvent.click(screen.getByRole("button", { name: /^Unread/ }));
		expect(screen.getByText(/Unread one/)).toBeInTheDocument();
		expect(screen.queryByText(/Read one/)).not.toBeInTheDocument();
	});

	it("says you are caught up when nothing is unread", async () => {
		mocks.listNotificationsMock.mockResolvedValue({
			items: [makeNotification({ read_at: "2026-01-02T00:00:00Z" })],
			page_size: 20,
			next_cursor: null,
			unread_count: 0,
		});
		await renderAndOpen();
		await screen.findByText(/Ship the thing/);
		await userEvent.click(screen.getByRole("button", { name: /^Unread/ }));
		expect(screen.getByText("You're all caught up")).toBeInTheDocument();
	});

	it("marks one notification read without opening it", async () => {
		mocks.listNotificationsMock.mockResolvedValue({
			items: [makeNotification({ id: "n-7" })],
			page_size: 20,
			next_cursor: null,
			unread_count: 1,
		});
		await renderAndOpen();
		await userEvent.click(
			await screen.findByRole("button", { name: "Mark as read" }),
		);
		await waitFor(() =>
			expect(mocks.markNotificationAsReadMock).toHaveBeenCalled(),
		);
		expect(mocks.markNotificationAsReadMock.mock.calls[0][0]).toBe("n-7");
		expect(mocks.navigateMock).not.toHaveBeenCalled();
	});

	it("closes the list when a notification is opened", async () => {
		mocks.listNotificationsMock.mockResolvedValue({
			items: [makeNotification()],
			page_size: 20,
			next_cursor: null,
			unread_count: 1,
		});
		await renderAndOpen();
		await userEvent.click(await screen.findByText(/assigned you to task #42/));
		await waitFor(() =>
			expect(
				screen.queryByText(/assigned you to task #42/),
			).not.toBeInTheDocument(),
		);
	});

	it("offers a retry when the list cannot be loaded", async () => {
		mocks.listNotificationsMock.mockRejectedValueOnce(new Error("boom"));
		mocks.listNotificationsMock.mockResolvedValue({
			items: [makeNotification()],
			page_size: 20,
			next_cursor: null,
			unread_count: 1,
		});
		await renderAndOpen();
		expect(
			await screen.findByText("Couldn't load your notifications."),
		).toBeInTheDocument();
		await userEvent.click(screen.getByRole("button", { name: "Try again" }));
		expect(
			await screen.findByText(/assigned you to task #42/),
		).toBeInTheDocument();
	});

	it("opens the notification settings", async () => {
		mocks.listNotificationsMock.mockResolvedValue({
			items: [],
			page_size: 20,
			next_cursor: null,
			unread_count: 0,
		});
		await renderAndOpen();
		await userEvent.click(
			await screen.findByRole("button", { name: "Notification settings" }),
		);
		expect(mocks.navigateMock).toHaveBeenCalledWith({
			to: "/account/preferences",
		});
	});

	it("shows when Do Not Disturb is on", async () => {
		mocks.pushSettingsMock.mockResolvedValue({
			public_key: "K",
			subscription_count: 0,
			preferences: [],
			do_not_disturb: { active: true, until: null, indefinite: true },
		});
		mocks.listNotificationsMock.mockResolvedValue({
			items: [],
			page_size: 20,
			next_cursor: null,
			unread_count: 0,
		});
		await renderAndOpen();
		expect(await screen.findByText("Do Not Disturb is on")).toBeInTheDocument();
	});
});
