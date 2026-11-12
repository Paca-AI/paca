import { Search, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { SearchableList } from "@/components/projects/interactions/task-detail/property-field/searchable-list";
import { RemoteSelect } from "@/components/shared/remote-select";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { useRoleSearch } from "@/hooks/use-role-search";

export const USERS_SEARCH_DEBOUNCE_MS = 300;

interface UsersFiltersProps {
	/** Whether to offer the role menu (the viewer may read roles). Its options
	 *  are searched and paged on the server. */
	canFilterByRole: boolean;
	selectedRole: string;
	/** A request is in flight — shows a spinner in the search box. */
	isFetching: boolean;
	/** Number of users matching the active filters; shown with a clear
	 *  action only while a filter is active. */
	resultCount: number;
	isFiltering: boolean;
	onSearchChange: (search: string) => void;
	onRoleChange: (role: string) => void;
	onClear: () => void;
}

export function UsersFilters({
	canFilterByRole,
	selectedRole,
	isFetching,
	resultCount,
	isFiltering,
	onSearchChange,
	onRoleChange,
	onClear,
}: UsersFiltersProps) {
	const { t } = useTranslation("admin");
	// The input is uncontrolled by the page so typing never waits on a
	// request; only the debounced value is reported upward.
	const [value, setValue] = useState("");
	const debouncedSearch = useDebouncedCallback(
		onSearchChange,
		USERS_SEARCH_DEBOUNCE_MS,
	);

	const update = (next: string) => {
		setValue(next);
		debouncedSearch(next);
	};

	return (
		<div className="flex flex-col gap-2">
			<div className="flex flex-col gap-2 sm:flex-row">
				<div className="relative flex-1">
					<Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
					<Input
						type="search"
						value={value}
						onChange={(e) => update(e.target.value)}
						onKeyDown={(e) => {
							if (e.key === "Escape" && value) {
								e.preventDefault();
								update("");
							}
						}}
						placeholder={t("users.filters.searchPlaceholder")}
						aria-label={t("users.filters.searchLabel")}
						// Hide the browser's own clear glyph; ours is consistent
						// across browsers and also cancels the debounce wait.
						className="pl-8 pr-8 [&::-webkit-search-cancel-button]:hidden"
					/>
					<div className="absolute right-2 top-1/2 -translate-y-1/2">
						{isFetching ? (
							<Spinner
								aria-hidden="true"
								className="size-3.5 text-muted-foreground"
							/>
						) : value ? (
							<button
								type="button"
								onClick={() => update("")}
								aria-label={t("users.filters.clearSearch")}
								className="flex size-5 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
							>
								<X className="size-3.5" />
							</button>
						) : null}
					</div>
				</div>
				{canFilterByRole ? (
					<RoleFilterSelect
						selectedRole={selectedRole}
						onChange={onRoleChange}
					/>
				) : null}
			</div>
			{isFiltering ? (
				<div className="flex items-center justify-between text-xs text-muted-foreground">
					<span aria-live="polite">
						{t("users.filters.resultsCount", { count: resultCount })}
					</span>
					<Button
						type="button"
						variant="ghost"
						size="xs"
						onClick={() => {
							setValue("");
							onClear();
						}}
					>
						{t("users.filters.clearAll")}
					</Button>
				</div>
			) : null}
		</div>
	);
}

/** Role menu of the filter bar. The filter is by role name, so the selected
 *  value needs no lookup; the options are searched and paged as the menu opens. */
function RoleFilterSelect({
	selectedRole,
	onChange,
}: {
	selectedRole: string;
	onChange: (role: string) => void;
}) {
	const { t } = useTranslation("admin");
	const [open, setOpen] = useState(false);
	const search = useRoleSearch(undefined, open);
	return (
		<RemoteSelect
			open={open}
			onOpenChange={setOpen}
			label={selectedRole || t("users.filters.allRoles")}
			className="w-full sm:w-48"
			contentClassName="w-64"
		>
			<SearchableList
				remote={{
					query: search.query,
					onQueryChange: search.onQueryChange,
					hasMore: search.hasMore,
					isLoadingMore: search.isLoadingMore,
					isError: search.isLoadMoreError,
					onLoadMore: search.onLoadMore,
				}}
				pinned={
					<button
						type="button"
						className="flex w-full items-center gap-2.5 rounded-lg px-3 py-2 text-sm hover:bg-muted/60 transition-colors duration-100"
						onClick={() => {
							onChange("");
							setOpen(false);
						}}
					>
						{t("users.filters.allRoles")}
					</button>
				}
				items={search.roles.map((r) => ({
					key: r.id,
					label: r.name,
					selected: r.name === selectedRole,
					onSelect: () => {
						onChange(r.name);
						setOpen(false);
					},
					content: <span className="flex-1 truncate text-left">{r.name}</span>,
				}))}
			/>
		</RemoteSelect>
	);
}
