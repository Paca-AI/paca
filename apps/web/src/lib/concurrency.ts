/**
 * Runs `fn` over `items` with at most `limit` in flight, returning results in
 * input order. Stops starting new items after the first failure and rejects
 * with that error once the in-flight ones settle.
 */
export async function mapPool<T, R>(
	items: readonly T[],
	limit: number,
	fn: (item: T, index: number) => Promise<R>,
): Promise<R[]> {
	const results = new Array<R>(items.length);
	let next = 0;
	let failed = false;
	let firstError: unknown;
	const worker = async () => {
		while (!failed && next < items.length) {
			const index = next++;
			try {
				results[index] = await fn(items[index], index);
			} catch (error) {
				if (!failed) {
					failed = true;
					firstError = error;
				}
			}
		}
	};
	await Promise.all(
		Array.from({ length: Math.min(limit, items.length) }, worker),
	);
	if (failed) throw firstError;
	return results;
}

/** Returns a function that runs tasks with at most `limit` in flight, queuing the rest. */
export function createLimiter(limit: number) {
	let active = 0;
	const queue: (() => void)[] = [];
	const release = () => {
		active--;
		queue.shift()?.();
	};
	return async <T>(task: () => Promise<T>): Promise<T> => {
		if (active >= limit) await new Promise<void>((r) => queue.push(r));
		active++;
		try {
			return await task();
		} finally {
			release();
		}
	};
}
