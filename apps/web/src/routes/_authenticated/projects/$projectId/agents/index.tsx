import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
	Bot,
	BriefcaseBusiness,
	CalendarRange,
	ChevronLeft,
	ChevronRight,
	Code2,
	Cpu,
	Eye,
	EyeOff,
	FlaskConical,
	GitBranch,
	Loader2,
	Lock,
	MessageSquare,
	MoreHorizontal,
	Plus,
	Search,
	Settings,
	Sparkles,
	Trash2,
	Zap,
} from "lucide-react";
import { type ComponentType, useState } from "react";
import { Trans, useTranslation } from "react-i18next";

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
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	AGENT_PRESETS,
	type Agent,
	agentsQueryOptions,
	createAgent,
	deleteAgent,
	llmModelsQueryOptions,
	TRIGGER_PROMPTS,
} from "@/lib/agent-api";
import { projectRolesQueryOptions } from "@/lib/project-api";
import { cn } from "@/lib/utils";

const CUSTOM = "__custom__";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/agents/",
)({
	loader: async ({ context: { queryClient }, params: { projectId } }) => {
		await Promise.all([
			queryClient.ensureQueryData(agentsQueryOptions(projectId)),
			queryClient.ensureQueryData(projectRolesQueryOptions(projectId)),
			queryClient.ensureQueryData(llmModelsQueryOptions),
		]);
	},
	component: AgentsPage,
});

// ── Create Agent Dialog ───────────────────────────────────────────────────────

const PRESET_ICON_MAP: Record<string, ComponentType<{ className?: string }>> = {
	"software-engineer": Code2,
	"code-reviewer": Search,
	"qa-engineer": FlaskConical,
	planner: CalendarRange,
	"business-analyst": BriefcaseBusiness,
	custom: Settings,
};

