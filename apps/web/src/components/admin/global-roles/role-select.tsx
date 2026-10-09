import { Check, ChevronsUpDown, Search, X } from "lucide-react";
import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";

import { PermissionSummary } from "@/components/admin/global-roles/permission-summary";
import { isFullAccessRole } from "@/components/admin/global-roles/utils";
import {
	RoleBadgeList,
	RoleTag,
	roleTags,
	toRoleBadgeData,
} from "@/components/shared/role-badge";
import { Button } from "@/components/ui/button";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import type { Policy } from "@/lib/policy";
import { cn } from "@/lib/utils";

/** What the selector needs to know about a role: workspace and project roles both fit. */
export interface RoleOption {
	id: string;
	name: string;
	policy: Policy;
	description?: string;
	is_system?: boolean;
	/** Workspace roles only: the role new accounts start with, marked "Default". */
	is_default?: boolean;
}

/** How many of a role's permissions an option lists when it has no description. */
const PREVIEW_COUNT = 3;

export interface RoleSelectPanelProps<T extends RoleOption> {
	roles: T[];
	/** Ids of the roles picked. A holder may have several, or none. */
	values: string[];
	/** Called with the new set of picked role ids, and those roles. */
	onChange: (roleIds: string[], picked: T[]) => void;
	/** Ids of the roles held today, marked "Current". */
	currentRoleIds?: string[];
	disabled?: boolean;
	/** Accessible name of the list of choices. */
	label: string;
	/** Colors a permission's badge. Workspace permissions by default. */
	badgeClass?: (permission: string) => string;
	/** The fewest roles that must stay picked (1 for a project member). The
	 * last ones can't be unpicked, and Clear goes away. */
	minSelected?: number;
	/** Focus the search box on mount. */
	autoFocus?: boolean;
	className?: string;
}

/**
 * The roles as a searchable multi-select list: type to filter by name or
 * description, arrow keys to move, Enter or click to toggle. Each option
 * carries its description and flags (Current, Default, Built-in, Full
 * access). Purely a chooser: it changes nothing until the caller acts on
 * `onChange`.
 */
