import { useRef } from "react";

/** Returns the previous value while the new one serializes the same, so a
 *  value recomputed to equal contents (column definitions rebuilt every time
 *  the task list changes, say) doesn't invalidate memoized children. Only for
 *  small plain-data values: it compares by JSON. */
export function useStableValue<T>(value: T): T {
	const ref = useRef<{ json: string; value: T } | null>(null);
	const json = JSON.stringify(value);
	if (ref.current === null || ref.current.json !== json) {
		ref.current = { json, value };
	}
	return ref.current.value;
}
