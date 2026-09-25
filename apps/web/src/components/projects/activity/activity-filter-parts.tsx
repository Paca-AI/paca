// Building blocks shared by the activity filter bars (project activity page,
// agent activity tab): popover section labels, checkbox rows and a local
// date range picker.
import { Calendar } from "@/components/ui/calendar";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";

function parseDateOnly(s?: string): Date | undefined {
	if (!s) return undefined;
	const [y, m, d] = s.split("-").map(Number);
	if (!y || !m || !d) return undefined;
	return new Date(y, m - 1, d);
}

function toDateOnlyString(date: Date): string {
	const y = date.getFullYear();
	const m = String(date.getMonth() + 1).padStart(2, "0");
	const d = String(date.getDate()).padStart(2, "0");
	return `${y}-${m}-${d}`;
}

const dateTriggerClassName = (active: boolean) =>
	cn(
		"inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium transition-all duration-150",
		active
			? "border-primary/40 bg-primary/8 text-primary"
			: "border-border/25 bg-muted/25 text-muted-foreground hover:border-border/50 hover:bg-muted/40",
	);

// ── Popover section building blocks ──────────────────────────────────────────

export function FilterSectionLabel({
	children,
}: {
	children: React.ReactNode;
}) {
	return (
		<p className="px-0.5 pb-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/60">
			{children}
		</p>
	);
}

export function CheckListRow({
	label,
	checked,
	onChange,
}: {
	label: string;
	checked: boolean;
	onChange: () => void;
}) {
	return (
		<label className="flex cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-xs hover:bg-muted/40">
			<input
				type="checkbox"
				checked={checked}
				onChange={onChange}
				className="size-3.5 shrink-0 cursor-pointer rounded accent-primary"
			/>
			<span className="flex-1 truncate">{label}</span>
		</label>
	);
}

// ── Date range ────────────────────────────────────────────────────────────────

export function DateRangeFilter({
	createdAfter,
	createdBefore,
	fromLabel,
	toLabel,
	onChange,
}: {
	createdAfter?: string;
	createdBefore?: string;
	fromLabel: string;
	toLabel: string;
	onChange: (next: { createdAfter?: string; createdBefore?: string }) => void;
}) {
	return (
		<div className="flex items-center gap-1.5">
			<Popover>
				<PopoverTrigger
					type="button"
					className={dateTriggerClassName(!!createdAfter)}
				>
					{createdAfter ?? fromLabel}
				</PopoverTrigger>
				<PopoverContent className="w-auto p-2" align="start">
					<Calendar
						mode="single"
						selected={parseDateOnly(createdAfter)}
						onSelect={(d) =>
							onChange({ createdAfter: d ? toDateOnlyString(d) : undefined })
						}
					/>
				</PopoverContent>
			</Popover>
			<Popover>
				<PopoverTrigger
					type="button"
					className={dateTriggerClassName(!!createdBefore)}
				>
					{createdBefore ?? toLabel}
				</PopoverTrigger>
				<PopoverContent className="w-auto p-2" align="start">
					<Calendar
						mode="single"
						selected={parseDateOnly(createdBefore)}
						onSelect={(d) =>
							onChange({ createdBefore: d ? toDateOnlyString(d) : undefined })
						}
					/>
				</PopoverContent>
			</Popover>
		</div>
	);
}
