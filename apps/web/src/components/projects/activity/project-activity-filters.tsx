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
import {
	ACTIVITY_ENTITY_TYPES,
	ACTIVITY_ORIGINS,
	type ProjectActivityFilters as Filters,
} from "@/lib/activity-api";
import type { ProjectMember } from "@/lib/project-api";
import { cn } from "@/lib/utils";

/** Adds value to list, or removes it when present; empty becomes undefined. */
function toggle<T>(list: T[] | undefined, value: T): T[] | undefined {
	const next = list?.includes(value)
		? list.filter((v) => v !== value)
		: [...(list ?? []), value];
	return next.length > 0 ? next : undefined;
}

export function memberDisplayName(m: ProjectMember): string {
	if (m.member_type === "agent") return m.agent_name || m.username;
	return m.full_name || m.username;
}

export interface ProjectActivityFiltersProps {
	filters: Filters;
	onFiltersChange: (next: Filters) => void;
	members: ProjectMember[];
}

export function ProjectActivityFilters({
	filters,
	onFiltersChange,
	members,
}: ProjectActivityFiltersProps) {
	const { t } = useTranslation("projects");
	const [searchInput, setSearchInput] = useState(filters.search ?? "");
	const [open, setOpen] = useState(false);

	const debouncedSetSearch = useDebouncedCallback((value: string) => {
		onFiltersChange({ ...filters, search: value || undefined });
	}, 300);

	// Keeps the input in sync when search is cleared from outside (the
	// clear-all button).
	useEffect(() => {
		setSearchInput(filters.search ?? "");
	}, [filters.search]);

	const activeCount =
		(filters.entityTypes?.length ? 1 : 0) +
		(filters.actorIds?.length ? 1 : 0) +
		(filters.origins?.length ? 1 : 0) +
		(filters.createdAfter || filters.createdBefore ? 1 : 0);
	const hasAnyActive = activeCount > 0 || !!filters.search;

	return (
		<div className="flex items-center gap-2">
			<div className="relative min-w-0 flex-1 sm:max-w-sm">
				<Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground/60" />
				<Input
					value={searchInput}
					onChange={(e) => {
						setSearchInput(e.target.value);
						debouncedSetSearch(e.target.value);
					}}
					placeholder={t("activityLog.filters.searchPlaceholder")}
					aria-label={t("activityLog.filters.searchPlaceholder")}
					className="h-8 pl-8 text-sm"
				/>
			</div>

			<Popover open={open} onOpenChange={setOpen}>
				<PopoverTrigger
					type="button"
					className={cn(
						"relative inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-colors",
						open || activeCount > 0
							? "border-primary/40 bg-primary/8 text-primary"
							: "border-border/50 text-muted-foreground hover:bg-muted/50 hover:text-foreground",
					)}
				>
					<SlidersHorizontal className="size-3.5" />
					{t("activityLog.filters.title")}
					{activeCount > 0 && (
						<span className="flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold leading-none text-primary-foreground">
							{activeCount}
						</span>
					)}
				</PopoverTrigger>
				<PopoverContent
					side="bottom"
					align="start"
					sideOffset={6}
					className="w-80 rounded-xl border border-border/40 p-0 shadow-xl"
				>
					<div className="max-h-[70vh] space-y-3 overflow-y-auto p-3">
						<section>
							<FilterSectionLabel>
								{t("activityLog.filters.type")}
							</FilterSectionLabel>
							<div className="grid grid-cols-2 gap-0.5">
								{ACTIVITY_ENTITY_TYPES.map((type) => (
									<CheckListRow
										key={type}
										label={t(`activityLog.entityTypes.${type}`)}
										checked={filters.entityTypes?.includes(type) ?? false}
										onChange={() =>
											onFiltersChange({
												...filters,
												entityTypes: toggle(filters.entityTypes, type),
											})
										}
									/>
								))}
							</div>
						</section>

						<div className="border-t border-border/20" />

						<section>
							<FilterSectionLabel>
								{t("activityLog.filters.people")}
							</FilterSectionLabel>
							<div className="flex max-h-44 flex-col gap-0.5 overflow-y-auto">
								{members.map((m) => (
									<CheckListRow
										key={m.id}
										label={memberDisplayName(m)}
										checked={filters.actorIds?.includes(m.id) ?? false}
										onChange={() =>
											onFiltersChange({
												...filters,
												actorIds: toggle(filters.actorIds, m.id),
											})
										}
									/>
								))}
							</div>
						</section>

						<div className="border-t border-border/20" />

						<section>
							<FilterSectionLabel>
								{t("activityLog.filters.source")}
							</FilterSectionLabel>
							<div className="grid grid-cols-2 gap-0.5">
								{ACTIVITY_ORIGINS.map((origin) => (
									<CheckListRow
										key={origin}
										label={t(`activityLog.origins.${origin}`)}
										checked={filters.origins?.includes(origin) ?? false}
										onChange={() =>
											onFiltersChange({
												...filters,
												origins: toggle(filters.origins, origin),
											})
										}
									/>
								))}
							</div>
						</section>

						<div className="border-t border-border/20" />

						<section>
							<FilterSectionLabel>
								{t("activityLog.filters.dateRange")}
							</FilterSectionLabel>
							<DateRangeFilter
								createdAfter={filters.createdAfter}
								createdBefore={filters.createdBefore}
								fromLabel={t("activityLog.filters.dateFrom")}
								toLabel={t("activityLog.filters.dateTo")}
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
					className="inline-flex h-8 shrink-0 items-center gap-1 rounded-md px-2 text-xs text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
				>
					<X className="size-3.5" />
					{t("activityLog.filters.clearAll")}
				</button>
			)}
		</div>
	);
}
