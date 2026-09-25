import { Search, SlidersHorizontal, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	CheckListRow,
	DateRangeFilter,
	FilterSectionLabel,
} from "@/components/projects/activity/activity-filter-parts";
import { Input } from "@/components/ui/input";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import type {
	AgentActivityFilters as AgentActivityFiltersState,
	AgentActivitySourceType,
} from "@/lib/agent-api";
import { cn } from "@/lib/utils";

const ALL_SOURCE_TYPES: AgentActivitySourceType[] = ["task", "doc"];

// ── Filter bar ────────────────────────────────────────────────────────────────

export interface AgentActivityFiltersProps {
	filters: AgentActivityFiltersState;
	onFiltersChange: (next: AgentActivityFiltersState) => void;
}

export function AgentActivityFilters({
	filters,
	onFiltersChange,
}: AgentActivityFiltersProps) {
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
		((filters.sourceTypes?.length ?? 0) ? 1 : 0) +
		(filters.createdAfter || filters.createdBefore ? 1 : 0);
	const hasAnyActive = activeFilterCount > 0 || !!filters.search;

	const toggleSourceType = (value: AgentActivitySourceType) => {
		const current = filters.sourceTypes;
		const next = current?.includes(value)
			? current.filter((v) => v !== value)
			: [...(current ?? []), value];
		onFiltersChange({
			...filters,
			sourceTypes: next.length > 0 ? next : undefined,
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
					placeholder={t("agents.detail.activity.filters.searchPlaceholder")}
					className="h-7 pl-6 text-xs"
				/>
			</div>

			<Popover open={filtersOpen} onOpenChange={setFiltersOpen}>
				<PopoverTrigger
					type="button"
					aria-label={t("agents.detail.activity.filters.title")}
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
							{t("agents.detail.activity.filters.title")}
						</p>
					</div>
					<div className="max-h-[60vh] space-y-3 overflow-y-auto p-3">
						<section>
							<FilterSectionLabel>
								{t("agents.detail.activity.filters.type")}
							</FilterSectionLabel>
							<div className="flex flex-col gap-0.5">
								{ALL_SOURCE_TYPES.map((st) => (
									<CheckListRow
										key={st}
										label={t(`agents.detail.activity.sourceType.${st}`)}
										checked={filters.sourceTypes?.includes(st) ?? false}
										onChange={() => toggleSourceType(st)}
									/>
								))}
							</div>
						</section>

						<div className="border-t border-border/20" />

						<section>
							<FilterSectionLabel>
								{t("agents.detail.activity.filters.dateRange")}
							</FilterSectionLabel>
							<DateRangeFilter
								createdAfter={filters.createdAfter}
								createdBefore={filters.createdBefore}
								fromLabel={t("agents.detail.activity.filters.dateFrom")}
								toLabel={t("agents.detail.activity.filters.dateTo")}
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
					aria-label={t("agents.detail.activity.filters.clearAll")}
					title={t("agents.detail.activity.filters.clearAll")}
					className="flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground/60 transition-all duration-150 hover:bg-muted/60 hover:text-foreground"
				>
					<X className="size-3.5" />
				</button>
			)}
		</div>
	);
}
