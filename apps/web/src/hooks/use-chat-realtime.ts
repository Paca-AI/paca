// Live chat for the channel on screen, and the unread nudges for the rest.
//
// The realtime service (services/realtime/src/chat.ts) sends only small
// announcements ({channel_id, seq, message_id, thread_root_id}); the content
// is fetched here. A channel's messages are cached as a window
// (lib/chat-cache.ts) and every announcement, reconnect or gap is answered the
// same way: ask the API for what came after the newest message held.

import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef } from "react";
import {
	chatKeys,
	DM_SCOPE,
	listMessages,
	type MessagePage,
} from "@/lib/chat-api";
import {
	lastSeq,
	type MessageWindow,
	replaceMessage,
	withNewest,
} from "@/lib/chat-cache";
import { connectSocket, getSocket } from "@/lib/socket-client";

interface ChatAnnouncement {
	type: string;
	payload: {
		project_id?: string;
		channel_id?: string;
		seq?: number;
		message_id?: string;
		thread_root_id?: string | null;
		actor_id?: string;
	};
}

const MAX_CATCH_UP_PAGES = 5;
const JOIN_RETRY_MS = [1_000, 3_000, 10_000];

/** Fetches the newest page into the cache, replacing what was there. */
export function useLoadNewest(projectId: string, channelId: string) {
	const queryClient = useQueryClient();
	return useCallback(async () => {
		const page = await listMessages(projectId, channelId, { pageSize: 50 });
		queryClient.setQueryData<MessageWindow>(
			chatKeys.messages(projectId, channelId),
			(cur) => withNewest(cur, page, true),
		);
		return page;
	}, [queryClient, projectId, channelId]);
}

/**
 * Keeps the open channel's cache current. Joins the channel's realtime room
 * (the API confirms access once), applies announcements, and catches up after
 * a reconnect. `onNewMessage` fires for messages other people wrote.
 */
