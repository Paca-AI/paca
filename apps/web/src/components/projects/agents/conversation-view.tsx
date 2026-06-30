import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	Bot,
	ChevronDown,
	ChevronRight,
	GitBranch,
	GitPullRequest,
	Loader2,
	Square,
	Terminal,
	User,
	Zap,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import {
	type AgentConversation,
	type AgentConversationEvent,
	CONVERSATION_STATUS_COLORS,
	conversationEventsQueryOptions,
	conversationQueryOptions,
	stopConversation,
} from "@/lib/agent-api";
import { cn } from "@/lib/utils";

// ── Message extraction ────────────────────────────────────────────────────────

interface ChatMessage {
	id: string;
	role: "agent" | "user" | "system";
	text: string;
	eventType: string;
	isToolCall?: boolean;
	toolName?: string;
	eventIndex: number;
	createdAt: string;
}

const THINKING_TYPES = new Set([
	"ThinkingEvent",
	"agent.thinkingevent",
	"agent.thinking",
	"thinking",
]);

// Extract plain text from a content block array [{type:"text", text:"..."}] or a bare string.
function extractContentText(content: unknown): string | null {
	if (Array.isArray(content)) {
		const parts = (content as Array<unknown>)
			.map((c) => {
				if (typeof c === "object" && c !== null && "text" in c) {
					const t = (c as { text: unknown }).text;
					return typeof t === "string" ? t : null;
				}
				return null;
			})
			.filter((t): t is string => t !== null && t.trim().length > 0);
		return parts.length > 0 ? parts.join("\n\n").trim() : null;
	}
	if (typeof content === "string" && content.trim().length > 0) {
		return content.trim();
	}
	return null;
}

// Returns 0, 1, or 2 messages for one event.
// ActionEvent may emit a thought bubble + a tool-call row.
function eventToChatMessages(ev: AgentConversationEvent): ChatMessage[] {
	const p = ev.payload;
	const t = ev.event_type;

	// Skip events that carry no user-visible content.
	if (
		t === "ConversationStateUpdateEvent" ||
		t === "SystemPromptEvent" ||
		t === "StreamingDeltaEvent"
	) {
		return [];
	}

	// ── User / agent text messages ─────────────────────────────────────────────
	if (t === "MessageEvent") {
		const llmMsg = p.llm_message as { content?: unknown } | undefined;
		const text =
			extractContentText(llmMsg?.content) ?? extractContentText(p.content);
		if (!text) return [];
		const role =
			ev.event_source === "user"
				? "user"
				: ev.event_source === "system"
					? "system"
					: "agent";
		return [
			{
				id: ev.id,
				role,
				text,
				eventType: t,
				isToolCall: false,
				eventIndex: ev.event_index,
				createdAt: ev.created_at,
			},
		];
	}

	// ── Agent action (tool call + optional visible thought) ────────────────────
	if (t === "ActionEvent") {
		const messages: ChatMessage[] = [];

		// The "thought" array contains what the agent says before the tool call.
		const thoughtText = extractContentText(p.thought);
		if (thoughtText) {
			messages.push({
				id: `${ev.id}-thought`,
				role: "agent",
				text: thoughtText,
				eventType: "MessageEvent",
				isToolCall: false,
				eventIndex: ev.event_index,
				createdAt: ev.created_at,
			});
		}

		// Tool-call row (always shown, even when thought is empty).
		const toolCall = p.tool_call as
			| { name?: string; arguments?: unknown }
			| undefined;
		const toolName =
			toolCall?.name ??
			(typeof p.tool_name === "string" ? p.tool_name : undefined);
		const expandedText =
			(typeof toolCall?.arguments === "string" ? toolCall.arguments : null) ??
			JSON.stringify(toolCall, null, 2) ??
			(typeof p.summary === "string" ? p.summary : null) ??
			toolName ??
			t;

		messages.push({
			id: ev.id,
			role: "agent",
			text: expandedText,
			eventType: t,
			isToolCall: true,
			toolName,
			eventIndex: ev.event_index,
			createdAt: ev.created_at,
		});

		return messages;
	}

	// ── Tool observation ───────────────────────────────────────────────────────
	if (t === "ObservationEvent") {
		const obs = p.observation as Record<string, unknown> | undefined;
		let text: string | null = null;
		if (obs) {
			text =
				extractContentText(obs.content) ??
				(typeof obs.message === "string" ? obs.message : null);
		}
		if (!text) {
			text =
				extractContentText(p.content) ??
				(typeof p.message === "string" ? p.message : null) ??
				(typeof p.output === "string" ? p.output : null);
		}
		if (!text) return [];
		return [
			{
				id: ev.id,
				role: "agent",
				text,
				eventType: "agent.observation",
				isToolCall: true,
				toolName: typeof p.tool_name === "string" ? p.tool_name : undefined,
				eventIndex: ev.event_index,
				createdAt: ev.created_at,
			},
		];
	}

	// ── Fallback for other / legacy event types ────────────────────────────────
	const text =
		extractContentText(p.content) ??
		extractContentText(p.thought) ??
		(typeof p.message === "string" ? p.message : null) ??
		(p.tool_call ? JSON.stringify(p.tool_call, null, 2) : null) ??
		(p.args ? JSON.stringify(p.args, null, 2) : null) ??
		(typeof p.output === "string" ? p.output : null);
	if (!text) return [];

	const role =
		ev.event_source === "user"
			? "user"
			: ev.event_source === "system"
				? "system"
				: "agent";
	const isObservation =
		t === "ObservationEvent" ||
		t === "agent.observation" ||
		t === "observation";
	const isToolCall =
		t === "ActionEvent" ||
		t === "agent.action" ||
		t === "action" ||
		isObservation;
	const toolCallObj = p.tool_call as { name?: string } | undefined;
	const toolName =
		toolCallObj?.name ??
		(typeof p.action === "string" ? p.action : undefined) ??
		(typeof p.tool === "string" ? p.tool : undefined) ??
		(typeof p.tool_name === "string" ? p.tool_name : undefined);

	return [
		{
			id: ev.id,
			role,
			text,
			eventType: isObservation ? "agent.observation" : t,
			isToolCall,
			toolName,
			eventIndex: ev.event_index,
			createdAt: ev.created_at,
		},
	];
}

