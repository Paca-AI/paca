import { Lock, ShieldAlert, Star } from "lucide-react";
import { useTranslation } from "react-i18next";

import { isFullAccessRole } from "@/components/admin/global-roles/utils";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import {
	Tooltip,
	TooltipContent,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import type { Policy } from "@/lib/policy";
import { cn } from "@/lib/utils";

/**
 * What a badge needs to know about a role. Role summaries (id and name, as
 * users, members and agents carry them) fit; richer data from the roles list
 * adds the description and the kind of role.
 */
export interface RoleBadgeData {
	id?: string;
	name: string;
	description?: string;
	is_system?: boolean;
	is_default?: boolean;
	/** The role holds the `*` wildcard. */
	full_access?: boolean;
}

/** A role as the roles list returns it, enough to enrich a summary. */
export interface RoleLike {
	id: string;
	name: string;
	description?: string;
	is_system?: boolean;
	is_default?: boolean;
	policy?: Policy;
}

export type RoleKind = "fullAccess" | "system" | "default" | "custom";

/** The strongest fact about a role decides its colour. */
export function roleKind(role: RoleBadgeData): RoleKind {
	if (role.full_access) return "fullAccess";
	if (role.is_system) return "system";
	if (role.is_default) return "default";
	return "custom";
}

const KIND_CLASS: Record<RoleKind, string> = {
	fullAccess:
		"border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-700/30 dark:bg-amber-900/20 dark:text-amber-400",
	system: "border-border bg-muted text-foreground/80",
	default: "border-primary/25 bg-primary/10 text-primary",
	custom: "border-border bg-background text-foreground/80",
};

const KIND_ICON = {
	fullAccess: ShieldAlert,
	system: Lock,
	default: Star,
	custom: null,
} as const;

/** One role's data from the roles list, ready for a badge. */
export function toRoleBadgeData(role: RoleLike): RoleBadgeData {
	return {
		id: role.id,
		name: role.name,
		description: role.description,
		is_system: role.is_system,
		is_default: role.is_default,
		full_access: role.policy
			? isFullAccessRole({ policy: role.policy })
			: undefined,
	};
}

/**
 * Role summaries (id and name) enriched with what the roles list knows about
 * them. A role the list doesn't know stays a plain, custom-looking badge.
 */
export function enrichRoles(
	summaries: { id: string; name: string }[],
	directory: RoleLike[] | undefined,
): RoleBadgeData[] {
	return summaries.map((summary) => {
		const known = directory?.find((r) => r.id === summary.id);
		return known ? toRoleBadgeData(known) : { ...summary };
	});
}

type TagKind = "fullAccess" | "system" | "default" | "current";

const TAG_CLASS: Record<TagKind, string> = {
	fullAccess: KIND_CLASS.fullAccess,
	system: "border-border bg-muted text-muted-foreground",
	default:
		"border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-700/30 dark:bg-amber-900/20 dark:text-amber-400",
	current: "border-transparent bg-muted text-muted-foreground",
};

/** A small labelled flag about a role: Default, Built-in, Full access, Current. */
export function RoleTag({
	kind,
	id,
	className,
}: {
	kind: TagKind;
	id?: string;
	className?: string;
}) {
	const { t } = useTranslation("shared");
	const Icon = kind === "current" ? null : KIND_ICON[kind];
	const label =
		kind === "fullAccess"
			? t("roleBadge.fullAccess")
			: kind === "system"
				? t("roleBadge.builtIn")
				: kind === "default"
					? t("roleBadge.default")
					: t("roleBadge.current");
	return (
		<span
			id={id}
			className={cn(
				"inline-flex shrink-0 items-center gap-1 rounded-full border px-1.5 py-0.5 text-[11px] font-medium leading-none",
				TAG_CLASS[kind],
				className,
			)}
		>
			{Icon ? (
				<Icon
					className={cn("size-2.5", kind === "default" && "fill-current")}
					aria-hidden="true"
				/>
			) : null}
			{label}
		</span>
	);
}

/** The flags that apply to a role, strongest first. */
export function roleTags(role: RoleBadgeData): TagKind[] {
	const tags: TagKind[] = [];
	if (role.full_access) tags.push("fullAccess");
	if (role.is_system) tags.push("system");
	if (role.is_default) tags.push("default");
	return tags;
}

interface RoleBadgeProps {
	role: RoleBadgeData;
	/** Hover details (description and kind). Off inside buttons and menus,
	 * where the surrounding control already explains itself. */
	interactive?: boolean;
	className?: string;
}

/**
 * A compact pill with the role's name, tinted by its kind (full access,
 * built-in, default, custom). A long name is truncated; hovering shows the
 * full name, the kind and the description.
 */
export function RoleBadge({
	role,
	interactive = true,
	className,
}: RoleBadgeProps) {
	const { t } = useTranslation("shared");
	const kind = roleKind(role);
	const Icon = KIND_ICON[kind];
	const pill = (
		<span
			data-kind={kind}
			className={cn(
				"inline-flex min-w-0 max-w-40 items-center gap-1 rounded-full border px-2 py-0.5 font-mono text-xs font-medium leading-none",
				KIND_CLASS[kind],
				className,
			)}
		>
			{Icon ? (
				<Icon
					className={cn(
						"size-3 shrink-0",
						kind === "default" && "fill-current",
					)}
					aria-hidden="true"
				/>
			) : null}
			<span className="truncate">{role.name}</span>
		</span>
	);
	if (!interactive) return pill;

	const tags = roleTags(role);
	return (
		<Tooltip>
			<TooltipTrigger
				render={
					<span
						className="inline-flex min-w-0 max-w-full shrink"
						data-slot="role-badge"
					/>
				}
			>
				{pill}
			</TooltipTrigger>
			<TooltipContent className="flex-col items-start gap-1">
				<span className="break-all font-mono font-semibold">{role.name}</span>
				{tags.length > 0 ? (
					<span className="opacity-80">
						{tags
							.map((tag) =>
								tag === "fullAccess"
									? t("roleBadge.fullAccess")
									: tag === "system"
										? t("roleBadge.builtIn")
										: t("roleBadge.default"),
							)
							.join(" · ")}
					</span>
				) : null}
				<span className={role.description ? undefined : "italic opacity-70"}>
					{role.description || t("roleBadge.noDescription")}
				</span>
			</TooltipContent>
		</Tooltip>
	);
}

interface RoleBadgeListProps {
	roles: RoleBadgeData[];
	/** How many badges to show before folding the rest into "+N". */
	max?: number;
	/** Tooltips on badges and a popover for the overflow. Turn off inside
	 * another button, where nesting controls would be invalid. */
	interactive?: boolean;
	/** Shown when there are no roles. */
	emptyLabel?: string;
	className?: string;
}

/**
 * A row of role badges: the first few, then "+N" that opens a popover
 * listing the rest.
 */
export function RoleBadgeList({
	roles,
	max = 2,
	interactive = true,
	emptyLabel,
	className,
}: RoleBadgeListProps) {
	const { t } = useTranslation("shared");
	if (roles.length === 0) {
		return (
			<span className="text-xs italic text-muted-foreground/60">
				{emptyLabel ?? t("roleBadge.noRoles")}
			</span>
		);
	}
	const shown = roles.slice(0, max);
	const hidden = roles.slice(max);
	const key = (r: RoleBadgeData, i: number) => r.id ?? `${r.name}-${i}`;

	return (
		<span
			className={cn(
				"inline-flex min-w-0 max-w-full items-center gap-1",
				className,
			)}
		>
			{shown.map((role, i) => (
				<RoleBadge key={key(role, i)} role={role} interactive={interactive} />
			))}
			{hidden.length > 0 ? (
				interactive ? (
					<Popover>
						<PopoverTrigger
							type="button"
							aria-label={t("roleBadge.showMore", { count: hidden.length })}
							className="inline-flex h-5 shrink-0 items-center rounded-full border border-dashed border-border px-1.5 text-xs font-medium leading-none text-muted-foreground transition-colors hover:border-primary/40 hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
						>
							+{hidden.length}
						</PopoverTrigger>
						<PopoverContent className="w-64 gap-2 p-3" align="start">
							<p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
								{t("roleBadge.allRoles", { count: roles.length })}
							</p>
							<ul className="flex flex-wrap gap-1.5">
								{roles.map((role, i) => (
									<li key={key(role, i)} className="flex min-w-0 max-w-full">
										<RoleBadge role={role} />
									</li>
								))}
							</ul>
						</PopoverContent>
					</Popover>
				) : (
					<span
						className="shrink-0 px-0.5 text-xs font-medium text-muted-foreground"
						title={hidden.map((r) => r.name).join(", ")}
					>
						+{hidden.length}
					</span>
				)
			) : null}
		</span>
	);
}
