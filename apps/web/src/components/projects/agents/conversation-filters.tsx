import { Search, SlidersHorizontal, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Calendar } from "@/components/ui/calendar";
import { Input } from "@/components/ui/input";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import {
	CONVERSATION_STATUS_COLORS,
	CONVERSATION_STATUS_LABELS,
	CONVERSATION_TRIGGER_TYPE_LABELS,
	type ConversationFilters as ConversationFiltersState,
	type ConversationStatus,
	type ConversationTriggerType,
} from "@/lib/agent-api";
import { cn } from "@/lib/utils";

const ALL_STATUSES: ConversationStatus[] = [
	"queued",
	"running",
	"paused",
	"finished",
	"failed",
	"stopped",
];

const ALL_TRIGGER_TYPES: ConversationTriggerType[] = [
	"task_assigned",
	"comment_mention",
	"chat_message",
	"description_write",
	"automation_message",
];

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

function FilterSectionLabel({ children }: { children: React.ReactNode }) {
	return (
		<p className="px-0.5 pb-1 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/60">
			{children}
		</p>
	);
}

function CheckListRow({
	label,
	checked,
	onChange,
	dotClassName,
}: {
	label: string;
	checked: boolean;
	onChange: () => void;
	dotClassName?: string;
}) {
	return (
		<label className="flex cursor-pointer items-center gap-2 rounded-md px-1.5 py-1 text-xs hover:bg-muted/40">
			<input
				type="checkbox"
				checked={checked}
				onChange={onChange}
				className="size-3.5 shrink-0 cursor-pointer rounded accent-primary"
			/>
			{dotClassName && (
				<span className={cn("size-1.5 shrink-0 rounded-full", dotClassName)} />
			)}
			<span className="flex-1 truncate">{label}</span>
		</label>
	);
}

// ── Date range ────────────────────────────────────────────────────────────────

