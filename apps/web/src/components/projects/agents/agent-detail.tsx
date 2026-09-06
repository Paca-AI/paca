import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
	Activity as ActivityIcon,
	Bot,
	Check,
	CircleCheck,
	Code2,
	ExternalLink,
	KeyRound,
	Loader2,
	Plus,
	Save,
	Server,
	Trash2,
	Wand2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	DefaultEnvironmentSelect,
	DefaultFolderSelect,
} from "@/components/projects/environments/environment-folder-select";
import { AvatarUpload } from "@/components/shared/avatar-upload";
import { Badge } from "@/components/ui/badge";
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
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	type ACPProvider,
	type Agent,
	type AgentMCPServer,
	type AgentSkill,
	addEnvVar,
	addGlobalEnvVar,
	addGlobalMCPServer,
	addGlobalSkill,
	addMCPServer,
	addSkill,
	agentEnvVarsQueryOptions,
	agentMCPServersQueryOptions,
	agentQueryOptions,
	agentSkillsQueryOptions,
	type CLIProvider,
	deleteEnvVar,
	deleteGlobalEnvVar,
	deleteGlobalMCPServer,
	deleteGlobalSkill,
	deleteMCPServer,
	deleteSkill,
	globalAgentEnvVarsQueryOptions,
	globalAgentMCPServersQueryOptions,
	globalAgentQueryOptions,
	globalAgentSkillsQueryOptions,
	llmModelsQueryOptions,
	updateAgent,
	updateGlobalAgent,
	updateGlobalMCPServer,
	updateGlobalSkill,
	updateMCPServer,
	updateSkill,
	verifyCLILogin,
} from "@/lib/agent-api";
import { environmentsQueryOptions } from "@/lib/environment-api";
import { resolveAgentAvatarUrl } from "@/lib/provider-logos";
import { splitShellCommand } from "@/lib/shell-command";
import { AcpBridgeSetup } from "./acp-bridge-setup";
import { AgentActivityTab } from "./agent-activity-tab";

// Shared between the project agent detail page
// (routes/.../projects/$projectId/agents/$agentId/index.tsx) and the global
// one (routes/.../admin/agents/$agentId.tsx) — same tabs, same forms, same
// layout either way. Every project-scoped API call here has an
// agentId-only global sibling (see agent-api.ts's global MCP/skill/env-var
// functions) since those services were never project-scoped on the backend
// to begin with — only the HTTP route prefix differs. The one tab that
// can't carry over is Activity: an agent's activity feed is inherently
// project-shaped (task/doc activity within one project), and a global agent
// may be invited into many projects or none — see AgentDetailView's
// visibleTabs filtering below.

type Tab = "overview" | "mcp-servers" | "skills" | "env-vars" | "activity";

const CUSTOM = "__custom__";

// ── Overview Tab ──────────────────────────────────────────────────────────────

