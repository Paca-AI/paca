// Realtime service entry point.
//
// Starts the Socket.IO server and the Valkey Pub/Sub subscriber, then blocks
// until a SIGTERM or SIGINT signal triggers a graceful shutdown.
//
// The ioredis subscriber is a dedicated client locked into subscriber mode
// (subscribe/psubscribe only), which ioredis requires. Nothing else talks to
// Valkey: sessions live in memory and every decision comes from the API.

import pino from "pino";
import { AccessDecider } from "./access.ts";
import { loadConfig } from "./config.ts";
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

const { httpServer, io, resyncer, expiry, collab } = createSocketServer(
	config,
	logger,
	decider,
);
const subscriber = createSubscriber(
	config.valkey.url,
	io,
	logger,
	decider,
	() => {
		resyncer.onIamChanged();
		void collab.recheck();
	},
);

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
			collab.close();
			subscriber.disconnect();
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