export function useChatRealtime(
	projectId: string,
	channelId: string,
	currentUserId: string | undefined,
	onNewMessage?: () => void,
): void {
	const queryClient = useQueryClient();
	const onNew = useRef(onNewMessage);
	onNew.current = onNewMessage;

	const catchUp = useCallback(async () => {
		const key = chatKeys.messages(projectId, channelId);
		for (let i = 0; i < MAX_CATCH_UP_PAGES; i++) {
			const cur = queryClient.getQueryData<MessageWindow>(key);
			if (!cur) return; // not loaded yet: the initial fetch gets everything
			const page: MessagePage = await listMessages(projectId, channelId, {
				afterSeq: lastSeq(cur),
				pageSize: 100,
			});
			queryClient.setQueryData<MessageWindow>(key, (w) =>
				withNewest(w, page, false),
			);
			if (!page.has_more) return;
		}
		// Far behind: start over from the newest page.
		await queryClient.invalidateQueries({ queryKey: key });
	}, [queryClient, projectId, channelId]);

	useEffect(() => {
		const socket = connectSocket();
		let cancelled = false;
		let retry: ReturnType<typeof setTimeout> | undefined;
		let attempt = 0;

		const join = () => {
			if (cancelled || !socket.connected) return;
			socket.emit(
				"chat:join",
				// A direct message has no project.
				{
					projectId: projectId === DM_SCOPE ? undefined : projectId,
					channelId,
				},
				(res: { ok: boolean; reason?: string }) => {
					if (cancelled) return;
					if (res?.ok) {
						attempt = 0;
						return;
					}
					// "unavailable" is a failed check, worth retrying; a refusal or a
					// bad request is not.
					if (res?.reason === "unavailable" && attempt < JOIN_RETRY_MS.length) {
						retry = setTimeout(join, JOIN_RETRY_MS[attempt++]);
					}
				},
			);
		};

		// A reply arrived: reload that thread and the root's reply count.
		const refreshThread = async (rootId: string) => {
			void queryClient.invalidateQueries({
				queryKey: chatKeys.thread(projectId, channelId, rootId),
			});
			const win = queryClient.getQueryData<MessageWindow>(
				chatKeys.messages(projectId, channelId),
			);
			const root = win?.items.find((m) => m.id === rootId);
			if (!root) return;
			const page = await listMessages(projectId, channelId, {
				afterSeq: root.seq - 1,
				pageSize: 1,
			});
			const fresh = page.items[0];
			if (fresh && fresh.id === rootId) {
				queryClient.setQueryData<MessageWindow | undefined>(
					chatKeys.messages(projectId, channelId),
					(w) => replaceMessage(w, fresh),
				);
			}
		};

		const refreshOne = async (a: ChatAnnouncement["payload"]) => {
			if (typeof a.seq !== "number") return;
			if (a.thread_root_id) {
				// An edit, delete or reaction on a reply: reload its thread.
				void queryClient.invalidateQueries({
					queryKey: chatKeys.thread(projectId, channelId, a.thread_root_id),
				});
				return;
			}
			const page = await listMessages(projectId, channelId, {
				afterSeq: a.seq - 1,
				pageSize: 1,
			});
			const m = page.items[0];
			if (m && m.seq === a.seq) {
				queryClient.setQueryData<MessageWindow | undefined>(
					chatKeys.messages(projectId, channelId),
					(w) => replaceMessage(w, m),
				);
			}
		};

		const onChat = (ev: ChatAnnouncement) => {
			const p = ev?.payload;
			if (!p || p.channel_id !== channelId) return;
			switch (ev.type) {
				case "chat.message.new":
					if (p.thread_root_id) {
						void refreshThread(p.thread_root_id).catch(() => undefined);
						break;
					}
					void catchUp();
					if (p.actor_id !== currentUserId) onNew.current?.();
					break;
				case "chat.message.updated":
				case "chat.message.deleted":
				case "chat.reaction.changed":
					void refreshOne(p).catch(() => undefined);
					break;
			}
		};

		// A channel I may no longer read: drop its cache so nothing stale shows.
		const onRevoked = (ev: { channelId?: string }) => {
			if (ev?.channelId === channelId) {
				queryClient.removeQueries({
					queryKey: chatKeys.messages(projectId, channelId),
				});
				void queryClient.invalidateQueries({
					queryKey: chatKeys.channels(projectId),
				});
			}
		};

		// After a reconnect the room is gone and announcements were missed.
		const onConnect = () => {
			join();
			void catchUp()
				.catch(() => undefined)
				.then(() => {
					// Edits, deletes, reactions and reply counts are live-only
					// announcements: refetch what is on screen.
					void queryClient.invalidateQueries({
						queryKey: chatKeys.messages(projectId, channelId),
					});
					void queryClient.invalidateQueries({
						queryKey: ["chat", projectId, "thread", channelId],
					});
				});
		};

		// Anything that happened since this channel was last on screen.
		void catchUp().catch(() => undefined);

		socket.on("chat", onChat);
		socket.on("chat:revoked", onRevoked);
		socket.on("connect", onConnect);
		join();

		return () => {
			cancelled = true;
			if (retry) clearTimeout(retry);
			socket.off("chat", onChat);
			socket.off("chat:revoked", onRevoked);
			socket.off("connect", onConnect);
			getSocket()?.emit("chat:leave", { channelId });
		};
	}, [queryClient, projectId, channelId, currentUserId, catchUp]);
}

/**
 * Unread nudges for every project (the account's own room, joined by the
 * server at connect): refreshes the sidebar badges and channel lists. Mount
 * once in the authenticated layout.
 */
export function useChatUnreadRealtime(enabled = true): void {
	const queryClient = useQueryClient();
	useEffect(() => {
		if (!enabled) return;
		const socket = connectSocket();
		const refresh = (projectId?: string) => {
			void queryClient.invalidateQueries({ queryKey: chatKeys.unread() });
			void queryClient.invalidateQueries({ queryKey: chatKeys.inboxAll() });
			// No project: a direct message, so the DM list changed.
			void queryClient.invalidateQueries({
				queryKey: chatKeys.channels(projectId ?? DM_SCOPE),
			});
		};
		const onUnread = (ev: ChatAnnouncement) => refresh(ev?.payload?.project_id);
		// A channel was created, renamed or archived in a project I read.
		const onEvent = (ev: {
			type?: string;
			payload?: { project_id?: string };
		}) => {
			if (ev?.type === "chat.channel.changed") {
				const pid = ev.payload?.project_id;
				if (pid) {
					void queryClient.invalidateQueries({
						queryKey: chatKeys.channels(pid),
					});
				}
			}
		};
		const onConnect = () => refresh();
		socket.on("chat:unread", onUnread);
		socket.on("event", onEvent);
		socket.on("connect", onConnect);
		return () => {
			socket.off("chat:unread", onUnread);
			socket.off("event", onEvent);
			socket.off("connect", onConnect);
		};
	}, [queryClient, enabled]);
}