function OverviewTab({
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
	const { data: llmModels = {} } = useQuery(llmModelsQueryOptions);
	// Only fetched at project scope — a global agent has no project to
	// default an environment from, and the Select below is hidden entirely
	// in that case (see the !isAcp block below).
	const { data: environments = [] } = useQuery({
		...environmentsQueryOptions(projectId ?? ""),
		enabled: !!projectId,
	});

	const providers = Object.keys(llmModels);

	// Provider select: if agent's provider is known, use it directly; otherwise custom mode
	const knownProvider =
		providers.length > 0 && providers.includes(agent.llm_provider);
	const [providerSelect, setProviderSelect] = useState(
		knownProvider
			? agent.llm_provider
			: agent.llm_provider
				? CUSTOM
				: "anthropic",
	);
	const [customProvider, setCustomProvider] = useState(
		knownProvider ? "" : agent.llm_provider,
	);

	// Model select: check against the provider's model list once loaded
	const initialModels = llmModels[agent.llm_provider]?.models ?? [];
	const knownModel = initialModels.includes(agent.llm_model);
	const [modelSelect, setModelSelect] = useState(
		knownModel ? agent.llm_model : agent.llm_model ? CUSTOM : "",
	);
	const [customModel, setCustomModel] = useState(
		knownModel ? "" : agent.llm_model,
	);

	const [name, setName] = useState(agent.name);
	const [llmApiKey, setLlmApiKey] = useState("");
	const [llmBaseUrl, setLlmBaseUrl] = useState(agent.llm_base_url ?? "");
	const [systemPrompt, setSystemPrompt] = useState(agent.system_prompt);
	const [committerName, setCommitterName] = useState(agent.git_committer_name);
	const [committerEmail, setCommitterEmail] = useState(
		agent.git_committer_email,
	);
	const [dockerEnabled, setDockerEnabled] = useState(agent.docker_enabled);
	const [parallelismLimit, setParallelismLimit] = useState(
		agent.parallelism_limit,
	);
	const [defaultEnvironmentId, setDefaultEnvironmentIdState] = useState(
		agent.default_environment_id ?? "",
	);
	const [defaultFolderId, setDefaultFolderId] = useState(
		agent.default_folder_id ?? "",
	);
	// Changing the default environment clears any previously-picked default
	// folder — a folder only ever belongs to one environment, so a folder
	// picked under the old one is never valid under the new one (mirrors
	// agent-picker.tsx's useEnvironmentPicker.onEnvironmentChange, which
	// clears folderId the same way for the composer's own pickers).
	//
	// Also resets parallelismLimit back to 1 when a real environment is
	// picked: the server rejects parallelism_limit > 1 for any agent
	// attached to a static default_environment_id (its filesystem is shared
	// across every conversation attached to it, unlike the default
	// ephemeral per-conversation sandbox — see
	// agentdom.ErrParallelismLimitRequiresIsolatedSandbox on the server),
	// and the field itself becomes disabled once requiresSerialDispatch
	// below is true, so this keeps the save payload consistent with what
	// the disabled input already shows instead of silently carrying over a
	// stale value the user can no longer see or edit.
	const setDefaultEnvironmentId = (id: string) => {
		if (id !== defaultEnvironmentId) {
			setDefaultFolderId("");
		}
		if (id && parallelismLimit > 1) {
			setParallelismLimit(1);
		}
		setDefaultEnvironmentIdState(id);
	};
	const selectedEnvironment = environments.find(
		(env) => env.id === defaultEnvironmentId,
	);
	const [acpProviderSelect, setAcpProviderSelect] = useState<ACPProvider>(
		agent.acp_provider ?? "claude-code",
	);
	const [acpCommand, setAcpCommand] = useState(
		(agent.acp_command ?? []).join(" "),
	);
	const [cliProviderSelect, setCliProviderSelect] = useState<CLIProvider>(
		agent.cli_provider ?? "claude-code",
	);
	const [cliModel, setCliModel] = useState(agent.cli_model ?? "");

	// Derived final values sent to the API
	const llmProvider =
		providerSelect === CUSTOM ? customProvider.trim() : providerSelect;
	const llmModel = modelSelect === CUSTOM ? customModel.trim() : modelSelect;
	const acpCommandParts = splitShellCommand(acpCommand);
	const isAcp = agent.agent_type === "acp";
	const isProviderCli = agent.agent_type === "provider_cli";
	// Mirrors agentsvc.requiresSerialDispatch on the server exactly: an
	// ACP-type agent (apps/acp-bridge's own session model rejects a second
	// concurrent turn rather than queueing it) or any agent attached to a
	// static environment (provider_cli always is) can never safely run more
	// than one conversation at once, regardless of what parallelism_limit
	// is configured to — see agentdom.ErrParallelismLimitRequiresIsolatedSandbox.
	const requiresSerialDispatch = isAcp || !!defaultEnvironmentId;

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

	const availableModels: string[] =
		providerSelect !== CUSTOM ? (llmModels[providerSelect]?.models ?? []) : [];

	// Defense in depth alongside setDefaultEnvironmentId's reset and the
	// disabled input below: the value actually compared/saved is always
	// forced to 1 when requiresSerialDispatch, regardless of whatever
	// parallelismLimit itself currently holds.
	const effectiveParallelismLimit = requiresSerialDispatch
		? 1
		: parallelismLimit;

	const isDirty =
		name !== agent.name ||
		effectiveParallelismLimit !== agent.parallelism_limit ||
		(isAcp
			? acpProviderSelect !== (agent.acp_provider ?? "claude-code") ||
				acpCommandParts.join(" ") !== (agent.acp_command ?? []).join(" ")
			: isProviderCli
				? cliProviderSelect !== (agent.cli_provider ?? "claude-code") ||
					cliModel !== (agent.cli_model ?? "") ||
					defaultEnvironmentId !== (agent.default_environment_id ?? "") ||
					defaultFolderId !== (agent.default_folder_id ?? "")
				: llmProvider !== agent.llm_provider ||
					llmModel !== agent.llm_model ||
					llmApiKey !== "" ||
					llmBaseUrl !== (agent.llm_base_url ?? "") ||
					systemPrompt !== agent.system_prompt ||
					committerName !== agent.git_committer_name ||
					committerEmail !== agent.git_committer_email ||
					dockerEnabled !== agent.docker_enabled ||
					defaultEnvironmentId !== (agent.default_environment_id ?? "") ||
					defaultFolderId !== (agent.default_folder_id ?? ""));

	const saveMutation = useMutation({
		mutationFn: () => {
			const payload = {
				name: name.trim(),
				parallelism_limit: effectiveParallelismLimit,
				...(isAcp
					? {
							acp_provider: acpProviderSelect,
							...(acpProviderSelect === "custom"
								? { acp_command: acpCommandParts }
								: {}),
						}
					: isProviderCli
						? {
								cli_provider: cliProviderSelect,
								cli_model: cliModel,
								...(projectId
									? {
											default_environment_id: defaultEnvironmentId || undefined,
											default_folder_id: defaultFolderId || undefined,
										}
									: {}),
							}
						: {
								llm_provider: llmProvider,
								llm_model: llmModel,
								...(llmApiKey ? { llm_api_key: llmApiKey } : {}),
								llm_base_url: llmBaseUrl,
								system_prompt: systemPrompt,
								git_committer_name: committerName.trim(),
								git_committer_email: committerEmail.trim(),
								docker_enabled: dockerEnabled,
								...(projectId
									? {
											default_environment_id: defaultEnvironmentId || null,
											default_folder_id: defaultFolderId || null,
										}
									: {}),
							}),
			};
			return projectId
				? updateAgent(projectId, agent.id, payload)
				: updateGlobalAgent(agent.id, payload);
		},
		onSuccess: (updated) => {
			qc.setQueryData(
				(projectId
					? agentQueryOptions(projectId, agent.id)
					: globalAgentQueryOptions(agent.id)
				).queryKey,
				updated,
			);
			setLlmApiKey("");
		},
	});

	const canSave =
		isDirty &&
		(isAcp
			? !!acpProviderSelect &&
				(acpProviderSelect !== "custom" || acpCommandParts.length > 0)
			: isProviderCli
				? !!cliProviderSelect && !!defaultEnvironmentId
				: !!llmProvider && !!llmModel && !!llmBaseUrl.trim()) &&
		!saveMutation.isPending;

	return (
		<div className="space-y-6 max-w-2xl">
			<div className="space-y-1.5">
				<Label>{t("agents.detail.overview.nameLabel")}</Label>
				<Input
					value={name}
					onChange={(e) => setName(e.target.value)}
					disabled={!canWrite}
				/>
			</div>

			<div className="space-y-1.5">
				<Label>{t("agents.detail.overview.parallelismLimitLabel")}</Label>
				<Input
					type="number"
					min={1}
					className="w-24"
					value={effectiveParallelismLimit}
					onChange={(e) =>
						setParallelismLimit(Math.max(1, Number(e.target.value) || 1))
					}
					disabled={!canWrite || requiresSerialDispatch}
				/>
				<p className="text-xs text-muted-foreground">
					{requiresSerialDispatch
						? t("agents.detail.overview.parallelismLimitFixedHint")
						: t("agents.detail.overview.parallelismLimitHint")}
				</p>
			</div>

			<Separator />

			{!isAcp && !isProviderCli && (
				<div>
					<p className="text-sm font-medium mb-3">
						{t("agents.detail.overview.llmConfiguration")}
					</p>
					<div className="grid grid-cols-2 gap-3">
						<div className="space-y-1.5">
							<Label>{t("agents.detail.overview.providerLabel")}</Label>
							<Select
								value={providerSelect}
								onValueChange={handleProviderChange}
								disabled={!canWrite}
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
										{t("agents.detail.overview.customOption")}
									</SelectItem>
								</SelectContent>
							</Select>
							{providerSelect === CUSTOM && (
								<Input
									placeholder="my-provider"
									value={customProvider}
									onChange={(e) => setCustomProvider(e.target.value)}
									disabled={!canWrite}
								/>
							)}
						</div>
						<div className="space-y-1.5">
							<Label>{t("agents.detail.overview.modelLabel")}</Label>
							{providerSelect === CUSTOM ? (
								<Input
									placeholder="my-model-name"
									value={customModel}
									onChange={(e) => setCustomModel(e.target.value)}
									disabled={!canWrite}
								/>
							) : (
								<>
									<Select
										value={modelSelect}
										onValueChange={(v) => v && setModelSelect(v)}
										disabled={!canWrite}
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
												{t("agents.detail.overview.customOption")}
											</SelectItem>
										</SelectContent>
									</Select>
									{modelSelect === CUSTOM && (
										<Input
											placeholder="my-model-name"
											value={customModel}
											onChange={(e) => setCustomModel(e.target.value)}
											disabled={!canWrite}
										/>
									)}
								</>
							)}
						</div>
					</div>
					<div className="space-y-1.5 mt-3">
						<Label>
							{t("agents.detail.overview.apiKeyUpdateLabel")}{" "}
							<span className="text-muted-foreground font-normal text-xs">
								{t("agents.detail.overview.apiKeyUpdateHint")}
							</span>
						</Label>
						<Input
							type="password"
							placeholder="sk-ant-…"
							value={llmApiKey}
							onChange={(e) => setLlmApiKey(e.target.value)}
							disabled={!canWrite}
						/>
					</div>
					<div className="space-y-1.5 mt-3">
						<Label>
							{t("agents.detail.overview.baseUrlLabel")}{" "}
							<span className="text-destructive">*</span>
						</Label>
						<Input
							placeholder="https://api.openai.com/v1"
							value={llmBaseUrl}
							onChange={(e) => setLlmBaseUrl(e.target.value)}
							disabled={!canWrite}
						/>
					</div>
				</div>
			)}

			{isAcp && (
				<div>
					<p className="text-sm font-medium mb-3">
						{t("agents.detail.overview.acpConfiguration")}
					</p>
					<div className="space-y-1.5">
						<Label>{t("agents.detail.overview.acpProviderLabel")}</Label>
						<Select
							value={acpProviderSelect}
							onValueChange={(v) => v && setAcpProviderSelect(v as ACPProvider)}
							disabled={!canWrite}
						>
							<SelectTrigger>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="claude-code">
									{t("agents.detail.overview.acpProviderClaudeCode")}
								</SelectItem>
								<SelectItem value="codex">
									{t("agents.detail.overview.acpProviderCodex")}
								</SelectItem>
								<SelectItem value="gemini-cli">
									{t("agents.detail.overview.acpProviderGeminiCli")}
								</SelectItem>
								<SelectItem value="goose">
									{t("agents.detail.overview.acpProviderGoose")}
								</SelectItem>
								<SelectSeparator />
								<SelectItem value="custom">
									{t("agents.detail.overview.acpProviderCustom")}
								</SelectItem>
							</SelectContent>
						</Select>
					</div>
					{acpProviderSelect === "custom" && (
						<div className="space-y-1.5 mt-3">
							<Label>{t("agents.detail.overview.acpCommandLabel")}</Label>
							<Input
								placeholder="npx -y my-acp-server"
								value={acpCommand}
								onChange={(e) => setAcpCommand(e.target.value)}
								disabled={!canWrite}
							/>
							<p className="text-xs text-muted-foreground">
								{t("agents.detail.overview.acpCommandHint")}
							</p>
						</div>
					)}
					<p className="text-xs text-muted-foreground rounded-md bg-muted/40 px-3 py-2 mt-3">
						{t("agents.detail.overview.acpGuidanceBanner")}
					</p>
				</div>
			)}

			{isProviderCli && (
				<div>
					<p className="text-sm font-medium mb-3">
						{t("agents.detail.overview.cliConfiguration")}
					</p>
					<div className="space-y-1.5">
						<Label>{t("agents.detail.overview.cliProviderLabel")}</Label>
						<Select
							value={cliProviderSelect}
							onValueChange={(v) => {
								if (!v) return;
								setCliProviderSelect(v as CLIProvider);
							}}
							disabled={!canWrite}
							items={[
								{
									value: "claude-code",
									label: t("agents.detail.overview.cliProviderClaudeCode"),
								},
								{
									value: "codex",
									label: t("agents.detail.overview.cliProviderCodex"),
								},
								{
									value: "gemini-cli",
									label: t("agents.detail.overview.cliProviderGeminiCli"),
								},
								{
									value: "cursor-agent",
									label: t("agents.detail.overview.cliProviderCursorAgent"),
								},
							]}
						>
							<SelectTrigger>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="claude-code">
									{t("agents.detail.overview.cliProviderClaudeCode")}
								</SelectItem>
								<SelectItem value="codex">
									{t("agents.detail.overview.cliProviderCodex")}
								</SelectItem>
								<SelectItem value="gemini-cli">
									{t("agents.detail.overview.cliProviderGeminiCli")}
								</SelectItem>
								<SelectItem value="cursor-agent">
									{t("agents.detail.overview.cliProviderCursorAgent")}
								</SelectItem>
							</SelectContent>
						</Select>
					</div>
					<div className="space-y-1.5 mt-3">
						<Label>
							{t("agents.detail.overview.cliModelLabel")}{" "}
							<span className="text-muted-foreground font-normal text-xs">
								{t("agents.createDialog.optional")}
							</span>
						</Label>
						<Input
							placeholder={t("agents.detail.overview.cliModelPlaceholder")}
							value={cliModel}
							onChange={(e) => setCliModel(e.target.value)}
							disabled={!canWrite}
						/>
					</div>
					<p className="text-xs text-muted-foreground rounded-md bg-muted/40 px-3 py-2 mt-3">
						{t("agents.detail.overview.cliGuidanceBanner")}
					</p>
				</div>
			)}

			<Separator />

			{isAcp && (
				<>
					<LocalBridgePanel
						projectId={projectId}
						agentId={agent.id}
						acpProvider={agent.acp_provider ?? "claude-code"}
						hasToken={agent.has_acp_bridge_token}
						hasKey={agent.has_mcp_api_key}
						canWrite={canWrite}
						onTokenGenerated={() =>
							qc.setQueryData(
								(projectId
									? agentQueryOptions(projectId, agent.id)
									: globalAgentQueryOptions(agent.id)
								).queryKey,
								(old: Agent | undefined) =>
									old ? { ...old, has_acp_bridge_token: true } : old,
							)
						}
					/>
					<Separator />
				</>
			)}

			{isProviderCli && (
				<>
					<VerifyCLILoginPanel
						projectId={projectId}
						agentId={agent.id}
						environmentId={agent.default_environment_id ?? null}
						lastVerifiedAt={agent.cli_login_verified_at ?? null}
						canWrite={canWrite}
						onVerified={() =>
							qc.invalidateQueries({
								queryKey: (projectId
									? agentQueryOptions(projectId, agent.id)
									: globalAgentQueryOptions(agent.id)
								).queryKey,
							})
						}
					/>
					<Separator />
				</>
			)}

			{/* System prompt and git committer identity are LLM-only — an ACP
			    or provider_cli agent's underlying CLI owns its own prompt and
			    git identity. */}
			{!isAcp && !isProviderCli && (
				<>
					<div className="space-y-1.5">
						<Label>{t("agents.detail.overview.systemPromptLabel")}</Label>
						<Textarea
							value={systemPrompt}
							onChange={(e) => setSystemPrompt(e.target.value)}
							rows={5}
							disabled={!canWrite}
							className="font-mono text-xs"
						/>
					</div>

					<Separator />

					<div>
						<p className="text-sm font-medium mb-1">
							{t("agents.detail.overview.gitCommitterIdentity")}
						</p>
						<p className="text-xs text-muted-foreground mb-3">
							{t("agents.detail.overview.gitCommitterHint")}
						</p>
						<div className="grid grid-cols-2 gap-3">
							<div className="space-y-1.5">
								<Label>{t("agents.detail.overview.committerNameLabel")}</Label>
								<Input
									value={committerName}
									onChange={(e) => setCommitterName(e.target.value)}
									disabled={!canWrite}
									placeholder="paca-agent"
								/>
							</div>
							<div className="space-y-1.5">
								<Label>{t("agents.detail.overview.committerEmailLabel")}</Label>
								<Input
									type="email"
									value={committerEmail}
									onChange={(e) => setCommitterEmail(e.target.value)}
									disabled={!canWrite}
									placeholder="paca-agent@users.noreply.github.com"
								/>
							</div>
						</div>
					</div>
				</>
			)}

			{/* Environment section applies to both llm (optional default) and
			    provider_cli (mandatory — see canSave's isProviderCli branch)
			    agents; an ACP agent's sandboxing is owned entirely by the
			    user's own local ACP client, so it's excluded here too. */}
			{!isAcp && (
				<>
					<Separator />

					<div>
						<p className="text-sm font-medium mb-3">
							{t("agents.detail.overview.environmentSection")}
						</p>
						<div className="space-y-4">
							{projectId && (
								<div className="flex items-center justify-between gap-3">
									<div>
										<p className="text-sm font-medium">
											{t("agents.detail.overview.defaultEnvironmentLabel")}
										</p>
										<p className="text-xs text-muted-foreground">
											{t("agents.detail.overview.defaultEnvironmentHint")}
										</p>
									</div>
									<DefaultEnvironmentSelect
										projectId={projectId}
										environments={environments}
										value={defaultEnvironmentId}
										onChange={setDefaultEnvironmentId}
										disabled={!canWrite}
										className="w-56"
									/>
								</div>
							)}

							{projectId && selectedEnvironment && (
								<div className="flex items-center justify-between gap-3">
									<div>
										<p className="text-sm font-medium">
											{t("agents.detail.overview.defaultFolderLabel")}
										</p>
										<p className="text-xs text-muted-foreground">
											{t("agents.detail.overview.defaultFolderHint")}
										</p>
									</div>
									<DefaultFolderSelect
										projectId={projectId}
										environment={selectedEnvironment}
										value={defaultFolderId}
										onChange={setDefaultFolderId}
										disabled={!canWrite}
										className="w-56"
									/>
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
										disabled={!canWrite}
									/>
								</div>
							)}
						</div>
					</div>
				</>
			)}

			{canWrite && (
				<div className="flex items-center gap-3 pt-2">
					<Button onClick={() => saveMutation.mutate()} disabled={!canSave}>
						{saveMutation.isPending ? (
							<Loader2 className="size-4 mr-2 animate-spin" />
						) : (
							<Save className="size-4 mr-2" />
						)}
						{t("agents.detail.overview.saveChanges")}
					</Button>
					{saveMutation.isSuccess && (
						<span className="flex items-center gap-1 text-xs text-emerald-600">
							<Check className="size-3" />
							{t("agents.detail.overview.saved")}
						</span>
					)}
					{saveMutation.isError && (
						<span className="text-xs text-destructive">
							{t("agents.detail.overview.saveFailed")}
						</span>
					)}
				</div>
			)}
		</div>
	);
}

// ── Local Bridge Panel (ACP agents, embedded in Overview) ───────────────────────

function LocalBridgePanel({
	projectId,
	agentId,
	acpProvider,
	hasToken,
	hasKey,
	canWrite,
	onTokenGenerated,
}: {
	projectId?: string;
	agentId: string;
	acpProvider: ACPProvider;
	hasToken: boolean;
	hasKey: boolean;
	canWrite: boolean;
	onTokenGenerated: () => void;
}) {
	const { t } = useTranslation("projects");
	return (
		<div>
			<p className="text-sm font-medium mb-1">
				{t("agents.acpSetup.panelTitle")}
			</p>
			<p className="text-xs text-muted-foreground mb-3">
				{t("agents.acpSetup.panelDescription")}
			</p>
			<AcpBridgeSetup
				projectId={projectId}
				agentId={agentId}
				acpProvider={acpProvider}
				hasToken={hasToken}
				hasKey={hasKey}
				canWrite={canWrite}
				onTokenGenerated={onTokenGenerated}
			/>
		</div>
	);
}

// ── Verify CLI Login Panel (provider_cli agents, embedded in Overview) ──────────
//
// provider_cli agents can't be global-scope (decision enforced server-side —
// see agentdom.ErrCLIProviderNotSupportedForGlobalAgents), so projectId is
// always defined in practice whenever this renders; still typed optional
// (matching OverviewTab's own prop) and guarded rather than asserted.

function VerifyCLILoginPanel({
	projectId,
	agentId,
	environmentId,
	lastVerifiedAt,
	canWrite,
	onVerified,
}: {
	projectId?: string;
	agentId: string;
	environmentId: string | null;
	lastVerifiedAt: string | null;
	canWrite: boolean;
	onVerified: () => void;
}) {
	const { t } = useTranslation("projects");
	const verifyMutation = useMutation({
		mutationFn: () => {
			if (!projectId) throw new Error("no project");
			return verifyCLILogin(projectId, agentId);
		},
		onSuccess: onVerified,
	});

	return (
		<div>
			<p className="text-sm font-medium mb-1">
				{t("agents.detail.overview.cliLoginPanelTitle")}
			</p>
			<p className="text-xs text-muted-foreground mb-3">
				{t("agents.detail.overview.cliLoginPanelDescription")}
			</p>
			<div className="flex flex-wrap items-center gap-2">
				{projectId && environmentId && (
					<Link
						to="/projects/$projectId/environments/$environmentId/terminal"
						params={{ projectId, environmentId }}
						target="_blank"
						rel="noopener noreferrer"
						className={buttonVariants({ variant: "outline", size: "sm" })}
					>
						<ExternalLink className="size-3.5 mr-1.5" />
						{t("agents.detail.overview.cliLoginOpenTerminal")}
					</Link>
				)}
				{canWrite && projectId && (
					<Button
						size="sm"
						variant="outline"
						onClick={() => verifyMutation.mutate()}
						disabled={verifyMutation.isPending || !environmentId}
					>
						{verifyMutation.isPending ? (
							<Loader2 className="size-3.5 mr-1.5 animate-spin" />
						) : (
							<CircleCheck className="size-3.5 mr-1.5" />
						)}
						{t("agents.detail.overview.cliLoginVerify")}
					</Button>
				)}
			</div>
			{verifyMutation.isSuccess && (
				<p
					className={`mt-2 text-xs ${verifyMutation.data.authenticated ? "text-emerald-600" : "text-muted-foreground"}`}
				>
					{verifyMutation.data.authenticated
						? t("agents.detail.overview.cliLoginVerifiedNow")
						: t("agents.detail.overview.cliLoginNotAuthenticated")}
				</p>
			)}
			{verifyMutation.isError && (
				<p className="mt-2 text-xs text-destructive">
					{t("agents.detail.overview.cliLoginVerifyFailed")}
				</p>
			)}
			{!verifyMutation.isSuccess && lastVerifiedAt && (
				<p className="mt-2 text-xs text-muted-foreground">
					{t("agents.detail.overview.cliLoginLastVerified", {
						date: new Date(lastVerifiedAt).toLocaleString(),
					})}
				</p>
			)}
			{!environmentId && (
				<p className="mt-2 text-xs text-destructive">
					{t("agents.detail.overview.cliLoginNoEnvironment")}
				</p>
			)}
		</div>
	);
}

// ── MCP Servers Tab ───────────────────────────────────────────────────────────

function AddMCPServerDialog({
	projectId,
	agentId,
	open,
	onOpenChange,
}: {
	projectId?: string;
	agentId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [serverName, setServerName] = useState("");
	const [transport, setTransport] = useState<"stdio" | "sse" | "http">("stdio");
	const [command, setCommand] = useState("");
	const [args, setArgs] = useState("");
	const [url, setUrl] = useState("");

	const addMutation = useMutation({
		mutationFn: () => {
			const payload = {
				server_name: serverName.trim(),
				transport,
				command: transport === "stdio" ? command.trim() || null : null,
				args:
					transport === "stdio"
						? args
								.split(/\s+/)
								.map((a) => a.trim())
								.filter(Boolean)
						: [],
				url: transport !== "stdio" ? url.trim() || null : null,
			};
			return projectId
				? addMCPServer(projectId, agentId, payload)
				: addGlobalMCPServer(agentId, payload);
		},
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: (projectId
					? agentMCPServersQueryOptions(projectId, agentId)
					: globalAgentMCPServersQueryOptions(agentId)
				).queryKey,
			});
			onOpenChange(false);
			setServerName("");
			setCommand("");
			setArgs("");
			setUrl("");
		},
	});

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-md">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						<Server className="size-4 text-primary" />
						{t("agents.detail.mcp.addDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("agents.detail.mcp.addDialog.description")}
					</DialogDescription>
				</DialogHeader>
				<div className="space-y-4 py-2">
					<div className="space-y-1.5">
						<Label>{t("agents.detail.mcp.addDialog.serverNameLabel")}</Label>
						<Input
							placeholder="filesystem"
							value={serverName}
							onChange={(e) => setServerName(e.target.value)}
						/>
					</div>
					<div className="space-y-1.5">
						<Label>{t("agents.detail.mcp.addDialog.transportLabel")}</Label>
						<Select
							value={transport}
							onValueChange={(v) => setTransport(v as typeof transport)}
						>
							<SelectTrigger>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="stdio">stdio</SelectItem>
								<SelectItem value="sse">SSE</SelectItem>
								<SelectItem value="http">HTTP</SelectItem>
							</SelectContent>
						</Select>
					</div>
					{transport === "stdio" ? (
						<>
							<div className="space-y-1.5">
								<Label>{t("agents.detail.mcp.addDialog.commandLabel")}</Label>
								<Input
									placeholder="npx"
									value={command}
									onChange={(e) => setCommand(e.target.value)}
								/>
							</div>
							<div className="space-y-1.5">
								<Label>
									{t("agents.detail.mcp.addDialog.argsLabel")}{" "}
									<span className="text-muted-foreground font-normal text-xs">
										{t("agents.detail.mcp.addDialog.argsHint")}
									</span>
								</Label>
								<Input
									placeholder="-y @modelcontextprotocol/server-filesystem /tmp"
									value={args}
									onChange={(e) => setArgs(e.target.value)}
								/>
							</div>
						</>
					) : (
						<div className="space-y-1.5">
							<Label>{t("agents.detail.mcp.addDialog.urlLabel")}</Label>
							<Input
								placeholder="https://mcp.example.com/sse"
								value={url}
								onChange={(e) => setUrl(e.target.value)}
							/>
						</div>
					)}
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						{t("agents.detail.mcp.addDialog.cancel")}
					</Button>
					<Button
						onClick={() => addMutation.mutate()}
						disabled={!serverName.trim() || addMutation.isPending}
					>
						{addMutation.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : (
							t("agents.detail.mcp.addDialog.addServer")
						)}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function MCPServersTab({
	projectId,
	agentId,
	canWrite,
}: {
	projectId?: string;
	agentId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { data: servers = [] } = useQuery(
		projectId
			? agentMCPServersQueryOptions(projectId, agentId)
			: globalAgentMCPServersQueryOptions(agentId),
	);
	const [addOpen, setAddOpen] = useState(false);

	const mcpServersKey = (
		projectId
			? agentMCPServersQueryOptions(projectId, agentId)
			: globalAgentMCPServersQueryOptions(agentId)
	).queryKey;

	const toggleMutation = useMutation({
		mutationFn: (s: AgentMCPServer) => {
			const payload = { is_enabled: !s.is_enabled };
			return projectId
				? updateMCPServer(projectId, agentId, s.id, payload)
				: updateGlobalMCPServer(agentId, s.id, payload);
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: mcpServersKey });
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (id: string) =>
			projectId
				? deleteMCPServer(projectId, agentId, id)
				: deleteGlobalMCPServer(agentId, id),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: mcpServersKey });
		},
	});

	return (
		<div className="space-y-4">
			<div className="flex items-center justify-between">
				<p className="text-sm text-muted-foreground">
					{t("agents.detail.mcp.serverCount", { count: servers.length })}
				</p>
				{canWrite && (
					<Button size="sm" onClick={() => setAddOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("agents.detail.mcp.addServer")}
					</Button>
				)}
			</div>

			{servers.length === 0 ? (
				<div className="flex flex-col items-center justify-center gap-3 py-14 rounded-xl border border-dashed border-border">
					<Server className="size-8 text-muted-foreground/40" />
					<p className="text-sm text-muted-foreground">
						{t("agents.detail.mcp.empty.title")}
					</p>
					{canWrite && (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setAddOpen(true)}
						>
							<Plus className="size-3.5 mr-1" />
							{t("agents.detail.mcp.empty.addFirstServer")}
						</Button>
					)}
				</div>
			) : (
				<div className="space-y-2">
					{servers.map((s) => (
						<div
							key={s.id}
							className="flex items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-4 py-3"
						>
							<div className="flex items-center gap-3 min-w-0">
								<Server className="size-4 text-muted-foreground shrink-0" />
								<div className="min-w-0">
									<p className="text-sm font-medium truncate">
										{s.server_name}
									</p>
									<p className="text-xs text-muted-foreground font-mono truncate">
										{s.transport}
										{s.command ? ` · ${s.command}` : ""}
										{s.url ? ` · ${s.url}` : ""}
									</p>
								</div>
							</div>
							<div className="flex items-center gap-2 shrink-0">
								<Switch
									checked={s.is_enabled}
									onCheckedChange={() => canWrite && toggleMutation.mutate(s)}
									disabled={!canWrite || toggleMutation.isPending}
								/>
								{canWrite && (
									<Button
										variant="ghost"
										size="icon"
										className="size-7 text-muted-foreground hover:text-destructive"
										onClick={() => deleteMutation.mutate(s.id)}
										disabled={deleteMutation.isPending}
									>
										<Trash2 className="size-3.5" />
									</Button>
								)}
							</div>
						</div>
					))}
				</div>
			)}

			<AddMCPServerDialog
				projectId={projectId}
				agentId={agentId}
				open={addOpen}
				onOpenChange={setAddOpen}
			/>
		</div>
	);
}

