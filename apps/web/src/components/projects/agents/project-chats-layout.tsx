import { useInfiniteQuery } from "@tanstack/react-query";
import {
	Link,
	Outlet,
	useNavigate,
	useParams,
	useSearch,
} from "@tanstack/react-router";
import { Bot, Clock3, MessageSquare, Plus, Search } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
	type ProjectChatSessionSummary,
	projectChatSessionsQueryOptions,
} from "@/lib/agent-api";
import { shouldShowProjectChatMainOnMobile } from "@/lib/project-chat-navigation";
import { cn } from "@/lib/utils";

function SessionListItem({
	projectId,
	item,
	active,
}: {
	projectId: string;
	item: ProjectChatSessionSummary;
	active: boolean;
}) {
	const { t } = useTranslation("projects");
	const status = item.latest_turn?.status;
	return (
		<Link
			to="/projects/$projectId/chats/$sessionId"
			params={{ projectId, sessionId: item.session.id }}
			search={{ turnId: undefined }}
			className={cn(
				"flex w-full flex-col gap-1.5 rounded-lg border px-3 py-2.5 text-left transition-colors",
				active
					? "border-primary/40 bg-primary/5"
					: "border-transparent hover:border-border hover:bg-accent/30",
			)}
		>
			<div className="flex min-w-0 items-center gap-2">
				<Bot className="size-4 shrink-0 text-primary/70" />
				<span className="min-w-0 flex-1 truncate text-sm font-medium">
					{item.session.title || item.agent_name}
				</span>
				{status && (
					<span
						className={cn(
							"shrink-0 rounded-full border px-1.5 py-0.5 text-[10px] font-medium",
							status === "running" || status === "queued"
								? "border-blue-500/30 text-blue-600 dark:text-blue-300"
								: status === "succeeded"
									? "border-emerald-500/30 text-emerald-600 dark:text-emerald-300"
									: "border-border text-muted-foreground",
						)}
					>
						{t(`chats.status.${status}`)}
					</span>
				)}
			</div>
			<div className="flex items-center gap-1.5 pl-6 text-xs text-muted-foreground">
				<span className="truncate">@{item.agent_handle}</span>
				<span className="ml-auto inline-flex shrink-0 items-center gap-1">
					<Clock3 className="size-3" />
					{new Date(
						item.session.last_message_at ?? item.session.created_at,
					).toLocaleDateString()}
				</span>
			</div>
		</Link>
	);
}

export function ProjectChatsLayout({ projectId }: { projectId: string }) {
	const { t } = useTranslation("projects");
	const { sessionId } = useParams({ strict: false });
	const routeSearch = useSearch({ strict: false }) as {
		contextTaskId?: string;
		draft?: string;
	};
	const navigate = useNavigate();
	const [searchText, setSearchText] = useState("");
	const query = useInfiniteQuery(
		projectChatSessionsQueryOptions(projectId, { search: searchText }),
	);
	const sessions = query.data?.pages.flatMap((page) => page.items) ?? [];
	const scrollRef = useRef<HTMLDivElement | null>(null);
	const moreRef = useRef<HTMLDivElement | null>(null);
	const showMobileMain = shouldShowProjectChatMainOnMobile(
		sessionId,
		routeSearch,
	);

	useEffect(() => {
		const root = scrollRef.current;
		const target = moreRef.current;
		if (!root || !target) return;
		const observer = new IntersectionObserver(
			([entry]) => {
				if (
					entry?.isIntersecting &&
					query.hasNextPage &&
					!query.isFetchingNextPage
				) {
					void query.fetchNextPage();
				}
			},
			{ root, rootMargin: "160px" },
		);
		observer.observe(target);
		return () => observer.disconnect();
	}, [query.hasNextPage, query.isFetchingNextPage, query.fetchNextPage]);

	return (
		<div className="flex min-h-0 flex-1">
			<aside
				className={cn(
					"min-h-0 w-full flex-col border-r border-border/50 md:flex md:w-80 md:shrink-0",
					showMobileMain ? "hidden" : "flex",
				)}
			>
				<div className="flex shrink-0 items-center justify-between gap-2 border-b border-border/50 px-4 py-3">
					<h1 className="text-sm font-semibold">{t("chats.title")}</h1>
					<Button
						size="sm"
						className="gap-1.5"
						onClick={() =>
							void navigate({
								to: "/projects/$projectId/chats",
								params: { projectId },
								search: {
									contextTaskId: undefined,
									draft: crypto.randomUUID(),
									agentId: undefined,
								},
							})
						}
					>
						<Plus className="size-3.5" />
						{t("chats.new")}
					</Button>
				</div>
				<div className="relative shrink-0 px-3 py-2">
					<Search className="pointer-events-none absolute left-5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
					<Input
						type="search"
						value={searchText}
						onChange={(event) => setSearchText(event.target.value)}
						placeholder={t("chats.search")}
						className="h-8 pl-8 text-xs"
					/>
				</div>
				<div
					ref={scrollRef}
					className="min-h-0 flex-1 space-y-1.5 overflow-y-auto p-2"
				>
					{query.isLoading ? (
						Array.from({ length: 5 }).map((_, index) => (
							// biome-ignore lint/suspicious/noArrayIndexKey: deterministic skeleton rows
							<Skeleton key={index} className="h-16 rounded-lg" />
						))
					) : sessions.length === 0 ? (
						<div className="flex flex-col items-center gap-3 px-4 py-14 text-center text-muted-foreground">
							<MessageSquare className="size-8 opacity-40" />
							<p className="text-sm">{t("chats.empty")}</p>
							<p className="text-xs">{t("chats.emptyDescription")}</p>
						</div>
					) : (
						<>
							{sessions.map((item) => (
								<SessionListItem
									key={item.session.id}
									projectId={projectId}
									item={item}
									active={item.session.id === sessionId}
								/>
							))}
							{query.hasNextPage && (
								<div ref={moreRef}>
									{query.isFetchingNextPage && (
										<Skeleton className="h-16 rounded-lg" />
									)}
								</div>
							)}
						</>
					)}
				</div>
			</aside>

			<main
				className={cn(
					"min-h-0 flex-1",
					showMobileMain ? "block" : "hidden md:block",
				)}
			>
				<Outlet />
			</main>
		</div>
	);
}
