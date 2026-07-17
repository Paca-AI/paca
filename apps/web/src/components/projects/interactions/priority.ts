export type PriorityLabelKey =
	| "priority.none"
	| "priority.low"
	| "priority.medium"
	| "priority.high"
	| "priority.critical";

export interface PriorityMeta {
	labelKey: PriorityLabelKey;
	color: string;
}

export const PRIORITY_LABELS: Record<number, PriorityMeta> = {
	0: { labelKey: "priority.none", color: "oklch(var(--muted-foreground))" },
	1: { labelKey: "priority.low", color: "#60a5fa" },
	2: { labelKey: "priority.medium", color: "#f59e0b" },
	3: { labelKey: "priority.high", color: "#f97316" },
	4: { labelKey: "priority.critical", color: "#ef4444" },
};

export const PRIORITY_LEVELS = Object.entries(PRIORITY_LABELS).map(
	([value, meta]) => ({ value: Number(value), ...meta }),
);

/**
 * Maps a raw importance number to a bucket index (0-4).
 * 0 → None, 1-19 → Low (1), 20-49 → Medium (2), 50-99 → High (3), 100+ → Critical (4)
 */
export function getImportanceBucket(importance: number): number {
	if (importance <= 0) return 0;
	if (importance < 20) return 1;
	if (importance < 50) return 2;
	if (importance < 100) return 3;
	return 4;
}

/**
 * Mid-range importance value for each bucket.
 * Used when assigning an importance level by label (e.g. picking "High" from
 * a dropdown) so the task lands in the middle of the range rather than at the
 * boundary.  Bucket 4 (Critical, 100+) uses 150 as a reasonable midpoint.
 */
export const IMPORTANCE_BUCKET_VALUES: Record<number, number> = {
	0: 0, // None
	1: 10, // Low:      mid of 1–19
	2: 35, // Medium:   mid of 20–49
	3: 75, // High:     mid of 50–99
	4: 150, // Critical: mid of 100–200 (representative)
};

export function getPriority(importance: number): PriorityMeta {
	return PRIORITY_LABELS[getImportanceBucket(importance)] ?? PRIORITY_LABELS[0];
}

/**
 * Returns the min/max raw importance bounds for a given bucket index (0-4).
 */
export function getImportanceBucketBounds(bucket: number): {
	min: number;
	max: number;
} {
	switch (bucket) {
		case 0:
			return { min: 0, max: 0 };
		case 1:
			return { min: 1, max: 19 };
		case 2:
			return { min: 20, max: 49 };
		case 3:
			return { min: 50, max: 99 };
		default:
			// Matches Postgres' `importance` column type (INTEGER, max 2147483647) —
			// the API binds this value directly into a SQL integer comparison, so a
			// larger bound (e.g. Number.MAX_SAFE_INTEGER) overflows and errors.
			return { min: 100, max: 2147483647 };
	}
}
