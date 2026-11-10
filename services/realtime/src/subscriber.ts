// Valkey Pub/Sub subscriber.
//
// The API publishes every real-time event to the "paca.events" channel as
// JSON-serialised messages with the shape:
//
//   { "type": "<event-type>", "payload": { ...event-specific fields... },
//     "authz": { "action": "tasks:read", "resource": "project/<p>/task/<t>" } }
//
// `authz` says what IAM must allow before a client sees the event (see
// events.AuthzFor in services/api). It is the only routing and authorization
// input this service trusts for project events: the resource names the
// project, the action names the namespace room that prefilters the sockets,
// and each candidate socket's account is checked against the API on that
// resource before the event is emitted (see access.ts). A Deny on one task
// therefore keeps that task's events from an account that may read the rest.
//
// Other messages:
//
//   iam.changed          the API changed a role or an attachment: drop cached
//                        decisions and re-sync open sockets (see resync.ts).
//   notification.*       routed to the recipient's own room.
//   agent.* with an actor_account_id
//                        an owner-private conversation: the owner's own room.
//   agent.* with a project_id and conversation_id
//                        normally carries an `authz` (services/agent-runner
//                        and the API's agent.session.started); a publisher
//                        that predates it is checked as conversations:read on
//                        the conversation all the same. A payload-free
//                        agent.session.started also goes to the tasks room so
//                        task boards refresh.
//
// An event with a project_id but no `authz` (a publisher that predates the
// descriptor) falls back to the old prefix routing and is delivered to its
// whole room; this is logged once per type so it gets fixed at the source.
// Events with no project and no actor scope, or an unrecognised type, are
// dropped.

import Redis from "ioredis";
import type { Logger } from "pino";
import type { Server } from "socket.io";
import { type AccessDecider, deliverToReaders, KeyedQueue } from "./access.ts";
import {
	agentChatRoomName,
	eventNamespace,
	namespaceForAction,
	projectRoomName,
} from "./permissions.ts";

// CHANNEL is the Valkey Pub/Sub channel the API publishes to.
// Must stay in sync with events.ChannelRealtime in services/api.
const CHANNEL = "paca.events";

// TOPIC_IAM_CHANGED must stay in sync with events.TopicIAMChanged.
const TOPIC_IAM_CHANGED = "iam.changed";

interface RealtimeMessage {
	type: string;
	payload?: unknown;
	data?: unknown;
	authz?: unknown;
}

interface Authz {
	action: string;
	resource: string;
}

// Routing is everything the subscriber needs besides the Socket.IO server.
interface Routing {
	decider: AccessDecider;
	queue: KeyedQueue;
	onIamChanged: () => void;
	// A doc / task update announcement, for merging into live collaborative
	// rooms (see CollabHub.handleContentEvent). Delivery to sockets is separate.
	onContentChange?: (type: string, payload: Record<string, unknown>) => void;
	// Types already reported as published without an authz descriptor.
	warnedNoAuthz: Set<string>;
}

// createSubscriber connects a dedicated ioredis client in subscriber mode,
// subscribes to CHANNEL, and wires incoming messages to Socket.IO rooms.
// Returns the ioredis client so the caller can disconnect it on shutdown.
export function createSubscriber(
	valkeyUrl: string,
	io: Server,
	logger: Logger,
	decider: AccessDecider,
	onIamChanged: () => void,
	onContentChange?: (type: string, payload: Record<string, unknown>) => void,
): Redis {
	const routing: Routing = {
		decider,
		queue: new KeyedQueue(),
		onIamChanged,
		onContentChange,
		warnedNoAuthz: new Set(),
	};
	// A separate client is required because ioredis clients in subscriber mode
	// cannot issue regular commands.
	const client = new Redis(valkeyUrl, {
		autoResubscribe: true,
		lazyConnect: false,
		maxRetriesPerRequest: null,
	});

	client.on("connect", () => {
		logger.info("valkey subscriber connected");
	});

	client.on("reconnecting", () => {
		logger.warn("valkey subscriber reconnecting");
	});

	client.on("error", (err: unknown) => {
		logger.error({ err }, "valkey subscriber error");
	});

	client.subscribe(CHANNEL, (err, count) => {
		if (err) {
			logger.error({ err }, `failed to subscribe to ${CHANNEL}`);
		} else {
			logger.info(
				{ channel: CHANNEL, subscriptions: count },
				"subscribed to valkey channel",
			);
		}
	});

	client.on("message", (_channel: string, raw: string) => {
		let msg: RealtimeMessage;
		try {
			msg = JSON.parse(raw) as RealtimeMessage;
		} catch (err) {
			logger.warn({ err, raw }, "failed to parse valkey message — skipping");
			return;
		}

		routeEvent(io, msg, logger, routing);
	});

	return client;
}