export function RoleSelectPanel<T extends RoleOption>({
	roles,
	values,
	onChange,
	currentRoleIds = [],
	disabled,
	label,
	badgeClass,
	minSelected = 0,
	autoFocus,
	className,
}: RoleSelectPanelProps<T>) {
	const { t } = useTranslation("shared");
	const baseId = useId();
	const listRef = useRef<HTMLDivElement>(null);
	const [query, setQuery] = useState("");
	const [active, setActive] = useState(0);

	const visible = useMemo(() => {
		const q = query.trim().toLowerCase();
		if (!q) return roles;
		return roles.filter(
			(r) =>
				r.name.toLowerCase().includes(q) ||
				(r.description ?? "").toLowerCase().includes(q),
		);
	}, [roles, query]);

	// Keep the highlighted option in range as the list narrows.
	const activeIndex = Math.min(active, Math.max(visible.length - 1, 0));
	const optionId = (id: string) => `${baseId}-opt-${id}`;
	const activeRole = visible[activeIndex];

	const activeId = activeRole ? optionId(activeRole.id) : undefined;
	useEffect(() => {
		if (activeId) {
			document.getElementById(activeId)?.scrollIntoView?.({ block: "nearest" });
		}
	}, [activeId]);

	const atFloor = values.length <= minSelected;

	const toggle = (role: T) => {
		if (disabled) return;
		const selected = values.includes(role.id);
		if (selected && atFloor) return;
		const next = selected
			? values.filter((v) => v !== role.id)
			: [...values, role.id];
		onChange(
			next,
			roles.filter((r) => next.includes(r.id)),
		);
	};

	const clear = () => {
		if (disabled || minSelected > 0) return;
		onChange([], []);
	};

	const move = (delta: number) => {
		if (visible.length === 0) return;
		setActive((activeIndex + delta + visible.length) % visible.length);
	};

	const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
		switch (e.key) {
			case "ArrowDown":
				e.preventDefault();
				move(1);
				break;
			case "ArrowUp":
				e.preventDefault();
				move(-1);
				break;
			case "Home":
				if (!query) {
					e.preventDefault();
					setActive(0);
				}
				break;
			case "End":
				if (!query) {
					e.preventDefault();
					setActive(Math.max(visible.length - 1, 0));
				}
				break;
			case "Enter":
				e.preventDefault();
				if (activeRole) toggle(activeRole);
				break;
			case "Escape":
				// First Escape clears the search; the next one closes the popover.
				if (query) {
					e.preventDefault();
					e.stopPropagation();
					setQuery("");
				}
				break;
		}
	};

	const listId = `${baseId}-list`;
	const hintId = `${baseId}-hint`;

	return (
		<div className={cn("flex min-w-0 flex-col gap-2", className)}>
			<div className="relative">
				<Search
					className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
					aria-hidden="true"
				/>
				<input
					// biome-ignore lint/a11y/noAutofocus: the search box is the first thing wanted when a selector opens
					autoFocus={autoFocus}
					type="text"
					role="combobox"
					aria-expanded="true"
					aria-controls={listId}
					aria-activedescendant={
						activeRole ? optionId(activeRole.id) : undefined
					}
					aria-autocomplete="list"
					aria-label={t("roleSelect.searchLabel")}
					aria-describedby={hintId}
					value={query}
					onChange={(e) => {
						setQuery(e.target.value);
						setActive(0);
					}}
					onKeyDown={onKeyDown}
					placeholder={t("roleSelect.searchPlaceholder")}
					autoComplete="off"
					spellCheck={false}
					className="h-8 w-full rounded-md border border-input bg-transparent pl-8 pr-8 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
				/>
				{query ? (
					<button
						type="button"
						onClick={() => setQuery("")}
						aria-label={t("roleSelect.clearSearch")}
						className="absolute right-1.5 top-1/2 flex size-5 -translate-y-1/2 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground"
					>
						<X className="size-3.5" aria-hidden="true" />
					</button>
				) : null}
			</div>

			<div
				ref={listRef}
				id={listId}
				role="listbox"
				aria-label={label}
				aria-multiselectable="true"
				className="-mx-1 flex max-h-64 flex-col gap-0.5 overflow-y-auto px-1"
			>
				{visible.length === 0 ? (
					<p
						role="status"
						className="px-2 py-6 text-center text-sm text-muted-foreground"
					>
						{t("roleSelect.noMatch", { query: query.trim() })}
					</p>
				) : null}
				{visible.map((role, index) => {
					const selected = values.includes(role.id);
					const current = currentRoleIds.includes(role.id);
					const locked = selected && atFloor && minSelected > 0;
					const data = toRoleBadgeData(role);
					const tags = roleTags(data);
					const nameId = `${baseId}-${role.id}-name`;
					const tagsId = `${baseId}-${role.id}-tags`;
					const descId = `${baseId}-${role.id}-desc`;
					const isActive = index === activeIndex;
					return (
						// biome-ignore lint/a11y/useKeyWithClickEvents: keyboard use goes through the search box (aria-activedescendant)
						<div
							key={role.id}
							id={optionId(role.id)}
							role="option"
							tabIndex={-1}
							aria-selected={selected}
							aria-disabled={disabled || locked || undefined}
							aria-labelledby={nameId}
							aria-describedby={[
								current || tags.length > 0 ? tagsId : null,
								descId,
							]
								.filter(Boolean)
								.join(" ")}
							data-active={isActive || undefined}
							onClick={() => toggle(role)}
							onMouseMove={() => setActive(index)}
							className={cn(
								"flex items-start gap-2.5 rounded-md px-2 py-2 text-sm transition-colors",
								isActive && "bg-accent",
								selected && !isActive && "bg-primary/5",
								disabled || locked
									? "cursor-not-allowed opacity-70"
									: "cursor-pointer",
							)}
						>
							<span
								aria-hidden="true"
								className={cn(
									"mt-0.5 flex size-4 shrink-0 items-center justify-center rounded border transition-colors",
									selected
										? "border-primary bg-primary text-primary-foreground"
										: "border-input bg-background",
								)}
							>
								{selected ? <Check className="size-3" strokeWidth={3} /> : null}
							</span>
							<div className="flex min-w-0 flex-1 flex-col gap-1">
								<div className="flex flex-wrap items-center gap-x-1.5 gap-y-1">
									<span
										id={nameId}
										className="min-w-0 truncate font-mono text-sm font-medium"
									>
										{role.name}
									</span>
									<span id={tagsId} className="contents">
										{current ? <RoleTag kind="current" /> : null}
										{tags.map((tag) => (
											<RoleTag key={tag} kind={tag} />
										))}
									</span>
								</div>
								<div id={descId} className="min-w-0">
									{role.description ? (
										<p className="line-clamp-2 text-xs text-muted-foreground">
											{role.description}
										</p>
									) : isFullAccessRole(role) ? null : (
										<PermissionSummary
											role={role}
											limit={PREVIEW_COUNT}
											badgeClass={badgeClass}
										/>
									)}
								</div>
							</div>
						</div>
					);
				})}
			</div>

			<div className="flex items-center justify-between gap-2 border-t pt-2 text-xs text-muted-foreground">
				<span id={hintId} aria-live="polite">
					{t("roleSelect.selectedCount", { count: values.length })}
					{minSelected > 0 && atFloor ? (
						<span className="ml-1.5">· {t("roleSelect.minOne")}</span>
					) : null}
				</span>
				{minSelected === 0 && values.length > 0 ? (
					<Button
						type="button"
						variant="ghost"
						size="xs"
						onClick={clear}
						disabled={disabled}
					>
						{t("roleSelect.clear")}
					</Button>
				) : null}
			</div>
		</div>
	);
}

