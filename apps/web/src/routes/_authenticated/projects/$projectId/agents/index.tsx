import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import {
	Bot,
	Loader2,
	MoreHorizontal,
	Plus,
	Settings,
	Trash2,
	Zap,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

import {
	AcpSetupDialog,
	CreateAgentDialog,
} from "@/components/projects/agents/create-agent-dialog";
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
import { Skeleton } from "@/components/ui/skeleton";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	type AcpBridgeToken,
	type Agent,
	acpBridgeStatusQueryOptions,
	agentsQueryOptions,
	deleteAgent,
	llmModelsQueryOptions,
} from "@/lib/agent-api";
import {
	projectQueryOptions,
	projectRolesQueryOptions,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/agents/",
)({
	validateSearch: (search: Record<string, unknown>) => ({
		create: search.create === true || search.create === "true",
	}),
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		await Promise.all([
			queryClient.ensureQueryData(agentsQueryOptions(projectId)),
			queryClient.ensureQueryData(projectRolesQueryOptions(projectId)),
			queryClient.ensureQueryData(llmModelsQueryOptions),
		]);
	},
	component: AgentsPage,
});

// ── Agent Card ────────────────────────────────────────────────────────────────

function AgentCard({
	agent,
	projectId,
	canWrite,
}: {
	agent: Agent;
	projectId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const navigate = useNavigate();
	const [confirmDelete, setConfirmDelete] = useState(false);
	const isAcp = agent.agent_type === "acp";

	const deleteMutation = useMutation({
		mutationFn: () => deleteAgent(projectId, agent.id),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["projects", projectId, "agents"] });
			setConfirmDelete(false);
		},
	});

	// Kept live via useProjectRealtime's socket-driven cache write on
	// "agent.acp_bridge.status" (same query key the detail page's Local
	// Bridge panel reads), so this stays in sync without polling.
	const { data: acpStatus } = useQuery(
		acpBridgeStatusQueryOptions(projectId, agent.id, {
			enabled: isAcp && agent.has_acp_bridge_token,
		}),
	);

	const initials = agent.name
		.split(" ")
		.map((w) => w[0])
		.join("")
		.toUpperCase()
		.slice(0, 2);

	return (
		<>
			{/* biome-ignore lint/a11y/noStaticElementInteractions: card navigates to agent detail; the dropdown menu below stays keyboard-reachable on its own */}
			{/* biome-ignore lint/a11y/useKeyWithClickEvents: click-to-navigate card; the agent name is also reachable via the configure menu item */}
			<div
				onClick={() =>
					navigate({
						to: "/projects/$projectId/agents/$agentId",
						params: { projectId, agentId: agent.id },
					})
				}
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
												to="/projects/$projectId/agents/$agentId"
												params={{ projectId, agentId: agent.id }}
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
							{t("agents.card.deleteDialog.description")}
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

// ── Page ──────────────────────────────────────────────────────────────────────

function AgentsPage() {
	const { t } = useTranslation("projects");
	const { projectId } = Route.useParams();
	const { create } = Route.useSearch();
	const navigate = Route.useNavigate();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("agents.write");

	const { data: project } = useQuery(projectQueryOptions(projectId));
	const { data: agents = [], isLoading } = useQuery(
		agentsQueryOptions(projectId),
	);
	const [createOpen, setCreateOpen] = useState(create);
	const [acpSetupAgent, setAcpSetupAgent] = useState<Agent | null>(null);
	const [acpSetupToken, setAcpSetupToken] = useState<AcpBridgeToken | null>(
		null,
	);

	// `?create=true` (from the agent picker's "no agents yet" empty state)
	// only needs to open the dialog once — leaving it in the URL would
	// reopen the dialog on every refresh/back-navigation after the user
	// closes it, so strip it once the dialog's opened state is consumed.
	function handleCreateOpenChange(nextOpen: boolean) {
		setCreateOpen(nextOpen);
		if (!nextOpen && create) {
			navigate({
				search: (prev) => ({ ...prev, create: false }),
				replace: true,
			});
		}
	}

	return (
		<div className="flex flex-col">
			{/* Header */}
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
				<div className="relative flex items-end justify-between px-6 py-8">
					<div>
						<h1 className="font-[Syne] text-2xl font-bold tracking-tight">
							{t("agents.page.title")}
						</h1>
						<p className="mt-1 text-sm text-muted-foreground">
							{project?.name} · {t("agents.page.subtitle")}
						</p>
					</div>
					{canWrite ? (
						<Button
							size="sm"
							className="gap-1.5 shadow-sm shadow-primary/20"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-3.5" />
							{t("agents.page.newAgent")}
						</Button>
					) : null}
				</div>
			</div>

			{/* Content */}
			<div className="p-6">
				{isLoading ? (
					<div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
						{Array.from({ length: 3 }).map((_, i) => (
							// biome-ignore lint/suspicious/noArrayIndexKey: skeleton
							<Skeleton key={i} className="h-36 rounded-xl" />
						))}
					</div>
				) : agents.length === 0 ? (
					<div className="flex flex-col items-center justify-center gap-4 py-20 text-center">
						<div className="flex size-16 items-center justify-center rounded-2xl bg-muted/50">
							<Bot className="size-8 text-muted-foreground/50" />
						</div>
						<div>
							<p className="font-medium text-sm">
								{t("agents.page.empty.title")}
							</p>
							<p className="text-xs text-muted-foreground mt-1 max-w-xs">
								{t("agents.page.empty.description")}
							</p>
						</div>
						{canWrite && (
							<Button size="sm" onClick={() => setCreateOpen(true)}>
								<Plus className="size-4 mr-1.5" />
								{t("agents.page.empty.createFirstAgent")}
							</Button>
						)}
					</div>
				) : (
					<div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
						{agents.map((agent) => (
							<AgentCard
								key={agent.id}
								agent={agent}
								projectId={projectId}
								canWrite={canWrite}
							/>
						))}
					</div>
				)}
			</div>

			<CreateAgentDialog
				projectId={projectId}
				open={createOpen}
				onOpenChange={handleCreateOpenChange}
				onAcpAgentCreated={(agent, token) => {
					setAcpSetupAgent(agent);
					setAcpSetupToken(token);
				}}
			/>
			<AcpSetupDialog
				projectId={projectId}
				agent={acpSetupAgent}
				token={acpSetupToken}
				open={acpSetupAgent !== null}
				canWrite={canWrite}
				onOpenChange={(v) => {
					if (!v) {
						setAcpSetupAgent(null);
						setAcpSetupToken(null);
					}
				}}
				onTokenGenerated={() =>
					setAcpSetupAgent((a) =>
						a ? { ...a, has_acp_bridge_token: true } : a,
					)
				}
			/>
		</div>
	);
}
