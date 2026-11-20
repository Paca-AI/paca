// Chat delivery (docs/architecture/chat-and-meetings.md).
//
// Message traffic does not use the project rooms or the change log. Two kinds
// of room carry it:
//
//   channel:<channelId>   the sockets viewing one channel. Access is checked
//                         once, at "chat:join", with the API's IAM engine
//                         (channels:read on the channel), and again when IAM
//                         or the channel's membership changes. A message is
//                         then one emit to the room: no check per message.
//   user:<userId>:chat    the account's own room, auto-joined at connect. It
//                         carries small unread nudges for every project.
//
// The API publishes only nudges ({channel_id, seq, message_id}), never message
// content: a client catches up with GET .../messages?after_seq=. A nudge a
// client misses costs it one refetch, never a message.

import type { Logger } from "pino";
import type { Server, Socket } from "socket.io";
import type { AccessDecider } from "./access.ts";
import { channelRoomName, userChatRoomName } from "./permissions.ts";

// Most channels one socket may watch; a client opens a few at a time.
export const MAX_CHANNELS_PER_SOCKET = 50;

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const CHANNEL_ACTION = "channels:read";

export type ChatJoinResult =
	| { ok: true; channelId: string }
	| { ok: false; reason: "invalid" | "forbidden" | "limit" | "unavailable" };

export class ChatHub {
	constructor(
		private readonly io: Server,
		private readonly decider: AccessDecider,
		private readonly logger: Logger,
	) {}

	// A direct message has no project: its resource is channel/<id>.
	private resource(projectId: string, channelId: string): string {
		return projectId
			? `project/${projectId}/channel/${channelId}`
			: `channel/${channelId}`;
	}

	// join puts the socket in the channel's room if its account may read it.
	async join(
		socket: Socket,
		projectId: unknown,
		channelId: unknown,
	): Promise<ChatJoinResult> {
		// No project (undefined, null or "") names a direct message.
		const project = projectId ?? "";
		if (
			typeof project !== "string" ||
			typeof channelId !== "string" ||
			(project !== "" && !UUID.test(project)) ||
			!UUID.test(channelId)
		) {
			return { ok: false, reason: "invalid" };
		}
		const watched = socket.data.chatChannels;
		if (!watched.has(channelId) && watched.size >= MAX_CHANNELS_PER_SOCKET) {
			return { ok: false, reason: "limit" };
		}
		try {
			const allowed = await this.decider.can(socket.data, project, {
				action: CHANNEL_ACTION,
				resource: this.resource(project, channelId),
			});
			if (!allowed) return { ok: false, reason: "forbidden" };
		} catch (err) {
			this.logger.warn(
				{ userId: socket.data.userId, channelId, err },
				"chat join check failed",
			);
			return { ok: false, reason: "unavailable" };
		}
		// The socket may have disconnected while the check ran.
		if (!socket.connected) return { ok: false, reason: "unavailable" };
		watched.set(channelId, project);
		void socket.join(channelRoomName(channelId));
		return { ok: true, channelId };
	}

	leave(socket: Socket, channelId: unknown): void {
		if (typeof channelId !== "string") return;
		socket.data.chatChannels.delete(channelId);
		void socket.leave(channelRoomName(channelId));
	}

	// recheck asks the API again for every watched channel (or one) and takes
	// out the sockets whose account may no longer read it. A failed check keeps
	// the socket where it is until the next pass: what the room carries is a
	// nudge, and the content behind it is still gated by the REST routes.
	async recheck(onlyChannel?: string): Promise<void> {
		const jobs: Array<() => Promise<void>> = [];
		for (const socket of this.io.sockets.sockets.values()) {
			for (const [channelId, projectId] of socket.data.chatChannels ?? []) {
				if (onlyChannel && channelId !== onlyChannel) continue;
				jobs.push(() => this.recheckOne(socket, projectId, channelId));
			}
		}
		const workers = Array.from({ length: 8 }, async () => {
			for (let job = jobs.shift(); job; job = jobs.shift()) await job();
		});
		await Promise.all(workers);
	}

	private async recheckOne(
		socket: Socket,
		projectId: string,
		channelId: string,
	): Promise<void> {
		try {
			const allowed = await this.decider.can(socket.data, projectId, {
				action: CHANNEL_ACTION,
				resource: this.resource(projectId, channelId),
			});
			if (allowed) return;
			this.leave(socket, channelId);
			socket.emit("chat:revoked", { channelId });
		} catch (err) {
			this.logger.warn(
				{ userId: socket.data.userId, channelId, err },
				"chat recheck failed — keeping room",
			);
		}
	}

	// route delivers an announcement the API published (type "chat.*", without
	// an authz descriptor).
	route(
		type: string,
		payload: Record<string, unknown>,
		recipientIds: unknown,
	): void {
		const channelId = payload.channel_id;
		if (typeof channelId !== "string" || !UUID.test(channelId)) {
			this.logger.debug({ type }, "chat event without a channel — skipped");
			return;
		}
		if (type === "chat.channel.access_changed") {
			// Who may read the channel changed: forget what was cached and
			// re-check its room. Nothing is sent to clients.
			this.decider.flush();
			void this.recheck(channelId);
			return;
		}
		this.io.to(channelRoomName(channelId)).emit("chat", { type, payload });

		// The unread nudge for members who are not looking at the channel. A new
		// direct message also nudges its members, so it appears in their lists.
		if (
			!(type.startsWith("chat.message.") || type === "chat.dm.created") ||
			!Array.isArray(recipientIds) ||
			recipientIds.length === 0
		) {
			return;
		}
		const nudge = {
			type,
			payload: {
				project_id: payload.project_id,
				channel_id: channelId,
				seq: payload.seq,
				message_id: payload.message_id,
				thread_root_id: payload.thread_root_id,
				actor_id: payload.actor_id,
			},
		};
		for (const id of recipientIds) {
			if (typeof id === "string" && UUID.test(id)) {
				this.io.to(userChatRoomName(id)).emit("chat:unread", nudge);
			}
		}
	}
}