// ── Skills Tab ────────────────────────────────────────────────────────────────

function AddSkillDialog({
	projectId,
	agentId,
	open,
	onOpenChange,
}: {
	projectId?: string;
	agentId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [skillName, setSkillName] = useState("");
	const [source, setSource] = useState<"inline" | "marketplace" | "github_url">(
		"inline",
	);
	const [skillContent, setSkillContent] = useState("");
	const [sourceUrl, setSourceUrl] = useState("");

	const addMutation = useMutation({
		mutationFn: () => {
			const payload = {
				skill_name: skillName.trim(),
				skill_source: source,
				skill_content: source === "inline" ? skillContent : undefined,
				source_url: source !== "inline" ? sourceUrl.trim() : null,
			};
			return projectId
				? addSkill(projectId, agentId, payload)
				: addGlobalSkill(agentId, payload);
		},
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: (projectId
					? agentSkillsQueryOptions(projectId, agentId)
					: globalAgentSkillsQueryOptions(agentId)
				).queryKey,
			});
			onOpenChange(false);
			setSkillName("");
			setSkillContent("");
			setSourceUrl("");
		},
	});

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-md">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						<Wand2 className="size-4 text-primary" />
						{t("agents.detail.skills.addDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("agents.detail.skills.addDialog.description")}
					</DialogDescription>
				</DialogHeader>
				<div className="space-y-4 py-2">
					<div className="space-y-1.5">
						<Label>{t("agents.detail.skills.addDialog.skillNameLabel")}</Label>
						<Input
							placeholder="code-reviewer"
							value={skillName}
							onChange={(e) => setSkillName(e.target.value)}
						/>
					</div>
					<div className="space-y-1.5">
						<Label>{t("agents.detail.skills.addDialog.sourceLabel")}</Label>
						<Select
							value={source}
							onValueChange={(v) => setSource(v as typeof source)}
						>
							<SelectTrigger>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="inline">
									{t("agents.detail.skills.addDialog.sourceInline")}
								</SelectItem>
								<SelectItem value="marketplace">
									{t("agents.detail.skills.addDialog.sourceMarketplace")}
								</SelectItem>
								<SelectItem value="github_url">
									{t("agents.detail.skills.addDialog.sourceGithubUrl")}
								</SelectItem>
							</SelectContent>
						</Select>
					</div>
					{source === "inline" ? (
						<div className="space-y-1.5">
							<Label>
								{t("agents.detail.skills.addDialog.skillContentLabel")}
							</Label>
							<Textarea
								placeholder="# Code Reviewer&#10;&#10;You review pull requests for security, performance…"
								value={skillContent}
								onChange={(e) => setSkillContent(e.target.value)}
								rows={5}
								className="font-mono text-xs"
							/>
						</div>
					) : (
						<div className="space-y-1.5">
							<Label>{t("agents.detail.skills.addDialog.urlLabel")}</Label>
							<Input
								placeholder={
									source === "marketplace"
										? "paca/code-reviewer@1.0.0"
										: "https://github.com/org/skills/blob/main/SKILL.md"
								}
								value={sourceUrl}
								onChange={(e) => setSourceUrl(e.target.value)}
							/>
						</div>
					)}
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						{t("agents.detail.skills.addDialog.cancel")}
					</Button>
					<Button
						onClick={() => addMutation.mutate()}
						disabled={!skillName.trim() || addMutation.isPending}
					>
						{addMutation.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : (
							t("agents.detail.skills.addDialog.addSkill")
						)}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function SkillsTab({
	projectId,
	agentId,
	canWrite,
}: {
	projectId?: string;
	agentId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { data: skills = [] } = useQuery(
		projectId
			? agentSkillsQueryOptions(projectId, agentId)
			: globalAgentSkillsQueryOptions(agentId),
	);
	const [addOpen, setAddOpen] = useState(false);

	const skillsKey = (
		projectId
			? agentSkillsQueryOptions(projectId, agentId)
			: globalAgentSkillsQueryOptions(agentId)
	).queryKey;

	const toggleMutation = useMutation({
		mutationFn: (s: AgentSkill) => {
			const payload = { is_enabled: !s.is_enabled };
			return projectId
				? updateSkill(projectId, agentId, s.id, payload)
				: updateGlobalSkill(agentId, s.id, payload);
		},
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: skillsKey });
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (id: string) =>
			projectId
				? deleteSkill(projectId, agentId, id)
				: deleteGlobalSkill(agentId, id),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: skillsKey });
		},
	});

	return (
		<div className="space-y-4">
			<div className="flex items-center justify-between">
				<p className="text-sm text-muted-foreground">
					{t("agents.detail.skills.skillCount", { count: skills.length })}
				</p>
				{canWrite && (
					<Button size="sm" onClick={() => setAddOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("agents.detail.skills.addSkill")}
					</Button>
				)}
			</div>

			{skills.length === 0 ? (
				<div className="flex flex-col items-center justify-center gap-3 py-14 rounded-xl border border-dashed border-border">
					<Wand2 className="size-8 text-muted-foreground/40" />
					<p className="text-sm text-muted-foreground">
						{t("agents.detail.skills.empty.title")}
					</p>
					{canWrite && (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setAddOpen(true)}
						>
							<Plus className="size-3.5 mr-1" />
							{t("agents.detail.skills.empty.addFirstSkill")}
						</Button>
					)}
				</div>
			) : (
				<div className="space-y-2">
					{skills.map((s) => (
						<div
							key={s.id}
							className="flex items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-4 py-3"
						>
							<div className="flex items-center gap-3 min-w-0">
								<Code2 className="size-4 text-muted-foreground shrink-0" />
								<div className="min-w-0">
									<p className="text-sm font-medium truncate">{s.skill_name}</p>
									<p className="text-xs text-muted-foreground">
										{s.skill_source}
										{s.source_url ? ` · ${s.source_url}` : ""}
									</p>
								</div>
							</div>
							<div className="flex items-center gap-2 shrink-0">
								<Switch
									checked={s.is_enabled}
									onCheckedChange={() => canWrite && toggleMutation.mutate(s)}
									disabled={!canWrite || toggleMutation.isPending}
								/>
								{canWrite && (
									<Button
										variant="ghost"
										size="icon"
										className="size-7 text-muted-foreground hover:text-destructive"
										onClick={() => deleteMutation.mutate(s.id)}
										disabled={deleteMutation.isPending}
									>
										<Trash2 className="size-3.5" />
									</Button>
								)}
							</div>
						</div>
					))}
				</div>
			)}

			<AddSkillDialog
				projectId={projectId}
				agentId={agentId}
				open={addOpen}
				onOpenChange={setAddOpen}
			/>
		</div>
	);
}