// ── Sub-components ────────────────────────────────────────────────────────────

function ToolCallMessage({ msg }: { msg: ChatMessage }) {
	const [expanded, setExpanded] = useState(false);
	const isObservation =
		msg.eventType === "agent.observation" || msg.eventType === "observation";

	return (
		<div className="flex gap-3 my-1">
			<div className="flex size-6 shrink-0 items-center justify-center rounded-md bg-muted/60 text-muted-foreground mt-0.5">
				<Terminal className="size-3" />
			</div>
			<div className="flex-1 min-w-0">
				<button
					type="button"
					onClick={() => setExpanded((p) => !p)}
					className="flex items-center gap-1.5 text-xs text-muted-foreground/70 hover:text-foreground/70 transition-colors"
				>
					{expanded ? (
						<ChevronDown className="size-3" />
					) : (
						<ChevronRight className="size-3" />
					)}
					<span className="font-mono">
						{isObservation
							? "observation"
							: msg.toolName
								? `tool: ${msg.toolName}`
								: msg.eventType}
					</span>
				</button>
				{expanded && (
					<pre className="mt-1.5 rounded-lg border border-border/40 bg-muted/30 px-3 py-2 text-xs font-mono text-muted-foreground/80 overflow-x-auto whitespace-pre-wrap wrap-break-word">
						{msg.text}
					</pre>
				)}
			</div>
		</div>
	);
}

function ThinkingMessage({ msg }: { msg: ChatMessage }) {
	const { t } = useTranslation();
	const [expanded, setExpanded] = useState(false);

	return (
		<div className="flex gap-3 my-1">
			<div className="flex size-6 shrink-0 items-center justify-center rounded-md bg-primary/5 text-primary/40 mt-0.5">
				<Zap className="size-3" />
			</div>
			<div className="flex-1 min-w-0">
				<button
					type="button"
					onClick={() => setExpanded((p) => !p)}
					className="flex items-center gap-1.5 text-xs text-muted-foreground/60 italic hover:text-foreground/60 transition-colors"
				>
					{expanded ? (
						<ChevronDown className="size-3" />
					) : (
						<ChevronRight className="size-3" />
					)}
					{t("project.agents.thinking")}
				</button>
				{expanded && (
					<p className="mt-1.5 text-xs text-muted-foreground/60 italic leading-relaxed border-l-2 border-border/30 pl-3">
						{msg.text}
					</p>
				)}
			</div>
		</div>
	);
}

