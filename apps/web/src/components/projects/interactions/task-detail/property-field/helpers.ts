// Date-only custom fields (and sprint start/due dates) are stored as
// "YYYY-MM-DDT00:00:00Z" — a calendar date pinned to UTC midnight, NOT an
// instant. Parsing/formatting it via the local timezone shifts the day back
// for any negative UTC offset (e.g. America/Sao_Paulo, UTC-3), which showed
// the previous day. So we read the Y-M-D parts directly and format in UTC.

export function toDateObject(iso?: string | null): Date | undefined {
	if (!iso) return undefined;
	const parts = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso);
	if (parts) {
		// Build a LOCAL date on that calendar day so the calendar highlights
		// the same day the user picked, regardless of timezone.
		return new Date(Number(parts[1]), Number(parts[2]) - 1, Number(parts[3]));
	}
	const d = new Date(iso);
	return Number.isNaN(d.getTime()) ? undefined : d;
}

export function toISODate(date: Date): string {
	const y = date.getFullYear();
	const m = String(date.getMonth() + 1).padStart(2, "0");
	const d = String(date.getDate()).padStart(2, "0");
	return `${y}-${m}-${d}T00:00:00Z`;
}

export function displayDate(iso?: string | null) {
	if (!iso) return null;
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return null;
	return d.toLocaleDateString(undefined, {
		month: "short",
		day: "numeric",
		year: "numeric",
		timeZone: "UTC",
	});
}
