import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
	Activity,
	BookOpen,
	Bot,
	CheckSquare,
	Cog,
	FolderKanban,
	Layers,
	LayoutGrid,
	type LucideIcon,
	Server,
	Shield,
	UserRound,
	Workflow,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	type ActivityDescription,
	activityEntityTitle,
	describeProjectActivity,
} from "@/components/projects/activity/describe-project-activity";
import {
	memberDisplayName,
	ProjectActivityFilters,
} from "@/components/projects/activity/project-activity-filters";
import type { ActivityNameMaps } from "@/components/projects/interactions/task-detail/activity-item";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Skeleton } from "@/components/ui/skeleton";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	type ActivityEntityType,
	type ProjectActivityFilters as Filters,
	type ProjectActivity,
	projectActivitiesQueryOptions,
} from "@/lib/activity-api";
import { isForbiddenError } from "@/lib/api-error";
import { sprintsQueryOptions } from "@/lib/interaction-api";
import {
	projectMembersQueryOptions,
	projectQueryOptions,
} from "@/lib/project-api";
import { timeAgo } from "@/lib/time-ago";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/activity/",
)({
	component: ProjectActivityPage,
});

const ENTITY_ICONS: Record<ActivityEntityType, LucideIcon> = {
	task: CheckSquare,
	doc: BookOpen,
	sprint: Layers,
	view: LayoutGrid,
	automation: Workflow,
	environment: Server,
	member: UserRound,
	role: Shield,
	agent: Bot,
	project: FolderKanban,
};

function ProjectActivityPage() {
	const { t } = useTranslation("projects");
	const { projectId } = Route.useParams();
	const { hasProjectPermission, isLoading: isPermissionsLoading } =
		useProjectPermissions(projectId);
	const canRead = hasProjectPermission("project.activities.read");
	const [filters, setFilters] = useState<Filters>({});

	const { data: project } = useQuery(projectQueryOptions(projectId));
	const { data: members = [] } = useQuery({
		...projectMembersQueryOptions(projectId),
		enabled: canRead,
	});
	const { data: sprints = [] } = useQuery({
		...sprintsQueryOptions(projectId),
		enabled: canRead,
	});
	const {
		data,
		isLoading: isDataLoading,
		isError,
		error,
		fetchNextPage,
		hasNextPage,
		isFetchingNextPage,
	} = useInfiniteQuery({
		...projectActivitiesQueryOptions(projectId, filters),
		enabled: canRead,
	});

	// Resolves member/sprint IDs inside task change entries to names.
	const names = useMemo<ActivityNameMaps>(() => {
		const m: Record<string, string> = {};
		for (const member of members) m[member.id] = memberDisplayName(member);
		const s: Record<string, string> = {};
		for (const sprint of sprints) s[sprint.id] = sprint.name;
		return { members: m, sprints: s };
	}, [members, sprints]);

	const items = data?.pages.flatMap((p) => p.items) ?? [];
	const groups = useGroupedByDay(items);
	const hasFilters = Object.values(filters).some((v) =>
		Array.isArray(v) ? v.length > 0 : !!v,
	);

	// While permissions load, canRead reads as false — don't flash the
	// no-permission state before the real answer arrives.
	const isLoading = isPermissionsLoading || (canRead && isDataLoading);
	const noPermission =
		!isPermissionsLoading && (!canRead || (isError && isForbiddenError(error)));

	const loadMoreRef = useRef<HTMLDivElement | null>(null);
	useEffect(() => {
		const target = loadMoreRef.current;
		if (!target) return;
		const observer = new IntersectionObserver(
			([entry]) => {
				if (entry?.isIntersecting && hasNextPage && !isFetchingNextPage) {
					void fetchNextPage();
				}
			},
			{ rootMargin: "200px" },
		);
		observer.observe(target);
		return () => observer.disconnect();
	}, [hasNextPage, isFetchingNextPage, fetchNextPage]);

	return (
		<div className="flex flex-col">
			<div className="relative overflow-hidden border-b border-border/50">
				<div
					className="pointer-events-none absolute inset-0 opacity-50"
					style={{
						backgroundImage:
							"radial-gradient(circle, color-mix(in oklch, var(--color-primary) 12%, transparent) 1px, transparent 1px)",
						backgroundSize: "20px 20px",
						maskImage:
							"radial-gradient(ellipse 70% 100% at 0% 0%, black 20%, transparent 70%)",
					}}
				/>
				<div className="relative px-6 py-8">
					<h1 className="font-[Syne] text-2xl font-bold tracking-tight">
						{t("activityLog.title")}
					</h1>
					<p className="mt-1 text-sm text-muted-foreground">
						{project?.name} · {t("activityLog.subtitle")}
					</p>
				</div>
			</div>

			<div className="mx-auto w-full max-w-4xl p-6">
				{noPermission ? (
					<NoPermissionState
						icon={Activity}
						title={t("activityLog.noPermission.title")}
						description={t("activityLog.noPermission.description")}
					/>
				) : (
					<>
						<ProjectActivityFilters
							filters={filters}
							onFiltersChange={setFilters}
							members={members}
						/>
						<div className="mt-5">
							{isLoading ? (
								<div className="space-y-2">
									{Array.from({ length: 8 }).map((_, i) => (
										// biome-ignore lint/suspicious/noArrayIndexKey: skeleton
										<Skeleton key={i} className="h-12 rounded-lg" />
									))}
								</div>
							) : items.length === 0 ? (
								<EmptyState filtered={hasFilters} />
							) : (
								<div className="space-y-6">
									{groups.map((group) => (
										<section key={group.key}>
											<h2 className="sticky top-0 z-10 bg-background/95 py-1.5 text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 backdrop-blur">
												{group.label}
											</h2>
											<ol className="mt-1 divide-y divide-border/30 rounded-xl border border-border/50">
												{group.items.map((item) => (
													<ActivityRow
														key={item.id}
														projectId={projectId}
														item={item}
														description={describeProjectActivity(
															item,
															names,
															t,
														)}
													/>
												))}
											</ol>
										</section>
									))}
									{hasNextPage && (
										<div ref={loadMoreRef} className="py-2">
											{isFetchingNextPage && (
												<Skeleton className="h-12 rounded-lg" />
											)}
										</div>
									)}
								</div>
							)}
						</div>
					</>
				)}
			</div>
		</div>
	);
}

