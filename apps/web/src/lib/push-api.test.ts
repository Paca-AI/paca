import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }));
vi.mock("./api-client", () => ({ apiClient: { instance: http } }));

import {
	currentPushSubscription,
	disablePush,
	enablePush,
	isPushOptedOut,
	isPushSupported,
	setPushOptedOut,
	setPushPreference,
	urlBase64ToUint8Array,
} from "./push-api";

describe("urlBase64ToUint8Array", () => {
	it("decodes base64url with no padding", () => {
		// "hello?>" is base64 "aGVsbG8/Pg==", base64url "aGVsbG8_Pg".
		const bytes = urlBase64ToUint8Array("aGVsbG8_Pg");
		expect(String.fromCharCode(...bytes)).toBe("hello?>");
	});
});

function fakeSubscription(endpoint = "https://push.example/a") {
	return {
		endpoint,
		toJSON: () => ({ endpoint, keys: { p256dh: "pk", auth: "au" } }),
		unsubscribe: vi.fn().mockResolvedValue(true),
	};
}

function installBrowser(opts: {
	permission: NotificationPermission;
	request?: NotificationPermission;
	existing?: ReturnType<typeof fakeSubscription> | null;
}) {
	const created = fakeSubscription();
	const pushManager = {
		getSubscription: vi.fn().mockResolvedValue(opts.existing ?? null),
		subscribe: vi.fn().mockResolvedValue(created),
	};
	const registration = { pushManager };
	vi.stubGlobal("Notification", {
		permission: opts.permission,
		requestPermission: vi.fn().mockResolvedValue(opts.request ?? "granted"),
	});
	vi.stubGlobal("PushManager", class {});
	Object.defineProperty(navigator, "serviceWorker", {
		configurable: true,
		value: {
			register: vi.fn().mockResolvedValue(registration),
			ready: Promise.resolve(registration),
			getRegistration: vi.fn().mockResolvedValue(registration),
		},
	});
	return { pushManager, created };
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => {
	vi.unstubAllGlobals();
	Reflect.deleteProperty(navigator, "serviceWorker");
});

describe("push support", () => {
	it("is unsupported without a service worker", () => {
		expect(isPushSupported()).toBe(false);
	});
	it("is supported when the browser has all three APIs", () => {
		installBrowser({ permission: "default" });
		vi.stubGlobal("PushManager", class {});
		(window as unknown as Record<string, unknown>).PushManager = class {};
		expect(isPushSupported()).toBe(true);
	});
});

describe("enablePush", () => {
	beforeEach(() => {
		(window as unknown as Record<string, unknown>).PushManager = class {};
	});

	it("asks for permission, subscribes and registers the subscription", async () => {
		const { pushManager, created } = installBrowser({ permission: "default" });
		http.post.mockResolvedValue({});

		await expect(enablePush("aGVsbG8_Pg")).resolves.toBe("enabled");

		expect(Notification.requestPermission).toHaveBeenCalled();
		expect(pushManager.subscribe).toHaveBeenCalledWith(
			expect.objectContaining({ userVisibleOnly: true }),
		);
		expect(http.post).toHaveBeenCalledWith(
			"/users/me/push/subscriptions",
			created.toJSON(),
		);
	});

	it("does not subscribe when permission is refused", async () => {
		const { pushManager } = installBrowser({
			permission: "default",
			request: "denied",
		});
		await expect(enablePush("aGVsbG8_Pg")).resolves.toBe("denied");
		expect(pushManager.subscribe).not.toHaveBeenCalled();
		expect(http.post).not.toHaveBeenCalled();
	});

	it("reports a dismissed prompt separately from a block", async () => {
		const { pushManager } = installBrowser({
			permission: "default",
			request: "default",
		});
		await expect(enablePush("aGVsbG8_Pg")).resolves.toBe("dismissed");
		expect(pushManager.subscribe).not.toHaveBeenCalled();
	});

	it("does not ask again when already blocked", async () => {
		installBrowser({ permission: "denied" });
		await expect(enablePush("aGVsbG8_Pg")).resolves.toBe("denied");
		expect(Notification.requestPermission).not.toHaveBeenCalled();
	});

	it("replaces a subscription made against another server key", async () => {
		const stale = {
			...fakeSubscription("https://push.example/old"),
			options: { applicationServerKey: new Uint8Array([9, 9, 9]).buffer },
		};
		const { pushManager, created } = installBrowser({
			permission: "granted",
			existing: stale,
		});
		http.post.mockResolvedValue({});

		await enablePush("aGVsbG8_Pg");

		expect(stale.unsubscribe).toHaveBeenCalled();
		expect(pushManager.subscribe).toHaveBeenCalledTimes(1);
		expect(http.post).toHaveBeenCalledWith(
			"/users/me/push/subscriptions",
			created.toJSON(),
		);
	});

	it("keeps a subscription whose key is the server's", async () => {
		const current = {
			...fakeSubscription("https://push.example/current"),
			options: {
				applicationServerKey: urlBase64ToUint8Array("aGVsbG8_Pg").buffer,
			},
		};
		const { pushManager } = installBrowser({
			permission: "granted",
			existing: current,
		});
		http.post.mockResolvedValue({});

		await enablePush("aGVsbG8_Pg");

		expect(current.unsubscribe).not.toHaveBeenCalled();
		expect(pushManager.subscribe).not.toHaveBeenCalled();
	});

	it("reuses an existing browser subscription instead of creating another", async () => {
		const existing = fakeSubscription("https://push.example/old");
		const { pushManager } = installBrowser({
			permission: "granted",
			existing,
		});
		http.post.mockResolvedValue({});
		await enablePush("aGVsbG8_Pg");
		expect(pushManager.subscribe).not.toHaveBeenCalled();
		expect(http.post).toHaveBeenCalledWith(
			"/users/me/push/subscriptions",
			existing.toJSON(),
		);
	});
});

describe("disablePush", () => {
	beforeEach(() => {
		(window as unknown as Record<string, unknown>).PushManager = class {};
	});

	it("tells the server, then unsubscribes the browser", async () => {
		const existing = fakeSubscription();
		installBrowser({ permission: "granted", existing });
		http.post.mockResolvedValue({});

		await disablePush();

		expect(http.post).toHaveBeenCalledWith(
			"/users/me/push/subscriptions/delete",
			{ endpoint: existing.endpoint },
		);
		expect(existing.unsubscribe).toHaveBeenCalled();
	});

	it("does nothing when this browser has no subscription", async () => {
		installBrowser({ permission: "granted", existing: null });
		await disablePush();
		expect(http.post).not.toHaveBeenCalled();
		await expect(currentPushSubscription()).resolves.toBeNull();
	});
});

describe("setPushPreference", () => {
	it("puts the type and its new state", async () => {
		http.put.mockResolvedValue({});
		await setPushPreference("mentioned", false);
		expect(http.put).toHaveBeenCalledWith("/users/me/push/preferences", {
			type: "mentioned",
			enabled: false,
		});
	});
});

describe("push opt-out", () => {
	afterEach(() => window.localStorage.clear());

	it("defaults to not opted out and remembers an explicit off", () => {
		expect(isPushOptedOut()).toBe(false);
		setPushOptedOut(true);
		expect(isPushOptedOut()).toBe(true);
		setPushOptedOut(false);
		expect(isPushOptedOut()).toBe(false);
	});
});
