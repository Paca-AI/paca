import { format, isToday, isYesterday } from "date-fns";

/** A rule with a label in the middle: a day boundary, or where the unread begin. */
function Rule({ label, tone }: { label: string; tone?: "day" | "new" }) {
	const line = tone === "new" ? "bg-destructive/40" : "bg-border";
	return (
		<div className="my-2 flex items-center gap-3 px-4">
			<span aria-hidden className={`h-px flex-1 ${line}`} />
			<span
				className={
					tone === "new"
						? "text-xs font-medium text-destructive"
						: "text-xs font-medium text-muted-foreground"
				}
			>
				{label}
			</span>
			<span aria-hidden className={`h-px flex-1 ${line}`} />
		</div>
	);
}

export function dayLabel(
	iso: string,
	today: string,
	yesterday: string,
): string {
	const d = new Date(iso);
	if (isToday(d)) return today;
	if (isYesterday(d)) return yesterday;
	return format(d, "EEE d MMM yyyy");
}

export function DayDivider({
	iso,
	today,
	yesterday,
}: {
	iso: string;
	today: string;
	yesterday: string;
}) {
	return <Rule label={dayLabel(iso, today, yesterday)} />;
}

export function NewMessagesDivider({ label }: { label: string }) {
	return <Rule label={label} tone="new" />;
}
