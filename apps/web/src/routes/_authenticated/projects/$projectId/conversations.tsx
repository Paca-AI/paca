import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import {
	createFileRoute,
	Link,
	Outlet,
	useParams,
} from "@tanstack/react-router";
import { Clock, MessageSquare, Zap } from "lucide-react";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { useProjectRealtime } from "@/hooks/use-project-realtime";
import {
	type Agent,
	type AgentConversation,
	agentsQueryOptions,
	CONVERSATION_STATUS_COLORS,
	CONVERSATION_STATUS_LABELS,
	conversationsQueryOptions,
} from "@/lib/agent-api";
import { cn } from "@/lib/utils";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/conversations",
)({
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		await Promise.all([
			queryClient.ensureInfiniteQueryData(conversationsQueryOptions(projectId)),
			queryClient.ensureQueryData(agentsQueryOptions(projectId)),
		]);
	},
	component: ConversationsLayout,
});

// ── List item ─────────────────────────────────────────────────────────────────

function ConversationListItem({
	conv,
	agent,
	projectId,
	isActive,
}: {
	conv: AgentConversation;
	agent: Agent | undefined;
	projectId: string;
	isActive: boolean;
}) {
	const { t } = useTranslation("projects");
	const statusColor = CONVERSATION_STATUS_COLORS[conv.status];
	const statusLabel = CONVERSATION_STATUS_LABELS[conv.status];
	const triggerLabel =
		conv.trigger_type === "chat_message"
			? t("conversationsPage.triggerChat")
			: conv.trigger_type === "description_write"
				? t("conversationsPage.triggerWriteDescription")
				: t("conversationsPage.triggerTask");
	const initials = (agent?.name ?? "?")
		.split(" ")
		.map((w) => w[0])
		.join("")
		.toUpperCase()
		.slice(0, 2);

	return (
		<Link
			to="/projects/$projectId/conversations/$conversationId"
			params={{ projectId, conversationId: conv.id }}
			className={cn(
				"flex w-full flex-col gap-1.5 rounded-lg border px-3 py-2.5 text-left transition-colors",
				isActive
					? "border-primary/40 bg-primary/5"
					: "border-transparent hover:border-border hover:bg-accent/30",
			)}
		>
			<div className="flex items-center gap-2 min-w-0">
				<Avatar className="size-6 rounded-md bg-primary/10 shrink-0">
					<AvatarFallback className="rounded-md bg-primary/10 text-primary text-[10px] font-semibold">
						{initials}
					</AvatarFallback>
				</Avatar>
				<span className="text-sm font-medium truncate flex-1">
					{agent?.name ?? conv.agent_id.slice(0, 8)}
				</span>
				<Badge
					variant="outline"
					className={cn("text-[10px] font-semibold shrink-0", statusColor)}
				>
					{statusLabel}
				</Badge>
			</div>
			<div className="flex items-center gap-1.5 text-xs text-muted-foreground pl-8">
				<span className="flex items-center gap-1 shrink-0">
					<Zap className="size-3" />
					{t("conversationsPage.iterations", { count: conv.iteration_count })}
				</span>
				<span className="text-muted-foreground/40">·</span>
				<span className="truncate">{triggerLabel}</span>
				<span className="ml-auto shrink-0 flex items-center gap-1">
					<Clock className="size-3" />
					{new Date(conv.created_at).toLocaleDateString()}
				</span>
			</div>
		</Link>
	);
}

// ── Layout ────────────────────────────────────────────────────────────────────

function ConversationsLayout() {
	const { t } = useTranslation("projects");
	const { projectId } = Route.useParams();
	const { conversationId: activeConversationId } = useParams({
		strict: false,
	});

	useProjectRealtime(projectId);

	const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
		useInfiniteQuery(conversationsQueryOptions(projectId));
	const { data: agents = [] } = useQuery(agentsQueryOptions(projectId));
	const agentsById = new Map(agents.map((a) => [a.id, a]));

	// Pages are fetched newest-first (offset-paginated, ordered by created_at
	// DESC on the backend), so concatenating them in fetch order already
	// yields the correct display order — no client-side re-sort needed.
	const conversations = data?.pages.flatMap((page) => page.items) ?? [];

	const scrollContainerRef = useRef<HTMLDivElement | null>(null);
	const loadMoreRef = useRef<HTMLDivElement | null>(null);

	useEffect(() => {
		const container = scrollContainerRef.current;
		const target = loadMoreRef.current;
		if (!container || !target) return;

		const observer = new IntersectionObserver(
			([entry]) => {
				if (entry?.isIntersecting && hasNextPage && !isFetchingNextPage) {
					void fetchNextPage();
				}
			},
			{ root: container, rootMargin: "150px" },
		);
		observer.observe(target);
		return () => observer.disconnect();
	}, [hasNextPage, isFetchingNextPage, fetchNextPage]);

	return (
		<div className="flex flex-1 min-h-0">
			<div className="w-80 shrink-0 border-r border-border/50 flex flex-col min-h-0">
				<div className="shrink-0 border-b border-border/50 px-4 py-3">
					<h2 className="text-sm font-semibold">
						{t("conversationsPage.title")}
					</h2>
				</div>
				<div
					ref={scrollContainerRef}
					className="flex-1 overflow-y-auto p-2 space-y-1.5"
				>
					{isLoading ? (
						Array.from({ length: 4 }).map((_, i) => (
							// biome-ignore lint/suspicious/noArrayIndexKey: skeleton
							<Skeleton key={i} className="h-16 rounded-lg" />
						))
					) : conversations.length === 0 ? (
						<div className="flex flex-col items-center justify-center gap-3 py-14 px-3 text-center">
							<MessageSquare className="size-8 text-muted-foreground/40" />
							<p className="text-sm text-muted-foreground">
								{t("conversationsPage.list.empty.title")}
							</p>
							<p className="text-xs text-muted-foreground max-w-xs">
								{t("conversationsPage.list.empty.description")}
							</p>
						</div>
					) : (
						<>
							{conversations.map((conv) => (
								<ConversationListItem
									key={conv.id}
									conv={conv}
									agent={agentsById.get(conv.agent_id)}
									projectId={projectId}
									isActive={conv.id === activeConversationId}
								/>
							))}
							{hasNextPage && (
								<div ref={loadMoreRef}>
									{isFetchingNextPage && (
										<Skeleton className="h-16 rounded-lg" />
									)}
								</div>
							)}
						</>
					)}
				</div>
			</div>

			<div className="flex-1 min-h-0">
				<Outlet />
			</div>
		</div>
	);
}
