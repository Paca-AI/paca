import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
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
	Loader2,
	Lock,
	MoreHorizontal,
	Plus,
	Search,
	Settings,
	Sparkles,
	Trash2,
	Zap,
} from "lucide-react";
import { type ComponentType, useState } from "react";
import { useTranslation } from "react-i18next";

import { AcpBridgeSetup } from "@/components/projects/agents/acp-bridge-setup";
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
	type ACPProvider,
	type AcpBridgeToken,
	AGENT_PRESETS,
	type Agent,
	type AgentType,
	acpBridgeStatusQueryOptions,
	agentsQueryOptions,
	createAgent,
	deleteAgent,
	generateAcpBridgeToken,
	llmModelsQueryOptions,
} from "@/lib/agent-api";
import {
	projectQueryOptions,
	projectRolesQueryOptions,
} from "@/lib/project-api";
import { splitShellCommand } from "@/lib/shell-command";
import { cn } from "@/lib/utils";

const CUSTOM = "__custom__";

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
	onAcpAgentCreated,
}: {
	projectId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onAcpAgentCreated: (agent: Agent, token: AcpBridgeToken | null) => void;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { data: roles = [] } = useQuery(projectRolesQueryOptions(projectId));
	const { data: llmModels = {} } = useQuery(llmModelsQueryOptions);

	const [step, setStep] = useState<1 | 2>(1);
	const [name, setName] = useState("");
	const [handle, setHandle] = useState("");
	const [presetId, setPresetId] = useState("");
	const [roleId, setRoleId] = useState("");
	const [agentType, setAgentType] = useState<AgentType>("llm");
	const [providerSelect, setProviderSelect] = useState("anthropic");
	const [customProvider, setCustomProvider] = useState("");
	const [modelSelect, setModelSelect] = useState("claude-sonnet-4-5-20250929");
	const [customModel, setCustomModel] = useState("");
	const [llmApiKey, setLlmApiKey] = useState("");
	const [llmBaseUrl, setLlmBaseUrl] = useState(
		llmModels.anthropic?.base_url ?? "",
	);
	const [acpProvider, setAcpProvider] = useState<ACPProvider>("claude-code");
	const [acpCommand, setAcpCommand] = useState("");
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
		setAgentType("llm");
		setProviderSelect("anthropic");
		setCustomProvider("");
		setModelSelect("claude-sonnet-4-5-20250929");
		setCustomModel("");
		setLlmApiKey("");
		setLlmBaseUrl(llmModels.anthropic?.base_url ?? "");
		setAcpProvider("claude-code");
		setAcpCommand("");
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

	const acpCommandParts = splitShellCommand(acpCommand);

	const createMutation = useMutation({
		mutationFn: async () => {
			const agent = await createAgent(projectId, {
				name: name.trim(),
				handle: handle.trim(),
				agent_type: agentType,
				project_role_id: roleId,
				...(agentType === "llm"
					? {
							llm_provider: llmProvider,
							llm_model: llmModel,
							llm_api_key: llmApiKey,
							llm_base_url: llmBaseUrl,
						}
					: {
							acp_provider: acpProvider,
							...(acpProvider === "custom"
								? { acp_command: acpCommandParts }
								: {}),
						}),
				system_prompt: systemPrompt,
			});
			if (agent.agent_type !== "acp") {
				return { agent, token: null };
			}
			// Generate the bridge token as part of the same "Creating…" spinner
			// instead of a separate loading step inside the setup dialog — the
			// dialog can then just display an already-resolved result. If this
			// sub-step fails, the agent still exists; fall through with
			// token: null so the setup dialog offers a manual retry instead of
			// failing the whole creation.
			try {
				const token = await generateAcpBridgeToken(projectId, agent.id);
				return { agent, token };
			} catch {
				return { agent, token: null };
			}
		},
		onSuccess: ({ agent, token }) => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "agents"],
			});
			handleClose(false);
			if (agent.agent_type === "acp") {
				onAcpAgentCreated(agent, token);
			}
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
	const step2Valid =
		agentType === "llm"
			? !!(llmProvider && llmModel && llmBaseUrl.trim() && llmApiKey.trim())
			: !!(
					acpProvider &&
					(acpProvider !== "custom" || acpCommandParts.length > 0)
				);
	const canSubmit = !!(step1Valid && step2Valid && !createMutation.isPending);

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
									{t("agents.createDialog.title")}
								</DialogTitle>
								<DialogDescription className="text-xs text-muted-foreground mt-0.5">
									{step === 1
										? t("agents.createDialog.step1Description")
										: t("agents.createDialog.step2Description")}
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
								{t("agents.createDialog.stepIndicator", { step })}
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
								{t("agents.createDialog.startFromPreset")}
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
									{t("agents.createDialog.nameLabel")}{" "}
									<span className="text-destructive">*</span>
								</Label>
								<Input
									id="agent-name"
									placeholder={t("agents.createDialog.namePlaceholder")}
									value={name}
									onChange={(e) => onNameChange(e.target.value)}
									autoFocus
								/>
							</div>
							<div className="space-y-1.5">
								<Label htmlFor="agent-handle">
									{t("agents.createDialog.handleLabel")}{" "}
									<span className="text-destructive">*</span>
								</Label>
								<div className="relative">
									<span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-sm text-muted-foreground select-none">
										@
									</span>
									<Input
										id="agent-handle"
										placeholder="dev-bot"
										value={handle}
										onChange={(e) => setHandle(e.target.value)}
										className="pl-7"
									/>
								</div>
								<p className="text-xs text-muted-foreground">
									{t("agents.createDialog.handleHint")}
								</p>
							</div>
						</div>

						{/* Project Role */}
						<div className="space-y-1.5">
							<Label>
								{t("agents.createDialog.projectRoleLabel")}{" "}
								<span className="text-destructive">*</span>
							</Label>
							<Select value={roleId} onValueChange={(v) => v && setRoleId(v)}>
								<SelectTrigger>
									<SelectValue
										placeholder={t(
											"agents.createDialog.projectRolePlaceholder",
										)}
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
								{t("agents.createDialog.projectRoleHint")}
							</p>
						</div>
					</div>
				)}

				{/* ── Step 2: AI Configuration ─────────────────────────────────── */}
				{step === 2 && (
					<div className="overflow-y-auto max-h-[62vh] px-6 py-5 space-y-5">
						{/* Agent Type */}
						<div className="space-y-1.5">
							<Label>{t("agents.createDialog.agentTypeLabel")}</Label>
							<div className="grid grid-cols-2 gap-2">
								{(["llm", "acp"] as const).map((type) => {
									const isSelected = agentType === type;
									return (
										<button
											key={type}
											type="button"
											onClick={() => setAgentType(type)}
											className={cn(
												"rounded-lg border p-3 text-left transition-all",
												isSelected
													? "border-primary/40 bg-primary/5 ring-1 ring-primary/20 shadow-sm"
													: "border-border/60 hover:border-border hover:bg-muted/30",
											)}
										>
											<p className="text-xs font-semibold leading-tight">
												{t(
													type === "llm"
														? "agents.createDialog.agentTypeLLM"
														: "agents.createDialog.agentTypeACP",
												)}
											</p>
											<p className="mt-0.5 text-xs leading-tight text-muted-foreground">
												{t(
													type === "llm"
														? "agents.createDialog.agentTypeLLMHint"
														: "agents.createDialog.agentTypeACPHint",
												)}
											</p>
										</button>
									);
								})}
							</div>
						</div>

						{agentType === "acp" && (
							<div className="rounded-lg border border-border/60 bg-muted/20 p-4 space-y-3">
								<div className="flex items-center gap-1.5">
									<Cpu className="size-3.5 text-muted-foreground" />
									<span className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
										{t("agents.createDialog.acpSection")}
									</span>
								</div>
								<div className="space-y-1.5">
									<Label>{t("agents.createDialog.acpProviderLabel")}</Label>
									<Select
										value={acpProvider}
										onValueChange={(v) => v && setAcpProvider(v as ACPProvider)}
									>
										<SelectTrigger>
											<SelectValue />
										</SelectTrigger>
										<SelectContent>
											<SelectItem value="claude-code">
												{t("agents.createDialog.acpProviderClaudeCode")}
											</SelectItem>
											<SelectItem value="codex">
												{t("agents.createDialog.acpProviderCodex")}
											</SelectItem>
											<SelectItem value="gemini-cli">
												{t("agents.createDialog.acpProviderGeminiCli")}
											</SelectItem>
											<SelectSeparator />
											<SelectItem value="custom">
												{t("agents.createDialog.acpProviderCustom")}
											</SelectItem>
										</SelectContent>
									</Select>
								</div>
								{acpProvider === "custom" && (
									<div className="space-y-1.5">
										<Label htmlFor="agent-acp-command">
											{t("agents.createDialog.acpCommandLabel")}
										</Label>
										<Input
											id="agent-acp-command"
											placeholder={t(
												"agents.createDialog.acpCommandPlaceholder",
											)}
											value={acpCommand}
											onChange={(e) => setAcpCommand(e.target.value)}
										/>
										<p className="text-xs text-muted-foreground">
											{t("agents.createDialog.acpCommandHint")}
										</p>
									</div>
								)}
								<p className="text-xs text-muted-foreground rounded-md bg-muted/40 px-3 py-2">
									{t("agents.createDialog.acpGuidance")}
								</p>
							</div>
						)}

						{/* Provider + Model card */}
						{agentType === "llm" && (
							<>
								<div className="rounded-lg border border-border/60 bg-muted/20 p-4 space-y-3">
									<div className="flex items-center gap-1.5">
										<Cpu className="size-3.5 text-muted-foreground" />
										<span className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
											{t("agents.createDialog.modelSection")}
										</span>
									</div>
									<div className="grid grid-cols-2 gap-3">
										<div className="space-y-1.5">
											<Label>{t("agents.createDialog.providerLabel")}</Label>
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
													<SelectItem value={CUSTOM}>
														{t("agents.createDialog.customOption")}
													</SelectItem>
												</SelectContent>
											</Select>
											{providerSelect === CUSTOM && (
												<Input
													placeholder="my-provider"
													value={customProvider}
													onChange={(e) => setCustomProvider(e.target.value)}
												/>
											)}
										</div>
										<div className="space-y-1.5">
											<Label>{t("agents.createDialog.modelLabel")}</Label>
											{providerSelect === CUSTOM ? (
												<Input
													placeholder="my-model-name"
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
															<SelectItem value={CUSTOM}>
																{t("agents.createDialog.customOption")}
															</SelectItem>
														</SelectContent>
													</Select>
													{modelSelect === CUSTOM && (
														<Input
															placeholder="my-model-name"
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
											{t("agents.createDialog.apiKeyLabel")}{" "}
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
													? t("agents.createDialog.hideApiKey")
													: t("agents.createDialog.showApiKey")
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
										{t("agents.createDialog.apiKeyHint")}
									</p>
								</div>

								{/* Base URL */}
								<div className="space-y-1.5">
									<Label htmlFor="agent-base-url">
										{t("agents.createDialog.baseUrlLabel")}{" "}
										<span className="text-destructive">*</span>
									</Label>
									<Input
										id="agent-base-url"
										placeholder="https://api.openai.com/v1"
										value={llmBaseUrl}
										onChange={(e) => setLlmBaseUrl(e.target.value)}
									/>
								</div>
							</>
						)}

						{/* System Prompt */}
						<div className="space-y-1.5">
							<div className="flex items-center justify-between">
								<Label htmlFor="agent-system-prompt">
									{t("agents.createDialog.systemPromptLabel")}{" "}
									<span className="text-muted-foreground font-normal text-xs">
										{t("agents.createDialog.optional")}
									</span>
								</Label>
								{systemPrompt.length > 0 && (
									<span className="text-xs text-muted-foreground">
										{t("agents.createDialog.charsCount", {
											count: systemPrompt.length,
										})}
									</span>
								)}
							</div>
							<Textarea
								id="agent-system-prompt"
								placeholder={t("agents.createDialog.systemPromptPlaceholder")}
								value={systemPrompt}
								onChange={(e) => setSystemPrompt(e.target.value)}
								rows={4}
								className="resize-none text-sm"
							/>
						</div>

						{createMutation.isError && (
							<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
								{t("agents.createDialog.createFailed")}
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
								{t("agents.createDialog.cancel")}
							</Button>
							<Button
								size="sm"
								onClick={() => setStep(2)}
								disabled={!step1Valid}
							>
								{t("agents.createDialog.continue")}
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
								{t("agents.createDialog.back")}
							</Button>
							<Button
								size="sm"
								onClick={() => createMutation.mutate()}
								disabled={!canSubmit}
							>
								{createMutation.isPending ? (
									<>
										<Loader2 className="size-4 mr-1.5 animate-spin" />
										{t("agents.createDialog.creating")}
									</>
								) : (
									<>
										<Sparkles className="size-4 mr-1.5" />
										{t("agents.createDialog.createAgent")}
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

// ── ACP Setup Dialog ─────────────────────────────────────────────────────────

// Shown right after a new ACP agent is created — walks the user through
// generating a bridge token and connecting the local bridge, skill, and MCP
// server, instead of leaving them to discover the Local Bridge panel on the
// agent's detail page on their own.
function AcpSetupDialog({
	projectId,
	agent,
	token,
	open,
	canWrite,
	onOpenChange,
	onTokenGenerated,
}: {
	projectId: string;
	agent: Agent | null;
	token: AcpBridgeToken | null;
	open: boolean;
	canWrite: boolean;
	onOpenChange: (open: boolean) => void;
	onTokenGenerated: () => void;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();

	if (!agent) return null;

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-lg">
				<DialogHeader>
					<DialogTitle>{t("agents.acpSetup.dialogTitle")}</DialogTitle>
					<DialogDescription>
						{t("agents.acpSetup.dialogDescription", { name: agent.name })}
					</DialogDescription>
				</DialogHeader>
				<div className="overflow-y-auto max-h-[60vh] pr-1 -mr-1">
					<AcpBridgeSetup
						projectId={projectId}
						agentId={agent.id}
						acpProvider={agent.acp_provider ?? "claude-code"}
						hasToken={agent.has_acp_bridge_token || token !== null}
						canWrite={canWrite}
						initialToken={token}
						onTokenGenerated={() => {
							qc.invalidateQueries({
								queryKey: ["projects", projectId, "agents"],
							});
							onTokenGenerated();
						}}
					/>
				</div>
				<DialogFooter>
					<Button onClick={() => onOpenChange(false)}>
						{t("agents.acpSetup.done")}
					</Button>
				</DialogFooter>
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
