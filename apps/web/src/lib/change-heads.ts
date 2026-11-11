// The change-log head a project's data was read at.
//
// The API stamps every read under /projects/{id} with the project's change-log
// head as it was when the read began (X-Change-Seq, X-Change-Epoch). A page
// loads its data over HTTP and joins the realtime feed a moment later; a change
// made in between is in neither. So until the project is joined, the first head
// seen is remembered, and the first join starts the feed from it: what changed
// since the oldest data on screen is replayed (applying a change the data
// already reflects is harmless), instead of missed.

export interface ChangeHead {
	seq: number;
	epoch: number;
}

const earliest = new Map<string, ChangeHead>();
const joined = new Set<string>();

/** Records a head seen on a response, while the project is not joined. */
export function observeChangeHead(
	projectId: string,
	seq: number,
	epoch: number,
): void {
	if (joined.has(projectId) || earliest.has(projectId)) return;
	if (!Number.isSafeInteger(seq) || !Number.isSafeInteger(epoch)) return;
	earliest.set(projectId, { seq, epoch });
}

/** The oldest head seen before the project was joined, if any. */
export function earliestChangeHead(projectId: string): ChangeHead | undefined {
	return earliest.get(projectId);
}

/** The feed is running for the project: heads on later responses are not needed. */
export function markJoined(projectId: string): void {
	joined.add(projectId);
	earliest.delete(projectId);
}

/** The project was left: start collecting heads again for its next join. */
export function markLeft(projectId: string): void {
	joined.delete(projectId);
	earliest.delete(projectId);
}

export function clearChangeHeads(): void {
	joined.clear();
	earliest.clear();
}

const PROJECT_URL = /^\/projects\/([0-9a-f-]{36})(?:[/?]|$)/i;

/** The project id of a request path like "/projects/<uuid>/tasks", or null. */
export function projectIdOfUrl(url: string | undefined): string | null {
	if (!url) return null;
	const path = url.startsWith("http") ? new URL(url).pathname : url;
	const rest = path.replace(/^\/api\/v1/, "");
	return PROJECT_URL.exec(rest)?.[1] ?? null;
}
