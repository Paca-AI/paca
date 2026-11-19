// Keeps the global Files page's general (workspace-wide) scope live.
//
// A project's files arrive through its project rooms (useProjectRealtime). The
// general scope belongs to no project, so the server puts every socket whose
// account may read the workspace's files into one `workspace:files` room at
// connect time (see services/realtime/src/server.ts) and sends it the
// `file.changed` events of that scope. This hook only listens for them.
//
// `enabled` lets a component reused at project and global scope call the hook
// unconditionally and subscribe only when it renders the global page.

import { useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import {
	applyFileChange,
	type FileChangedPayload,
	GENERAL_SCOPE,
	invalidateFiles,
} from "@/lib/files-api";
import {
	connectSocket,
	getSocket,
	type RealtimeEvent,
} from "@/lib/socket-client";

export function useWorkspaceFilesRealtime(enabled = true): void {
	const queryClient = useQueryClient();

	useEffect(() => {
		if (!enabled) return;
		// Read before connectSocket(), which re-subscribes an existing socket.
		const existing = getSocket();
		const mountedDown = !!existing && !existing.connected && !existing.active;
		const socket = connectSocket();

		function handleEvent(event: RealtimeEvent) {
			if (event.type !== "file.changed") return;
			const payload = event.payload as FileChangedPayload;
			if (payload.scope !== "general") return;
			applyFileChange(queryClient, GENERAL_SCOPE, payload);
		}
		socket.on("event", handleEvent);

		// Changes made while the socket was away were never delivered: after a
		// *re*connect, refetch the scope.
		let hasConnected = socket.connected;
		function handleConnect() {
			if (hasConnected || mountedDown) {
				void invalidateFiles(queryClient, GENERAL_SCOPE);
			}
			hasConnected = true;
		}
		socket.on("connect", handleConnect);

		return () => {
			socket.off("event", handleEvent);
			socket.off("connect", handleConnect);
		};
	}, [enabled, queryClient]);
}
