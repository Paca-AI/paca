// Realtime service entry point.
//
// Starts the Socket.IO server and the Valkey Pub/Sub subscriber, then blocks
// until a SIGTERM or SIGINT signal triggers a graceful shutdown.
//
// The ioredis subscriber is a dedicated client locked into subscriber mode
// (subscribe/psubscribe only), which ioredis requires. Nothing else talks to
// Valkey except a command client that reads the change log: sessions live in
// memory and every decision comes from the API.

import Redis from "ioredis";
import pino from "pino";
import { AccessDecider } from "./access.ts";
import { ValkeyChangeLogReader } from "./change-log.ts";
import { loadConfig } from "./config.ts";
import { IamEpochWatcher } from "./iam-epoch.ts";
import { createSocketServer } from "./server.ts";
import { createSubscriber } from "./subscriber.ts";

const config = loadConfig();

const logger = pino({
	level: config.logLevel,
	...(process.env.NODE_ENV !== "production" && {
		transport: { target: "pino-pretty", options: { colorize: true } },
	}),
});

logger.info("starting paca realtime service");

const decider = new AccessDecider(config.apiUrl);

// A regular (non-subscriber) client, used only to read the change log.
const commands = new Redis(config.valkey.url, { maxRetriesPerRequest: 2 });
commands.on("error", (err: unknown) => {
	logger.error({ err }, "valkey command client error");
});

const { httpServer, io, resyncer, expiry, collab, sync } = createSocketServer(
	config,
	logger,
	decider,
	new ValkeyChangeLogReader(commands),
);
// What an IAM change makes every open socket redo: drop decisions, re-sync
// rooms, re-check collab editors.
function onIamChanged(): void {
	resyncer.onIamChanged();
	void collab.recheck();
}

// A missed iam.changed announcement is noticed through the API's counter.
const iamEpoch = new IamEpochWatcher({
	read: async () => {
		const v = await commands.get("paca:iam:epoch"); // events.IAMEpochKey
		return v === null ? null : Number(v);
	},
	onChange: () => {
		logger.warn("iam epoch moved without an announcement — re-syncing");
		onIamChanged();
	},
});
iamEpoch.start();

const subscriber = createSubscriber(
	config.valkey.url,
	io,
	logger,
	decider,
	(epoch) => {
		iamEpoch.observe(epoch);
		onIamChanged();
	},
	(type, payload) => void collab.handleContentEvent(type, payload),
	(projectId) => sync?.notify(projectId),
);

// A missed iam.changed announcement must cost at most one interval, as it does
// for project rooms (see resync.ts): re-check collab editors periodically too.
// The decider caches answers for 30 s, so each pass sees fresh decisions.
const collabRecheck = setInterval(() => void collab.recheck(), 60_000);
collabRecheck.unref();

httpServer.listen(config.port, () => {
	logger.info({ port: config.port }, "realtime service listening");
});

// ── Graceful shutdown ────────────────────────────────────────────────────────

function shutdown(signal: string): void {
	logger.info({ signal }, "shutdown signal received");

	// Close Socket.IO first to actively terminate WebSocket connections,
	// then close the HTTP server so it stops accepting new connections.
	io.close(() => {
		httpServer.close(() => {
			resyncer.stop();
			expiry.stop();
			clearInterval(collabRecheck);
			collab.close();
			sync?.stop();
			iamEpoch.stop();
			subscriber.disconnect();
			commands.disconnect();
			logger.info("shutdown complete");
			process.exit(0);
		});
	});

	setTimeout(() => {
		logger.warn("forced shutdown after timeout");
		process.exit(1);
	}, 10_000).unref();
}

process.on("SIGTERM", () => shutdown("SIGTERM"));
process.on("SIGINT", () => shutdown("SIGINT"));
