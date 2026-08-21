import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Link, Outlet, useParams } from "@tanstack/react-router";
import { Clock, Coins, MessageSquare, Plus } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useGlobalAgentRealtime } from "@/hooks/use-global-agent-realtime";
import { useProjectRealtime } from "@/hooks/use-project-realtime";
import {
	type Agent,
	type AgentConversation,
	agentsQueryOptions,
	CONVERSATION_STATUS_COLORS,
	CONVERSATION_STATUS_LABELS,
	type ConversationFilters as ConversationFiltersState,
	chattableAgentsQueryOptions,
	conversationsQueryOptions,
	globalConversationsQueryOptions,
} from "@/lib/agent-api";
import { formatCompactTokens, formatUsageCost } from "@/lib/format-usage";
import { resolveAgentAvatarUrl } from "@/lib/provider-logos";
import { cn } from "@/lib/utils";
import { ConversationFilters } from "./conversation-filters";

// Shared between the project-scoped Conversations page
// (routes/_authenticated/projects/$projectId/conversations.tsx) and the
// global one (routes/_authenticated/conversations.tsx) — same list +
// filters + outlet UI either way, branching only on where the data comes
// from. See conversation-view.tsx for the equivalent generalization of the
// detail pane rendered in the Outlet.

// ── List item ─────────────────────────────────────────────────────────────────

function ConversationListItem({
	conv,
	agent,
	projectId,
	isActive,
}: {
	conv: AgentConversation;
	agent: Agent | undefined;
	/** Absent for a global conversation (no project to scope the link to). */
	projectId?: string;
	isActive: boolean;
}) {
	const { t } = useTranslation("projects");
	const statusColor = CONVERSATION_STATUS_COLORS[conv.status];
	const statusLabel = CONVERSATION_STATUS_LABELS[conv.status];
	// A conversation with no human actor was fired by the automation-workflow
	// engine (see agent_service.go's TriggerTaskAssigned/TriggerDirectMessage:
	// triggeredByMemberID is nil for automation-triggered runs), not by
	// someone assigning a task or messaging the agent by hand. Global
	// conversations are never automation-triggered — they always carry
	// actor_user_id instead.
	const isAutomationTriggered =
		!conv.triggered_by_member_id &&
		!conv.actor_user_id &&
		(conv.trigger_type === "task_assigned" ||
			conv.trigger_type === "automation_message");
	const triggerLabel = isAutomationTriggered
		? t("conversationsPage.triggerAutomation")
		: conv.trigger_type === "chat_message"
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
	const avatarUrl = agent ? resolveAgentAvatarUrl(agent) : undefined;

	const href = projectId
		? `/projects/${projectId}/conversations/${conv.id}`
		: `/conversations/${conv.id}`;

	return (
		<Link
			to={href}
			className={cn(
				"flex w-full flex-col gap-1.5 rounded-lg border px-3 py-2.5 text-left transition-colors",
				isActive
					? "border-primary/40 bg-primary/5"
					: "border-transparent hover:border-border hover:bg-accent/30",
			)}
		>
			<div className="flex items-center gap-2 min-w-0">
				<Avatar className="size-6 rounded-md bg-primary/10 shrink-0">
					{avatarUrl ? <AvatarImage src={avatarUrl} /> : null}
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
				<span className="truncate">{triggerLabel}</span>
				{conv.total_tokens > 0 && (
					<>
						<span className="text-muted-foreground/40">·</span>
						<span
							className="flex items-center gap-1 shrink-0"
							title={t("conversationsPage.usageTitle", {
								tokens: conv.total_tokens.toLocaleString(),
								cost:
									conv.cost_usd != null ? formatUsageCost(conv.cost_usd) : "—",
							})}
						>
							<Coins className="size-3" />
							{formatCompactTokens(conv.total_tokens)}
							{conv.cost_usd != null && ` · ${formatUsageCost(conv.cost_usd)}`}
						</span>
					</>
				)}
				<span className="ml-auto shrink-0 flex items-center gap-1">
					<Clock className="size-3" />
					{new Date(conv.created_at).toLocaleDateString()}
				</span>
			</div>
		</Link>
	);
}

// ── Layout ────────────────────────────────────────────────────────────────────

export function ConversationsLayout({ projectId }: { projectId?: string }) {
	const { t } = useTranslation("projects");
	const { conversationId: activeConversationId } = useParams({
		strict: false,
	});

	useProjectRealtime(projectId);
	useGlobalAgentRealtime(!projectId);

	const [filters, setFilters] = useState<ConversationFiltersState>({});

	const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } =
		useInfiniteQuery(
			projectId
				? conversationsQueryOptions(projectId, filters)
				: globalConversationsQueryOptions(filters),
		);
	const { data: agents = [] } = useQuery(
		projectId ? agentsQueryOptions(projectId) : chattableAgentsQueryOptions,
	);
	const agentsById = new Map(agents.map((a) => [a.id, a]));

	// Pages are fetched newest-first (offset-paginated, ordered by created_at
	// DESC on the backend), so concatenating them in fetch order already
	// yields the correct display order — no client-side re-sort needed.
	const conversations = data?.pages.flatMap((page) => page.items) ?? [];
	const hasActiveFilters = Object.values(filters).some((v) =>
		Array.isArray(v) ? v.length > 0 : !!v,
	);

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

	const newConversationHref = projectId
		? `/projects/${projectId}/chats`
		: "/conversations";

	return (
		<div className="flex flex-1 min-h-0">
			<div className="w-80 shrink-0 border-r border-border/50 flex flex-col min-h-0">
				<div className="shrink-0 border-b border-border/50 px-4 py-3 flex items-center justify-between gap-2">
					<h2 className="text-sm font-semibold">
						{t("conversationsPage.title")}
					</h2>
					<Button
						size="sm"
						className="gap-1.5"
						nativeButton={false}
						render={<Link to={newConversationHref} />}
					>
						<Plus className="size-3.5" />
						{t("aiChat.newConversation")}
					</Button>
				</div>
				<ConversationFilters
					agents={agents}
					filters={filters}
					onFiltersChange={setFilters}
				/>
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
								{hasActiveFilters
									? t("conversationsPage.list.emptyFiltered.title")
									: t("conversationsPage.list.empty.title")}
							</p>
							<p className="text-xs text-muted-foreground max-w-xs">
								{hasActiveFilters
									? t("conversationsPage.list.emptyFiltered.description")
									: t("conversationsPage.list.empty.description")}
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
