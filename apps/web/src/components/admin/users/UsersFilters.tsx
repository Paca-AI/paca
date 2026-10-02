import { Loader2, Search, X } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";

export const USERS_SEARCH_DEBOUNCE_MS = 300;

// Select needs a non-empty value for its "no filter" item.
const ALL_ROLES = "__all__";

interface UsersFiltersProps {
	/** Global role names to offer in the role menu. Empty hides the menu. */
	roles: string[];
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
	roles,
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
							<Loader2
								className="size-3.5 animate-spin text-muted-foreground"
								aria-hidden
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
				{roles.length > 0 ? (
					<Select
						value={selectedRole || ALL_ROLES}
						onValueChange={(v) => {
							if (v != null) onRoleChange(v === ALL_ROLES ? "" : v);
						}}
						items={[
							{ value: ALL_ROLES, label: t("users.filters.allRoles") },
							...roles.map((name) => ({ value: name, label: name })),
						]}
					>
						<SelectTrigger
							className="w-full sm:w-48"
							aria-label={t("users.filters.roleLabel")}
						>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							<SelectItem value={ALL_ROLES}>
								{t("users.filters.allRoles")}
							</SelectItem>
							{roles.map((name) => (
								<SelectItem key={name} value={name}>
									{name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
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
