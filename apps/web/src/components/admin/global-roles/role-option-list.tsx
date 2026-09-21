import type { ReactNode } from "react";
import { useEffect, useId, useRef } from "react";
import { useTranslation } from "react-i18next";

import { PermissionSummary } from "@/components/admin/global-roles/permission-summary";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/** What the list needs to know about a role: global and project roles both fit. */
export interface RoleOption {
	id: string;
	name: string;
	permissions: Record<string, unknown>;
}

/** How many of a role's permissions a card lists before folding the rest into "+N". */
const PREVIEW_COUNT = 4;

export interface RoleOptionListProps<T extends RoleOption> {
	roles: T[];
	/**
	 * Id of the role the person picked. `null` means none picked yet: the
	 * current role (if any) shows as selected. When `none` is offered, `null`
	 * instead means the "no role" choice.
	 */
	value: string | null;
	onChange: (role: T) => void;
	/**
	 * Offers a "no role" choice above the roles, for holders that may have none
	 * (an agent's global role; a user always holds one).
	 */
	none?: { label: string; hint: string; onSelect: () => void };
	/**
	 * The role held today, marked "Current" and shown as selected until a
	 * different one is picked. Pass the id when you have it...
	 */
	currentRoleId?: string | null;
	/** ...or the name, for callers that only know that (the user API returns it). */
	currentRoleName?: string | null;
	/** Text of the mark on the current role. Defaults to "Current". */
	currentBadge?: string;
	disabled?: boolean;
	/** Accessible name of the group of choices. */
	label: string;
	/** Colors a permission's badge. Global permissions by default. */
	badgeClass?: (permission: string) => string;
}

/**
 * The roles as radio cards, each with a glance at what it grants, so a role is
 * chosen for what it allows rather than for its name alone. Purely a chooser:
 * it changes nothing until the caller acts on `onChange`.
 */
export function RoleOptionList<T extends RoleOption>({
	roles,
	value,
	onChange,
	none,
	currentRoleId,
	currentRoleName,
	currentBadge,
	disabled,
	label,
	badgeClass,
}: RoleOptionListProps<T>) {
	const { t } = useTranslation("admin");
	const groupName = useId();

	// A long list scrolls. The role that is selected on arrival (the current or
	// default one) must not sit out of sight below the fold, or nothing looks
	// selected: bring it into view once, when the list first shows.
	const groupRef = useRef<HTMLDivElement>(null);
	useEffect(() => {
		const checked =
			groupRef.current?.querySelector<HTMLElement>("input:checked");
		(checked?.closest("label") ?? checked)?.scrollIntoView?.({
			block: "nearest",
		});
	}, []);

	const isCurrent = (role: T) =>
		role.id === currentRoleId || role.name === currentRoleName;
	const checkedId = none ? value : (value ?? roles.find(isCurrent)?.id ?? null);

	return (
		<div
			ref={groupRef}
			role="radiogroup"
			aria-label={label}
			// The padding/negative margin leave room for the focus ring, which the
			// scroll container would otherwise clip.
			className="-m-1 flex max-h-72 flex-col gap-2 overflow-y-auto p-1"
		>
			{none ? (
				<label
					className={cn(
						"flex items-start gap-3 rounded-lg border p-3 transition-colors has-focus-visible:ring-2 has-focus-visible:ring-primary/30",
						checkedId === null
							? "border-primary/50 bg-primary/5"
							: "border-border/40 hover:bg-muted/30",
						disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer",
					)}
				>
					<input
						type="radio"
						name={groupName}
						value=""
						checked={checkedId === null}
						disabled={disabled}
						onChange={none.onSelect}
						aria-labelledby={`${groupName}-none-name`}
						aria-describedby={`${groupName}-none-hint`}
						className="mt-1 accent-primary"
					/>
					<div className="flex min-w-0 flex-1 flex-col gap-1.5">
						<span id={`${groupName}-none-name`} className="text-sm font-medium">
							{none.label}
						</span>
						<span
							id={`${groupName}-none-hint`}
							className="text-xs text-muted-foreground"
						>
							{none.hint}
						</span>
					</div>
				</label>
			) : null}
			{roles.map((role) => {
				const checked = role.id === checkedId;
				const current = isCurrent(role);
				// The radio is named by the role's name alone; "Current" and what the
				// role grants are its description, read after the name.
				const nameId = `${groupName}-${role.id}-name`;
				const currentId = `${groupName}-${role.id}-current`;
				const summaryId = `${groupName}-${role.id}-summary`;
				return (
					<label
						key={role.id}
						className={cn(
							"flex items-start gap-3 rounded-lg border p-3 transition-colors has-focus-visible:ring-2 has-focus-visible:ring-primary/30",
							checked
								? "border-primary/50 bg-primary/5"
								: "border-border/40 hover:bg-muted/30",
							disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer",
						)}
					>
						<input
							type="radio"
							name={groupName}
							value={role.id}
							checked={checked}
							disabled={disabled}
							onChange={() => onChange(role)}
							aria-labelledby={nameId}
							aria-describedby={
								current ? `${currentId} ${summaryId}` : summaryId
							}
							className="mt-1 accent-primary"
						/>
						<div className="flex min-w-0 flex-1 flex-col gap-1.5">
							<div className="flex items-center gap-2">
								<span id={nameId} className="font-mono text-sm font-medium">
									{role.name}
								</span>
								{current ? (
									<span
										id={currentId}
										className="rounded-full bg-muted px-1.5 py-0.5 text-[11px] font-medium leading-none text-muted-foreground"
									>
										{currentBadge ?? t("rolePicker.current")}
									</span>
								) : null}
							</div>
							<div id={summaryId}>
								<PermissionSummary
									role={role}
									limit={PREVIEW_COUNT}
									badgeClass={badgeClass}
								/>
							</div>
						</div>
					</label>
				);
			})}
		</div>
	);
}

/** The part of a query result the boundary reads. */
interface RolesQuery<T> {
	data: T[] | undefined;
	isPending: boolean;
	isError: boolean;
	isRefetching: boolean;
	refetch: () => Promise<unknown>;
}

/**
 * Shows what a roles query is doing — placeholders while it loads, a retry
 * when it fails, a note when there are none — and hands the roles to
 * `children` once there are some.
 */
export function RolesQueryBoundary<T>({
	query,
	children,
}: {
	query: RolesQuery<T>;
	children: (roles: T[]) => ReactNode;
}) {
	const { t } = useTranslation("admin");

	if (query.isPending) {
		return (
			<div className="flex flex-col gap-2" aria-hidden="true">
				<Skeleton className="h-16 rounded-lg" />
				<Skeleton className="h-16 rounded-lg" />
				<Skeleton className="h-16 rounded-lg" />
			</div>
		);
	}

	if (query.isError) {
		return (
			<div
				role="alert"
				className="flex items-center justify-between gap-3 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
			>
				<span>{t("rolePicker.loadFailed")}</span>
				<Button
					variant="outline"
					size="sm"
					onClick={() => void query.refetch()}
					disabled={query.isRefetching}
				>
					{t("rolePicker.retry")}
				</Button>
			</div>
		);
	}

	if (!query.data || query.data.length === 0) {
		return (
			<p className="rounded-lg border border-dashed px-3 py-4 text-center text-sm text-muted-foreground">
				{t("rolePicker.empty")}
			</p>
		);
	}

	return <>{children(query.data)}</>;
}
