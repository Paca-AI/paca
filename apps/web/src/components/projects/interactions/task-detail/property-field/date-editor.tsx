import { CalendarDays, Minus, Trash2 } from "lucide-react";
import { useState } from "react";
import type { DateRange } from "react-day-picker";
import { useTranslation } from "react-i18next";
import { Calendar } from "@/components/ui/calendar";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { displayDate, toDateObject, toISODate } from "./helpers";

export function SingleDateEditor({
	value,
	placeholder,
	onChange,
}: {
	value?: string | null;
	placeholder?: string;
	onChange?: (v: string | null) => void;
}) {
	const { t } = useTranslation("projects");
	const resolvedPlaceholder =
		placeholder ?? t("taskDetail.propertyField.dateEditor.datePlaceholder");
	return (
		<Popover>
			<PopoverTrigger
				type="button"
				className="inline-flex items-center gap-1.5 rounded-lg border border-border/25 bg-muted/25 px-2.5 py-1.5 text-xs text-muted-foreground hover:border-border/50 hover:bg-muted/40 transition-all duration-150 font-medium"
			>
				<CalendarDays className="size-3 shrink-0 opacity-70" />
				<span>{displayDate(value) ?? resolvedPlaceholder}</span>
			</PopoverTrigger>
			<PopoverContent
				className="w-auto p-2 rounded-xl border border-border/40 shadow-lg"
				align="start"
			>
				<Calendar
					mode="single"
					selected={toDateObject(value)}
					onSelect={(d) => onChange?.(d ? toISODate(d) : null)}
					initialFocus
				/>
				{value && (
					<div className="border-t border-border/30 pt-1.5 px-1 pb-0.5">
						<button
							type="button"
							className="flex items-center gap-1.5 text-xs text-muted-foreground/60 hover:text-destructive transition-colors duration-150"
							onClick={() => onChange?.(null)}
						>
							<Trash2 className="size-3" />
							{t("taskDetail.propertyField.dateEditor.clearDate")}
						</button>
					</div>
				)}
			</PopoverContent>
		</Popover>
	);
}

export type DateRangeValue = {
	startDate: string | null;
	dueDate: string | null;
};

/** Start and due date edited together in one range calendar: click the first
 * day, then the last (the same day twice gives a one-day span). The change is
 * saved when the second day is clicked, or when the picker closes after only a
 * start was picked. */
export function DateRangeEditor({
	startDate,
	dueDate,
	canEdit,
	onChange,
}: {
	startDate?: string | null;
	dueDate?: string | null;
	canEdit: boolean;
	onChange?: (range: DateRangeValue) => void;
}) {
	const { t } = useTranslation("projects");
	const [open, setOpen] = useState(false);
	const [draft, setDraft] = useState<DateRange | undefined>(undefined);
	// First day clicked while a range is being picked; null between picks.
	const [anchor, setAnchor] = useState<Date | null>(null);

	const startLabel =
		displayDate(startDate) ??
		t("taskDetail.propertyField.dateEditor.startDate");
	const dueLabel =
		displayDate(dueDate) ?? t("taskDetail.propertyField.dateEditor.dueDate");
	const triggerClass =
		"inline-flex items-center gap-1.5 rounded-lg border border-border/25 bg-muted/25 px-2.5 py-1.5 text-xs text-muted-foreground font-medium";

	if (!canEdit) {
		return (
			<span className={triggerClass}>
				<CalendarDays className="size-3 shrink-0 opacity-70" />
				{startLabel}
				<Minus className="size-3 text-border/40 shrink-0" />
				{dueLabel}
			</span>
		);
	}

	const commit = (range: DateRange | undefined) => {
		const next: DateRangeValue = {
			startDate: range?.from ? toISODate(range.from) : null,
			dueDate: range?.to ? toISODate(range.to) : null,
		};
		if (
			next.startDate !== (startDate ?? null) ||
			next.dueDate !== (dueDate ?? null)
		) {
			onChange?.(next);
		}
	};

	const handleOpenChange = (next: boolean) => {
		if (next) {
			setDraft({ from: toDateObject(startDate), to: toDateObject(dueDate) });
			setAnchor(null);
		} else if (anchor) {
			// Closed after picking only a start: keep that start.
			commit({ from: anchor, to: undefined });
		}
		setOpen(next);
	};

	return (
		<Popover open={open} onOpenChange={handleOpenChange}>
			<PopoverTrigger
				type="button"
				className={`${triggerClass} hover:border-border/50 hover:bg-muted/40 transition-all duration-150`}
			>
				<CalendarDays className="size-3 shrink-0 opacity-70" />
				<span>{startLabel}</span>
				<Minus className="size-3 text-border/40 shrink-0" />
				<span>{dueLabel}</span>
			</PopoverTrigger>
			<PopoverContent
				className="w-auto p-2 rounded-xl border border-border/40 shadow-lg"
				align="start"
			>
				<Calendar
					mode="range"
					numberOfMonths={2}
					defaultMonth={toDateObject(startDate) ?? toDateObject(dueDate)}
					selected={draft}
					// DayPicker treats a first click as a finished one-day range, so
					// the two clicks are handled here: start, then end.
					onSelect={() => {}}
					onDayClick={(day) => {
						if (!anchor) {
							setAnchor(day);
							setDraft({ from: day, to: undefined });
							return;
						}
						const [from, to] = day < anchor ? [day, anchor] : [anchor, day];
						commit({ from, to });
						setAnchor(null);
						setDraft(undefined);
						setOpen(false);
					}}
				/>
				{(startDate || dueDate) && (
					<div className="border-t border-border/30 pt-1.5 px-1 pb-0.5">
						<button
							type="button"
							className="flex items-center gap-1.5 text-xs text-muted-foreground/60 hover:text-destructive transition-colors duration-150"
							onClick={() => {
								onChange?.({ startDate: null, dueDate: null });
								setAnchor(null);
								setDraft(undefined);
								setOpen(false);
							}}
						>
							<Trash2 className="size-3" />
							{t("taskDetail.propertyField.dateEditor.clearDates")}
						</button>
					</div>
				)}
			</PopoverContent>
		</Popover>
	);
}