export function routeEvent(
	io: Server,
	msg: RealtimeMessage,
	logger: Logger,
	routing: Routing,
): void {
	const { type } = msg;

	// A control message: not an event, never delivered.
	if (type === TOPIC_IAM_CHANGED) {
		logger.debug("iam changed — dropping decisions, re-syncing sockets");
		routing.onIamChanged();
		return;
	}

	const payload = eventPayload(msg);
	if (!payload) {
		logger.debug({ type }, "event has no payload — skipped");
		return;
	}

	// A write to a doc / task description may need merging into a live room.
	if (type === "doc.updated" || type === "task.updated") {
		routing.onContentChange?.(type, payload);
	}

	// notification.* events are routed to a user-specific room.
	if (type.startsWith("notification.")) {
		const recipientAccountId = payload.recipient_account_id;
		if (typeof recipientAccountId !== "string" || !recipientAccountId) {
			logger.debug(
				{ type },
				"notification event has no recipient_account_id — skipped",
			);
			return;
		}
		const room = `user:${recipientAccountId}:notifications`;
		logger.debug({ type, room }, "routing notification to user room");
		io.to(room).emit("notification", { type, payload });
		return;
	}

	// An owner-private agent.* event carries actor_account_id (the owning user)
	// and routes to that user's own per-user room — this covers both global
	// chat and a project's owner-private chat conversations. Checked before
	// project_id so a private conversation is never also broadcast into the
	// project room; a project-shared conversation has no actor_account_id and
	// falls through to the project-scoped branch below.
	if (type.startsWith("agent.")) {
		const actorAccountId = payload.actor_account_id;
		if (typeof actorAccountId === "string" && actorAccountId) {
			const room = agentChatRoomName(actorAccountId);
			logger.debug(
				{ type, room },
				"routing owner-private agent event to user room",
			);
			io.to(room).emit("event", { type, payload });
			return;
		}
	}

	const event = { type, payload };

	// The API said what IAM must allow: check each candidate socket on it.
	const authz = parseAuthz(msg.authz);
	if (authz) {
		const projectOfEvent = projectOfResource(authz.resource);
		const conversation = payload.conversation_id;
		if (projectOfEvent && typeof conversation === "string" && conversation) {
			announceSessionStarted(io, event.type, projectOfEvent, conversation);
		}
		routeAuthorized(io, event, authz, logger, routing);
		return;
	}

	// Project-scoped events without a descriptor.
	const projectId = payload.project_id;
	if (typeof projectId !== "string" || !projectId) {
		logger.debug({ type }, "event has no project or actor scope — skipped");
		return;
	}

	// Conversation content is authorized per conversation, not per room.
	const conversationId = payload.conversation_id;
	if (
		type.startsWith("agent.") &&
		type !== "agent.acp_bridge.status" &&
		typeof conversationId === "string" &&
		conversationId
	) {
		routeConversationEvent(
			io,
			event,
			projectId,
			conversationId,
			logger,
			routing,
		);
		return;
	}

	// Resolve the namespace room from the event type prefix.
	const ns = eventNamespace(type);
	if (!ns) {
		logger.debug({ type }, "unknown event namespace — skipped");
		return;
	}
	if (!routing.warnedNoAuthz.has(type)) {
		routing.warnedNoAuthz.add(type);
		logger.warn(
			{ type },
			"event published without an authz descriptor — delivered to its whole room",
		);
	}

	const room = projectRoomName(projectId, ns);
	logger.debug({ type, room }, "routing event to room");
	io.to(room).emit("event", event);
}

function parseAuthz(raw: unknown): Authz | undefined {
	if (!raw || typeof raw !== "object") return undefined;
	const { action, resource } = raw as Record<string, unknown>;
	if (typeof action !== "string" || !action) return undefined;
	if (typeof resource !== "string" || !resource) return undefined;
	return { action, resource };
}

// projectOfResource returns the project id of "project/<id>[/...]".
function projectOfResource(resource: string): string | undefined {
	const [root, id] = resource.split("/");
	return root === "project" && id ? id : undefined;
}

// routeAuthorized delivers an event to the sockets of the action's namespace
// room whose account may perform the action on the resource. The project comes
// from the resource, never from the payload. Events of one resource keep
// their order even when an earlier one waits on an access check.
function routeAuthorized(
	io: Server,
	event: { type: string; payload: Record<string, unknown> },
	authz: Authz,
	logger: Logger,
	{ decider, queue }: Routing,
): void {
	const projectId = projectOfResource(authz.resource);
	const ns = namespaceForAction(authz.action);
	if (!projectId || !ns) {
		logger.warn(
			{ type: event.type, authz },
			"authz descriptor names no project room — event dropped",
		);
		return;
	}
	const room = projectRoomName(projectId, ns);
	queue.run(authz.resource, async () => {
		const sockets = await io.in(room).fetchSockets();
		await deliverToReaders(
			sockets,
			decider,
			projectId,
			authz,
			event,
			(err, userId) =>
				logger.warn(
					{ type: event.type, resource: authz.resource, userId, err },
					"access check failed — event not delivered",
				),
		);
	});
}

// routeConversationEvent delivers an agent.* conversation event that carries
// no descriptor yet, as conversations:read on the conversation.
function routeConversationEvent(
	io: Server,
	event: { type: string; payload: Record<string, unknown> },
	projectId: string,
	conversationId: string,
	logger: Logger,
	routing: Routing,
): void {
	announceSessionStarted(io, event.type, projectId, conversationId);

	routeAuthorized(
		io,
		event,
		{
			action: "conversations:read",
			resource: `project/${projectId}/conversation/${conversationId}`,
		},
		logger,
		routing,
	);
}

// announceSessionStarted tells the project's task boards that a conversation
// started. The tasks-room copy carries no conversation content, only which
// conversation it was; the full event is delivered per viewer.
function announceSessionStarted(
	io: Server,
	type: string,
	projectId: string,
	conversationId: string,
): void {
	if (type !== "agent.session.started") return;
	io.to(projectRoomName(projectId, "tasks")).emit("event", {
		type,
		payload: { project_id: projectId, conversation_id: conversationId },
	});
}

function eventPayload(msg: RealtimeMessage): Record<string, unknown> | null {
	if (msg.payload && typeof msg.payload === "object") {
		return msg.payload as Record<string, unknown>;
	}
	if (msg.data && typeof msg.data === "object") {
		return msg.data as Record<string, unknown>;
	}
	return null;
}