// ── Environment Variables Tab ────────────────────────────────────────────────

const ENV_VAR_KEY_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;

function AddEnvVarDialog({
	projectId,
	agentId,
	open,
	onOpenChange,
}: {
	projectId?: string;
	agentId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [key, setKey] = useState("");
	const [value, setValue] = useState("");

	const isKeyValid = ENV_VAR_KEY_PATTERN.test(key.trim());

	const addMutation = useMutation({
		mutationFn: () => {
			const payload = { key: key.trim(), value };
			return projectId
				? addEnvVar(projectId, agentId, payload)
				: addGlobalEnvVar(agentId, payload);
		},
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: (projectId
					? agentEnvVarsQueryOptions(projectId, agentId)
					: globalAgentEnvVarsQueryOptions(agentId)
				).queryKey,
			});
			onOpenChange(false);
			setKey("");
			setValue("");
		},
	});

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-md">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						<KeyRound className="size-4 text-primary" />
						{t("agents.detail.envVars.addDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("agents.detail.envVars.addDialog.description")}
					</DialogDescription>
				</DialogHeader>
				<div className="space-y-4 py-2">
					<div className="space-y-1.5">
						<Label>{t("agents.detail.envVars.addDialog.keyLabel")}</Label>
						<Input
							placeholder={t("agents.detail.envVars.addDialog.keyPlaceholder")}
							className="font-mono"
							value={key}
							onChange={(e) => setKey(e.target.value)}
						/>
						<p className="text-xs text-muted-foreground">
							{t("agents.detail.envVars.addDialog.keyHint")}
						</p>
					</div>
					<div className="space-y-1.5">
						<Label>{t("agents.detail.envVars.addDialog.valueLabel")}</Label>
						<Input
							type="password"
							autoComplete="off"
							className="font-mono"
							value={value}
							onChange={(e) => setValue(e.target.value)}
						/>
					</div>
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						{t("agents.detail.envVars.addDialog.cancel")}
					</Button>
					<Button
						onClick={() => addMutation.mutate()}
						disabled={!isKeyValid || !value || addMutation.isPending}
					>
						{addMutation.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : (
							t("agents.detail.envVars.addDialog.addVariable")
						)}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function EnvVarsTab({
	projectId,
	agentId,
	canWrite,
}: {
	projectId?: string;
	agentId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { data: envVars = [] } = useQuery(
		projectId
			? agentEnvVarsQueryOptions(projectId, agentId)
			: globalAgentEnvVarsQueryOptions(agentId),
	);
	const [addOpen, setAddOpen] = useState(false);

	const envVarsKey = (
		projectId
			? agentEnvVarsQueryOptions(projectId, agentId)
			: globalAgentEnvVarsQueryOptions(agentId)
	).queryKey;

	const deleteMutation = useMutation({
		mutationFn: (id: string) =>
			projectId
				? deleteEnvVar(projectId, agentId, id)
				: deleteGlobalEnvVar(agentId, id),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: envVarsKey });
		},
	});

	return (
		<div className="space-y-4">
			<div className="flex items-center justify-between">
				<p className="text-sm text-muted-foreground">
					{t("agents.detail.envVars.count", { count: envVars.length })}
				</p>
				{canWrite && (
					<Button size="sm" onClick={() => setAddOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("agents.detail.envVars.addVariable")}
					</Button>
				)}
			</div>

			{envVars.length === 0 ? (
				<div className="flex flex-col items-center justify-center gap-3 py-14 rounded-xl border border-dashed border-border">
					<KeyRound className="size-8 text-muted-foreground/40" />
					<p className="text-sm text-muted-foreground">
						{t("agents.detail.envVars.empty.title")}
					</p>
					{canWrite && (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setAddOpen(true)}
						>
							<Plus className="size-3.5 mr-1" />
							{t("agents.detail.envVars.empty.addFirst")}
						</Button>
					)}
				</div>
			) : (
				<div className="space-y-2">
					{envVars.map((v) => (
						<div
							key={v.id}
							className="flex items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-4 py-3"
						>
							<div className="flex items-center gap-3 min-w-0">
								<KeyRound className="size-4 text-muted-foreground shrink-0" />
								<div className="min-w-0">
									<p className="text-sm font-medium font-mono truncate">
										{v.key}
									</p>
									<p className="text-xs text-muted-foreground font-mono truncate">
										{v.value}
									</p>
								</div>
							</div>
							{canWrite && (
								<Button
									variant="ghost"
									size="icon"
									className="size-7 text-muted-foreground hover:text-destructive shrink-0"
									onClick={() => deleteMutation.mutate(v.id)}
									disabled={deleteMutation.isPending}
								>
									<Trash2 className="size-3.5" />
								</Button>
							)}
						</div>
					))}
				</div>
			)}

			<AddEnvVarDialog
				projectId={projectId}
				agentId={agentId}
				open={addOpen}
				onOpenChange={setAddOpen}
			/>
		</div>
	);
}

