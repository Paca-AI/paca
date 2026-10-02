import { Search, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

interface MembersFiltersProps {
	search: string;
	/** Project role names to offer as chips, with how many members hold each
	 *  (over the whole team, so chips don't shift as the search narrows). */
	roles: { name: string; count: number }[];
	totalCount: number;
	selectedRole: string;
	/** Members matching the active filters. */
	shownCount: number;
	onSearchChange: (search: string) => void;
	onRoleChange: (role: string) => void;
	onClear: () => void;
}

export function MembersFilters({
	search,
	roles,
	totalCount,
	selectedRole,
	shownCount,
	onSearchChange,
	onRoleChange,
	onClear,
}: MembersFiltersProps) {
	const { t } = useTranslation("projects");
	const isFiltering = search.trim() !== "" || selectedRole !== "";

	const chips = [
		{ name: "", label: t("team.filters.allRoles"), count: totalCount },
		// Roles nobody holds would only ever produce an empty result.
		...roles
			.filter((r) => r.count > 0 || r.name === selectedRole)
			.map((r) => ({ name: r.name, label: r.name, count: r.count })),
	];

	return (
		<div className="mb-4 flex flex-col gap-3">
			<div className="relative max-w-md">
				<Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
				<Input
					type="search"
					value={search}
					onChange={(e) => onSearchChange(e.target.value)}
					onKeyDown={(e) => {
						if (e.key === "Escape" && search) {
							e.preventDefault();
							onSearchChange("");
						}
					}}
					placeholder={t("team.filters.searchPlaceholder")}
					aria-label={t("team.filters.searchLabel")}
					className="pl-8 pr-8 [&::-webkit-search-cancel-button]:hidden"
				/>
				{search ? (
					<button
						type="button"
						onClick={() => onSearchChange("")}
						aria-label={t("team.filters.clearSearch")}
						className="absolute right-2 top-1/2 flex size-5 -translate-y-1/2 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
					>
						<X className="size-3.5" />
					</button>
				) : null}
			</div>
			{chips.length > 2 ? (
				<fieldset className="flex min-w-0 flex-wrap gap-1.5 border-0 p-0">
					<legend className="sr-only">{t("team.filters.roleLabel")}</legend>
					{chips.map((chip) => {
						const active = selectedRole === chip.name;
						return (
							<Button
								key={chip.name || "all"}
								type="button"
								size="sm"
								variant={active ? "default" : "outline"}
								aria-pressed={active}
								className="rounded-full"
								onClick={() => onRoleChange(chip.name)}
							>
								{chip.label}
								<span
									className={cn(
										"tabular-nums",
										active
											? "text-primary-foreground/70"
											: "text-muted-foreground",
									)}
								>
									{chip.count}
								</span>
							</Button>
						);
					})}
				</fieldset>
			) : null}
			{isFiltering ? (
				<div className="flex items-center gap-2 text-xs text-muted-foreground">
					<span aria-live="polite">
						{t("team.filters.showingOf", {
							shown: shownCount,
							total: totalCount,
						})}
					</span>
					<Button type="button" variant="ghost" size="xs" onClick={onClear}>
						{t("team.noMatch.clear")}
					</Button>
				</div>
			) : null}
		</div>
	);
}
