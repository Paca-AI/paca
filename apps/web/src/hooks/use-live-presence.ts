import { useCallback, useEffect, useMemo, useSyncExternalStore } from "react";
import {
	EMPTY_PRESENCE,
	getLiveState,
	joinLive,
	type LivePresence,
	type LiveRef,
	setLiveTyping,
	subscribeLive,
} from "@/lib/live-presence";

/** Who is viewing and typing in a task, from the realtime presence layer.
 *  `setTyping` is safe to call on every keystroke. */
export function useTaskPresence(
	projectId: string | undefined,
	taskId: string | undefined,
): LivePresence & { setTyping: (typing: boolean) => void } {
	const ref = useMemo<LiveRef | undefined>(
		() =>
			projectId && taskId ? { kind: "task", projectId, id: taskId } : undefined,
		[projectId, taskId],
	);

	useEffect(() => (ref ? joinLive(ref) : undefined), [ref]);

	const subscribe = useCallback(
		(fn: () => void) => (ref ? subscribeLive(ref, fn) : () => {}),
		[ref],
	);
	const state = useSyncExternalStore(subscribe, () =>
		ref ? getLiveState(ref) : EMPTY_PRESENCE,
	);

	const setTyping = useCallback(
		(typing: boolean) => {
			if (ref) setLiveTyping(ref, typing);
		},
		[ref],
	);
	// Stop announcing typing when the view goes away.
	useEffect(() => () => (ref ? setLiveTyping(ref, false) : undefined), [ref]);

	return { ...state, setTyping };
}
