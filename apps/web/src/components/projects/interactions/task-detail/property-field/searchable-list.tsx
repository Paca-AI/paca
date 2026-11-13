import { Check } from "lucide-react";
import { Fragment, type ReactNode, useState } from "react";
import { useTranslation } from "react-i18next";
import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";
import {
	Combobox,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
} from "@/components/ui/combobox";
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

/** Server-driven search and paging for a list too large to hold whole: the
 *  caller owns the query text and fetches matching items page by page, so the
 *  list neither filters locally nor hides the search box. */
export interface RemoteSearch {
	query: string;
	onQueryChange: (query: string) => void;
	hasMore: boolean;
	isLoadingMore: boolean;
	isError?: boolean;
	onLoadMore: () => void;
}

/** Search box above a height-capped scrolling list (shadcn `Combobox`, inline),
 * shared by every picker in the task panel. `pinned` rows (e.g. "Unassigned", "Auto") live inside the
 * scroll area so the search box stays the first focusable element; they are
 * hidden while a query is active. */
export function SearchableList({
	items,
	pinned,
	emptyLabel,
	remote,
}: {
	items: SearchableListItem[];
	pinned?: ReactNode;
	emptyLabel?: string;
	remote?: RemoteSearch;
}) {
	const { t } = useTranslation("projects");
	const [localQuery, setLocalQuery] = useState("");
	const query = remote ? remote.query : localQuery;
	const setQuery = remote ? remote.onQueryChange : setLocalQuery;
	const searchable = remote ? true : items.length >= SEARCH_THRESHOLD;
	const searching = searchable && query.trim() !== "";
	// With remote search the items already are the matches.
	const visible =
		searching && !remote ? filterSearchableItems(items, query) : items;

	const list = (
		<ComboboxList className="relative max-h-72 p-0">
			{!searching && pinned}
			{visible.map((item, i) => (
				<Fragment key={item.key}>
					{item.group && item.group !== visible[i - 1]?.group && (
						<div className="px-3 pt-2 pb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground/60">
							{item.group}
						</div>
					)}
					<ComboboxItem
						value={item}
						className="gap-2.5 rounded-lg px-3 py-2 pr-3"
					>
						{item.content}
						{item.selected && (
							<Check className="size-3.5 shrink-0 text-primary" />
						)}
					</ComboboxItem>
				</Fragment>
			))}
			{remote && (
				<LoadMoreSentinel
					hasMore={remote.hasMore}
					isLoadingMore={remote.isLoadingMore}
					isError={remote.isError}
					onLoadMore={remote.onLoadMore}
				/>
			)}
			{searching && visible.length === 0 && (
				<p className="px-3 py-3 text-center text-xs text-muted-foreground/60">
					{emptyLabel ?? t("taskDetail.searchableList.noResults")}
				</p>
			)}
		</ComboboxList>
	);

	return (
		<Combobox<SearchableListItem>
			inline
			// Filtering is done above (locally, or by the server in `remote` mode).
			filter={null}
			value={null}
			inputValue={query}
			onInputValueChange={(value) => setQuery(value)}
			itemToStringLabel={(item) => item.label}
			// Highlight the first match only once something is typed, so an Enter
			// right after opening can't pick the first row by accident.
			autoHighlight={searching}
			onValueChange={(item, details) => {
				if (!item) return;
				// Never act on an already-selected row from the keyboard, so Enter
				// can't silently unassign / deselect.
				if (details.event.type === "keydown" && item.selected) return;
				item.onSelect();
			}}
		>
			<div>
				{searchable && (
					<ComboboxInput
						autoFocus
						showTrigger={false}
						placeholder={t("taskDetail.searchableList.searchPlaceholder")}
						aria-label={t("taskDetail.searchableList.searchPlaceholder")}
						className="m-1"
					/>
				)}
				{list}
			</div>
		</Combobox>
	);
}
