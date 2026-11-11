// Valkey-backed ports of the collab cluster (see collab-cluster.ts).

import type Redis from "ioredis";
import type {
	ClusterBus,
	ClusterMessage,
	LeaseStore,
} from "./collab-cluster.ts";

const OWNER = (room: string) => `paca:collab:owner:${room}`;
const REPLICA = (id: string) => `paca:collab:replica:${id}`;
const CHANNEL = (id: string) => `paca:collab:in:${id}`;

const CLAIM = `
local ok = redis.call('SET', KEYS[1], ARGV[1], 'NX', 'PX', ARGV[2])
if ok then return ARGV[1] end
local cur = redis.call('GET', KEYS[1])
if cur then return cur end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return ARGV[1]`;
const RENEW = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  return 1
end
return 0`;
const DEL_IF = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0`;

export class ValkeyLeases implements LeaseStore {
	constructor(private readonly redis: Redis) {}

	async claim(room: string, replica: string, ttlMs: number): Promise<string> {
		return String(await this.redis.eval(CLAIM, 1, OWNER(room), replica, ttlMs));
	}

	async renew(room: string, replica: string, ttlMs: number): Promise<boolean> {
		return (await this.redis.eval(RENEW, 1, OWNER(room), replica, ttlMs)) === 1;
	}

	async release(room: string, replica: string): Promise<void> {
		await this.redis.eval(DEL_IF, 1, OWNER(room), replica);
	}

	async takeover(room: string, from: string): Promise<void> {
		await this.redis.eval(DEL_IF, 1, OWNER(room), from);
	}

	owner(room: string): Promise<string | null> {
		return this.redis.get(OWNER(room));
	}

	async heartbeat(replica: string, ttlMs: number): Promise<void> {
		await this.redis.set(REPLICA(replica), "1", "PX", ttlMs);
	}

	async alive(replica: string): Promise<boolean> {
		return (await this.redis.exists(REPLICA(replica))) === 1;
	}
}

// ValkeyBus sends over Pub/Sub, one channel per replica. `commands` publishes;
// `subscriber` is a dedicated connection (a subscribing connection can do
// nothing else).
export class ValkeyBus implements ClusterBus {
	constructor(
		private readonly commands: Redis,
		private readonly subscriber: Redis,
	) {}

	async send(toReplica: string, message: ClusterMessage): Promise<void> {
		await this.commands.publish(CHANNEL(toReplica), JSON.stringify(message));
	}

	async listen(
		replica: string,
		handler: (m: ClusterMessage) => void,
	): Promise<void> {
		const channel = CHANNEL(replica);
		this.subscriber.on("message", (ch: string, raw: string) => {
			if (ch !== channel) return;
			try {
				handler(JSON.parse(raw) as ClusterMessage);
			} catch {
				// A message that is not ours to understand is dropped.
			}
		});
		await this.subscriber.subscribe(channel);
	}

	close(): void {
		this.subscriber.disconnect();
	}
}
