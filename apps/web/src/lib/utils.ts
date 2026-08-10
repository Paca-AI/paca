import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

/** Up to two uppercase initials from a display name, for avatar fallbacks
 *  (e.g. "Ada Lovelace" -> "AL", "Madonna" -> "M"). */
export function getInitials(name: string): string {
	return name
		.split(" ")
		.filter(Boolean)
		.map((n) => n[0])
		.join("")
		.toUpperCase()
		.slice(0, 2);
}

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
