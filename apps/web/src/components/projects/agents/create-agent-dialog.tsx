import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
	Bot,
	BriefcaseBusiness,
	CalendarRange,
	ChevronLeft,
	ChevronRight,
	Code2,
	Cpu,
	ExternalLink,
	Eye,
	EyeOff,
	FlaskConical,
	Loader2,
	Lock,
	Search,
	Server,
	Settings,
	Sparkles,
} from "lucide-react";
import { type ComponentType, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	DefaultEnvironmentSelect,
	DefaultFolderSelect,
} from "@/components/projects/environments/environment-folder-select";
import { Button, buttonVariants } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
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
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { globalRolesQueryOptions } from "@/lib/admin-api";
import {
	type ACPProvider,
	type AcpBridgeToken,
	AGENT_PRESETS,
	type Agent,
	type AgentType,
	type CLIProvider,
	createAgent,
	createGlobalAgent,
	generateAcpBridgeToken,
	generateAgentMCPKey,
	generateGlobalAcpBridgeToken,
	generateGlobalAgentMCPKey,
	globalAgentsQueryOptions,
	llmModelsQueryOptions,
	verifyEnvironmentCLILogin,
} from "@/lib/agent-api";
import { environmentsQueryOptions } from "@/lib/environment-api";
import { projectRolesQueryOptions } from "@/lib/project-api";
import { splitShellCommand } from "@/lib/shell-command";
import { cn } from "@/lib/utils";
import { AcpBridgeSetup, CommandBox } from "./acp-bridge-setup";

// Create Agent Dialog — shared between the project-scoped Agents page
// (routes/_authenticated/projects/$projectId/agents/index.tsx) and the
// global one (routes/_authenticated/admin/agents/index.tsx). Same 2-step
// wizard, presets, agent type toggle, and LLM/ACP configuration either way —
// the only real difference is the role picker in step 1 (a required project
// role vs an optional global role) and which create/token-generation
// endpoints get called underneath. See conversations-layout.tsx for the
// equivalent generalization applied to the Conversations page.

const CUSTOM = "__custom__";
const NO_GLOBAL_ROLE = "__none__";

const PRESET_ICON_MAP: Record<string, ComponentType<{ className?: string }>> = {
	"software-engineer": Code2,
	"code-reviewer": Search,
	"qa-engineer": FlaskConical,
	planner: CalendarRange,
	"business-analyst": BriefcaseBusiness,
	custom: Settings,
};

// cliLoginCommand is the exact command to run inside the agent's default
// environment terminal to authenticate a provider_cli agent's underlying
// CLI — api_key auth isn't offered for this agent type here (it's coming to
// the "normal" llm agent type instead), so terminal login is the only path,
// and showing the real command directly (rather than a vague "log in via
// the terminal" hint) is what makes the "Verify login" button below usable
// without leaving this dialog. Confirmed directly against a real container
// for claude-code/codex/cursor-agent (see services/agent-runner's
// internal/executor/providercli package, each adapter's own AuthStatusCommand
// doc comment for the verification side of this); gemini-cli has no
// confirmed dedicated login subcommand (`gemini --help` shows none), so this
// falls back to the bare command, which triggers its interactive OAuth flow
// on first run.
function cliLoginCommand(provider: CLIProvider): string {
	switch (provider) {
		case "codex":
			return "codex login";
		case "cursor-agent":
			return "cursor-agent login";
		case "gemini-cli":
			return "gemini";
		default:
			return "claude auth login";
	}
}

