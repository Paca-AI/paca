// Keeping room membership true to IAM while sockets stay connected.
//
// Rooms are joined from the account's effective actions at "join" time, which
// goes stale: a role edit, a detached role or a removed member changes the
// answer. The API announces such changes (iam.changed); on one, the decision
// cache is dropped and every open socket's rooms are re-synced. A periodic
// pass does the same, so a missed Pub/Sub message costs at most one interval.
//
// Work is deduplicated per account + project: one permissions request serves
// all of that account's sockets.

import type { Logger } from "pino";
import type { Server, Socket } from "socket.io";
import type { AccessDecider } from "./access.ts";
import {
	ApiRequestError,
	fetchProjectPermissions,
	verifyTokenWithAPI,
} from "./api-client.ts";
import { type EventNamespace, WORKSPACE_FILES_ROOM } from "./permissions.ts";
import { syncProjectRooms } from "./rooms.ts";

const DEFAULT_INTERVAL_MS = 60_000;
const CONCURRENCY = 8;

export interface Resyncer {
	// onIamChanged reacts to the API's iam.changed announcement.
	onIamChanged(): void;
	start(): void;
	stop(): void;
}

export function createResyncer(
	io: Server,
	apiUrl: string,
	decider: AccessDecider,
	logger: Logger,
	intervalMs = DEFAULT_INTERVAL_MS,
): Resyncer {
	let timer: ReturnType<typeof setInterval> | undefined;
	let running = false;
	let again = false;

	// resyncAll re-syncs every project of every open socket. `announce` also
	// tells the socket its permissions changed so the client refetches its
	// own permission queries; the periodic pass only does so when the rooms
	// actually changed.
	async function resyncAll(announce: boolean): Promise<void> {
		if (running) {
			// An announcement during a pass must not be lost: run once more.
			again = again || announce;
			return;
		}
		running = true;
		try {
			const groups = new Map<string, Socket[]>();
			for (const socket of io.sockets.sockets.values()) {
				for (const projectId of socket.data.projects ?? []) {
					const key = `${socket.data.userId}\u0000${projectId}`;
					const list = groups.get(key);
					if (list) list.push(socket);
					else groups.set(key, [socket]);
				}
			}
			const jobs = [...groups.entries()].map(
				([key, sockets]) =>
					() =>
						resyncGroup(key.split("\u0000")[1] as string, sockets, announce),
			);
			const workers = Array.from({ length: CONCURRENCY }, async () => {
				for (let job = jobs.shift(); job; job = jobs.shift()) await job();
			});
			await Promise.all(workers);
			// A role change can also grant or revoke the platform-level
			// files:read that the workspace file room stands for.
			if (announce) await resyncWorkspaceFiles();
		} finally {
			running = false;
			if (again) {
				again = false;
				void resyncAll(true);
			}
		}
	}

	async function resyncGroup(
		projectId: string,
		sockets: Socket[],
		announce: boolean,
	): Promise<void> {
		// One account's sockets can hold different tokens (a tab that
		// reconnected on a refreshed cookie next to one whose token has
		// expired), and a 401 for one token says nothing about the others. So
		// try each distinct token until one is accepted, ending only the
		// sockets whose own token the API rejected; the answer is the
		// account's, so it then applies to every socket still connected.
		let live = sockets;
		let perms: Record<string, boolean> | null | undefined;
		for (const token of new Set(sockets.map((s) => s.data.rawToken))) {
			const holders = live.filter((s) => s.data.rawToken === token);
			const first = holders[0];
			if (!first) continue;
			try {
				perms = await fetchProjectPermissions(apiUrl, token, projectId);
				break;
			} catch (err) {
				logger.warn(
					{ userId: first.data.userId, projectId, err },
					"room resync failed",
				);
				if (err instanceof ApiRequestError && err.status === 401) {
					// A dead token cannot be resynced: end its sockets so they
					// reconnect with a fresh one.
					for (const s of holders) s.disconnect(true);
					live = live.filter((s) => s.data.rawToken !== token);
					continue;
				}
				// Any other failure keeps the rooms as they are until the next
				// pass; per-event checks still protect delivery.
				return;
			}
		}
		if (perms === undefined) return;
		sockets = live;
		for (const socket of sockets) {
			const before = roomsOf(socket, projectId);
			const after = syncProjectRooms(socket, projectId, perms);
			if (perms === null) socket.data.projects.delete(projectId);
			if (announce || before !== [...after].sort().join(",")) {
				socket.emit("permissions:changed", { projectId, namespaces: after });
			}
		}
	}

	// resyncWorkspaceFiles puts each account's sockets in the workspace files
	// room, or takes them out, by their current platform-level files:read. One
	// request per account (and token); a failure leaves the room as it is.
	async function resyncWorkspaceFiles(): Promise<void> {
		const byToken = new Map<string, Socket[]>();
		for (const socket of io.sockets.sockets.values()) {
			const list = byToken.get(socket.data.rawToken);
			if (list) list.push(socket);
			else byToken.set(socket.data.rawToken, [socket]);
		}
		const jobs = [...byToken.entries()].map(([token, sockets]) => async () => {
			try {
				const auth = await verifyTokenWithAPI(apiUrl, token);
				const allowed = auth.globalPermissions.includes("files:read");
				for (const s of sockets) {
					if (allowed) s.join(WORKSPACE_FILES_ROOM);
					else s.leave(WORKSPACE_FILES_ROOM);
				}
			} catch (err) {
				logger.warn({ err }, "workspace files room resync failed");
				if (err instanceof ApiRequestError && err.status === 401) {
					for (const s of sockets) s.disconnect(true);
				}
			}
		});
		const workers = Array.from({ length: CONCURRENCY }, async () => {
			for (let job = jobs.shift(); job; job = jobs.shift()) await job();
		});
		await Promise.all(workers);
	}

	function roomsOf(socket: Socket, projectId: string): string {
		const prefix = `project:${projectId}:`;
		return [...socket.rooms]
			.filter((r) => r.startsWith(prefix))
			.map((r) => r.slice(prefix.length) as EventNamespace)
			.sort()
			.join(",");
	}

	return {
		onIamChanged() {
			// The announcement names roles, not accounts: nothing cached about
			// anyone can be trusted.
			decider.flush();
			void resyncAll(true);
		},
		start() {
			timer = setInterval(() => void resyncAll(false), intervalMs);
			timer.unref();
		},
		stop() {
			if (timer) clearInterval(timer);
		},
	};
}
