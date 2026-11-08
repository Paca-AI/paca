// Hook that listens for global-agent chat events (home page / admin pages,
// no project context) and invalidates the matching React Query caches.
//
// Unlike useProjectRealtime, there is no explicit room to join here: the
// server auto-joins every connected socket into its own
// `user:<userId>:agent-chat` room at connect time (see
// services/realtime/src/server.ts), the same way it auto-joins the
// notifications room — a global chat conversation has no project to gate
// room membership on, only the caller's own identity. So this hook only
// needs to attach a listener, mirroring useProjectRealtime's event handling
// but without the join/leave/rejoin bookkeeping.
//
// Usage: call once from wherever the global AIChatFloat is mounted.
//
// `enabled` (default true) lets a component that's reused at both project
// and global scope (e.g. the Conversations page) always call this hook —
// per the rules of hooks — while only actually subscribing when it's
// rendering in global scope.

import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import {
	applyRealtimeAgentEvent,
	conversationEventWindowKey,
} from "@/lib/agent-api";
import { connectSocket, type RealtimeEvent } from "@/lib/socket-client";

export function useGlobalAgentRealtime(enabled = true): void {
	const queryClient = useQueryClient();

	useEffect(() => {
		if (!enabled) return;
		const socket = connectSocket();

		function handleEvent(event: RealtimeEvent) {
			const { type } = event;
			if (!type.startsWith("agent.")) return;

			// See useProjectRealtime's identical agent.* handling — this hook
			// shares the same tail-cache logic via applyRealtimeAgentEvent
			// rather than a hand-copied version of it.
			const applied = applyRealtimeAgentEvent(queryClient, event.payload);

			if (applied && !applied.isPersistedEvent) {
				void queryClient.invalidateQueries({
					queryKey: conversationEventWindowKey(applied.conversationId),
				});
				void queryClient.invalidateQueries({
					queryKey: ["global-chat", "conversations"],
				});
			}
		}

		// Events emitted while the socket was away are lost: after a *re*connect
		// reconcile the conversations with the server.
		// A socket that mounted down and connects for the first time (after a
		// recovery) may have missed events too, so only a socket that connects
		// right as we mount counts as the initial connection.
		let hasConnected = socket.connected;
		const mountedDown = !socket.connected && !socket.active;
		function handleConnect() {
			if (hasConnected || mountedDown) {
				void queryClient.invalidateQueries({
					queryKey: ["conversation-event-window"],
				});
				void queryClient.invalidateQueries({
					queryKey: ["global-chat", "conversations"],
				});
			}
			hasConnected = true;
		}

		socket.on("event", handleEvent);
		socket.on("connect", handleConnect);
		return () => {
			socket.off("event", handleEvent);
			socket.off("connect", handleConnect);
		};
	}, [queryClient, enabled]);
}