function EmptyState({ filtered }: { filtered: boolean }) {
	const { t } = useTranslation("projects");
	const key = filtered ? "emptyFiltered" : "empty";
	return (
		<div className="flex flex-col items-center justify-center gap-3 py-20 text-center">
			<div className="flex size-14 items-center justify-center rounded-2xl bg-muted/50">
				<Activity className="size-7 text-muted-foreground/50" />
			</div>
			<p className="text-sm font-medium">{t(`activityLog.${key}.title`)}</p>
			<p className="max-w-xs text-xs text-muted-foreground">
				{t(`activityLog.${key}.description`)}
			</p>
		</div>
	);
}

// ── Row ───────────────────────────────────────────────────────────────────────

function ActivityRow({
	projectId,
	item,
	description,
}: {
	projectId: string;
	item: ProjectActivity;
	description: ActivityDescription;
}) {
	const { t } = useTranslation("projects");
	const { t: tCommon } = useTranslation("common");
	const Icon = ENTITY_ICONS[item.entity_type] ?? Cog;
	const actorName = item.actor_name || t(`activityLog.origins.${item.origin}`);
	const initials = actorName.slice(0, 2).toUpperCase();

	return (
		<li className="flex items-start gap-3 px-4 py-3">
			<Avatar className="mt-0.5 size-7 shrink-0">
				{item.actor_avatar_thumb_url && (
					<AvatarImage src={item.actor_avatar_thumb_url} alt="" />
				)}
				<AvatarFallback className="text-[10px]">
					{item.actor_id ? initials : <Cog className="size-3.5" />}
				</AvatarFallback>
			</Avatar>
			<div className="min-w-0 flex-1">
				<p className="text-sm leading-6">
					<span className="font-medium">{actorName}</span>{" "}
					<span className="text-foreground/75">{description.text}</span>{" "}
					<EntityChip projectId={projectId} item={item} Icon={Icon} />
				</p>
				{(description.detail || item.origin !== "user") && (
					<p className="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
						{description.detail && (
							<span className="truncate">{description.detail}</span>
						)}
						{item.origin !== "user" && item.actor_id && (
							<span className="rounded bg-muted/60 px-1.5 py-px text-[10px] font-medium uppercase tracking-wide">
								{t(`activityLog.origins.${item.origin}`)}
							</span>
						)}
					</p>
				)}
			</div>
			<time
				dateTime={item.created_at}
				title={new Date(item.created_at).toLocaleString()}
				className="shrink-0 pt-0.5 text-xs text-muted-foreground/60"
			>
				{timeAgo(item.created_at, tCommon)}
			</time>
		</li>
	);
}

