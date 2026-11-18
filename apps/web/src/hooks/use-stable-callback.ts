import { useCallback, useLayoutEffect, useRef } from "react";

/** A function with a stable identity that always calls the latest `fn`. Lets
 *  a parent hand callbacks to memoized children without every render of the
 *  parent invalidating them. Only call it from event handlers and effects,
 *  never while rendering. */
export function useStableCallback<A extends unknown[], R>(
	fn: (...args: A) => R,
): (...args: A) => R {
	const ref = useRef(fn);
	useLayoutEffect(() => {
		ref.current = fn;
	});
	return useCallback((...args: A) => ref.current(...args), []);
}