function AgentBubble({ msg }: { msg: ChatMessage }) {
	return (
		<div className="flex gap-3">
			<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary mt-0.5">
				<Bot className="size-4" />
			</div>
			<div className="flex-1 min-w-0 max-w-[85%]">
				<div className="rounded-2xl rounded-tl-md bg-card border border-border/40 px-4 py-3 shadow-sm">
					<p className="text-sm leading-relaxed text-foreground whitespace-pre-wrap wrap-break-word">
						{msg.text}
					</p>
				</div>
				<p className="text-xs text-muted-foreground/40 mt-1 pl-1">
					{new Date(msg.createdAt).toLocaleTimeString([], {
						hour: "2-digit",
						minute: "2-digit",
					})}
				</p>
			</div>
		</div>
	);
}

function UserBubble({ msg }: { msg: ChatMessage }) {
	return (
		<div className="flex gap-3 flex-row-reverse">
			<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground mt-0.5">
				<User className="size-4" />
			</div>
			<div className="flex-1 min-w-0 max-w-[85%] flex flex-col items-end">
				<div className="rounded-2xl rounded-tr-md bg-primary text-primary-foreground px-4 py-3 shadow-sm">
					<p className="text-sm leading-relaxed whitespace-pre-wrap wrap-break-word">
						{msg.text}
					</p>
				</div>
				<p className="text-xs text-muted-foreground/40 mt-1 pr-1">
					{new Date(msg.createdAt).toLocaleTimeString([], {
						hour: "2-digit",
						minute: "2-digit",
					})}
				</p>
			</div>
		</div>
	);
}

function SystemNote({ msg }: { msg: ChatMessage }) {
	return (
		<div className="flex justify-center">
			<span className="text-xs text-muted-foreground/50 bg-muted/30 rounded-full px-3 py-0.5">
				{msg.text}
			</span>
		</div>
	);
}

function MessageItem({ msg }: { msg: ChatMessage }) {
	if (msg.isToolCall) return <ToolCallMessage msg={msg} />;
	if (THINKING_TYPES.has(msg.eventType)) return <ThinkingMessage msg={msg} />;
	if (msg.role === "user") return <UserBubble msg={msg} />;
	if (msg.role === "system") return <SystemNote msg={msg} />;
	return <AgentBubble msg={msg} />;
}

function TypingIndicator() {
	return (
		<div className="flex gap-3">
			<div className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
				<Bot className="size-4" />
			</div>
			<div className="rounded-2xl rounded-tl-md bg-card border border-border/40 px-4 py-3 shadow-sm">
				<div className="flex items-center gap-1">
					<span className="size-1.5 rounded-full bg-muted-foreground/40 animate-bounce [animation-delay:0ms]" />
					<span className="size-1.5 rounded-full bg-muted-foreground/40 animate-bounce [animation-delay:150ms]" />
					<span className="size-1.5 rounded-full bg-muted-foreground/40 animate-bounce [animation-delay:300ms]" />
				</div>
			</div>
		</div>
	);
}

// ── Controls ──────────────────────────────────────────────────────────────────

function ConversationControls({
	projectId,
	conversation,
}: {
	projectId: string;
	conversation: AgentConversation;
}) {
	const { t } = useTranslation();
	const qc = useQueryClient();

	const invalidate = () => {
		qc.invalidateQueries({
			queryKey: ["projects", projectId, "conversations", conversation.id],
		});
		qc.invalidateQueries({
			queryKey: ["projects", projectId, "conversations"],
		});
	};

	const stopMut = useMutation({
		mutationFn: () => stopConversation(projectId, conversation.id),
		onSuccess: invalidate,
	});

	const isRunning = conversation.status === "running";

	if (!isRunning) return null;

	return (
		<div className="flex items-center gap-2">
			<Button
				size="sm"
				variant="outline"
				className="h-7 text-xs gap-1.5 text-destructive border-destructive/30 hover:bg-destructive/10"
				onClick={() => stopMut.mutate()}
				disabled={stopMut.isPending}
			>
				{stopMut.isPending ? (
					<Loader2 className="size-3 animate-spin" />
				) : (
					<Square className="size-3" />
				)}
				{t("project.agents.stop")}
			</Button>
		</div>
	);
}

