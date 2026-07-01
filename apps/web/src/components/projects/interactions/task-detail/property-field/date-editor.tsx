import { CalendarDays, Minus, Trash2 } from "lucide-react";
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
	const { t } = useTranslation();
	return (
		<Popover>
			<PopoverTrigger
				type="button"
				className="inline-flex items-center gap-1.5 rounded-lg border border-border/25 bg-muted/25 px-2.5 py-1.5 text-xs text-muted-foreground hover:border-border/50 hover:bg-muted/40 transition-all duration-150 font-medium"
			>
				<CalendarDays className="size-3 shrink-0 opacity-70" />
				<span>
					{displayDate(value) ??
						placeholder ??
						t("project.interactions.taskDetail.datePlaceholder")}
				</span>
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
							Clear date
						</button>
					</div>
				)}
			</PopoverContent>
		</Popover>
	);
}

export function DateRangeEditor({
	startDate,
	dueDate,
	canEdit,
	onStartDateChange,
	onDueDateChange,
}: {
	startDate?: string | null;
	dueDate?: string | null;
	canEdit: boolean;
	onStartDateChange?: (v: string | null) => void;
	onDueDateChange?: (v: string | null) => void;
}) {
	const { t } = useTranslation();
	if (!canEdit) {
		return (
			<div className="flex items-center gap-2 flex-wrap">
				<span className="inline-flex items-center gap-1.5 rounded-lg border border-border/25 bg-muted/25 px-2.5 py-1.5 text-xs text-muted-foreground font-medium">
					<CalendarDays className="size-3 shrink-0 opacity-70" />
					{displayDate(startDate) ??
						t("project.interactions.taskDetail.startDatePlaceholder")}
				</span>
				<Minus className="size-3 text-border/40 shrink-0" />
				<span className="inline-flex items-center gap-1.5 rounded-lg border border-border/25 bg-muted/25 px-2.5 py-1.5 text-xs text-muted-foreground font-medium">
					<CalendarDays className="size-3 shrink-0 opacity-70" />
					{displayDate(dueDate) ??
						t("project.interactions.taskDetail.dueDatePlaceholder")}
				</span>
			</div>
		);
	}

	return (
		<div className="flex items-center gap-2 flex-wrap">
			<SingleDateEditor
				value={startDate}
				placeholder={t("project.interactions.taskDetail.startDatePlaceholder")}
				onChange={onStartDateChange}
			/>
			<Minus className="size-3 text-border/40 shrink-0" />
			<SingleDateEditor
				value={dueDate}
				placeholder={t("project.interactions.taskDetail.dueDatePlaceholder")}
				onChange={onDueDateChange}
			/>
		</div>
	);
}
