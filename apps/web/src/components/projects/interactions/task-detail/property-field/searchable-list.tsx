import { Check, Search } from "lucide-react";
import { Fragment, type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { filterSearchableItems } from "./helpers";

/** Lists with fewer entries than this render without a search box. */
export const SEARCH_THRESHOLD = 8;

export interface SearchableListItem {
	key: string;
	/** Plain-text label the search matches against. */
	label: string;
	/** Optional group heading; also matched by the search. Items sharing a
	 * group should be adjacent — a heading is emitted whenever it changes. */
	group?: string;
	selected?: boolean;
	onSelect: () => void;
	/** Row body (avatar / dot / label…); the check mark is added by the list. */
	content: ReactNode;
}

/** Search box above a height-capped scrolling list, shared by every picker in
 * the task panel. `pinned` rows (e.g. "Unassigned", "Auto") live inside the
 * scroll area so the search box stays the first focusable element; they are
 * hidden while a query is active. */
export function SearchableList({
	items,
	pinned,
	emptyLabel,
}: {
	items: SearchableListItem[];
	pinned?: ReactNode;
	emptyLabel?: string;
}) {
	const { t } = useTranslation("projects");
	const [query, setQuery] = useState("");
	const searchable = items.length >= SEARCH_THRESHOLD;
	const searching = searchable && query.trim() !== "";
	const visible = searching ? filterSearchableItems(items, query) : items;

	return (
		<div>
			{searchable && (
				<div className="relative px-1 pt-1 pb-1.5">
					<Search className="pointer-events-none absolute left-3.5 top-1/2 size-3.5 -translate-y-[calc(50%+0.0625rem)] text-muted-foreground/50" />
					<input
						type="text"
						// biome-ignore lint/a11y/noAutofocus: the search box is the point of opening the picker
						autoFocus
						value={query}
						onChange={(e) => setQuery(e.target.value)}
						onKeyDown={(e) => {
							if (e.key === "Enter") {
								e.preventDefault();
								// Never act on an already-selected row, so Enter can't
								// silently unassign / deselect.
								const first = visible[0];
								if (first && !first.selected) first.onSelect();
							}
						}}
						placeholder={t("taskDetail.searchableList.searchPlaceholder")}
						aria-label={t("taskDetail.searchableList.searchPlaceholder")}
						className="w-full rounded-lg border border-border/30 bg-muted/25 py-1.5 pr-2 pl-8 text-sm placeholder:text-muted-foreground/50 transition-all duration-150 focus:border-primary/40 focus:outline-none focus:ring-2 focus:ring-primary/20"
					/>
				</div>
			)}
			<div className="max-h-72 overflow-y-auto">
				{!searching && pinned}
				{visible.map((item, i) => (
					<Fragment key={item.key}>
						{item.group && item.group !== visible[i - 1]?.group && (
							<div className="px-3 pt-2 pb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground/60">
								{item.group}
							</div>
						)}
						<button
							type="button"
							className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100"
							onClick={item.onSelect}
						>
							{item.content}
							{item.selected && (
								<Check className="size-3.5 shrink-0 text-primary" />
							)}
						</button>
					</Fragment>
				))}
				{searching && visible.length === 0 && (
					<p className="px-3 py-3 text-center text-xs text-muted-foreground/60">
						{emptyLabel ?? t("taskDetail.searchableList.noResults")}
					</p>
				)}
			</div>
		</div>
	);
}
