import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import {
	Bot,
	Eye,
	EyeOff,
	Loader2,
	Lock,
	MoreHorizontal,
	Plus,
	Shield,
	Sparkles,
	Trash2,
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
	DialogClose,
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
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectSeparator,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import { usePermissions } from "@/hooks/use-permissions";
import {
	globalRolesQueryOptions,
	myPermissionsQueryOptions,
} from "@/lib/admin-api";
import {
	type AcpBridgeToken,
	type Agent,
	deleteGlobalAgent,
	globalAgentsQueryOptions,
	llmModelsQueryOptions,
	updateGlobalAgent,
} from "@/lib/agent-api";
import { hasPermission } from "@/lib/permissions";

const NO_ROLE = "__none__";
const ZERO_UUID = "00000000-0000-0000-0000-000000000000";

export const Route = createFileRoute("/_authenticated/admin/agents/")({
	validateSearch: (search: Record<string, unknown>) => ({
		create: search.create === true || search.create === "true",
	}),
	beforeLoad: async ({ context: { queryClient } }) => {
		const permissions = await queryClient
			.fetchQuery(myPermissionsQueryOptions)
			.catch(() => [] as string[]);

		const canAccess =
			hasPermission(permissions, "agents.read") ||
			hasPermission(permissions, "agents.write");

		if (!canAccess) {
			throw redirect({ to: "/home" });
		}
	},
	loader: async ({ context: { queryClient } }) => {
		await Promise.all([
			queryClient.ensureQueryData(globalAgentsQueryOptions),
			queryClient.ensureQueryData(globalRolesQueryOptions),
			queryClient.ensureQueryData(llmModelsQueryOptions),
		]);
	},
	component: GlobalAgentsPage,
});

// ── Edit Dialog ───────────────────────────────────────────────────────────────
//
// Unlike creation (CreateAgentDialog, shared with the project Agents page —
// same wizard, just no project role step), editing an existing agent has no
// project-scoped dialog to mirror: project agents are edited from their own
// detail page (routes/.../agents/$agentId), which global agents don't have.
// This stays a single-panel form rather than a wizard.

function EditAgentDialog({
	agent,
	open,
	onOpenChange,
}: {
	agent: Agent;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("admin");
	const qc = useQueryClient();
	const isAcp = agent.agent_type === "acp";
	const { data: globalRoles = [] } = useQuery(globalRolesQueryOptions);
	const { data: llmModels = {} } = useQuery(llmModelsQueryOptions);

	const [name, setName] = useState(agent.name);
	const [handle, setHandle] = useState(agent.handle);
	const [roleId, setRoleId] = useState(agent.global_role_id ?? NO_ROLE);
	const [llmProvider, setLlmProvider] = useState(agent.llm_provider);
	const [llmModel, setLlmModel] = useState(agent.llm_model);
	const [llmApiKey, setLlmApiKey] = useState("");
	const [llmBaseUrl, setLlmBaseUrl] = useState(agent.llm_base_url);
	const [acpProvider, setAcpProvider] = useState(
		agent.acp_provider ?? "claude-code",
	);
	const [systemPrompt, setSystemPrompt] = useState(agent.system_prompt);
	const [showApiKey, setShowApiKey] = useState(false);
	const [error, setError] = useState<string | null>(null);

	const availableModels: string[] = llmModels[llmProvider]?.models ?? [];

	const handleClose = (v: boolean) => {
		if (!v) setError(null);
		onOpenChange(v);
	};

	const mutation = useMutation({
		mutationFn: () =>
			updateGlobalAgent(agent.id, {
				name: name.trim(),
				handle: handle.trim(),
				global_role_id: roleId === NO_ROLE ? ZERO_UUID : roleId,
				...(isAcp
					? { acp_provider: acpProvider }
					: {
							llm_provider: llmProvider,
							llm_model: llmModel,
							...(llmApiKey.trim() ? { llm_api_key: llmApiKey.trim() } : {}),
							llm_base_url: llmBaseUrl,
							system_prompt: systemPrompt,
						}),
			}),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: globalAgentsQueryOptions.queryKey });
			qc.invalidateQueries({ queryKey: ["global-agents", "chattable"] });
			handleClose(false);
		},
		onError: (err: unknown) => {
			const e = err as { response?: { data?: { error?: string } } };
			setError(e?.response?.data?.error ?? t("agents.formDialog.updateFailed"));
		},
	});

	const canSubmit = !!(
		name.trim() &&
		handle.trim() &&
		(isAcp || (llmProvider && llmModel && llmBaseUrl.trim())) &&
		!mutation.isPending
	);

	return (
		<Dialog open={open} onOpenChange={handleClose}>
			<DialogContent className="sm:max-w-lg p-0 gap-0 overflow-hidden">
				<div className="relative overflow-hidden border-b border-border/50">
					<div className="relative flex items-center gap-3 px-6 pt-5 pb-4">
						<div className="flex size-10 items-center justify-center rounded-xl bg-primary/10 ring-1 ring-primary/20">
							<Bot className="size-5 text-primary" />
						</div>
						<div>
							<DialogTitle className="text-sm font-semibold">
								{t("agents.formDialog.editTitle")}
							</DialogTitle>
							<DialogDescription className="text-xs text-muted-foreground mt-0.5">
								{t("agents.formDialog.description")}
							</DialogDescription>
						</div>
					</div>
				</div>

				<div className="overflow-y-auto max-h-[62vh] px-6 py-5 space-y-4">
					<div className="grid grid-cols-2 gap-3">
						<div className="space-y-1.5">
							<Label htmlFor="edit-agent-name">
								{t("agents.formDialog.nameLabel")}
							</Label>
							<Input
								id="edit-agent-name"
								value={name}
								onChange={(e) => setName(e.target.value)}
								autoFocus
							/>
						</div>
						<div className="space-y-1.5">
							<Label htmlFor="edit-agent-handle">
								{t("agents.formDialog.handleLabel")}
							</Label>
							<div className="relative">
								<span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground select-none">
									@
								</span>
								<Input
									id="edit-agent-handle"
									value={handle}
									onChange={(e) => setHandle(e.target.value)}
									className="pl-7"
								/>
							</div>
						</div>
					</div>

					<div className="space-y-1.5">
						<Label>{t("agents.formDialog.globalRoleLabel")}</Label>
						<Select value={roleId} onValueChange={(v) => v && setRoleId(v)}>
							<SelectTrigger className="w-full">
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value={NO_ROLE}>
									{t("agents.formDialog.noGlobalRole")}
								</SelectItem>
								<SelectSeparator />
								{globalRoles.map((r) => (
									<SelectItem key={r.id} value={r.id}>
										{r.name}
									</SelectItem>
								))}
							</SelectContent>
						</Select>
					</div>

					{isAcp ? (
						<div className="space-y-1.5">
							<Label>{t("agents.formDialog.acpProviderLabel")}</Label>
							<Select
								value={acpProvider}
								onValueChange={(v) =>
									v && setAcpProvider(v as typeof acpProvider)
								}
							>
								<SelectTrigger className="w-full">
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									<SelectItem value="claude-code">Claude Code</SelectItem>
									<SelectItem value="codex">Codex</SelectItem>
									<SelectItem value="gemini-cli">Gemini CLI</SelectItem>
									<SelectItem value="custom">Custom…</SelectItem>
								</SelectContent>
							</Select>
						</div>
					) : (
						<>
							<div className="rounded-lg border border-border/60 bg-muted/20 p-4 space-y-3">
								<div className="grid grid-cols-2 gap-3">
									<div className="space-y-1.5">
										<Label>{t("agents.formDialog.providerLabel")}</Label>
										<Input
											value={llmProvider}
											onChange={(e) => setLlmProvider(e.target.value)}
										/>
									</div>
									<div className="space-y-1.5">
										<Label>{t("agents.formDialog.modelLabel")}</Label>
										{availableModels.length > 0 ? (
											<Select
												value={llmModel}
												onValueChange={(v) => v && setLlmModel(v)}
											>
												<SelectTrigger>
													<SelectValue />
												</SelectTrigger>
												<SelectContent>
													{availableModels.map((m) => (
														<SelectItem key={m} value={m}>
															{m}
														</SelectItem>
													))}
												</SelectContent>
											</Select>
										) : (
											<Input
												value={llmModel}
												onChange={(e) => setLlmModel(e.target.value)}
											/>
										)}
									</div>
								</div>
							</div>

							<div className="space-y-1.5">
								<Label htmlFor="edit-agent-api-key">
									<span className="flex items-center gap-1.5">
										<Lock className="size-3 text-muted-foreground" />
										{t("agents.formDialog.apiKeyLabel")}
									</span>
								</Label>
								<div className="relative">
									<Input
										id="edit-agent-api-key"
										type={showApiKey ? "text" : "password"}
										placeholder={t("agents.formDialog.apiKeyKeepPlaceholder")}
										value={llmApiKey}
										onChange={(e) => setLlmApiKey(e.target.value)}
										className="pr-9"
									/>
									<button
										type="button"
										onClick={() => setShowApiKey((s) => !s)}
										className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground/60 hover:text-foreground transition-colors"
									>
										{showApiKey ? (
											<EyeOff className="size-4" />
										) : (
											<Eye className="size-4" />
										)}
									</button>
								</div>
							</div>

							<div className="space-y-1.5">
								<Label htmlFor="edit-agent-base-url">
									{t("agents.formDialog.baseUrlLabel")}
								</Label>
								<Input
									id="edit-agent-base-url"
									value={llmBaseUrl}
									onChange={(e) => setLlmBaseUrl(e.target.value)}
								/>
							</div>

							<div className="space-y-1.5">
								<Label htmlFor="edit-agent-system-prompt">
									{t("agents.formDialog.systemPromptLabel")}{" "}
									<span className="text-muted-foreground font-normal text-xs">
										{t("agents.formDialog.optional")}
									</span>
								</Label>
								<Textarea
									id="edit-agent-system-prompt"
									value={systemPrompt}
									onChange={(e) => setSystemPrompt(e.target.value)}
									rows={4}
									className="resize-none text-sm"
								/>
							</div>
						</>
					)}

					{error && (
						<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
							{error}
						</p>
					)}
				</div>

				<DialogFooter className="border-t border-border/50 bg-muted/20 px-6 py-4">
					<DialogClose
						render={
							<Button variant="ghost" size="sm" disabled={mutation.isPending} />
						}
					>
						{t("agents.formDialog.cancel")}
					</DialogClose>
					<Button
						size="sm"
						disabled={!canSubmit}
						onClick={() => mutation.mutate()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-4 mr-1.5 animate-spin" />
						) : (
							<Sparkles className="size-4 mr-1.5" />
						)}
						{t("agents.formDialog.save")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Delete Dialog ─────────────────────────────────────────────────────────────

function DeleteAgentDialog({
	agent,
	open,
	onOpenChange,
}: {
	agent: Agent | null;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("admin");
	const qc = useQueryClient();

	const mutation = useMutation({
		mutationFn: () => {
			if (!agent) return Promise.resolve();
			return deleteGlobalAgent(agent.id);
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: globalAgentsQueryOptions.queryKey });
			qc.invalidateQueries({ queryKey: ["global-agents", "chattable"] });
			onOpenChange(false);
		},
	});

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-sm">
				<DialogHeader>
					<div className="flex size-10 items-center justify-center rounded-full bg-destructive/10 mb-2">
						<Trash2 className="size-5 text-destructive" />
					</div>
					<DialogTitle>{t("agents.deleteDialog.title")}</DialogTitle>
					<DialogDescription>
						{t("agents.deleteDialog.confirmPrefix")}{" "}
						<span className="font-medium text-foreground">{agent?.name}</span>
						{t("agents.deleteDialog.confirmSuffix")}
					</DialogDescription>
				</DialogHeader>
				{mutation.isError && (
					<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
						{t("agents.deleteDialog.deleteFailed")}
					</p>
				)}
				<DialogFooter>
					<DialogClose
						render={
							<Button
								variant="outline"
								size="sm"
								disabled={mutation.isPending}
							/>
						}
					>
						{t("agents.deleteDialog.cancel")}
					</DialogClose>
					<Button
						variant="destructive"
						size="sm"
						disabled={mutation.isPending}
						onClick={() => mutation.mutate()}
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : (
							<Trash2 className="size-3.5" />
						)}
						{t("agents.deleteDialog.delete")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Page ───────────────────────────────────────────────────────────────────────

function GlobalAgentsPage() {
	const { t } = useTranslation("admin");
	const search = Route.useSearch();
	const navigate = Route.useNavigate();
	const { hasPermission } = usePermissions();
	const canWrite = hasPermission("agents.write");

	const { data: agents = [], isLoading } = useQuery(globalAgentsQueryOptions);
	const { data: globalRoles = [] } = useQuery(globalRolesQueryOptions);

	const [createOpen, setCreateOpen] = useState(search.create);
	const [acpSetupAgent, setAcpSetupAgent] = useState<Agent | null>(null);
	const [acpSetupToken, setAcpSetupToken] = useState<AcpBridgeToken | null>(
		null,
	);
	const [editAgent, setEditAgent] = useState<Agent | null>(null);
	const [deleteAgent, setDeleteAgent] = useState<Agent | null>(null);

	// Mirrors the project Agents page's handling of ?create=true — only needs
	// to open the dialog once, so strip it from the URL once consumed.
	function handleCreateOpenChange(nextOpen: boolean) {
		setCreateOpen(nextOpen);
		if (!nextOpen && search.create) {
			navigate({
				search: (prev) => ({ ...prev, create: false }),
				replace: true,
			});
		}
	}

	const roleName = (roleId?: string | null) =>
		globalRoles.find((r) => r.id === roleId)?.name;

	return (
		<div className="flex flex-col gap-6 p-6 max-w-5xl w-full mx-auto">
			<div className="flex items-center justify-between">
				<div>
					<h1 className="font-[Syne] text-2xl font-bold tracking-tight">
						{t("agents.header.title")}
					</h1>
					<p className="mt-1 text-sm text-muted-foreground">
						{t("agents.header.description")}
					</p>
				</div>
				{canWrite && (
					<Button
						size="sm"
						className="gap-1.5"
						onClick={() => setCreateOpen(true)}
					>
						<Plus className="size-3.5" />
						{t("agents.header.newAgent")}
					</Button>
				)}
			</div>

			{isLoading ? (
				<div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
					{[...Array(4)].map((_, i) => (
						// biome-ignore lint/suspicious/noArrayIndexKey: static skeleton
						<Skeleton key={i} className="h-20 rounded-xl" />
					))}
				</div>
			) : agents.length === 0 ? (
				<div className="flex flex-col items-center gap-3 rounded-xl border border-dashed border-border/60 bg-muted/10 py-14">
					<div className="flex size-12 items-center justify-center rounded-xl bg-primary/10">
						<Bot className="size-6 text-primary" />
					</div>
					<div className="text-center">
						<p className="text-sm font-medium">{t("agents.empty.title")}</p>
						<p className="mt-0.5 text-xs text-muted-foreground">
							{t("agents.empty.description")}
						</p>
					</div>
					{canWrite && (
						<Button
							size="sm"
							className="gap-1.5 mt-1"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-3.5" />
							{t("agents.empty.createAgent")}
						</Button>
					)}
				</div>
			) : (
				<div className="grid grid-cols-1 gap-2 lg:grid-cols-2">
					{agents.map((agent) => {
						const isAcp = agent.agent_type === "acp";
						return (
							<div
								key={agent.id}
								className="flex items-center gap-3 rounded-xl border border-border/50 bg-card px-4 py-3 transition-colors hover:bg-muted/30"
							>
								<Avatar className="size-9 shrink-0">
									<AvatarFallback className="text-xs bg-primary/10 text-primary font-semibold">
										<Bot className="size-4" />
									</AvatarFallback>
								</Avatar>
								<div className="min-w-0 flex-1">
									<p className="text-sm font-medium truncate">{agent.name}</p>
									<p className="text-xs text-muted-foreground truncate">
										@{agent.handle} ·{" "}
										{isAcp ? (agent.acp_provider ?? "acp") : agent.llm_model}
									</p>
								</div>
								{roleName(agent.global_role_id) && (
									<span className="flex shrink-0 items-center gap-1.5 rounded-full border border-border/60 bg-secondary/50 px-2.5 py-1 text-xs font-medium text-secondary-foreground">
										<Shield className="size-3 text-muted-foreground" />
										{roleName(agent.global_role_id)}
									</span>
								)}
								<Badge variant="outline" className="shrink-0 text-xs">
									{agent.agent_type}
								</Badge>
								{canWrite && (
									<DropdownMenu>
										<DropdownMenuTrigger className="flex size-7 shrink-0 items-center justify-center rounded-md p-0 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
											<MoreHorizontal className="size-4" />
										</DropdownMenuTrigger>
										<DropdownMenuContent align="end" className="w-44">
											<DropdownMenuItem onClick={() => setEditAgent(agent)}>
												{t("agents.table.editAction")}
											</DropdownMenuItem>
											<DropdownMenuItem
												className="text-destructive focus:text-destructive focus:bg-destructive/10"
												onClick={() => setDeleteAgent(agent)}
											>
												<Trash2 className="size-3.5 mr-2" />
												{t("agents.table.deleteAction")}
											</DropdownMenuItem>
										</DropdownMenuContent>
									</DropdownMenu>
								)}
							</div>
						);
					})}
				</div>
			)}

			{canWrite && (
				<CreateAgentDialog
					open={createOpen}
					onOpenChange={handleCreateOpenChange}
					onAcpAgentCreated={(agent, token) => {
						setAcpSetupAgent(agent);
						setAcpSetupToken(token);
					}}
				/>
			)}
			<AcpSetupDialog
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
			{editAgent && (
				<EditAgentDialog
					agent={editAgent}
					open={!!editAgent}
					onOpenChange={(open) => {
						if (!open) setEditAgent(null);
					}}
				/>
			)}
			<DeleteAgentDialog
				agent={deleteAgent}
				open={!!deleteAgent}
				onOpenChange={(open) => {
					if (!open) setDeleteAgent(null);
				}}
			/>
		</div>
	);
}
