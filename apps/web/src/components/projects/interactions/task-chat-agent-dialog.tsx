import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Bot, Loader2, MessageSquare } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { privateChatAgentOptions } from "@/components/projects/agents/agent-picker";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { agentsQueryOptions } from "@/lib/agent-api";
import { newTaskChatSearch } from "@/lib/project-chat-navigation";
import { cn } from "@/lib/utils";

interface TaskChatAgentDialogProps {
	projectId: string;
	taskId: string;
	taskTitle: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

export function TaskChatAgentDialog({
	projectId,
	taskId,
	taskTitle,
	open,
	onOpenChange,
}: TaskChatAgentDialogProps) {
	const { t } = useTranslation("projects");
	const navigate = useNavigate();
	const [selectedAgentId, setSelectedAgentId] = useState("");
	const { data: agents = [], isLoading } = useQuery({
		...agentsQueryOptions(projectId),
		enabled: open,
	});
	const options = privateChatAgentOptions(agents);

	useEffect(() => {
		if (!open) setSelectedAgentId("");
	}, [open]);

	const continueToChat = () => {
		if (!selectedAgentId) return;
		onOpenChange(false);
		void navigate({
			to: "/projects/$projectId/chats",
			params: { projectId },
			search: newTaskChatSearch(taskId, selectedAgentId),
		});
	};

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent
				onClick={(event) => event.stopPropagation()}
				onPointerDown={(event) => event.stopPropagation()}
				className="flex max-h-[calc(100dvh-2rem)] flex-col overflow-hidden sm:max-w-md"
			>
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						<MessageSquare className="size-4 text-primary" />
						{t("chats.selectAgentTitle")}
					</DialogTitle>
					<DialogDescription>
						{t("chats.selectAgentDescription", { task: taskTitle })}
					</DialogDescription>
				</DialogHeader>

				<fieldset
					className="min-h-0 flex-1 space-y-2 overflow-y-auto py-2 [scrollbar-gutter:stable]"
					aria-label={t("chats.selectAgentTitle")}
				>
					{isLoading ? (
						<div className="flex items-center justify-center py-8 text-muted-foreground">
							<Loader2 className="size-5 animate-spin" />
						</div>
					) : options.length === 0 ? (
						<p className="py-6 text-center text-sm text-muted-foreground">
							{t("taskDetail.description.writeWithAIDialog.noAgents")}
						</p>
					) : (
						options.map((agent) => {
							const unavailable = !!agent.disabledReason;
							return (
								<button
									key={agent.id}
									type="button"
									disabled={unavailable}
									aria-pressed={selectedAgentId === agent.id}
									onClick={() => setSelectedAgentId(agent.id)}
									className={cn(
										"flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition-all duration-150",
										selectedAgentId === agent.id
											? "border-primary/60 bg-primary/5 text-foreground"
											: "border-border/40 bg-card/50 text-muted-foreground hover:border-border/70 hover:bg-muted/30 hover:text-foreground",
										unavailable && "cursor-not-allowed opacity-55",
									)}
								>
									<div className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted">
										<Bot className="size-4" />
									</div>
									<div className="min-w-0 flex-1">
										<p className="truncate text-sm font-medium leading-tight text-foreground">
											{agent.name}
										</p>
										{agent.disabledReason === "acp_private_unavailable" && (
											<p className="mt-0.5 text-xs text-muted-foreground">
												{t("chats.acpUnavailableShort")}
											</p>
										)}
									</div>
								</button>
							);
						})
					)}
				</fieldset>

				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						{t("chats.selectAgentCancel")}
					</Button>
					<Button disabled={!selectedAgentId} onClick={continueToChat}>
						{t("chats.selectAgentContinue")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

export function TaskChatLauncher({
	projectId,
	taskId,
	taskTitle,
	children,
}: Omit<TaskChatAgentDialogProps, "open" | "onOpenChange"> & {
	children: (open: () => void) => ReactNode;
}) {
	const [open, setOpen] = useState(false);
	return (
		<>
			{children(() => setOpen(true))}
			{open && (
				<TaskChatAgentDialog
					projectId={projectId}
					taskId={taskId}
					taskTitle={taskTitle}
					open
					onOpenChange={setOpen}
				/>
			)}
		</>
	);
}
