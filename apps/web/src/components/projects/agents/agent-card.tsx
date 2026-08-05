import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Loader2, MoreHorizontal, Settings, Trash2, Zap } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
	type Agent,
	acpBridgeStatusQueryOptions,
	deleteAgent,
	deleteGlobalAgent,
	globalAcpBridgeStatusQueryOptions,
	globalAgentsQueryOptions,
} from "@/lib/agent-api";
import { cn } from "@/lib/utils";

// Shared between the project Agents page
// (routes/_authenticated/projects/$projectId/agents/index.tsx) and the
// global one (routes/_authenticated/admin/agents/index.tsx) — same card,
// same grid, same empty/loading states either way. Both scopes now have a
// real detail page (see components/projects/agents/agent-detail.tsx), so
// "Configure" always navigates — just to a different route.
export function AgentCard({
	agent,
	projectId,
	canWrite,
}: {
	agent: Agent;
	/** Absent for a global agent (no project of its own). */
	projectId?: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const navigate = useNavigate();
	const [confirmDelete, setConfirmDelete] = useState(false);
	const isAcp = agent.agent_type === "acp";

	const deleteMutation = useMutation({
		mutationFn: () =>
			projectId
				? deleteAgent(projectId, agent.id)
				: deleteGlobalAgent(agent.id),
		onSuccess: () => {
			if (projectId) {
				qc.invalidateQueries({ queryKey: ["projects", projectId, "agents"] });
			} else {
				qc.invalidateQueries({ queryKey: globalAgentsQueryOptions.queryKey });
				qc.invalidateQueries({ queryKey: ["global-agents", "chattable"] });
			}
			setConfirmDelete(false);
		},
	});

	// Kept live via useProjectRealtime's socket-driven cache write on
	// "agent.acp_bridge.status" (same query key the detail page's Local
	// Bridge panel reads), so this stays in sync without polling. A global
	// agent's status has no such live push (see globalAcpBridgeStatusQueryOptions'
	// doc comment) — fetch-once only there.
	const { data: acpStatus } = useQuery(
		projectId
			? acpBridgeStatusQueryOptions(projectId, agent.id, {
					enabled: isAcp && agent.has_acp_bridge_token,
				})
			: globalAcpBridgeStatusQueryOptions(agent.id, {
					enabled: isAcp && agent.has_acp_bridge_token,
				}),
	);

	const initials = agent.name
		.split(" ")
		.map((w) => w[0])
		.join("")
		.toUpperCase()
		.slice(0, 2);

	const detailHref = projectId
		? `/projects/${projectId}/agents/${agent.id}`
		: `/admin/agents/${agent.id}`;

	const handleCardClick = () => {
		navigate({ to: detailHref });
	};

	return (
		<>
			{/* biome-ignore lint/a11y/noStaticElementInteractions: card opens the agent's detail page or edit dialog; the dropdown menu below stays keyboard-reachable on its own */}
			{/* biome-ignore lint/a11y/useKeyWithClickEvents: click-to-configure card; the agent name is also reachable via the configure menu item */}
			<div
				onClick={handleCardClick}
				className="group relative flex flex-col gap-3 rounded-xl border border-border/60 bg-card p-5 transition-all hover:border-border hover:shadow-sm cursor-pointer"
			>
				{/* Header */}
				<div className="flex items-start justify-between gap-3">
					<div className="flex items-center gap-3">
						<Avatar className="size-10 rounded-lg bg-primary/10">
							<AvatarFallback className="rounded-lg bg-primary/10 text-primary font-semibold text-sm">
								{initials}
							</AvatarFallback>
						</Avatar>
						<div className="min-w-0">
							<p className="font-semibold text-sm leading-tight">
								{agent.name}
							</p>
							<p className="text-xs text-muted-foreground mt-0.5">
								@{agent.handle}
							</p>
						</div>
					</div>

					<div className="flex items-center gap-1.5 shrink-0">
						<Badge variant="secondary" className="text-xs font-medium">
							{isAcp ? (agent.acp_provider ?? "acp") : agent.llm_provider}
						</Badge>
						{canWrite && (
							<DropdownMenu>
								<DropdownMenuTrigger
									onClick={(e) => e.stopPropagation()}
									className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover:opacity-100"
								>
									<MoreHorizontal className="size-4" />
								</DropdownMenuTrigger>
								<DropdownMenuContent align="end" className="w-40">
									<DropdownMenuItem
										render={
											<Link
												to={detailHref}
												onClick={(e) => e.stopPropagation()}
											/>
										}
									>
										<Settings className="size-3.5 mr-2" />
										{t("agents.card.configure")}
									</DropdownMenuItem>
									<DropdownMenuSeparator />
									<DropdownMenuItem
										className="text-destructive focus:text-destructive"
										onClick={(e) => {
											e.stopPropagation();
											setConfirmDelete(true);
										}}
									>
										<Trash2 className="size-3.5 mr-2" />
										{t("agents.card.delete")}
									</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>
						)}
					</div>
				</div>

				{/* Stats row */}
				<div className="flex items-center gap-4 text-xs text-muted-foreground">
					{isAcp ? (
						<span className="flex items-center gap-1.5">
							<span
								className={cn(
									"size-1.5 rounded-full",
									acpStatus?.connected
										? "bg-emerald-500"
										: "bg-muted-foreground/40",
								)}
							/>
							{acpStatus?.connected
								? t("agents.card.acpStatusConnected")
								: t("agents.card.acpStatusDisconnected")}
						</span>
					) : (
						<span className="flex items-center gap-1">
							<Zap className="size-3" />
							{agent.llm_provider}/{agent.llm_model}
						</span>
					)}
				</div>
			</div>

			{/* Delete confirmation */}
			<Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
				<DialogContent className="max-w-sm">
					<DialogHeader>
						<DialogTitle>
							{t("agents.card.deleteDialog.title", { name: agent.name })}
						</DialogTitle>
						<DialogDescription>
							{t(
								projectId
									? "agents.card.deleteDialog.description"
									: "agents.card.deleteDialog.descriptionGlobal",
							)}
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => setConfirmDelete(false)}
							disabled={deleteMutation.isPending}
						>
							{t("agents.card.deleteDialog.cancel")}
						</Button>
						<Button
							variant="destructive"
							onClick={() => deleteMutation.mutate()}
							disabled={deleteMutation.isPending}
						>
							{deleteMutation.isPending ? (
								<Loader2 className="size-4 animate-spin" />
							) : (
								t("agents.card.deleteDialog.delete")
							)}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</>
	);
}
