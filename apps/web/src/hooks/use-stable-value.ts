import { useRef } from "react";

/** Returns the previous value while the new one is structurally equal, so a
 *  value recomputed to equal contents (column definitions rebuilt every time
 *  the task list changes, say) doesn't invalidate memoized children. Only for
 *  small plain-data values. The ref is written during render, which only ever
 *  affects identity, never contents. */
export function useStableValue<T>(value: T): T {
	const ref = useRef<T>(value);
	if (ref.current !== value && !deepEqual(ref.current, value)) {
		ref.current = value;
	}
	return ref.current;
}

function deepEqual(a: unknown, b: unknown): boolean {
	if (Object.is(a, b)) return true;
	if (
		typeof a !== "object" ||
		typeof b !== "object" ||
		a === null ||
		b === null
	) {
		return false;
	}
	if (Array.isArray(a) !== Array.isArray(b)) return false;
	const ka = Object.keys(a);
	if (ka.length !== Object.keys(b).length) return false;
	return ka.every(
		(k) =>
			Object.hasOwn(b, k) &&
			deepEqual(
				(a as Record<string, unknown>)[k],
				(b as Record<string, unknown>)[k],
			),
	);
}