// ── Main component ────────────────────────────────────────────────────────────

interface ConversationViewProps {
	projectId: string;
	conversationId: string;
}

export function ConversationView({
	projectId,
	conversationId,
}: ConversationViewProps) {
	const { t } = useTranslation();
	const scrollRef = useRef<HTMLDivElement>(null);

	const { data: conversation, isLoading: convLoading } = useQuery(
		conversationQueryOptions(projectId, conversationId),
	);
	const { data: events = [], isLoading: eventsLoading } = useQuery(
		conversationEventsQueryOptions(projectId, conversationId),
	);

	const messages: ChatMessage[] = events.flatMap(eventToChatMessages);

	// Scroll to bottom when new messages arrive
	// biome-ignore lint/correctness/useExhaustiveDependencies: events is needed to trigger scroll
	useEffect(() => {
		requestAnimationFrame(() => {
			const viewport = scrollRef.current?.querySelector(
				'[data-slot="scroll-area-viewport"]',
			) as HTMLElement;
			if (viewport) {
				viewport.scrollTop = viewport.scrollHeight;
			}
		});
	}, [events]);

	if (convLoading || eventsLoading) {
		return (
			<div className="flex flex-col h-full gap-4 p-6">
				<Skeleton className="h-10 w-full rounded-xl" />
				<div className="space-y-4 flex-1">
					{Array.from({ length: 4 }).map((_, i) => (
						// biome-ignore lint/suspicious/noArrayIndexKey: skeleton
						<Skeleton key={i} className="h-16 w-3/4 rounded-2xl" />
					))}
				</div>
			</div>
		);
	}

	if (!conversation) {
		return (
			<div className="flex flex-col h-full items-center justify-center text-muted-foreground/50 gap-3">
				<Bot className="size-10" />
				<p className="text-sm">{t("project.agents.convNotFound")}</p>
			</div>
		);
	}

	const statusColor = CONVERSATION_STATUS_COLORS[conversation.status];
	const statusLabel = t(`project.agents.status.${conversation.status}`);
	const isRunning = conversation.status === "running";

	return (
		<div className="flex flex-col h-full min-h-0">
			{/* Header */}
			<div className="shrink-0 border-b border-border/40 px-5 py-3 flex items-center gap-3 bg-background/80 backdrop-blur-sm">
				<div className="flex items-center gap-2 min-w-0 flex-1">
					<Bot className="size-4 text-primary shrink-0" />
					<span className="text-sm font-medium truncate">
						{conversation.trigger_type === "chat_message"
							? t("project.agents.chatSession")
							: t("project.agents.taskSession")}
					</span>
					<Badge
						variant="outline"
						className={cn("text-xs font-semibold shrink-0", statusColor)}
					>
						{statusLabel}
					</Badge>
				</div>

				<div className="flex items-center gap-3 shrink-0">
					{conversation.branch_name && (
						<span className="flex items-center gap-1 text-xs text-muted-foreground">
							<GitBranch className="size-3" />
							{conversation.branch_name}
						</span>
					)}
					{conversation.pr_url && (
						<a
							href={conversation.pr_url}
							target="_blank"
							rel="noreferrer"
							className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
						>
							<GitPullRequest className="size-3" />
							PR
						</a>
					)}
					<ConversationControls
						projectId={projectId}
						conversation={conversation}
					/>
				</div>
			</div>

			{/* Messages */}
			<ScrollArea ref={scrollRef} className="flex-1 min-h-0">
				<div className="px-5 py-5 space-y-4 max-w-3xl mx-auto">
					{messages.length === 0 && !isRunning ? (
						<div className="flex flex-col items-center justify-center gap-3 py-16 text-muted-foreground/40">
							<Bot className="size-10" />
							<p className="text-sm">{t("project.agents.noMessages")}</p>
						</div>
					) : (
						<>
							{messages.map((msg) => (
								<MessageItem key={msg.id} msg={msg} />
							))}
							{isRunning && <TypingIndicator />}
						</>
					)}
				</div>
			</ScrollArea>

			{/* Footer */}
			{conversation.error_message && (
				<div className="shrink-0 border-t border-destructive/20 bg-destructive/5 px-5 py-3">
					<p className="text-xs text-destructive">
						{conversation.error_message}
					</p>
				</div>
			)}
		</div>
	);
}
