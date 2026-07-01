import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot, Loader2, Plus, Send, X } from "lucide-react";
import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { agentsQueryOptions, startChatSession } from "@/lib/agent-api";
import { cn } from "@/lib/utils";
import { ConversationView } from "./agents/conversation-view";

// ── Types ─────────────────────────────────────────────────────────────────────

type ChatPhase =
	| { kind: "compose" }
	| { kind: "conversation"; conversationId: string };

interface AIChatFloatProps {
	projectId: string;
}

// ── Component ─────────────────────────────────────────────────────────────────

export function AIChatFloat({ projectId }: AIChatFloatProps) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);
	const [phase, setPhase] = useState<ChatPhase>({ kind: "compose" });
	const [agentId, setAgentId] = useState<string>("");
	const [message, setMessage] = useState("");
	const textareaRef = useRef<HTMLTextAreaElement>(null);

	const qc = useQueryClient();
	const { data: agents = [], isLoading: agentsLoading } = useQuery(
		agentsQueryOptions(projectId),
	);

	const sendMut = useMutation({
		mutationFn: () =>
			startChatSession(projectId, agentId, { message: message.trim() }),
		onSuccess: (result) => {
			setPhase({
				kind: "conversation",
				conversationId: result.conversation.id,
			});
			setMessage("");
			void qc.invalidateQueries({
				queryKey: ["projects", projectId, "conversations"],
			});
		},
	});

	function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
		if (e.key === "Enter" && !e.shiftKey) {
			e.preventDefault();
			if (canSend) sendMut.mutate();
		}
	}

	function handleNewConversation() {
		setPhase({ kind: "compose" });
		setMessage("");
		setTimeout(() => textareaRef.current?.focus(), 50);
	}

	const canSend = !!agentId && message.trim().length > 0 && !sendMut.isPending;

	return (
		<>
			{/* Floating trigger button */}
			<button
				type="button"
				aria-label={t("project.agents.chat.title")}
				onClick={() => setOpen((o) => !o)}
				className={cn(
					"fixed bottom-6 right-6 z-40 flex size-12 items-center justify-center rounded-full shadow-lg transition-all hover:scale-105",
					open
						? "bg-muted text-foreground border border-border"
						: "bg-primary text-primary-foreground hover:bg-primary/90",
				)}
			>
				{open ? <X className="size-5" /> : <Bot className="size-5" />}
			</button>

			{/* Chat panel */}
			{open && (
				<div className="fixed bottom-20 right-6 z-40 flex w-95 flex-col overflow-hidden rounded-2xl border border-border/60 bg-background shadow-2xl">
					{/* Panel header */}
					<div className="flex shrink-0 items-center justify-between border-b border-border/40 bg-muted/30 px-4 py-3">
						<div className="flex items-center gap-2">
							<Bot className="size-4 text-primary" />
							<span className="text-sm font-semibold">
								{t("project.agents.chat.title")}
							</span>
						</div>
						{phase.kind === "conversation" && (
							<Button
								size="sm"
								variant="outline"
								className="h-7 gap-1.5 text-xs"
								onClick={handleNewConversation}
							>
								<Plus className="size-3" />
								{t("project.agents.chat.newConversation")}
							</Button>
						)}
					</div>

					{phase.kind === "compose" ? (
						<ComposeForm
							agents={agents}
							agentsLoading={agentsLoading}
							agentId={agentId}
							onAgentChange={setAgentId}
							message={message}
							onMessageChange={setMessage}
							onKeyDown={handleKeyDown}
							textareaRef={textareaRef}
							canSend={canSend}
							isPending={sendMut.isPending}
							onSend={() => sendMut.mutate()}
							error={sendMut.error}
						/>
					) : (
						<div className="h-110">
							<ConversationView
								projectId={projectId}
								conversationId={phase.conversationId}
							/>
						</div>
					)}
				</div>
			)}
		</>
	);
}

// ── ComposeForm ───────────────────────────────────────────────────────────────

interface ComposeFormProps {
	agents: { id: string; name: string }[];
	agentsLoading: boolean;
	agentId: string;
	onAgentChange: (id: string) => void;
	message: string;
	onMessageChange: (msg: string) => void;
	onKeyDown: (e: React.KeyboardEvent<HTMLTextAreaElement>) => void;
	textareaRef: React.RefObject<HTMLTextAreaElement | null>;
	canSend: boolean;
	isPending: boolean;
	onSend: () => void;
	error: Error | null;
}

function ComposeForm({
	agents,
	agentsLoading,
	agentId,
	onAgentChange,
	message,
	onMessageChange,
	onKeyDown,
	textareaRef,
	canSend,
	isPending,
	onSend,
	error,
}: ComposeFormProps) {
	const { t } = useTranslation();
	return (
		<div className="flex flex-col gap-3 p-4">
			{/* Agent selector */}
			<div className="space-y-1.5">
				<p className="text-xs font-medium text-muted-foreground">
					{t("project.agents.chat.agent")}
				</p>
				{agentsLoading ? (
					<div className="h-9 animate-pulse rounded-md bg-muted" />
				) : agents.length === 0 ? (
					<p className="text-xs text-muted-foreground">
						{t("project.agents.chat.noAgents")}
					</p>
				) : (
					<Select
						value={agentId}
						onValueChange={(v) => v && onAgentChange(v)}
						items={agents.map((a) => ({ value: a.id, label: a.name }))}
					>
						<SelectTrigger className="h-9 text-sm">
							<SelectValue
								placeholder={t("project.agents.chat.selectAgent")}
							/>
						</SelectTrigger>
						<SelectContent>
							{agents.map((agent) => (
								<SelectItem key={agent.id} value={agent.id}>
									{agent.name}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				)}
			</div>

			{/* Message input */}
			<div className="space-y-1.5">
				<label
					htmlFor="chat-message"
					className="text-xs font-medium text-muted-foreground"
				>
					{t("project.agents.chat.message")}
					<span className="ml-1.5 font-normal opacity-50">
						{t("project.agents.chat.messageHint")}
					</span>
				</label>
				<Textarea
					id="chat-message"
					ref={textareaRef}
					placeholder={t("project.agents.chat.messagePlaceholder")}
					value={message}
					onChange={(e) => onMessageChange(e.target.value)}
					onKeyDown={onKeyDown}
					rows={5}
					className="resize-none text-sm"
				/>
			</div>

			{/* Error */}
			{error && <p className="text-xs text-destructive">{error.message}</p>}

			{/* Send button */}
			<Button className="w-full" onClick={onSend} disabled={!canSend}>
				{isPending ? (
					<Loader2 className="size-4 animate-spin" />
				) : (
					<Send className="size-4" />
				)}
				{t("project.agents.chat.send")}
			</Button>
		</div>
	);
}
