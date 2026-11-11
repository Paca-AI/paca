// Configuration loaded from environment variables.
// All required variables throw at startup if absent so misconfiguration is
// caught immediately rather than at first use.

export interface Config {
	port: number;
	apiUrl: string;
	valkey: {
		url: string;
	};
	cors: {
		origins: string[];
	};
	logLevel: string;
	// syncEnabled turns the resumable sync protocol (sync.ts) on. Off, a
	// client's "sync:join" is refused and it falls back to the legacy events.
	syncEnabled: boolean;
	// accessCacheTtlMs is how long the API's answer to "may this account see
	// this resource" is reused for the next event about it. Entity attributes
	// an IAM condition can use (a task's sprint, a status' category, a doc's
	// folder tree) change without any role changing, so a longer TTL trades
	// authorization freshness for fewer API calls: the default is deliberately short.
	accessCacheTtlMs: number;
	// collabCluster routes collaborative editing through per-room owners so it
	// works across replicas (see collab-cluster.ts). With a single replica it
	// costs one Valkey round trip per room join.
	collabCluster: boolean;
	// replicaId names this process in the cluster; unique per running replica.
	replicaId: string;
}

function requireEnv(name: string): string {
	const value = process.env[name];
	if (!value) throw new Error(`Missing required environment variable: ${name}`);
	return value;
}

// positiveInt reads a positive integer setting, or the default for anything else.
function positiveInt(raw: string | undefined, fallback: number): number {
	const n = Number.parseInt(raw ?? "", 10);
	return Number.isFinite(n) && n > 0 ? n : fallback;
}

export function loadConfig(): Config {
	return {
		port: parseInt(process.env.PORT ?? "3001", 10),
		// Internal API base URL (service-to-service, not via the public gateway).
		apiUrl: requireEnv("API_URL"),
		valkey: {
			url: requireEnv("REDIS_URL"),
		},
		cors: {
			// Comma-separated list of allowed origins, e.g. "http://localhost:3000,https://app.example.com"
			origins: (process.env.CORS_ORIGINS ?? "http://localhost:3000")
				.split(",")
				.map((s) => s.trim())
				.filter(Boolean),
		},
		logLevel: process.env.LOG_LEVEL ?? "info",
		syncEnabled: (process.env.SYNC_ENABLED ?? "true") !== "false",
		accessCacheTtlMs: positiveInt(process.env.ACCESS_CACHE_TTL_MS, 30_000),
		collabCluster: (process.env.COLLAB_CLUSTER ?? "true") !== "false",
		replicaId:
			process.env.REPLICA_ID ??
			`${process.env.HOSTNAME ?? "realtime"}-${crypto.randomUUID().slice(0, 8)}`,
	};
}