function DateRangeFilter({
	createdAfter,
	createdBefore,
	onChange,
}: {
	createdAfter?: string;
	createdBefore?: string;
	onChange: (next: { createdAfter?: string; createdBefore?: string }) => void;
}) {
	const { t } = useTranslation("projects");
	return (
		<div className="flex items-center gap-1.5">
			<Popover>
				<PopoverTrigger
					type="button"
					className={dateTriggerClassName(!!createdAfter)}
				>
					{createdAfter ?? t("conversationsPage.filters.dateFrom")}
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
					{createdBefore ?? t("conversationsPage.filters.dateTo")}
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

// ── Filter bar ────────────────────────────────────────────────────────────────

export interface ConversationFiltersProps {
	agents: { id: string; name: string }[];
	filters: ConversationFiltersState;
	onFiltersChange: (next: ConversationFiltersState) => void;
}

export function ConversationFilters({
	agents,
	filters,
	onFiltersChange,
}: ConversationFiltersProps) {
	const { t } = useTranslation("projects");
	const [searchInput, setSearchInput] = useState(filters.search ?? "");
	const [filtersOpen, setFiltersOpen] = useState(false);

	const debouncedSetSearch = useDebouncedCallback((value: string) => {
		onFiltersChange({ ...filters, search: value || undefined });
	}, 300);

	// Keeps the visible input in sync when search is cleared/changed from
	// outside this component (e.g. the "clear all" button below).
	useEffect(() => {
		setSearchInput(filters.search ?? "");
	}, [filters.search]);

	const activeFilterCount =
		((filters.agentIds?.length ?? 0) ? 1 : 0) +
		((filters.statuses?.length ?? 0) ? 1 : 0) +
		((filters.triggerTypes?.length ?? 0) ? 1 : 0) +
		(filters.createdAfter || filters.createdBefore ? 1 : 0);
	const hasAnyActive = activeFilterCount > 0 || !!filters.search;

	const toggle = <T extends string>(
		field: "agentIds" | "statuses" | "triggerTypes",
		value: T,
		current: T[] | undefined,
	) => {
		const next = current?.includes(value)
			? current.filter((v) => v !== value)
			: [...(current ?? []), value];
		onFiltersChange({
			...filters,
			[field]: next.length > 0 ? next : undefined,
		});
	};

	return (
		<div className="flex items-center gap-1.5 border-b border-border/50 px-2 py-2">
			<div className="relative min-w-0 flex-1">
				<Search className="absolute left-2 top-1/2 size-3 -translate-y-1/2 text-muted-foreground/60" />
				<Input
					value={searchInput}
					onChange={(e) => {
						setSearchInput(e.target.value);
						debouncedSetSearch(e.target.value);
					}}
					placeholder={t("conversationsPage.filters.searchPlaceholder")}
					className="h-7 pl-6 text-xs"
				/>
			</div>

			<Popover open={filtersOpen} onOpenChange={setFiltersOpen}>
				<PopoverTrigger
					type="button"
					aria-label={t("conversationsPage.filters.title")}
					className={cn(
						"relative flex size-7 shrink-0 items-center justify-center rounded-md transition-all duration-150",
						filtersOpen || activeFilterCount > 0
							? "bg-primary/8 text-primary/80"
							: "text-muted-foreground/60 hover:text-foreground hover:bg-muted/60",
					)}
				>
					<SlidersHorizontal className="size-3.5" />
					{activeFilterCount > 0 && (
						<span className="absolute -top-0.5 -right-0.5 flex h-3.5 min-w-3.5 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold leading-none text-primary-foreground">
							{activeFilterCount}
						</span>
					)}
				</PopoverTrigger>
				<PopoverContent
					side="bottom"
					align="end"
					sideOffset={6}
					className="w-72 rounded-xl border border-border/40 p-0 shadow-xl"
				>
					<div className="border-b border-border/30 bg-muted/20 px-3 py-2">
						<p className="text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/70">
							{t("conversationsPage.filters.title")}
						</p>
					</div>
					<div className="max-h-[60vh] space-y-3 overflow-y-auto p-3">
						{agents.length > 0 && (
							<>
								<section>
									<FilterSectionLabel>
										{t("conversationsPage.filters.agent")}
									</FilterSectionLabel>
									<div className="flex flex-col gap-0.5">
										{agents.map((a) => (
											<CheckListRow
												key={a.id}
												label={a.name}
												checked={filters.agentIds?.includes(a.id) ?? false}
												onChange={() =>
													toggle("agentIds", a.id, filters.agentIds)
												}
											/>
										))}
									</div>
								</section>
								<div className="border-t border-border/20" />
							</>
						)}

						<section>
							<FilterSectionLabel>
								{t("conversationsPage.filters.status")}
							</FilterSectionLabel>
							<div className="flex flex-col gap-0.5">
								{ALL_STATUSES.map((s) => (
									<CheckListRow
										key={s}
										label={CONVERSATION_STATUS_LABELS[s]}
										checked={filters.statuses?.includes(s) ?? false}
										onChange={() => toggle("statuses", s, filters.statuses)}
										dotClassName={CONVERSATION_STATUS_COLORS[s].replace(
											"text-",
											"bg-",
										)}
									/>
								))}
							</div>
						</section>

						<div className="border-t border-border/20" />

						<section>
							<FilterSectionLabel>
								{t("conversationsPage.filters.type")}
							</FilterSectionLabel>
							<div className="flex flex-col gap-0.5">
								{ALL_TRIGGER_TYPES.map((tt) => (
									<CheckListRow
										key={tt}
										label={CONVERSATION_TRIGGER_TYPE_LABELS[tt]}
										checked={filters.triggerTypes?.includes(tt) ?? false}
										onChange={() =>
											toggle("triggerTypes", tt, filters.triggerTypes)
										}
									/>
								))}
							</div>
						</section>

						<div className="border-t border-border/20" />

						<section>
							<FilterSectionLabel>
								{t("conversationsPage.filters.dateRange")}
							</FilterSectionLabel>
							<DateRangeFilter
								createdAfter={filters.createdAfter}
								createdBefore={filters.createdBefore}
								onChange={(range) => onFiltersChange({ ...filters, ...range })}
							/>
						</section>
					</div>
				</PopoverContent>
			</Popover>

			{hasAnyActive && (
				<button
					type="button"
					onClick={() => {
						setSearchInput("");
						onFiltersChange({});
					}}
					aria-label={t("conversationsPage.filters.clearAll")}
					title={t("conversationsPage.filters.clearAll")}
					className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground/60 transition-all duration-150 hover:bg-muted/60 hover:text-foreground"
				>
					<X className="size-3.5" />
				</button>
			)}
		</div>
	);
}
