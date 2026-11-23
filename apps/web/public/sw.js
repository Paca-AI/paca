// Paca service worker: shows Web Push notifications and opens the right page
// when one is clicked. It does no caching and no offline work.

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));

// How long a notification stays up when the app is already open and focused
// (where the in-app bell and chime have shown it). It is still shown, briefly,
// because browsers such as Safari revoke a subscription whose pushes show
// nothing.
const FOCUSED_DISPLAY_MS = 4000;

self.addEventListener("push", (event) => {
	let data = {};
	try {
		data = event.data ? event.data.json() : {};
	} catch {
		data = {};
	}
	const title = data.title || "Paca";
	const tag = data.id || undefined;

	event.waitUntil(
		(async () => {
			const windows = await self.clients.matchAll({
				type: "window",
				includeUncontrolled: true,
			});
			const appIsFocused = windows.some(
				(c) => c.visibilityState === "visible" && c.focused,
			);
			await self.registration.showNotification(title, {
				body: data.body || "",
				tag,
				data: { url: data.url || "/" },
				icon: "/paca-logo.svg",
			});
			if (appIsFocused) {
				await new Promise((resolve) => setTimeout(resolve, FOCUSED_DISPLAY_MS));
				const shown = await self.registration.getNotifications({ tag });
				for (const n of shown) n.close();
			}
		})(),
	);
});

self.addEventListener("notificationclick", (event) => {
	event.notification.close();
	// Same-origin paths only: the payload comes from the server, but a push
	// service sits in between, so never navigate somewhere else.
	let target = new URL("/", self.location.origin);
	try {
		const requested = new URL(
			(event.notification.data && event.notification.data.url) || "/",
			self.location.origin,
		);
		if (requested.origin === self.location.origin) target = requested;
	} catch {
		// keep the default
	}

	event.waitUntil(
		(async () => {
			const windows = await self.clients.matchAll({
				type: "window",
				includeUncontrolled: true,
			});
			for (const client of windows) {
				if (new URL(client.url).origin !== self.location.origin) continue;
				await client.focus();
				try {
					await client.navigate(target.href);
				} catch {
					// A client that cannot be navigated is still focused.
				}
				return;
			}
			await self.clients.openWindow(target.href);
		})(),
	);
});
