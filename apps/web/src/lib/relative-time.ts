const UNITS: Array<[Intl.RelativeTimeFormatUnit, number]> = [
	["year", 365 * 24 * 3600],
	["month", 30 * 24 * 3600],
	["week", 7 * 24 * 3600],
	["day", 24 * 3600],
	["hour", 3600],
	["minute", 60],
];

/**
 * A short, localized "4 minutes ago" for a past instant. Anything under a
 * minute reads as the locale's "now".
 */
export function relativeTime(
	iso: string,
	locale: string,
	now: number = Date.now(),
): string {
	const seconds = Math.round((new Date(iso).getTime() - now) / 1000);
	const fmt = new Intl.RelativeTimeFormat(locale, {
		numeric: "auto",
		style: "narrow",
	});
	for (const [unit, size] of UNITS) {
		if (Math.abs(seconds) >= size) {
			return fmt.format(Math.trunc(seconds / size), unit);
		}
	}
	return fmt.format(0, "second");
}
