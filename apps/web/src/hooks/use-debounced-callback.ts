import { useCallback, useEffect, useRef } from "react";

export function useDebouncedCallback<Args extends unknown[]>(
	callback: (...args: Args) => void,
	delay: number,
): (...args: Args) => void {
	const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
	const callbackRef = useRef(callback);
	callbackRef.current = callback;

	// Without this, a timer scheduled just before unmount still fires and
	// invokes callbackRef.current — harmless for a plain state setter, but
	// wasteful (or worse, a network call nobody reads) for heavier callbacks.
	useEffect(() => {
		return () => {
			if (timerRef.current) clearTimeout(timerRef.current);
		};
	}, []);

	return useCallback(
		(...args: Args) => {
			if (timerRef.current) clearTimeout(timerRef.current);
			timerRef.current = setTimeout(() => callbackRef.current(...args), delay);
		},
		[delay],
	);
}

/** Like useDebouncedCallback, but for callbacks the caller needs a result
 *  back from (e.g. a suggestion menu's getItems, which must return a
 *  Promise per call). Every call within the debounce window shares the
 *  single fetch actually fired once the timer settles — appropriate when
 *  callers only care about the latest value (live search), not a distinct
 *  response per invocation. Built on useDebouncedCallback for the timer
 *  itself; this just resolves each caller's own Promise once that fetch
 *  completes. */
export function useDebouncedAsyncCallback<Args extends unknown[], R>(
	callback: (...args: Args) => Promise<R>,
	delay: number,
): (...args: Args) => Promise<R> {
	const pendingRef = useRef<
		{ resolve: (value: R) => void; reject: (reason?: unknown) => void }[]
	>([]);

	const debouncedRun = useDebouncedCallback((...args: Args) => {
		const pending = pendingRef.current;
		pendingRef.current = [];
		callback(...args).then(
			(result) => {
				for (const p of pending) p.resolve(result);
			},
			(err) => {
				for (const p of pending) p.reject(err);
			},
		);
	}, delay);

	return useCallback(
		(...args: Args) =>
			new Promise<R>((resolve, reject) => {
				pendingRef.current.push({ resolve, reject });
				debouncedRun(...args);
			}),
		[debouncedRun],
	);
}
