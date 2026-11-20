import { useQuery } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useSyncExternalStore } from "react";
import { currentUserQueryOptions } from "@/lib/auth-api";
import {
	EMPTY_PRESENCE,
	getLiveState,
	joinLive,
	type LiveKind,
	type LivePresence,
	type LiveRef,
	setLiveTyping,
	subscribeLive,
} from "@/lib/live-presence";
import { useMemberDirectory } from "@/lib/member-directory";

export interface LivePresenceView extends LivePresence {
	/** Safe to call on every keystroke; throttled and ignored for read-only
	 *  viewers. */
	setTyping: (typing: boolean) => void;
	/** The other viewers' ids (you are left out). */
	otherViewers: string[];
	/** Display names of the others who are typing. */
	typingNames: string[];
	/** Display name of an account: the project's member list first, then the
	 *  username the server sent. */
	nameOf: (id: string) => string | undefined;
}

/** Who is viewing and typing in a task, doc or chat channel, from the realtime
 *  presence layer. A direct message has no project: pass "" for it. */
export function useLivePresence(
	kind: LiveKind,
	projectId: string | undefined,
	id: string | undefined,
): LivePresenceView {
	// Only a channel may be project-less (a direct message).
	const projectOk = kind === "channel" ? projectId !== undefined : !!projectId;
	const ref = useMemo<LiveRef | undefined>(
		() =>
			projectOk && id ? { kind, projectId: projectId ?? "", id } : undefined,
		[kind, projectOk, projectId, id],
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

	const { data: me } = useQuery(currentUserQueryOptions);
	const getMember = useMemberDirectory(projectId ?? "");
	const names = state.names;
	const nameOf = useCallback(
		(who: string) => {
			const m = projectId ? getMember(who) : undefined;
			return m?.full_name || m?.username || names[who];
		},
		[projectId, getMember, names],
	);
	const otherViewers = useMemo(
		() => state.viewers.filter((v) => v !== me?.id),
		[state.viewers, me?.id],
	);
	const typingNames = useMemo(
		() =>
			state.typing
				.filter((v) => v !== me?.id)
				.map(nameOf)
				.filter((n): n is string => !!n),
		[state.typing, me?.id, nameOf],
	);

	return { ...state, setTyping, otherViewers, typingNames, nameOf };
}

/** The same for a task. */
export function useTaskPresence(
	projectId: string | undefined,
	taskId: string | undefined,
): LivePresenceView {
	return useLivePresence("task", projectId, taskId);
}