export type RoleMultiSelectProps<T extends RoleOption> = Omit<
	RoleSelectPanelProps<T>,
	"autoFocus" | "className"
> & {
	/** Shown on the trigger when nothing is picked. */
	placeholder?: string;
};

/**
 * The roles chooser as a field: the picked roles show as badges, and the
 * field opens a searchable multi-select (see RoleSelectPanel) in a popover.
 */
export function RoleMultiSelect<T extends RoleOption>({
	placeholder,
	...panel
}: RoleMultiSelectProps<T>) {
	const { t } = useTranslation("shared");
	const [open, setOpen] = useState(false);
	const picked = panel.roles
		.filter((r) => panel.values.includes(r.id))
		.map(toRoleBadgeData);

	return (
		<Popover open={open} onOpenChange={setOpen}>
			<PopoverTrigger
				type="button"
				role="combobox"
				aria-haspopup="listbox"
				aria-expanded={open}
				aria-label={panel.label}
				disabled={panel.disabled}
				className="flex min-h-9 w-full items-center justify-between gap-2 rounded-lg border border-input bg-transparent px-2.5 py-1.5 text-left text-sm transition-colors hover:bg-muted/40 focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none disabled:cursor-not-allowed disabled:opacity-60"
			>
				<span className="min-w-0 flex-1">
					{picked.length > 0 ? (
						<RoleBadgeList roles={picked} max={3} interactive={false} />
					) : (
						<span className="text-muted-foreground">
							{placeholder ?? t("roleSelect.placeholder")}
						</span>
					)}
				</span>
				<ChevronsUpDown
					className="size-3.5 shrink-0 text-muted-foreground"
					aria-hidden="true"
				/>
			</PopoverTrigger>
			<PopoverContent
				align="start"
				className="w-(--anchor-width) min-w-72 p-2.5"
				initialFocus={false}
			>
				<RoleSelectPanel {...panel} autoFocus />
			</PopoverContent>
		</Popover>
	);
}
