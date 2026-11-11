// Reader of the per-project change log the API writes (see
// services/api/internal/platform/messaging/changelog.go).
//
// Each project has a meta hash ({seq, epoch}) and a stream whose entry IDs are
// "<seq>-0", so a cursor is turned into a range without scanning. Only reads
// happen here; the API is the sole writer.

import type Redis from "ioredis";

export interface ChangeAuthz {
	action: string;
	resource: string;
}

export interface ChangeEntry {
	seq: number;
	epoch: number;
	id: string;
	type: string;
	op: string;
	projectId: string;
	entityType?: string;
	entityId?: string;
	taskId?: string;
	actorId?: string;
	origin?: string;
	// The id the requesting client stamped on its mutation (X-Client-Op-Id):
	// that client skips the echo of its own change.
	clientOpId?: string;
	ts: string;
	payload: Record<string, unknown>;
	// entity is the entity after the change, when the API attached one (see
	// events.Event.Snapshot): clients patch their cache with it.
	entity?: Record<string, unknown>;
	authz: ChangeAuthz;
}

export interface ChangeHead {
	seq: number;
	epoch: number;
}

export interface ChangeLogReader {
	// head is the last allocated seq and the log's epoch; null before the
	// project has any change.
	head(projectId: string): Promise<ChangeHead | null>;
	// oldest is the seq of the oldest entry still kept, null when none is.
	oldest(projectId: string): Promise<number | null>;
	// after returns up to `limit` entries with seq > `seq`, in order.
	after(projectId: string, seq: number, limit: number): Promise<ChangeEntry[]>;
}

export function changeLogKeys(projectId: string): {
	meta: string;
	stream: string;
} {
	const base = `paca:changes:{${projectId}}`;
	return { meta: `${base}:meta`, stream: `${base}:stream` };
}

// parseEntry decodes a stream entry's flat field list. null when it is not a
// change this service can deliver (a malformed or foreign entry).
export function parseEntry(
	fields: string[],
	projectId: string,
): ChangeEntry | null {
	const f: Record<string, string> = {};
	for (let i = 0; i + 1 < fields.length; i += 2) {
		f[fields[i] as string] = fields[i + 1] as string;
	}
	const seq = Number(f.seq);
	const epoch = Number(f.epoch);
	if (!Number.isSafeInteger(seq) || seq < 1 || !Number.isFinite(epoch)) {
		return null;
	}
	let payload: unknown;
	let authz: unknown;
	try {
		payload = JSON.parse(f.payload ?? "null");
		authz = JSON.parse(f.authz ?? "null");
	} catch {
		return null;
	}
	if (!payload || typeof payload !== "object") return null;
	const a = authz as Partial<ChangeAuthz> | null;
	if (!a || typeof a.action !== "string" || typeof a.resource !== "string") {
		return null;
	}
	let entity: Record<string, unknown> | undefined;
	if (f.entity) {
		try {
			const parsed: unknown = JSON.parse(f.entity);
			if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
				entity = parsed as Record<string, unknown>;
			}
		} catch {
			// An unreadable snapshot only costs the client a refetch.
		}
	}
	return {
		seq,
		epoch,
		id: f.id ?? "",
		type: f.type ?? "",
		op: f.op ?? "event",
		projectId: f.project_id ?? projectId,
		entityType: f.entity_type,
		entityId: f.entity_id,
		taskId: f.task_id,
		actorId: f.actor_id,
		origin: f.origin,
		clientOpId: f.client_op_id,
		ts: f.ts ?? "",
		payload: payload as Record<string, unknown>,
		entity,
		authz: { action: a.action, resource: a.resource },
	};
}

export class ValkeyChangeLogReader implements ChangeLogReader {
	constructor(private readonly redis: Redis) {}

	async head(projectId: string): Promise<ChangeHead | null> {
		const { meta } = changeLogKeys(projectId);
		const [seq, epoch] = await this.redis.hmget(meta, "seq", "epoch");
		if (seq == null || epoch == null) return null;
		return { seq: Number(seq), epoch: Number(epoch) };
	}

	async oldest(projectId: string): Promise<number | null> {
		const { stream } = changeLogKeys(projectId);
		const rows = await this.redis.xrange(stream, "-", "+", "COUNT", 1);
		const first = rows[0];
		return first ? Number.parseInt(first[0], 10) : null;
	}

	async after(
		projectId: string,
		seq: number,
		limit: number,
	): Promise<ChangeEntry[]> {
		const { stream } = changeLogKeys(projectId);
		// "(" makes the start exclusive (Redis 6.2+, Valkey).
		const rows = await this.redis.xrange(
			stream,
			`(${seq}-0`,
			"+",
			"COUNT",
			limit,
		);
		const out: ChangeEntry[] = [];
		for (const [, fields] of rows) {
			const entry = parseEntry(fields, projectId);
			if (entry) out.push(entry);
		}
		return out;
	}
}