export function CreateAgentDialog({
	projectId,
	open,
	onOpenChange,
	onAcpAgentCreated,
}: {
	/** Absent when creating a global agent (no project of its own). */
	projectId?: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onAcpAgentCreated: (
		agent: Agent,
		token: AcpBridgeToken | null,
		mcpKey: string | null,
	) => void;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { data: projectRoles = [] } = useQuery({
		...projectRolesQueryOptions(projectId ?? ""),
		enabled: !!projectId,
	});
	const { data: globalRoles = [] } = useQuery({
		...globalRolesQueryOptions,
		enabled: !projectId,
	});
	const { data: llmModels = {} } = useQuery(llmModelsQueryOptions);
	// Environments are project-scoped only — a global agent has no project to
	// default one from, so this query no-ops (via `enabled`) at global scope,
	// same convention as agent-picker.tsx's useEnvironmentPicker.
	const { data: environments = [] } = useQuery({
		...environmentsQueryOptions(projectId ?? ""),
		enabled: !!projectId,
	});

	// Uniform {id, label} shape either way, so the role Select below is one
	// JSX block regardless of scope. A global role is optional — NO_GLOBAL_ROLE
	// is a real, always-present, always-truthy option so step1Valid's
	// `!!roleId` check needs no scope-specific branching.
	const roleOptions = projectId
		? projectRoles.map((r) => ({ id: r.id, label: r.role_name }))
		: [
				{
					id: NO_GLOBAL_ROLE,
					label: t("agents.createDialog.noGlobalRoleOption"),
				},
				...globalRoles.map((r) => ({ id: r.id, label: r.name })),
			];

	const [step, setStep] = useState<1 | 2>(1);
	const [name, setName] = useState("");
	const [handle, setHandle] = useState("");
	const [presetId, setPresetId] = useState("");
	const [roleId, setRoleId] = useState(() => (projectId ? "" : NO_GLOBAL_ROLE));
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
	const [cliProvider, setCliProvider] = useState<CLIProvider>("claude-code");
	const [cliModel, setCliModel] = useState("");
	const [copiedCliLoginCommand, setCopiedCliLoginCommand] = useState(false);
	const [systemPrompt, setSystemPrompt] = useState("");
	const [showApiKey, setShowApiKey] = useState(false);
	const [dockerEnabled, setDockerEnabled] = useState(false);
	const [defaultEnvironmentId, setDefaultEnvironmentIdState] = useState("");
	const [defaultFolderId, setDefaultFolderId] = useState("");
	// Changing the default environment clears any previously-picked default
	// folder — a folder only ever belongs to one environment, so a folder
	// picked under the old one is never valid under the new one (mirrors
	// agent-detail.tsx's own OverviewTab and agent-picker.tsx's
	// useEnvironmentPicker.onEnvironmentChange).
	const setDefaultEnvironmentId = (id: string) => {
		if (id !== defaultEnvironmentId) {
			setDefaultFolderId("");
		}
		setDefaultEnvironmentIdState(id);
	};
	const selectedEnvironment = environments.find(
		(env) => env.id === defaultEnvironmentId,
	);

	// Derived final values sent to the API
	const llmProvider =
		providerSelect === CUSTOM ? customProvider.trim() : providerSelect;
	const llmModel = modelSelect === CUSTOM ? customModel.trim() : modelSelect;

	const reset = () => {
		setStep(1);
		setName("");
		setHandle("");
		setPresetId("");
		setRoleId(projectId ? "" : NO_GLOBAL_ROLE);
		setAgentType("llm");
		setProviderSelect("anthropic");
		setCustomProvider("");
		setModelSelect("claude-sonnet-4-5-20250929");
		setCustomModel("");
		setLlmApiKey("");
		setLlmBaseUrl(llmModels.anthropic?.base_url ?? "");
		setAcpProvider("claude-code");
		setAcpCommand("");
		setCliProvider("claude-code");
		setCliModel("");
		setCopiedCliLoginCommand(false);
		setSystemPrompt("");
		setShowApiKey(false);
		setDockerEnabled(false);
		setDefaultEnvironmentIdState("");
		setDefaultFolderId("");
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
			const typeFields =
				agentType === "llm"
					? {
							llm_provider: llmProvider,
							llm_model: llmModel,
							llm_api_key: llmApiKey,
							llm_base_url: llmBaseUrl,
							system_prompt: systemPrompt,
							docker_enabled: dockerEnabled,
							...(projectId && defaultEnvironmentId
								? { default_environment_id: defaultEnvironmentId }
								: {}),
							...(projectId && defaultEnvironmentId && defaultFolderId
								? { default_folder_id: defaultFolderId }
								: {}),
						}
					: agentType === "provider_cli"
						? {
								cli_provider: cliProvider,
								...(cliModel.trim() ? { cli_model: cliModel.trim() } : {}),
								// api_key auth isn't offered here — see the auth
								// section's own comment below — so this is always
								// "login": the underlying CLI must already be
								// authenticated via its own terminal login inside
								// the selected environment.
								cli_auth_mode: "login" as const,
								// Required for this type — step2Valid blocks submission
								// otherwise, so defaultEnvironmentId is always set here.
								default_environment_id: defaultEnvironmentId,
								...(defaultFolderId
									? { default_folder_id: defaultFolderId }
									: {}),
							}
						: {
								acp_provider: acpProvider,
								...(acpProvider === "custom"
									? { acp_command: acpCommandParts }
									: {}),
							};
			const agent = projectId
				? await createAgent(projectId, {
						name: name.trim(),
						handle: handle.trim(),
						agent_type: agentType,
						project_role_id: roleId,
						...typeFields,
					})
				: await createGlobalAgent({
						name: name.trim(),
						handle: handle.trim(),
						agent_type: agentType,
						global_role_id: roleId === NO_GLOBAL_ROLE ? undefined : roleId,
						...typeFields,
					});
			if (agent.agent_type !== "acp") {
				return { agent, token: null, mcpKey: null };
			}
			// Generate the bridge token and MCP key as part of the same
			// "Creating…" spinner instead of separate loading steps inside the
			// setup dialog — the dialog can then just display already-resolved
			// results, with nothing left for the user to click. The two
			// generations are independent of each other, so they run
			// concurrently rather than adding their latency on top of each
			// other: if one sub-step fails, the agent still exists and the
			// other's result is unaffected — fall through with that one null
			// so the setup dialog offers a manual retry for just that value
			// instead of failing the whole creation.
			const [tokenResult, mcpKeyResult] = await Promise.allSettled([
				projectId
					? generateAcpBridgeToken(projectId, agent.id)
					: generateGlobalAcpBridgeToken(agent.id),
				projectId
					? generateAgentMCPKey(projectId, agent.id)
					: generateGlobalAgentMCPKey(agent.id),
			]);
			const token =
				tokenResult.status === "fulfilled" ? tokenResult.value : null;
			const mcpKey =
				mcpKeyResult.status === "fulfilled" ? mcpKeyResult.value.token : null;
			return { agent, token, mcpKey };
		},
		onSuccess: ({ agent, token, mcpKey }) => {
			if (projectId) {
				qc.invalidateQueries({
					queryKey: ["projects", projectId, "agents"],
				});
			} else {
				qc.invalidateQueries({ queryKey: globalAgentsQueryOptions.queryKey });
				qc.invalidateQueries({ queryKey: ["global-agents", "chattable"] });
			}
			handleClose(false);
			if (agent.agent_type === "acp") {
				onAcpAgentCreated(agent, token, mcpKey);
			}
		},
	});

	// Lets the user confirm their terminal login actually worked before
	// finishing agent creation — the environment-scoped sibling of
	// agent-detail.tsx's VerifyCLILoginPanel, needed here specifically
	// because this dialog's agent doesn't exist yet (verifyCLILogin's own
	// endpoint is agent-scoped).
	const verifyCliLoginMutation = useMutation({
		mutationFn: () => {
			if (!projectId || !defaultEnvironmentId) {
				throw new Error("no environment selected");
			}
			return verifyEnvironmentCLILogin(
				projectId,
				defaultEnvironmentId,
				cliProvider,
			);
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
			: agentType === "provider_cli"
				? !!(cliProvider && defaultEnvironmentId)
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
						{/* Agent Type — provider_cli is project-scoped only: it requires
						    a static environment, and a global agent has none of its own
						    to pick from (see agentdom.ErrCLIProviderNotSupportedForGlobalAgents
						    server-side). Still shown at global scope so the option is
						    discoverable, but disabled with a tooltip pointing the user at
						    a project's Agents page instead of being silently omitted. */}
						<div className="space-y-1.5">
							<Label>{t("agents.createDialog.agentTypeLabel")}</Label>
							<div className="grid grid-cols-3 gap-2">
								{(["llm", "provider_cli", "acp"] as const).map((type) => {
									const isSelected = agentType === type;
									const isDisabled = type === "provider_cli" && !projectId;
									const labelKey =
										type === "llm"
											? "agents.createDialog.agentTypeLLM"
											: type === "acp"
												? "agents.createDialog.agentTypeACP"
												: "agents.createDialog.agentTypeProviderCLI";
									const hintKey =
										type === "llm"
											? "agents.createDialog.agentTypeLLMHint"
											: type === "acp"
												? "agents.createDialog.agentTypeACPHint"
												: "agents.createDialog.agentTypeProviderCLIHint";
									const buttonClassName = cn(
										"rounded-lg border p-3 text-left transition-all",
										isDisabled
											? "cursor-not-allowed border-border/40 opacity-50"
											: isSelected
												? "border-primary/40 bg-primary/5 ring-1 ring-primary/20 shadow-sm"
												: "border-border/60 hover:border-border hover:bg-muted/30",
									);
									const content = (
										<>
											<p className="text-xs font-semibold leading-tight">
												{t(labelKey)}
											</p>
											<p className="mt-0.5 text-xs leading-tight text-muted-foreground">
												{t(hintKey)}
											</p>
										</>
									);

									if (!isDisabled) {
										return (
											<button
												key={type}
												type="button"
												onClick={() => setAgentType(type)}
												className={buttonClassName}
											>
												{content}
											</button>
										);
									}

									return (
										<Tooltip key={type}>
											<TooltipTrigger
												render={
													<button
														type="button"
														aria-disabled="true"
														className={buttonClassName}
													/>
												}
											>
												{content}
											</TooltipTrigger>
											<TooltipContent side="top">
												{t(
													"agents.createDialog.agentTypeProviderCLIGlobalDisabledHint",
												)}
											</TooltipContent>
										</Tooltip>
									);
								})}
							</div>
						</div>

						{/* Preset grid — LLM-only; ACP/provider_cli agents have no
						    model/prompt to preset (their underlying CLI owns its own). */}
						{agentType === "llm" && (
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
						)}

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

						{/* Project Role / Global Role */}
						<div className="space-y-1.5">
							<Label>
								{t(
									projectId
										? "agents.createDialog.projectRoleLabel"
										: "agents.createDialog.globalRoleLabel",
								)}{" "}
								{projectId && <span className="text-destructive">*</span>}
							</Label>
							<Select value={roleId} onValueChange={(v) => v && setRoleId(v)}>
								<SelectTrigger>
									<SelectValue
										placeholder={t(
											projectId
												? "agents.createDialog.projectRolePlaceholder"
												: "agents.createDialog.globalRolePlaceholder",
										)}
									>
										{roleOptions.find((r) => r.id === roleId)?.label}
									</SelectValue>
								</SelectTrigger>
								<SelectContent>
									{roleOptions.map((r) => (
										<SelectItem key={r.id} value={r.id}>
											{r.label}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
							<p className="text-xs text-muted-foreground">
								{t(
									projectId
										? "agents.createDialog.projectRoleHint"
										: "agents.createDialog.globalRoleHint",
								)}
							</p>
						</div>
					</div>
				)}

				{/* ── Step 2: AI Configuration ─────────────────────────────────── */}
				{step === 2 && (
					<div className="overflow-y-auto max-h-[62vh] px-6 py-5 space-y-5">
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
											<SelectItem value="goose">
												{t("agents.createDialog.acpProviderGoose")}
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

						{agentType === "provider_cli" && projectId && (
							<>
								<div className="rounded-lg border border-border/60 bg-muted/20 p-4 space-y-3">
									<div className="flex items-center gap-1.5">
										<Cpu className="size-3.5 text-muted-foreground" />
										<span className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
											{t("agents.createDialog.cliSection")}
										</span>
									</div>
									<div className="space-y-1.5">
										<Label>{t("agents.createDialog.cliProviderLabel")}</Label>
										<Select
											value={cliProvider}
											onValueChange={(v) => {
												if (!v) return;
												setCliProvider(v as CLIProvider);
												setCopiedCliLoginCommand(false);
												verifyCliLoginMutation.reset();
											}}
											items={[
												{
													value: "claude-code",
													label: t("agents.createDialog.cliProviderClaudeCode"),
												},
												{
													value: "codex",
													label: t("agents.createDialog.cliProviderCodex"),
												},
												{
													value: "gemini-cli",
													label: t("agents.createDialog.cliProviderGeminiCli"),
												},
												{
													value: "cursor-agent",
													label: t(
														"agents.createDialog.cliProviderCursorAgent",
													),
												},
											]}
										>
											<SelectTrigger>
												<SelectValue />
											</SelectTrigger>
											<SelectContent>
												<SelectItem value="claude-code">
													{t("agents.createDialog.cliProviderClaudeCode")}
												</SelectItem>
												<SelectItem value="codex">
													{t("agents.createDialog.cliProviderCodex")}
												</SelectItem>
												<SelectItem value="gemini-cli">
													{t("agents.createDialog.cliProviderGeminiCli")}
												</SelectItem>
												<SelectItem value="cursor-agent">
													{t("agents.createDialog.cliProviderCursorAgent")}
												</SelectItem>
											</SelectContent>
										</Select>
									</div>
									<div className="space-y-1.5">
										<Label>
											{t("agents.createDialog.cliModelLabel")}{" "}
											<span className="text-muted-foreground font-normal text-xs">
												{t("agents.createDialog.optional")}
											</span>
										</Label>
										<Input
											placeholder={t("agents.createDialog.cliModelPlaceholder")}
											value={cliModel}
											onChange={(e) => setCliModel(e.target.value)}
										/>
									</div>
									<p className="text-xs text-muted-foreground rounded-md bg-muted/40 px-3 py-2">
										{t("agents.createDialog.cliGuidance")}
									</p>
								</div>

								{/* Environment — REQUIRED for provider_cli (no "none" option):
								    the CLI's own login state must persist across
								    conversations, which only a static environment provides. */}
								<div className="rounded-lg border border-border/60 bg-muted/20 p-4 space-y-3">
									<div className="flex items-center gap-1.5">
										<Server className="size-3.5 text-muted-foreground" />
										<span className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
											{t("agents.createDialog.environmentSection")}
										</span>
									</div>
									<div className="space-y-1.5">
										<Label>
											{t("agents.detail.overview.defaultEnvironmentLabel")}{" "}
											<span className="text-destructive">*</span>
										</Label>
										<DefaultEnvironmentSelect
											projectId={projectId}
											environments={environments}
											value={defaultEnvironmentId}
											onChange={(id) => {
												setDefaultEnvironmentId(id);
												verifyCliLoginMutation.reset();
											}}
											required
											className="w-full"
										/>
										<p className="text-xs text-muted-foreground">
											{t("agents.createDialog.cliEnvironmentHint")}
										</p>
									</div>
									{selectedEnvironment && (
										<div className="space-y-1.5">
											<Label>
												{t("agents.detail.overview.defaultFolderLabel")}
											</Label>
											<DefaultFolderSelect
												projectId={projectId}
												environment={selectedEnvironment}
												value={defaultFolderId}
												onChange={setDefaultFolderId}
												className="w-full"
											/>
										</div>
									)}
								</div>

								{/* Auth — Goose never brokers auth for a CLI provider; this is
								    entirely about how the underlying CLI itself gets
								    authenticated. Only terminal login is offered here —
								    api_key auth is coming to the "normal" (llm) agent type
								    instead, not this one. */}
								<div className="rounded-lg border border-border/60 bg-muted/20 p-4 space-y-3">
									<Label>{t("agents.createDialog.cliAuthModeLabel")}</Label>
									<p className="text-xs text-muted-foreground">
										{t("agents.createDialog.cliLoginModeHint")}
									</p>
									<div className="space-y-1.5">
										<Label className="text-xs text-muted-foreground font-normal">
											{t("agents.createDialog.cliLoginCommandLabel")}
										</Label>
										<CommandBox
											command={cliLoginCommand(cliProvider)}
											copied={copiedCliLoginCommand}
											onCopy={() => {
												navigator.clipboard
													.writeText(cliLoginCommand(cliProvider))
													.then(() => {
														setCopiedCliLoginCommand(true);
														setTimeout(
															() => setCopiedCliLoginCommand(false),
															2000,
														);
													})
													.catch(() => {
														// Clipboard write can fail (permission denied,
														// insecure context) — leave copiedCliLoginCommand
														// untouched so the button doesn't falsely claim
														// success for a command the user may need to copy
														// manually.
													});
											}}
										/>
									</div>
									{defaultEnvironmentId && (
										<Link
											to="/projects/$projectId/environments/$environmentId/terminal"
											params={{
												projectId,
												environmentId: defaultEnvironmentId,
											}}
											target="_blank"
											rel="noopener noreferrer"
											className={buttonVariants({
												variant: "outline",
												size: "sm",
											})}
										>
											<ExternalLink className="size-3.5 mr-1.5" />
											{t("agents.createDialog.cliOpenTerminal")}
										</Link>
									)}
									<div className="space-y-1.5 pt-1">
										<Button
											type="button"
											variant="outline"
											size="sm"
											onClick={() => verifyCliLoginMutation.mutate()}
											disabled={
												!defaultEnvironmentId ||
												verifyCliLoginMutation.isPending
											}
										>
											{verifyCliLoginMutation.isPending ? (
												<Loader2 className="size-3.5 mr-1.5 animate-spin" />
											) : (
												<Lock className="size-3.5 mr-1.5" />
											)}
											{t("agents.createDialog.cliVerifyLogin")}
										</Button>
										{verifyCliLoginMutation.isSuccess && (
											<p
												className={cn(
													"text-xs",
													verifyCliLoginMutation.data.authenticated
														? "text-emerald-600"
														: "text-muted-foreground",
												)}
											>
												{verifyCliLoginMutation.data.authenticated
													? t("agents.detail.overview.cliLoginVerifiedNow")
													: t(
															"agents.detail.overview.cliLoginNotAuthenticated",
														)}
											</p>
										)}
										{verifyCliLoginMutation.isError && (
											<p className="text-xs text-destructive">
												{t("agents.detail.overview.cliLoginVerifyFailed")}
											</p>
										)}
									</div>
								</div>
							</>
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

								{/* Environment + Docker card */}
								<div className="rounded-lg border border-border/60 bg-muted/20 p-4 space-y-3">
									<div className="flex items-center gap-1.5">
										<Server className="size-3.5 text-muted-foreground" />
										<span className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
											{t("agents.createDialog.environmentSection")}
										</span>
									</div>
									{projectId && (
										<div className="space-y-1.5">
											<Label>
												{t("agents.detail.overview.defaultEnvironmentLabel")}
											</Label>
											<DefaultEnvironmentSelect
												projectId={projectId}
												environments={environments}
												value={defaultEnvironmentId}
												onChange={setDefaultEnvironmentId}
												className="w-full"
											/>
											<p className="text-xs text-muted-foreground">
												{t("agents.detail.overview.defaultEnvironmentHint")}
											</p>
										</div>
									)}
									{projectId && selectedEnvironment && (
										<div className="space-y-1.5">
											<Label>
												{t("agents.detail.overview.defaultFolderLabel")}
											</Label>
											<DefaultFolderSelect
												projectId={projectId}
												environment={selectedEnvironment}
												value={defaultFolderId}
												onChange={setDefaultFolderId}
												className="w-full"
											/>
											<p className="text-xs text-muted-foreground">
												{t("agents.detail.overview.defaultFolderHint")}
											</p>
										</div>
									)}
									{/* Docker access only applies to the disposable per-conversation
									    sandbox — once a static default environment is picked, that
									    environment's own Docker setting (set at creation) governs
									    instead, so this toggle no longer means anything. */}
									{!defaultEnvironmentId && (
										<div className="flex items-center justify-between gap-3">
											<div>
												<p className="text-sm font-medium">
													{t("agents.detail.overview.dockerEnabledLabel")}
												</p>
												<p className="text-xs text-muted-foreground">
													{t("agents.detail.overview.dockerEnabledHint")}
												</p>
											</div>
											<Switch
												checked={dockerEnabled}
												onCheckedChange={setDockerEnabled}
											/>
										</div>
									)}
								</div>
							</>
						)}

						{/* System Prompt — LLM-only; an ACP agent's local CLI owns its own prompt */}
						{agentType === "llm" && (
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
						)}

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

export function AcpSetupDialog({
	projectId,
	agent,
	token,
	mcpKey,
	open,
	canWrite,
	onOpenChange,
	onTokenGenerated,
}: {
	/** Absent for a global agent (no project of its own). */
	projectId?: string;
	agent: Agent | null;
	token: AcpBridgeToken | null;
	mcpKey: string | null;
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
						hasKey={agent.has_mcp_api_key || mcpKey !== null}
						canWrite={canWrite}
						initialToken={token}
						initialKey={mcpKey}
						onTokenGenerated={() => {
							if (projectId) {
								qc.invalidateQueries({
									queryKey: ["projects", projectId, "agents"],
								});
							} else {
								qc.invalidateQueries({
									queryKey: globalAgentsQueryOptions.queryKey,
								});
							}
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
