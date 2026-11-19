/**
 * Runs `fn` over `items` with at most `limit` in flight, returning results in
 * input order. Stops starting new items after the first failure and rejects
 * with that error once the in-flight ones settle.
 *
 * `fn` receives an AbortSignal that fires as soon as one item fails (or when
 * `outerSignal` aborts), so in-flight siblings can cancel instead of running on.
 * The first error is always the one reported, never a sibling's abort error.
 */
export async function mapPool<T, R>(
	items: readonly T[],
	limit: number,
	fn: (item: T, index: number, signal: AbortSignal) => Promise<R>,
	outerSignal?: AbortSignal,
): Promise<R[]> {
	const results = new Array<R>(items.length);
	const controller = new AbortController();
	const onOuterAbort = () => controller.abort();
	if (outerSignal?.aborted) controller.abort();
	else outerSignal?.addEventListener("abort", onOuterAbort, { once: true });
	let next = 0;
	let failed = false;
	let firstError: unknown;
	const worker = async () => {
		while (!failed && next < items.length) {
			const index = next++;
			try {
				results[index] = await fn(items[index], index, controller.signal);
			} catch (error) {
				if (!failed) {
					failed = true;
					firstError = error;
					controller.abort();
				}
			}
		}
	};
	try {
		const workers = Math.max(1, Math.min(Math.floor(limit) || 1, items.length));
		await Promise.all(Array.from({ length: workers }, worker));
	} finally {
		outerSignal?.removeEventListener("abort", onOuterAbort);
	}
	if (failed) throw firstError;
	return results;
}

/** Returns a function that runs tasks with at most `limit` in flight, queuing the rest. */
export function createLimiter(limit: number) {
	const max = Math.max(1, Math.floor(limit) || 1);
	let active = 0;
	const queue: (() => void)[] = [];
	const release = () => {
		const next = queue.shift();
		// Hand the slot straight to the next waiter so a caller arriving in
		// between cannot take it and push us over the limit.
		if (next) next();
		else active--;
	};
	return async <T>(task: () => Promise<T>): Promise<T> => {
		if (active >= max) await new Promise<void>((r) => queue.push(r));
		else active++;
		try {
			return await task();
		} finally {
			release();
		}
	};
}
