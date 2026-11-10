import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

export { getInitials } from "./initials";

export function cleanBlocks(
	blocks: unknown[] | null | undefined,
): unknown[] | null {
	if (!blocks || !Array.isArray(blocks)) return null;
	const strip = (arr: unknown[]): unknown[] => {
		return arr.map((item) => {
			if (!item || typeof item !== "object") return item;
			const { id, children, ...rest } = item as Record<string, unknown>;
			const res: Record<string, unknown> = { ...rest };
			if (Array.isArray(children)) {
				res.children = strip(children as unknown[]);
			}
			return res;
		});
	};
	return strip(blocks);
}

/**
 * True when a BlockNote onChange was caused only by a collaborator's edit
 * arriving over Yjs. Those must not mark the local editor dirty: the peer who
 * made the edit saves it.
 */
export function isRemoteOnlyChange(ctx?: {
	getChanges?: () => { source: { type: string } }[];
}): boolean {
	const changes = ctx?.getChanges?.() ?? [];
	return (
		changes.length > 0 && changes.every((c) => c.source.type === "yjs-remote")
	);
}