// ── Page ──────────────────────────────────────────────────────────────────────

const TABS = [
	{ id: "overview", labelKey: "agents.detail.tabs.overview", icon: Bot },
	{
		id: "mcp-servers",
		labelKey: "agents.detail.tabs.mcpServers",
		icon: Server,
	},
	{ id: "skills", labelKey: "agents.detail.tabs.skills", icon: Wand2 },
	{
		id: "env-vars",
		labelKey: "agents.detail.tabs.envVars",
		icon: KeyRound,
	},
	{
		id: "activity",
		labelKey: "agents.detail.tabs.activity",
		icon: ActivityIcon,
	},
] as const satisfies {
	id: Tab;
	labelKey: string;
	icon: React.ComponentType<{ className?: string }>;
}[];

export function AgentDetailView({
	projectId,
	agentId,
}: {
	/** Absent for a global agent (no project of its own). */
	projectId?: string;
	agentId: string;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();

	// Both permission hooks are always called (never conditionally, per the
	// rules of hooks) — useProjectPermissions no-ops its own query when
	// projectId is falsy, so only the one matching the current scope
	// actually fetches.
	const { hasProjectPermission } = useProjectPermissions(projectId ?? "");
	const { hasPermission: hasGlobalPermission } = usePermissions();
	const canWrite = projectId
		? hasProjectPermission("agents.write")
		: hasGlobalPermission("agents.write");

	const { data: projectAgent } = useQuery({
		...agentQueryOptions(projectId ?? "", agentId),
		enabled: !!projectId,
	});
	const { data: globalAgentData } = useQuery({
		...globalAgentQueryOptions(agentId),
		enabled: !projectId,
	});
	const agent = projectId ? projectAgent : globalAgentData;

	const [activeTab, setActiveTab] = useState<Tab>(() => {
		const hash = window.location.hash.slice(1);
		if (hash && TABS.map((t) => t.id).includes(hash as Tab)) {
			return hash as Tab;
		}
		return "overview";
	});

	// Sync tab when hash changes (e.g., back button)
	useEffect(() => {
		const handleHashChange = () => {
			const hash = window.location.hash.slice(1);
			if (hash && TABS.map((t) => t.id).includes(hash as Tab)) {
				setActiveTab(hash as Tab);
			}
		};
		window.addEventListener("hashchange", handleHashChange);
		return () => window.removeEventListener("hashchange", handleHashChange);
	}, []);

	const handleTabChange = (tab: Tab) => {
		setActiveTab(tab);
		const url = new URL(window.location.href);
		url.hash = tab;
		window.history.pushState(null, "", url);
	};

	// ACP agents run entirely in the user's own local environment (see the
	// Local Bridge panel on the Overview tab) — Paca never forwards tools,
	// MCP servers, skills, or environment variables into that local process,
	// so none of those tabs apply. A global agent's activity is inherently
	// project-shaped (task/doc activity within one project) and it may be
	// invited into many projects or none, so Activity has no single feed to
	// show and is hidden entirely at global scope. Computed here (before the
	// !agent early return below) so both the correction effect and the render
	// below share one definition — agent?.agent_type is undefined pre-load,
	// which simply leaves the ACP-only filter a no-op until agent arrives.
	const acpHiddenTabs: Tab[] = ["mcp-servers", "skills", "env-vars"];
	// A provider_cli agent's skills are synced into its underlying CLI's own
	// config (see internal/executor/providercli on the agent-runner side) —
	// shipped for Claude Code only, since Codex/Cursor/Gemini CLI's native
	// skill-file formats aren't confirmed with enough confidence to sync
	// blindly. MCP servers and env vars still apply to every provider_cli
	// agent regardless, so only the Skills tab is conditionally hidden here.
	const cliSkillsUnsupported =
		agent?.agent_type === "provider_cli" &&
		agent.cli_provider !== "claude-code";
	const visibleTabs = TABS.filter((tab) => {
		if (tab.id === "activity" && !projectId) return false;
		if (agent?.agent_type === "acp" && acpHiddenTabs.includes(tab.id)) {
			return false;
		}
		if (tab.id === "skills" && cliSkillsUnsupported) return false;
		return true;
	});

	// A tab restored from location.hash on mount (or left over from a
	// previous agent/scope navigated to via the same route) may not apply
	// here — e.g. "#activity" on a global agent page, or "#mcp-servers" for
	// an agent that turns out to be ACP-type once it loads. Without this,
	// the tab strip silently shows no active button and the content pane
	// below renders nothing (none of its activeTab===X branches match),
	// rather than falling back to a tab that's actually visible.
	useEffect(() => {
		if (!visibleTabs.some((tab) => tab.id === activeTab)) {
			setActiveTab("overview");
		}
	}, [visibleTabs, activeTab]);

	if (!agent) {
		return (
			<div className="flex flex-col gap-4 p-6">
				<Skeleton className="h-16 w-full rounded-xl" />
				<Skeleton className="h-64 w-full rounded-xl" />
			</div>
		);
	}

	const initials = agent.name
		.split(" ")
		.map((w) => w[0])
		.join("")
		.toUpperCase()
		.slice(0, 2);

	return (
		<div className="flex flex-col flex-1 min-h-0">
			{/* Agent header */}
			<div className="border-b border-border/50 px-6 py-5 shrink-0">
				<div className="flex items-center gap-4">
					<AvatarUpload
						basePath={
							projectId
								? `/projects/${projectId}/agents/${agent.id}`
								: `/admin/agents/${agent.id}`
						}
						avatarUrl={resolveAgentAvatarUrl(agent, "full")}
						canRemove={!!agent.avatar_url}
						fallback={initials}
						disabled={!canWrite}
						className="size-12 rounded-xl bg-primary/10"
						fallbackClassName="bg-primary/10 text-primary font-bold text-base"
						labels={{
							change: t("agents.detail.avatar.change"),
							remove: t("agents.detail.avatar.remove"),
							uploading: t("agents.detail.avatar.uploading"),
							invalidType: t("agents.detail.avatar.errors.invalidType"),
							tooLarge: t("agents.detail.avatar.errors.tooLarge"),
							uploadFailed: t("agents.detail.avatar.errors.uploadFailed"),
							removeFailed: t("agents.detail.avatar.errors.removeFailed"),
						}}
						onChange={(result) => {
							qc.setQueryData(
								(projectId
									? agentQueryOptions(projectId, agent.id)
									: globalAgentQueryOptions(agent.id)
								).queryKey,
								(old) => (old ? { ...old, ...result } : old),
							);
							// The single-agent cache above only fixes this page. Every
							// other place that shows this agent's avatar — the agent
							// list/cards, the chat agent picker, the conversations list,
							// and (via project members) the team page and task
							// assignee/reporter chips — reads from separate query caches
							// that won't pick up the change until invalidated. A global
							// agent can also belong to several projects at once, so the
							// members invalidation matches every project's members query,
							// not just this one.
							qc.invalidateQueries({
								queryKey: projectId
									? ["projects", projectId, "agents"]
									: ["global-agents"],
							});
							qc.invalidateQueries({
								predicate: (query) =>
									query.queryKey[0] === "projects" &&
									query.queryKey[2] === "members",
							});
						}}
					/>
					<div>
						<h1 className="text-lg font-semibold">{agent.name}</h1>
						<div className="flex items-center gap-2 mt-0.5">
							<span className="text-sm text-muted-foreground">
								@{agent.handle}
							</span>
							<span className="text-muted-foreground/40">·</span>
							<Badge variant="secondary" className="text-xs">
								{agent.agent_type === "acp"
									? (agent.acp_provider ?? "acp")
									: agent.agent_type === "provider_cli"
										? (agent.cli_provider ?? "provider_cli")
										: agent.llm_provider}
							</Badge>
						</div>
					</div>
				</div>
			</div>

			{/* Tabs */}
			<div className="border-b border-border/50 px-6 shrink-0">
				<div className="flex items-center gap-1 -mb-px">
					{visibleTabs.map((tab) => {
						const Icon = tab.icon;
						const isActive = activeTab === tab.id;
						return (
							<button
								key={tab.id}
								type="button"
								onClick={() => handleTabChange(tab.id)}
								className={`flex items-center gap-1.5 px-3 py-2.5 text-sm font-medium border-b-2 transition-colors ${
									isActive
										? "border-primary text-primary"
										: "border-transparent text-muted-foreground hover:text-foreground"
								}`}
							>
								<Icon className="size-3.5" />
								{t(tab.labelKey)}
							</button>
						);
					})}
				</div>
			</div>

			{/* Tab content */}
			<div
				className={
					activeTab === "activity"
						? "flex flex-1 min-h-0 flex-col p-6"
						: "flex-1 overflow-auto p-6"
				}
			>
				{activeTab === "overview" && (
					<OverviewTab
						agent={agent}
						projectId={projectId}
						canWrite={canWrite}
					/>
				)}
				{activeTab === "mcp-servers" && agent.agent_type !== "acp" && (
					<MCPServersTab
						projectId={projectId}
						agentId={agentId}
						canWrite={canWrite}
					/>
				)}
				{activeTab === "skills" &&
					agent.agent_type !== "acp" &&
					!cliSkillsUnsupported && (
						<SkillsTab
							projectId={projectId}
							agentId={agentId}
							canWrite={canWrite}
						/>
					)}
				{activeTab === "env-vars" && agent.agent_type !== "acp" && (
					<EnvVarsTab
						projectId={projectId}
						agentId={agentId}
						canWrite={canWrite}
					/>
				)}
				{activeTab === "activity" && projectId && (
					<AgentActivityTab projectId={projectId} agentId={agentId} />
				)}
			</div>
		</div>
	);
}
