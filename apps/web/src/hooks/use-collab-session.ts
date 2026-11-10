import { useQuery } from "@tanstack/react-query";
import { useEffect, useState, useSyncExternalStore } from "react";
import { currentUserQueryOptions } from "@/lib/auth-api";
import {
	acquireCollabSession,
	type CollabKind,
	type CollabSession,
	type CollabStatus,
	colorForUser,
} from "@/lib/collab-session";

export type CollabState =
	/** Collaboration is not wanted (read-only, no ids yet) or could not start. */
	| { phase: "off"; session: null }
	| { phase: "connecting"; session: null }
	| { phase: "ready"; session: CollabSession; status: CollabStatus };

const OFF: CollabState = { phase: "off", session: null };
const CONNECTING: CollabState = { phase: "connecting", session: null };

/**
 * Joins the real-time editing room of a doc or task description.
 *
 * `enabled` should be true only when the user may edit and the stored content
 * has loaded (the first editor seeds the room from it). The result is "off"
 * when the room cannot be joined (no write access, realtime service down), in
 * which case callers fall back to the plain, non-collaborative editor.
 */
export function useCollabSession(
	kind: CollabKind,
	projectId: string | undefined,
	id: string | undefined,
	enabled: boolean,
): CollabState {
	const { data: user } = useQuery(currentUserQueryOptions);
	const userId = user?.id;
	const userName = user?.full_name || user?.username;
	const [joined, setJoined] = useState<CollabSession | null>(null);
	const [failed, setFailed] = useState(false);

	useEffect(() => {
		setJoined(null);
		setFailed(false);
		if (!enabled || !projectId || !id || !userId || !userName) return;
		const handle = acquireCollabSession(
			{ projectId, kind, id },
			{ name: userName, color: colorForUser(userId) },
		);
		let cancelled = false;
		handle.ready
			.catch(() => false)
			.then((ok) => {
				if (cancelled) return;
				if (ok) setJoined(handle.session);
				else setFailed(true);
			});
		return () => {
			cancelled = true;
			handle.release();
		};
	}, [enabled, kind, projectId, id, userId, userName]);

	const status = useSyncExternalStore(
		(cb) => joined?.subscribe(cb) ?? (() => {}),
		() => joined?.status ?? "connecting",
	);

	if (!enabled || failed) return OFF;
	if (!joined) return CONNECTING;
	return { phase: "ready", session: joined, status };
}
