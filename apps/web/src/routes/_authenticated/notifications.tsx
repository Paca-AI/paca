import {
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Bell } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { openNotificationTarget } from "@/components/app-shell/notification-bell";
import {
	groupNotifications,
	NotificationCard,
} from "@/components/app-shell/notification-card";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
	filteredNotificationsQueryOptions,
	filterForTab,
	markAllNotificationsAsRead,
	markNotificationAsRead,
	type Notification,
	type NotificationTab,
} from "@/lib/notification-api";
import { projectsQueryOptions } from "@/lib/project-api";

export const Route = createFileRoute("/_authenticated/notifications")({
	component: NotificationsPage,
});

const TABS: NotificationTab[] = ["all", "mentions", "assigned", "agents"];

/** The full list behind the bell: filters by kind and project, mentions with
 *  the project, sprint and excerpt they were made in. */
function NotificationsPage() {
	const { t } = useTranslation("appShell");
	const navigate = useNavigate();
	const queryClient = useQueryClient();
	const [tab, setTab] = useState<NotificationTab>("all");
	const [projectId, setProjectId] = useState("");

	const { data: projects } = useQuery(projectsQueryOptions());
	const { data, fetchNextPage, hasNextPage, isFetchingNextPage, isPending } =
		useInfiniteQuery(
			filteredNotificationsQueryOptions(
				filterForTab(tab, projectId || undefined),
			),
		);

	const groups = useMemo(
		() => groupNotifications(data?.pages.flatMap((p) => p.items) ?? []),
		[data],
	);
	const unreadCount = data?.pages[0]?.unread_count ?? 0;

	const invalidate = () =>
		queryClient.invalidateQueries({ queryKey: ["notifications"] });
	const { mutate: markRead } = useMutation({
		mutationFn: markNotificationAsRead,
		onSuccess: invalidate,
	});
	const { mutate: markAllRead } = useMutation({
		mutationFn: markAllNotificationsAsRead,
		onSuccess: invalidate,
	});

	function open(n: Notification, siblings: Notification[]) {
		for (const x of siblings) if (!x.read_at) markRead(x.id);
		openNotificationTarget(navigate, n);
	}

	return (
		<div className="mx-auto flex w-full max-w-3xl flex-col gap-4 p-4 md:p-6">
			<header className="flex flex-wrap items-center gap-3">
				<h1 className="text-xl font-semibold">{t("notifications.title")}</h1>
				{unreadCount > 0 && (
					<Button
						variant="ghost"
						size="sm"
						className="ml-auto"
						onClick={() => markAllRead()}
					>
						{t("notifications.markAllAsRead")}
					</Button>
				)}
			</header>

			<div className="flex flex-wrap items-center gap-3">
				<ToggleGroup
					aria-label={t("notifications.page.filterLabel")}
					variant="outline"
					size="sm"
					spacing={0}
					value={[tab]}
					onValueChange={(next) => {
						const v = next[0] as NotificationTab | undefined;
						if (v && TABS.includes(v)) setTab(v);
					}}
				>
					{TABS.map((value) => (
						<ToggleGroupItem key={value} value={value} className="px-3 text-xs">
							{t(`notifications.page.tabs.${value}`)}
						</ToggleGroupItem>
					))}
				</ToggleGroup>
				<select
					aria-label={t("notifications.page.projectFilter")}
					value={projectId}
					onChange={(e) => setProjectId(e.target.value)}
					className="ml-auto h-8 max-w-48 rounded-md border border-input bg-background px-2 text-xs"
				>
					<option value="">{t("notifications.page.allProjects")}</option>
					{projects?.items.map((p) => (
						<option key={p.id} value={p.id}>
							{p.name}
						</option>
					))}
				</select>
			</div>

			<div className="overflow-hidden rounded-xl border border-border/60 bg-card">
				{isPending ? (
					<div className="flex justify-center py-10">
						<Spinner aria-hidden="true" />
					</div>
				) : groups.length === 0 ? (
					<div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
						<Bell className="mb-2 h-8 w-8 opacity-30" />
						<p className="text-sm">{t("notifications.empty")}</p>
					</div>
				) : (
					<ul className="divide-y">
						{groups.map((g) => (
							<li key={g.head.id}>
								<NotificationCard
									roomy
									group={g}
									onOpen={(n) => open(n, g.items)}
								/>
							</li>
						))}
					</ul>
				)}
			</div>

			{hasNextPage && (
				<Button
					variant="outline"
					size="sm"
					className="self-center"
					disabled={isFetchingNextPage}
					onClick={() => void fetchNextPage()}
				>
					{isFetchingNextPage
						? t("notifications.loadingMore")
						: t("notifications.page.loadMore")}
				</Button>
			)}
		</div>
	);
}
