// Re-syncs the notification inbox after the realtime socket was away.
//
// `notification.created` is delivered over Valkey Pub/Sub, which is
// fire-and-forget: anything emitted while this client's socket was down (a
// network blip, a sleeping laptop, a realtime restart) is never replayed, so
// the unread badge would stay stale until something else refetched it. So,
// from the database, which is the source of truth:
//   - after a *re*connect, refetch everything under ["notifications"] (the
//     bell, the /notifications page and the unread-mention badges on task
//     cards and sprint headers all live there);
//   - while the tab is visible, do the same once a minute, as a safety net for
//     an event lost without the socket ever dropping.
// A tab regaining focus is covered by React Query's refetchOnWindowFocus.

import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { connectSocket, getSocket } from "@/lib/socket-client";

/** How often a visible tab re-reads the inbox. */
export const NOTIFICATIONS_POLL_INTERVAL_MS = 60_000;

export function useNotificationCatchUp(enabled = true): void {
	const queryClient = useQueryClient();

	useEffect(() => {
		if (!enabled) return;
		// Read before connectSocket(): it re-subscribes an existing socket
		// (`active` becomes true), which would hide that it was down.
		const existing = getSocket();
		const mountedDown = !!existing && !existing.connected && !existing.active;
		const socket = connectSocket();

		// Only a socket that connects right as we mount counts as the initial
		// connection, whose data the page load already fetched. A socket that
		// mounted down and connects for the first time (after a recovery) may
		// have missed events too.
		let hasConnected = socket.connected;
		function handleConnect() {
			if (hasConnected || mountedDown) {
				void queryClient.invalidateQueries({ queryKey: ["notifications"] });
			}
			hasConnected = true;
		}

		socket.on("connect", handleConnect);

		// Not while the tab is hidden: nobody is looking, and the refetch on
		// focus catches it up the moment they are.
		const poll = window.setInterval(() => {
			if (document.visibilityState === "visible") {
				void queryClient.invalidateQueries({ queryKey: ["notifications"] });
			}
		}, NOTIFICATIONS_POLL_INTERVAL_MS);

		return () => {
			socket.off("connect", handleConnect);
			window.clearInterval(poll);
		};
	}, [queryClient, enabled]);
}
