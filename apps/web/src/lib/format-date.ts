import i18n from "@/i18n";

export function formatDate(
	iso: string,
	options: Intl.DateTimeFormatOptions = {
		year: "numeric",
		month: "long",
		day: "numeric",
	},
	// Date-only values (custom Date fields, task start/due dates) are stored as
	// "YYYY-MM-DDT00:00:00Z" — a calendar date pinned to UTC midnight, not an
	// instant. Rendering them in the viewer's local timezone shifts the day for
	// negative UTC offsets. Pass dateOnly to format in UTC so the stored day is
	// shown as-is. Real timestamps (created_at/updated_at) keep local time.
	opts?: { dateOnly?: boolean },
): string {
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return "";
	const finalOptions = opts?.dateOnly
		? { ...options, timeZone: "UTC" }
		: options;
	return new Intl.DateTimeFormat(i18n.language, finalOptions).format(d);
}