/** The entity the entry is about, linked to its page while it still exists. */
function EntityChip({
	projectId,
	item,
	Icon,
}: {
	projectId: string;
	item: ProjectActivity;
	Icon: LucideIcon;
}) {
	const { t } = useTranslation("projects");
	const title = activityEntityTitle(item);
	if (!title) return null;
	const chipClass =
		"inline-flex max-w-full items-center gap-1 rounded-md px-1.5 align-middle text-sm font-medium";
	const inner = (
		<>
			<Icon className="size-3.5 shrink-0 opacity-70" />
			<span className="truncate">{title}</span>
		</>
	);
	const href =
		!item.entity_deleted && item.entity_id
			? entityHref(projectId, item)
			: undefined;
	if (!href) {
		return (
			<span
				className={`${chipClass} bg-muted/40 text-muted-foreground ${item.entity_deleted ? "line-through decoration-muted-foreground/40" : ""}`}
				title={item.entity_deleted ? t("activityLog.deletedEntity") : undefined}
			>
				{inner}
			</span>
		);
	}
	return (
		<Link
			to={href}
			className={`${chipClass} bg-primary/8 text-primary hover:bg-primary/15`}
		>
			{inner}
		</Link>
	);
}

function entityHref(
	projectId: string,
	item: ProjectActivity,
): string | undefined {
	const base = `/projects/${projectId}`;
	const id = item.entity_id;
	switch (item.entity_type) {
		case "task":
			return `${base}/tasks/${id}`;
		case "doc":
			return `${base}/docs/${id}`;
		case "sprint":
			return `${base}/interactions/sprints/${id}`;
		case "automation":
			return `${base}/automation/${id}`;
		case "environment":
			return `${base}/environments/${id}`;
		case "agent":
			return `${base}/agents/${id}`;
		case "member":
		case "role":
			return `${base}/team`;
		case "project":
			return `${base}/settings`;
		default:
			return undefined;
	}
}

// ── Day grouping ──────────────────────────────────────────────────────────────

interface DayGroup {
	key: string;
	label: string;
	items: ProjectActivity[];
}

function useGroupedByDay(items: ProjectActivity[]): DayGroup[] {
	const { t, i18n } = useTranslation("projects");
	return useMemo(() => {
		const dayKey = (d: Date) =>
			`${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
		const today = new Date();
		const yesterday = new Date(today);
		yesterday.setDate(today.getDate() - 1);
		const fmt = new Intl.DateTimeFormat(i18n.language, {
			weekday: "long",
			month: "long",
			day: "numeric",
			year: "numeric",
		});

		const groups: DayGroup[] = [];
		for (const item of items) {
			const d = new Date(item.created_at);
			const key = dayKey(d);
			let group = groups[groups.length - 1];
			if (!group || group.key !== key) {
				const label =
					key === dayKey(today)
						? t("activityLog.today")
						: key === dayKey(yesterday)
							? t("activityLog.yesterday")
							: fmt.format(d);
				group = { key, label, items: [] };
				groups.push(group);
			}
			group.items.push(item);
		}
		return groups;
	}, [items, t, i18n.language]);
}
