import { queryOptions } from "@tanstack/react-query";

import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";
import type { NotificationType } from "./notification-api";

export interface PushPreference {
	type: NotificationType;
	enabled: boolean;
}

export interface DoNotDisturb {
	/** Whether pushes are silenced right now. */
	active: boolean;
	/** When it ends; null when it is off or has no end. */
	until: string | null;
	/** True when it lasts until the user turns it off. */
	indefinite: boolean;
}

export interface PushSettings {
	/** VAPID public key a browser subscribes against. */
	public_key: string;
	/** How many of the user's browsers are subscribed. */
	subscription_count: number;
	preferences: PushPreference[];
	do_not_disturb: DoNotDisturb;
}

export const pushSettingsQueryOptions = queryOptions({
	queryKey: ["push", "settings"],
	queryFn: async () => {
		const { data } =
			await apiClient.instance.get<SuccessEnvelope<PushSettings>>(
				"/users/me/push",
			);
		return data.data;
	},
});

export async function setPushPreference(
	type: NotificationType,
	enabled: boolean,
): Promise<void> {
	await apiClient.instance.put("/users/me/push/preferences", { type, enabled });
}

/** Turns Do Not Disturb on until a time, or until turned off, or off (null). */
export async function setDoNotDisturb(
	until: Date | "indefinite" | null,
): Promise<DoNotDisturb> {
	const body =
		until === "indefinite"
			? { indefinite: true }
			: { until: until ? until.toISOString() : null };
	const { data } = await apiClient.instance.put<SuccessEnvelope<DoNotDisturb>>(
		"/users/me/push/do-not-disturb",
		body,
	);
	return data.data;
}

async function sendSubscription(subscription: PushSubscription): Promise<void> {
	// toJSON() carries { endpoint, keys: { p256dh, auth } }, the shape the API reads.
	await apiClient.instance.post(
		"/users/me/push/subscriptions",
		subscription.toJSON(),
	);
}

async function removeSubscription(endpoint: string): Promise<void> {
	await apiClient.instance.post("/users/me/push/subscriptions/delete", {
		endpoint,
	});
}

// ── This browser ─────────────────────────────────────────────────────────────

/** Whether this browser can do Web Push at all. */
export function isPushSupported(): boolean {
	return (
		typeof window !== "undefined" &&
		"serviceWorker" in navigator &&
		"PushManager" in window &&
		"Notification" in window
	);
}

/** "granted", "denied" or "default" (not asked yet). */
export function pushPermission(): NotificationPermission {
	return typeof Notification === "undefined"
		? "denied"
		: Notification.permission;
}

async function registration(): Promise<ServiceWorkerRegistration> {
	await navigator.serviceWorker.register("/sw.js");
	return navigator.serviceWorker.ready;
}

/** This browser's current push subscription, if it has one. */
export async function currentPushSubscription(): Promise<PushSubscription | null> {
	if (!isPushSupported()) return null;
	const reg = await navigator.serviceWorker.getRegistration("/sw.js");
	return (await reg?.pushManager.getSubscription()) ?? null;
}

/** VAPID keys are base64url; PushManager wants bytes. */
export function urlBase64ToUint8Array(base64: string): Uint8Array<ArrayBuffer> {
	const padded = (base64 + "=".repeat((4 - (base64.length % 4)) % 4))
		.replace(/-/g, "+")
		.replace(/_/g, "/");
	const raw = atob(padded);
	const out = new Uint8Array(new ArrayBuffer(raw.length));
	for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
	return out;
}

const OPT_OUT_KEY = "paca:push-opted-out";

/** Whether the user switched push off on this browser. Push is on by default
 * here, so only an explicit "off" is remembered (per browser, like the chime
 * mute), and it is what stops the card from turning push back on. */
export function isPushOptedOut(): boolean {
	try {
		return window.localStorage.getItem(OPT_OUT_KEY) === "true";
	} catch {
		return false;
	}
}

export function setPushOptedOut(optedOut: boolean): void {
	try {
		if (optedOut) window.localStorage.setItem(OPT_OUT_KEY, "true");
		else window.localStorage.removeItem(OPT_OUT_KEY);
	} catch {
		// Private mode or blocked storage: the choice just isn't remembered.
	}
}

/**
 * Subscribes this browser and registers the subscription with the server.
 * Returns "denied" when the permission is blocked, and "dismissed" when the
 * browser's prompt was closed without an answer (or, in browsers that only
 * prompt after a click, could not be shown).
 */
export async function enablePush(
	publicKey: string,
): Promise<"enabled" | "denied" | "dismissed"> {
	const permission =
		Notification.permission === "default"
			? await Notification.requestPermission()
			: Notification.permission;
	if (permission === "denied") return "denied";
	if (permission !== "granted") return "dismissed";

	const reg = await registration();
	const key = urlBase64ToUint8Array(publicKey);
	let subscription = await reg.pushManager.getSubscription();
	// A subscription is bound to the key it was made against. If the server's key
	// has changed since (a restored database, a rotated key), the old one can never
	// be signed for again and would stay silently dead: drop it and subscribe anew.
	if (
		subscription &&
		!sameKey(subscription.options?.applicationServerKey, key)
	) {
		const stale = subscription.endpoint;
		await subscription.unsubscribe();
		subscription = null;
		// Best effort: the server's row for it would otherwise linger until a
		// send to it is rejected.
		await removeSubscription(stale).catch(() => undefined);
	}
	subscription ??= await reg.pushManager.subscribe({
		userVisibleOnly: true,
		applicationServerKey: key,
	});
	await sendSubscription(subscription);
	return "enabled";
}

/** Whether a subscription's recorded key is the given one. A browser that does
 *  not expose the key (it is null or absent) cannot be told apart, so it is
 *  trusted rather than resubscribed on every visit. */
function sameKey(
	recorded: ArrayBuffer | null | undefined,
	key: Uint8Array,
): boolean {
	if (!recorded) return true;
	const bytes = new Uint8Array(recorded);
	return bytes.length === key.length && bytes.every((b, i) => b === key[i]);
}

/** Unsubscribes this browser and tells the server to forget it. */
export async function disablePush(): Promise<void> {
	const subscription = await currentPushSubscription();
	if (!subscription) return;
	await removeSubscription(subscription.endpoint);
	await subscription.unsubscribe();
}