function CreateAgentDialog({
	projectId,
	open,
	onOpenChange,
}: {
	projectId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation();
	const qc = useQueryClient();
	const { data: roles = [] } = useQuery(projectRolesQueryOptions(projectId));
	const { data: llmModels = {} } = useQuery(llmModelsQueryOptions);

	const [step, setStep] = useState<1 | 2>(1);
	const [name, setName] = useState("");
	const [handle, setHandle] = useState("");
	const [presetId, setPresetId] = useState("");
	const [roleId, setRoleId] = useState("");
	const [providerSelect, setProviderSelect] = useState("anthropic");
	const [customProvider, setCustomProvider] = useState("");
	const [modelSelect, setModelSelect] = useState("claude-sonnet-4-5-20250929");
	const [customModel, setCustomModel] = useState("");
	const [llmApiKey, setLlmApiKey] = useState("");
	const [llmBaseUrl, setLlmBaseUrl] = useState(
		llmModels.anthropic?.base_url ?? "",
	);
	const [systemPrompt, setSystemPrompt] = useState("");
	const [showApiKey, setShowApiKey] = useState(false);

	// Derived final values sent to the API
	const llmProvider =
		providerSelect === CUSTOM ? customProvider.trim() : providerSelect;
	const llmModel = modelSelect === CUSTOM ? customModel.trim() : modelSelect;

	const reset = () => {
		setStep(1);
		setName("");
		setHandle("");
		setPresetId("");
		setRoleId("");
		setProviderSelect("anthropic");
		setCustomProvider("");
		setModelSelect("claude-sonnet-4-5-20250929");
		setCustomModel("");
		setLlmApiKey("");
		setLlmBaseUrl(llmModels.anthropic?.base_url ?? "");
		setSystemPrompt("");
		setShowApiKey(false);
	};

	const handleClose = (v: boolean) => {
		if (!v) reset();
		onOpenChange(v);
	};

	const handleProviderChange = (v: string | null) => {
		if (!v) return;
		setProviderSelect(v);
		if (v !== CUSTOM) {
			const info = llmModels[v];
			setLlmBaseUrl(info?.base_url ?? "");
			const firstModel = info?.models?.[0] ?? "";
			setModelSelect(firstModel || CUSTOM);
			if (!firstModel) setCustomModel("");
		} else {
			setModelSelect(CUSTOM);
			setCustomModel("");
		}
	};

	const onPresetChange = (id: string) => {
		setPresetId(id);
		const preset = AGENT_PRESETS.find((p) => p.id === id);
		if (preset) {
			if (preset.defaultLLMProvider) {
				setProviderSelect(preset.defaultLLMProvider);
				setLlmBaseUrl(llmModels[preset.defaultLLMProvider]?.base_url ?? "");
			}
			if (preset.defaultLLMModel) setModelSelect(preset.defaultLLMModel);
			if (preset.defaultSystemPrompt)
				setSystemPrompt(preset.defaultSystemPrompt);
		}
	};

	const providers = Object.keys(llmModels);
	const availableModels: string[] =
		providerSelect !== CUSTOM ? (llmModels[providerSelect]?.models ?? []) : [];

	const createMutation = useMutation({
		mutationFn: () =>
			createAgent(projectId, {
				name: name.trim(),
				handle: handle.trim(),
				llm_provider: llmProvider,
				llm_model: llmModel,
				llm_api_key: llmApiKey,
				llm_base_url: llmBaseUrl,
				system_prompt: systemPrompt,
				task_trigger_prompt: TRIGGER_PROMPTS.task,
				doc_comment_trigger_prompt: TRIGGER_PROMPTS.docComment,
				chat_trigger_prompt: TRIGGER_PROMPTS.chat,
				description_write_trigger_prompt: TRIGGER_PROMPTS.descriptionWrite,
				project_role_id: roleId,
			}),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "agents"],
			});
			handleClose(false);
		},
	});

	const onNameChange = (v: string) => {
		setName(v);
		setHandle(
			v
				.toLowerCase()
				.replace(/[^a-z0-9]+/g, "-")
				.replace(/^-+|-+$/g, ""),
		);
	};

	const step1Valid = !!(name.trim() && handle.trim() && roleId);
	const canSubmit = !!(
		step1Valid &&
		llmProvider &&
		llmModel &&
		llmBaseUrl.trim() &&
		llmApiKey.trim() &&
		!createMutation.isPending
	);

	return (
		<Dialog open={open} onOpenChange={handleClose}>
			<DialogContent className="sm:max-w-2xl p-0 gap-0 overflow-hidden">
				{/* Gradient header */}
				<div className="relative overflow-hidden border-b border-border/50">
					<div
						className="pointer-events-none absolute inset-0 opacity-[0.35]"
						style={{
							backgroundImage:
								"radial-gradient(circle, color-mix(in oklch, var(--color-primary) 14%, transparent) 1px, transparent 1px)",
							backgroundSize: "16px 16px",
						}}
					/>
					<div className="relative flex items-center justify-between px-6 pt-5 pb-4">
						<div className="flex items-center gap-3">
							<div className="flex size-10 items-center justify-center rounded-xl bg-primary/10 ring-1 ring-primary/20 shadow-sm">
								<Bot className="size-5 text-primary" />
							</div>
							<div>
								<DialogTitle className="text-sm font-semibold">
									{t("project.agents.list.createAgent")}
								</DialogTitle>
								<DialogDescription className="text-xs text-muted-foreground mt-0.5">
									{step === 1
										? t("project.agents.list.step1Desc")
										: t("project.agents.list.step2Desc")}
								</DialogDescription>
							</div>
						</div>
						{/* Step pill */}
						<div className="flex items-center gap-1 rounded-full border border-border/60 bg-muted/50 px-2.5 py-1">
							<div className="flex items-center gap-1">
								<span
									className={cn(
										"size-1.5 rounded-full transition-colors duration-200",
										step >= 1 ? "bg-primary" : "bg-muted-foreground/30",
									)}
								/>
								<span
									className={cn(
										"size-1.5 rounded-full transition-colors duration-200",
										step >= 2 ? "bg-primary" : "bg-muted-foreground/30",
									)}
								/>
							</div>
							<span className="text-xs text-muted-foreground font-medium ml-1">
								{step} / 2
							</span>
						</div>
					</div>
				</div>

				{/* ── Step 1: Identity ─────────────────────────────────────────── */}
				{step === 1 && (
					<div className="overflow-y-auto max-h-[62vh] px-6 py-5 space-y-5">
						{/* Preset grid */}
						<div className="space-y-2">
							<Label className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
								{t("project.agents.list.startFromPreset")}
							</Label>
							<div className="grid grid-cols-2 gap-2">
								{AGENT_PRESETS.map((preset) => {
									const Icon = PRESET_ICON_MAP[preset.id] ?? Bot;
									const isSelected = presetId === preset.id;
									return (
										<button
											key={preset.id}
											type="button"
											onClick={() => onPresetChange(preset.id)}
											className={cn(
												"flex items-start gap-2.5 rounded-lg border p-3 text-left transition-all",
												isSelected
													? "border-primary/40 bg-primary/5 ring-1 ring-primary/20 shadow-sm"
													: "border-border/60 hover:border-border hover:bg-muted/30",
											)}
										>
											<div
												className={cn(
													"flex size-7 shrink-0 items-center justify-center rounded-md transition-colors",
													isSelected
														? "bg-primary/15 text-primary"
														: "bg-muted text-muted-foreground",
												)}
											>
												<Icon className="size-3.5" />
											</div>
											<div className="min-w-0">
												<p className="text-xs font-semibold leading-tight">
													{preset.label}
												</p>
												<p className="mt-0.5 line-clamp-2 text-xs leading-tight text-muted-foreground">
													{preset.description}
												</p>
											</div>
										</button>
									);
								})}
							</div>
						</div>

						{/* Name + Handle */}
						<div className="space-y-3">
							<div className="space-y-1.5">
								<Label htmlFor="agent-name">
									{t("project.agents.name")}{" "}
									<span className="text-destructive">*</span>
								</Label>
								<Input
									id="agent-name"
									placeholder={t("project.agents.list.namePlaceholder")}
									value={name}
									onChange={(e) => onNameChange(e.target.value)}
									autoFocus
								/>
							</div>
							<div className="space-y-1.5">
								<Label htmlFor="agent-handle">
									{t("project.agents.list.handle")}{" "}
									<span className="text-destructive">*</span>
								</Label>
								<div className="relative">
									<span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground select-none">
										@
									</span>
									<Input
										id="agent-handle"
										placeholder={t("project.agents.list.handlePlaceholder")}
										value={handle}
										onChange={(e) => setHandle(e.target.value)}
										className="pl-7"
									/>
								</div>
								<p className="text-xs text-muted-foreground">
									{t("project.agents.list.handleHint")}
								</p>
							</div>
						</div>

						{/* Project Role */}
						<div className="space-y-1.5">
							<Label>
								{t("project.agents.list.projectRole")}{" "}
								<span className="text-destructive">*</span>
							</Label>
							<Select value={roleId} onValueChange={(v) => v && setRoleId(v)}>
								<SelectTrigger>
									<SelectValue
										placeholder={t("project.agents.list.selectProjectRole")}
									>
										{roles.find((r) => r.id === roleId)?.role_name}
									</SelectValue>
								</SelectTrigger>
								<SelectContent>
									{roles.map((r) => (
										<SelectItem key={r.id} value={r.id}>
											{r.role_name}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
							<p className="text-xs text-muted-foreground">
								{t("project.agents.list.projectRoleHint")}
							</p>
						</div>
					</div>
				)}

				{/* ── Step 2: AI Configuration ─────────────────────────────────── */}
				{step === 2 && (
					<div className="overflow-y-auto max-h-[62vh] px-6 py-5 space-y-5">
						{/* Provider + Model card */}
						<div className="rounded-lg border border-border/60 bg-muted/20 p-4 space-y-3">
							<div className="flex items-center gap-1.5">
								<Cpu className="size-3.5 text-muted-foreground" />
								<span className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
									{t("project.agents.model")}
								</span>
							</div>
							<div className="grid grid-cols-2 gap-3">
								<div className="space-y-1.5">
									<Label>{t("project.agents.provider")}</Label>
									<Select
										value={providerSelect}
										onValueChange={handleProviderChange}
									>
										<SelectTrigger>
											<SelectValue />
										</SelectTrigger>
										<SelectContent>
											{providers.map((p) => (
												<SelectItem key={p} value={p}>
													{p}
												</SelectItem>
											))}
											<SelectSeparator />
											<SelectItem value={CUSTOM}>{t("project.agents.custom")}</SelectItem>
										</SelectContent>
									</Select>
									{providerSelect === CUSTOM && (
										<Input
											placeholder={t("project.agents.providerPlaceholder")}
											value={customProvider}
											onChange={(e) => setCustomProvider(e.target.value)}
										/>
									)}
								</div>
								<div className="space-y-1.5">
									<Label>{t("project.agents.model")}</Label>
									{providerSelect === CUSTOM ? (
										<Input
											placeholder={t("project.agents.modelPlaceholder")}
											value={customModel}
											onChange={(e) => setCustomModel(e.target.value)}
										/>
									) : (
										<>
											<Select
												value={modelSelect}
												onValueChange={(v) => v && setModelSelect(v)}
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
													<SelectSeparator />
													<SelectItem value={CUSTOM}>{t("project.agents.custom")}</SelectItem>
												</SelectContent>
											</Select>
											{modelSelect === CUSTOM && (
												<Input
													placeholder={t("project.agents.modelPlaceholder")}
													value={customModel}
													onChange={(e) => setCustomModel(e.target.value)}
												/>
											)}
										</>
									)}
								</div>
							</div>
						</div>

						{/* API Key */}
						<div className="space-y-1.5">
							<Label htmlFor="agent-api-key">
								<span className="flex items-center gap-1.5">
									<Lock className="size-3 text-muted-foreground" />
									{t("project.agents.list.apiKey")}{" "}
									<span className="text-destructive">*</span>
								</span>
							</Label>
							<div className="relative">
								<Input
									id="agent-api-key"
									type={showApiKey ? "text" : "password"}
									placeholder={
										llmProvider === "anthropic" ? "sk-ant-…" : "sk-…"
									}
									value={llmApiKey}
									onChange={(e) => setLlmApiKey(e.target.value)}
									className="pr-9"
								/>
								<button
									type="button"
									onClick={() => setShowApiKey((s) => !s)}
									className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground/60 hover:text-foreground transition-colors"
									aria-label={
										showApiKey
											? t("project.agents.list.hideApiKey")
											: t("project.agents.list.showApiKey")
									}
								>
									{showApiKey ? (
										<EyeOff className="size-4" />
									) : (
										<Eye className="size-4" />
									)}
								</button>
							</div>
							<p className="text-xs text-muted-foreground flex items-center gap-1.5">
								<span className="size-1.5 shrink-0 rounded-full bg-emerald-500 inline-block" />
								{t("project.agents.list.apiKeyHint")}
							</p>
						</div>

						{/* Base URL */}
						<div className="space-y-1.5">
							<Label htmlFor="agent-base-url">
								{t("project.agents.baseUrl")}{" "}
								<span className="text-destructive">*</span>
							</Label>
							<Input
								id="agent-base-url"
								placeholder="https://api.openai.com/v1"
								value={llmBaseUrl}
								onChange={(e) => setLlmBaseUrl(e.target.value)}
							/>
						</div>

						{/* System Prompt */}
						<div className="space-y-1.5">
							<div className="flex items-center justify-between">
								<Label htmlFor="agent-system-prompt">
									{t("project.agents.systemPrompt")}{" "}
									<span className="text-muted-foreground font-normal text-xs">
										{t("project.agents.list.optional")}
									</span>
								</Label>
								{systemPrompt.length > 0 && (
									<span className="text-xs text-muted-foreground">
										{t("project.agents.list.chars", {
											n: systemPrompt.length,
										})}
									</span>
								)}
							</div>
							<Textarea
								id="agent-system-prompt"
								placeholder={t(
									"project.agents.list.systemPromptPlaceholder",
								)}
								value={systemPrompt}
								onChange={(e) => setSystemPrompt(e.target.value)}
								rows={4}
								className="resize-none text-sm"
							/>
						</div>

						{createMutation.isError && (
							<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
								{t("project.agents.list.createError")}
							</p>
						)}
					</div>
				)}
				{/* Footer */}
				<div className="border-t border-border/50 bg-muted/20 px-6 py-4 flex items-center justify-between">
					{step === 1 ? (
						<>
							<Button
								variant="ghost"
								size="sm"
								onClick={() => handleClose(false)}
								className="text-muted-foreground"
							>
								{t("project.agents.cancel")}
							</Button>
							<Button
								size="sm"
								onClick={() => setStep(2)}
								disabled={!step1Valid}
							>
								{t("project.agents.list.continue")}
								<ChevronRight className="size-4 ml-1" />
							</Button>
						</>
					) : (
						<>
							<Button
								variant="ghost"
								size="sm"
								onClick={() => setStep(1)}
								className="text-muted-foreground"
							>
								<ChevronLeft className="size-4 mr-1" />
								{t("project.agents.list.back")}
							</Button>
							<Button
								size="sm"
								onClick={() => createMutation.mutate()}
								disabled={!canSubmit}
							>
								{createMutation.isPending ? (
									<>
										<Loader2 className="size-4 mr-1.5 animate-spin" />
										{t("project.agents.list.creating")}
									</>
								) : (
									<>
										<Sparkles className="size-4 mr-1.5" />
										{t("project.agents.list.createAgentButton")}
									</>
								)}
							</Button>
						</>
					)}
				</div>
			</DialogContent>
		</Dialog>
	);
}

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
	const { t } = useTranslation();
	const qc = useQueryClient();
	const [confirmDelete, setConfirmDelete] = useState(false);

	const deleteMutation = useMutation({
		mutationFn: () => deleteAgent(projectId, agent.id),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: ["projects", projectId, "agents"] });
			setConfirmDelete(false);
		},
	});

	const initials = agent.name
		.split(" ")
		.map((w) => w[0])
		.join("")
		.toUpperCase()
		.slice(0, 2);

	return (
		<>
			<div className="group relative flex flex-col gap-3 rounded-xl border border-border/60 bg-card p-5 transition-all hover:border-border hover:shadow-sm">
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
							{agent.llm_provider}
						</Badge>
						{canWrite && (
							<DropdownMenu>
								<DropdownMenuTrigger className="inline-flex size-7 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover:opacity-100">
									<MoreHorizontal className="size-4" />
								</DropdownMenuTrigger>
								<DropdownMenuContent align="end" className="w-40">
									<DropdownMenuItem
										render={
											<Link
												to="/projects/$projectId/agents/$agentId"
												params={{ projectId, agentId: agent.id }}
											/>
										}
									>
										<Settings className="size-3.5 mr-2" />
										{t("project.agents.list.configure")}
									</DropdownMenuItem>
									<DropdownMenuSeparator />
									<DropdownMenuItem
										className="text-destructive focus:text-destructive"
										onClick={() => setConfirmDelete(true)}
									>
										<Trash2 className="size-3.5 mr-2" />
										{t("project.agents.delete")}
									</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>
						)}
					</div>
				</div>

				{/* Stats row */}
				<div className="flex items-center gap-4 text-xs text-muted-foreground">
					<span className="flex items-center gap-1">
						<Zap className="size-3" />
						{agent.llm_provider}/{agent.llm_model}
					</span>
					{agent.can_clone_repos && (
						<span className="flex items-center gap-1">
							<GitBranch className="size-3" />
							{t("project.agents.list.repos")}
						</span>
					)}
				</div>

				{/* Footer link */}
				<Link
					to="/projects/$projectId/agents/$agentId"
					params={{ projectId, agentId: agent.id }}
					className="mt-1 flex items-center gap-1 text-xs font-medium text-primary hover:underline"
				>
					{t("project.agents.list.configureViewActivity")}
					<ChevronRight className="size-3" />
				</Link>
			</div>

			{/* Delete confirmation */}
			<Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
				<DialogContent className="max-w-sm">
					<DialogHeader>
						<DialogTitle>
							{t("project.agents.list.deleteTitle", { name: agent.name })}
						</DialogTitle>
						<DialogDescription>
							{t("project.agents.list.deleteDesc")}
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => setConfirmDelete(false)}
							disabled={deleteMutation.isPending}
						>
							{t("project.agents.cancel")}
						</Button>
						<Button
							variant="destructive"
							onClick={() => deleteMutation.mutate()}
							disabled={deleteMutation.isPending}
						>
							{deleteMutation.isPending ? (
								<Loader2 className="size-4 animate-spin" />
							) : (
								t("project.agents.delete")
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
	const { t } = useTranslation();
	const { projectId } = Route.useParams();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("agents.write");

	const { data: agents = [], isLoading } = useQuery(
		agentsQueryOptions(projectId),
	);
	const [createOpen, setCreateOpen] = useState(false);

	return (
		<div className="flex flex-col flex-1 min-h-0">
			{/* Header */}
			<div className="relative overflow-hidden border-b border-border/50 shrink-0">
				<div
					className="pointer-events-none absolute inset-0 opacity-40"
					style={{
						backgroundImage:
							"radial-gradient(circle, color-mix(in oklch, var(--color-primary) 10%, transparent) 1px, transparent 1px)",
						backgroundSize: "20px 20px",
					}}
				/>
				<div className="relative flex items-center justify-between px-6 py-5">
					<div className="flex items-center gap-3">
						<div className="flex size-9 items-center justify-center rounded-lg bg-primary/10">
							<Sparkles className="size-4 text-primary" />
						</div>
						<div>
							<h1 className="text-lg font-semibold">
								{t("project.agents.list.title")}
							</h1>
							<p className="text-xs text-muted-foreground mt-0.5">
								{t("project.agents.list.subtitle")}
							</p>
						</div>
					</div>
					{canWrite && (
						<Button size="sm" onClick={() => setCreateOpen(true)}>
							<Plus className="size-4 mr-1.5" />
							{t("project.agents.list.newAgent")}
						</Button>
					)}
				</div>
			</div>

			{/* Content */}
			<div className="flex-1 overflow-auto p-6">
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
								{t("project.agents.list.empty")}
							</p>
							<p className="text-xs text-muted-foreground mt-1 max-w-xs">
								{t("project.agents.list.emptyDesc")}
							</p>
						</div>
						{canWrite && (
							<Button size="sm" onClick={() => setCreateOpen(true)}>
								<Plus className="size-4 mr-1.5" />
								{t("project.agents.list.createFirst")}
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

			{/* Chat shortcut section */}
			{agents.length > 0 && (
				<div className="border-t border-border/50 px-6 py-4 shrink-0">
					<p className="text-xs text-muted-foreground flex items-center gap-1.5">
						<MessageSquare className="size-3.5" />
						<Trans
							i18nKey="project.agents.list.chatShortcut"
							components={{ strong: <strong /> }}
						/>
					</p>
				</div>
			)}

			<CreateAgentDialog
				projectId={projectId}
				open={createOpen}
				onOpenChange={setCreateOpen}
			/>
		</div>
	);
}
