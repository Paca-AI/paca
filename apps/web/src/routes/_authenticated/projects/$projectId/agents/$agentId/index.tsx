import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
	Bot,
	Check,
	Clock,
	Code2,
	GitBranch,
	GitPullRequest,
	Loader2,
	MessageSquare,
	Plus,
	Save,
	Server,
	Trash2,
	Wand2,
	Zap,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConversationView } from "@/components/projects/agents/conversation-view";

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
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	type Agent,
	type AgentConversation,
	type AgentMCPServer,
	type AgentSkill,
	addMCPServer,
	addSkill,
	agentMCPServersQueryOptions,
	agentQueryOptions,
	agentSkillsQueryOptions,
	CONVERSATION_STATUS_COLORS,
	conversationsQueryOptions,
	deleteMCPServer,
	deleteSkill,
	llmModelsQueryOptions,
	updateAgent,
	updateMCPServer,
	updateSkill,
} from "@/lib/agent-api";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/agents/$agentId/",
)({
	loader: async ({
		context: { queryClient },
		params: { projectId, agentId },
	}) => {
		await Promise.all([
			queryClient.ensureQueryData(agentQueryOptions(projectId, agentId)),
			queryClient.ensureQueryData(
				agentMCPServersQueryOptions(projectId, agentId),
			),
			queryClient.ensureQueryData(agentSkillsQueryOptions(projectId, agentId)),
			queryClient.ensureQueryData(
				conversationsQueryOptions(projectId, agentId),
			),
			queryClient.ensureQueryData(llmModelsQueryOptions),
		]);
	},
	component: AgentDetailPage,
});

type Tab = "overview" | "mcp-servers" | "skills" | "conversations";

const CUSTOM = "__custom__";

// ── Overview Tab ──────────────────────────────────────────────────────────────

function OverviewTab({
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
	const { data: llmModels = {} } = useQuery(llmModelsQueryOptions);

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
	const [taskTriggerPrompt, setTaskTriggerPrompt] = useState(
		agent.task_trigger_prompt,
	);
	const [docCommentTriggerPrompt, setDocCommentTriggerPrompt] = useState(
		agent.doc_comment_trigger_prompt,
	);
	const [chatTriggerPrompt, setChatTriggerPrompt] = useState(
		agent.chat_trigger_prompt,
	);
	const [descriptionWriteTriggerPrompt, setDescriptionWriteTriggerPrompt] =
		useState(agent.description_write_trigger_prompt);
	const [canClone, setCanClone] = useState(agent.can_clone_repos);
	const [committerName, setCommitterName] = useState(agent.git_committer_name);
	const [committerEmail, setCommitterEmail] = useState(
		agent.git_committer_email,
	);

	// Derived final values sent to the API
	const llmProvider =
		providerSelect === CUSTOM ? customProvider.trim() : providerSelect;
	const llmModel = modelSelect === CUSTOM ? customModel.trim() : modelSelect;

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

	const isDirty =
		name !== agent.name ||
		llmProvider !== agent.llm_provider ||
		llmModel !== agent.llm_model ||
		llmApiKey !== "" ||
		llmBaseUrl !== (agent.llm_base_url ?? "") ||
		systemPrompt !== agent.system_prompt ||
		taskTriggerPrompt !== agent.task_trigger_prompt ||
		docCommentTriggerPrompt !== agent.doc_comment_trigger_prompt ||
		chatTriggerPrompt !== agent.chat_trigger_prompt ||
		descriptionWriteTriggerPrompt !== agent.description_write_trigger_prompt ||
		canClone !== agent.can_clone_repos ||
		committerName !== agent.git_committer_name ||
		committerEmail !== agent.git_committer_email;

	const saveMutation = useMutation({
		mutationFn: () =>
			updateAgent(projectId, agent.id, {
				name: name.trim(),
				llm_provider: llmProvider,
				llm_model: llmModel,
				...(llmApiKey ? { llm_api_key: llmApiKey } : {}),
				llm_base_url: llmBaseUrl,
				system_prompt: systemPrompt,
				task_trigger_prompt: taskTriggerPrompt,
				doc_comment_trigger_prompt: docCommentTriggerPrompt,
				chat_trigger_prompt: chatTriggerPrompt,
				description_write_trigger_prompt: descriptionWriteTriggerPrompt,
				can_clone_repos: canClone,
				git_committer_name: committerName.trim(),
				git_committer_email: committerEmail.trim(),
			}),
		onSuccess: (updated) => {
			qc.setQueryData(["projects", projectId, "agents", agent.id], updated);
			setLlmApiKey("");
		},
	});

	const canSave =
		isDirty &&
		!!llmProvider &&
		!!llmModel &&
		!!llmBaseUrl.trim() &&
		!saveMutation.isPending;

	return (
		<div className="space-y-6 max-w-2xl">
			<div className="space-y-1.5">
				<Label>{t("project.agents.name")}</Label>
				<Input
					value={name}
					onChange={(e) => setName(e.target.value)}
					disabled={!canWrite}
				/>
			</div>

			<Separator />

			<div>
				<p className="text-sm font-medium mb-3">
					{t("project.agents.detail.llmConfig")}
				</p>
				<div className="grid grid-cols-2 gap-3">
					<div className="space-y-1.5">
						<Label>{t("project.agents.provider")}</Label>
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
								<SelectItem value={CUSTOM}>{t("project.agents.custom")}</SelectItem>
							</SelectContent>
						</Select>
						{providerSelect === CUSTOM && (
							<Input
								placeholder={t("project.agents.providerPlaceholder")}
								value={customProvider}
								onChange={(e) => setCustomProvider(e.target.value)}
								disabled={!canWrite}
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
										<SelectItem value={CUSTOM}>{t("project.agents.custom")}</SelectItem>
									</SelectContent>
								</Select>
								{modelSelect === CUSTOM && (
									<Input
										placeholder={t("project.agents.modelPlaceholder")}
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
						{t("project.agents.detail.apiKeyUpdate")}{" "}
						<span className="text-muted-foreground font-normal text-xs">
							{t("project.agents.detail.apiKeyUpdateHint")}
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
						{t("project.agents.baseUrl")}{" "}
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

			<Separator />

			<div className="space-y-1.5">
				<Label>{t("project.agents.systemPrompt")}</Label>
				<Textarea
					value={systemPrompt}
					onChange={(e) => setSystemPrompt(e.target.value)}
					rows={5}
					disabled={!canWrite}
					className="font-mono text-xs"
				/>
			</div>

			<div className="space-y-2">
				<div>
					<Label className="text-xs font-semibold uppercase tracking-widest text-muted-foreground">
						{t("project.agents.detail.triggerPrompts")}
					</Label>
					<p className="mt-1 text-xs text-muted-foreground">
						{t("project.agents.detail.triggerPromptsHint")}
					</p>
				</div>
				{(
					[
						[
							t("project.agents.detail.triggerTask"),
							taskTriggerPrompt,
							setTaskTriggerPrompt,
						],
						[
							t("project.agents.detail.triggerDocComment"),
							docCommentTriggerPrompt,
							setDocCommentTriggerPrompt,
						],
						[
							t("project.agents.detail.triggerChat"),
							chatTriggerPrompt,
							setChatTriggerPrompt,
						],
						[
							t("project.agents.detail.triggerDescriptionWrite"),
							descriptionWriteTriggerPrompt,
							setDescriptionWriteTriggerPrompt,
						],
					] as [string, string, (v: string) => void][]
				).map(([label, value, setValue]) => (
					<details
						key={label}
						className="group rounded-md border border-border/60 bg-muted/20"
					>
						<summary className="flex cursor-pointer select-none items-center gap-2 px-3 py-2 text-xs font-medium">
							{label}
						</summary>
						<div className="border-t border-border/60 px-3 py-2">
							<Textarea
								value={value}
								onChange={(e) => setValue(e.target.value)}
								rows={6}
								disabled={!canWrite}
								className="font-mono text-xs leading-relaxed"
							/>
						</div>
					</details>
				))}
			</div>

			<Separator />

			<div>
				<p className="text-sm font-medium mb-3">Capabilities</p>
				<div className="space-y-3">
					<div className="flex items-center justify-between">
						<div>
							<p className="text-sm">Clone repositories</p>
							<p className="text-xs text-muted-foreground">
								{t("project.agents.detail.cloneReposHint")}
							</p>
						</div>
						<Switch
							checked={canClone}
							onCheckedChange={setCanClone}
							disabled={!canWrite}
						/>
					</div>
				</div>
			</div>

			<Separator />

			<div>
				<p className="text-sm font-medium mb-1">{t("project.agents.detail.gitCommitter")}</p>
				<p className="text-xs text-muted-foreground mb-3">
					{t("project.agents.detail.gitCommitterHint")}
				</p>
				<div className="grid grid-cols-2 gap-3">
					<div className="space-y-1.5">
						<Label>Committer name</Label>
						<Input
							value={committerName}
							onChange={(e) => setCommitterName(e.target.value)}
							disabled={!canWrite}
							placeholder="paca-agent"
						/>
					</div>
					<div className="space-y-1.5">
						<Label>Committer email</Label>
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

			{canWrite && (
				<div className="flex items-center gap-3 pt-2">
					<Button onClick={() => saveMutation.mutate()} disabled={!canSave}>
						{saveMutation.isPending ? (
							<Loader2 className="size-4 mr-2 animate-spin" />
						) : (
							<Save className="size-4 mr-2" />
						)}
						{t("project.agents.detail.saveChanges")}
					</Button>
					{saveMutation.isSuccess && (
						<span className="flex items-center gap-1 text-xs text-emerald-600">
							<Check className="size-3" />
							{t("project.agents.detail.saved")}
						</span>
					)}
					{saveMutation.isError && (
						<span className="text-xs text-destructive">
							{t("project.agents.detail.saveError")}
						</span>
					)}
				</div>
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
	projectId: string;
	agentId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation();
	const qc = useQueryClient();
	const [serverName, setServerName] = useState("");
	const [transport, setTransport] = useState<"stdio" | "sse" | "http">("stdio");
	const [command, setCommand] = useState("");
	const [args, setArgs] = useState("");
	const [url, setUrl] = useState("");

	const addMutation = useMutation({
		mutationFn: () =>
			addMCPServer(projectId, agentId, {
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
			}),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "agents", agentId, "mcp-servers"],
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
						{t("project.agents.detail.addMcpServerTitle")}
					</DialogTitle>
					<DialogDescription>
						{t("project.agents.detail.addMcpServerDesc")}
					</DialogDescription>
				</DialogHeader>
				<div className="space-y-4 py-2">
					<div className="space-y-1.5">
						<Label>{t("project.agents.detail.serverName")}</Label>
						<Input
							placeholder="filesystem"
							value={serverName}
							onChange={(e) => setServerName(e.target.value)}
						/>
					</div>
					<div className="space-y-1.5">
						<Label>{t("project.agents.detail.transport")}</Label>
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
								<Label>{t("project.agents.detail.command")}</Label>
								<Input
									placeholder="npx"
									value={command}
									onChange={(e) => setCommand(e.target.value)}
								/>
							</div>
							<div className="space-y-1.5">
								<Label>
									{t("project.agents.detail.args")}{" "}
									<span className="text-muted-foreground font-normal text-xs">
										{t("project.agents.detail.spaceSeparated")}
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
							<Label>{t("project.agents.url")}</Label>
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
						{t("project.agents.cancel")}
					</Button>
					<Button
						onClick={() => addMutation.mutate()}
						disabled={!serverName.trim() || addMutation.isPending}
					>
						{addMutation.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : (
							t("project.agents.detail.addServer")
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
	projectId: string;
	agentId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation();
	const qc = useQueryClient();
	const { data: servers = [] } = useQuery(
		agentMCPServersQueryOptions(projectId, agentId),
	);
	const [addOpen, setAddOpen] = useState(false);

	const toggleMutation = useMutation({
		mutationFn: (s: AgentMCPServer) =>
			updateMCPServer(projectId, agentId, s.id, {
				is_enabled: !s.is_enabled,
			}),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "agents", agentId, "mcp-servers"],
			});
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (id: string) => deleteMCPServer(projectId, agentId, id),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "agents", agentId, "mcp-servers"],
			});
		},
	});

	return (
		<div className="space-y-4">
			<div className="flex items-center justify-between">
				<p className="text-sm text-muted-foreground">
					{t("project.agents.detail.serversConfigured", { count: servers.length })}
				</p>
				{canWrite && (
					<Button size="sm" onClick={() => setAddOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("project.agents.detail.addServer")}
					</Button>
				)}
			</div>

			{servers.length === 0 ? (
				<div className="flex flex-col items-center justify-center gap-3 py-14 rounded-xl border border-dashed border-border">
					<Server className="size-8 text-muted-foreground/40" />
					<p className="text-sm text-muted-foreground">
						{t("project.agents.detail.noServers")}
					</p>
					{canWrite && (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setAddOpen(true)}
						>
							<Plus className="size-3.5 mr-1" />
							{t("project.agents.detail.addFirstServer")}
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
	projectId: string;
	agentId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation();
	const qc = useQueryClient();
	const [skillName, setSkillName] = useState("");
	const [source, setSource] = useState<"inline" | "marketplace" | "github_url">(
		"inline",
	);
	const [skillContent, setSkillContent] = useState("");
	const [sourceUrl, setSourceUrl] = useState("");

	const addMutation = useMutation({
		mutationFn: () =>
			addSkill(projectId, agentId, {
				skill_name: skillName.trim(),
				skill_source: source,
				skill_content: source === "inline" ? skillContent : undefined,
				source_url: source !== "inline" ? sourceUrl.trim() : null,
			}),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "agents", agentId, "skills"],
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
						{t("project.agents.detail.addSkillTitle")}
					</DialogTitle>
					<DialogDescription>
						{t("project.agents.detail.addSkillDesc")}
					</DialogDescription>
				</DialogHeader>
				<div className="space-y-4 py-2">
					<div className="space-y-1.5">
						<Label>{t("project.agents.detail.skillName")}</Label>
						<Input
							placeholder="code-reviewer"
							value={skillName}
							onChange={(e) => setSkillName(e.target.value)}
						/>
					</div>
					<div className="space-y-1.5">
						<Label>{t("project.agents.detail.source")}</Label>
						<Select
							value={source}
							onValueChange={(v) => setSource(v as typeof source)}
						>
							<SelectTrigger>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="inline">
									{t("project.agents.detail.sourceInline")}
								</SelectItem>
								<SelectItem value="marketplace">
									{t("project.agents.detail.sourceMarketplace")}
								</SelectItem>
								<SelectItem value="github_url">
									{t("project.agents.detail.sourceGithubUrl")}
								</SelectItem>
							</SelectContent>
						</Select>
					</div>
					{source === "inline" ? (
						<div className="space-y-1.5">
							<Label>{t("project.agents.detail.skillContent")}</Label>
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
							<Label>{t("project.agents.url")}</Label>
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
						{t("project.agents.cancel")}
					</Button>
					<Button
						onClick={() => addMutation.mutate()}
						disabled={!skillName.trim() || addMutation.isPending}
					>
						{addMutation.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : (
							t("project.agents.detail.addSkill")
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
	projectId: string;
	agentId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation();
	const qc = useQueryClient();
	const { data: skills = [] } = useQuery(
		agentSkillsQueryOptions(projectId, agentId),
	);
	const [addOpen, setAddOpen] = useState(false);

	const toggleMutation = useMutation({
		mutationFn: (s: AgentSkill) =>
			updateSkill(projectId, agentId, s.id, { is_enabled: !s.is_enabled }),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "agents", agentId, "skills"],
			});
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (id: string) => deleteSkill(projectId, agentId, id),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: ["projects", projectId, "agents", agentId, "skills"],
			});
		},
	});

	return (
		<div className="space-y-4">
			<div className="flex items-center justify-between">
				<p className="text-sm text-muted-foreground">
					{t("project.agents.detail.skillsAttached", { count: skills.length })}
				</p>
				{canWrite && (
					<Button size="sm" onClick={() => setAddOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("project.agents.detail.addSkill")}
					</Button>
				)}
			</div>

			{skills.length === 0 ? (
				<div className="flex flex-col items-center justify-center gap-3 py-14 rounded-xl border border-dashed border-border">
					<Wand2 className="size-8 text-muted-foreground/40" />
					<p className="text-sm text-muted-foreground">
						{t("project.agents.detail.noSkills")}
					</p>
					{canWrite && (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setAddOpen(true)}
						>
							<Plus className="size-3.5 mr-1" />
							{t("project.agents.detail.addFirstSkill")}
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

// ── Conversations Tab ─────────────────────────────────────────────────────────

function ConversationRow({
	conv,
	projectId,
	onClick,
}: {
	conv: AgentConversation;
	projectId: string;
	onClick: () => void;
}) {
	const { t, i18n } = useTranslation();
	const statusColor = CONVERSATION_STATUS_COLORS[conv.status];
	const statusLabel = t(`project.agents.status.${conv.status}`);

	return (
		<div className="w-full flex items-center gap-4 rounded-lg border border-border/60 bg-card px-4 py-3 transition-colors hover:border-border hover:bg-accent/30">
			<button
				type="button"
				onClick={onClick}
				className="flex flex-col gap-0.5 min-w-0 flex-1 text-left"
			>
				<div className="flex items-center gap-2">
					<span className="text-sm font-medium truncate">
						{conv.trigger_type === "chat_message"
							? t("project.agents.detail.convTypeChat")
							: conv.trigger_type === "description_write"
								? t("project.agents.detail.convTypeWriteDescription")
								: t("project.agents.detail.convTypeTask")}{" "}
						· {conv.id.slice(0, 8)}
					</span>
					<Badge
						variant="outline"
						className={`text-xs font-semibold shrink-0 ${statusColor}`}
					>
						{statusLabel}
					</Badge>
				</div>
				<div className="flex items-center gap-3 text-xs text-muted-foreground">
					<span className="flex items-center gap-1">
						<Zap className="size-3" />
						{t("project.agents.detail.iterations", {
							count: conv.iteration_count,
						})}
					</span>
					{conv.branch_name && (
						<span className="flex items-center gap-1 truncate">
							<GitBranch className="size-3" />
							{conv.branch_name}
						</span>
					)}
					{conv.pr_url && (
						<span className="flex items-center gap-1">
							<GitPullRequest className="size-3" />
							{t("project.agents.detail.prOpened")}
						</span>
					)}
					<span className="flex items-center gap-1 ml-auto">
						<Clock className="size-3" />
						{new Date(conv.created_at).toLocaleDateString(i18n.language)}
					</span>
				</div>
			</button>
			<Link
				to="/projects/$projectId/conversations/$conversationId"
				params={{ projectId, conversationId: conv.id }}
				className="shrink-0 text-xs font-medium text-primary/70 hover:text-primary transition-colors"
			>
				{t("project.agents.detail.watch")}
			</Link>
		</div>
	);
}

function ConversationModal({
	projectId,
	conversationId,
	open,
	onOpenChange,
}: {
	projectId: string;
	conversationId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="max-w-3xl sm:max-w-3xl h-[80vh] flex flex-col p-0 gap-0 overflow-hidden">
				<ConversationView
					projectId={projectId}
					conversationId={conversationId}
				/>
			</DialogContent>
		</Dialog>
	);
}

function ConversationsTab({
	projectId,
	agentId,
}: {
	projectId: string;
	agentId: string;
}) {
	const { t } = useTranslation();
	const { data: conversations = [], isLoading } = useQuery(
		conversationsQueryOptions(projectId, agentId),
	);
	const [modalConvId, setModalConvId] = useState<string | null>(null);

	if (isLoading) {
		return (
			<div className="space-y-2">
				{Array.from({ length: 3 }).map((_, i) => (
					// biome-ignore lint/suspicious/noArrayIndexKey: skeleton
					<Skeleton key={i} className="h-16 rounded-lg" />
				))}
			</div>
		);
	}

	if (conversations.length === 0) {
		return (
			<div className="flex flex-col items-center justify-center gap-3 py-14 rounded-xl border border-dashed border-border">
				<MessageSquare className="size-8 text-muted-foreground/40" />
				<p className="text-sm text-muted-foreground">
					{t("project.agents.detail.noConversations")}
				</p>
				<p className="text-xs text-muted-foreground max-w-xs text-center">
					{t("project.agents.detail.noConversationsDesc")}
				</p>
			</div>
		);
	}

	return (
		<>
			<div className="space-y-2">
				{conversations.map((conv) => (
					<ConversationRow
						key={conv.id}
						conv={conv}
						projectId={projectId}
						onClick={() => setModalConvId(conv.id)}
					/>
				))}
			</div>

			{modalConvId && (
				<ConversationModal
					projectId={projectId}
					conversationId={modalConvId}
					open
					onOpenChange={(open) => {
						if (!open) setModalConvId(null);
					}}
				/>
			)}
		</>
	);
}

// ── Page ──────────────────────────────────────────────────────────────────────

const TABS: {
	id: Tab;
	labelKey: string;
	icon: React.ComponentType<{ className?: string }>;
}[] = [
	{ id: "overview", labelKey: "project.agents.detail.tabOverview", icon: Bot },
	{
		id: "mcp-servers",
		labelKey: "project.agents.detail.tabMcpServers",
		icon: Server,
	},
	{ id: "skills", labelKey: "project.agents.detail.tabSkills", icon: Wand2 },
	{
		id: "conversations",
		labelKey: "project.agents.detail.tabConversations",
		icon: MessageSquare,
	},
];

function AgentDetailPage() {
	const { t } = useTranslation();
	const { projectId, agentId } = Route.useParams();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("agents.write");

	const { data: agent } = useQuery(agentQueryOptions(projectId, agentId));
	const [activeTab, setActiveTab] = useState<Tab>("overview");

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
					<Avatar className="size-12 rounded-xl bg-primary/10">
						<AvatarFallback className="rounded-xl bg-primary/10 text-primary font-bold text-base">
							{initials}
						</AvatarFallback>
					</Avatar>
					<div>
						<h1 className="text-lg font-semibold">{agent.name}</h1>
						<div className="flex items-center gap-2 mt-0.5">
							<span className="text-sm text-muted-foreground">
								@{agent.handle}
							</span>
							<span className="text-muted-foreground/40">·</span>
							<Badge variant="secondary" className="text-xs">
								{agent.llm_provider}
							</Badge>
						</div>
					</div>
				</div>
			</div>

			{/* Tabs */}
			<div className="border-b border-border/50 px-6 shrink-0">
				<div className="flex items-center gap-1 -mb-px">
					{TABS.map((tab) => {
						const Icon = tab.icon;
						const isActive = activeTab === tab.id;
						return (
							<button
								key={tab.id}
								type="button"
								onClick={() => setActiveTab(tab.id)}
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
			<div className="flex-1 overflow-auto p-6">
				{activeTab === "overview" && (
					<OverviewTab
						agent={agent}
						projectId={projectId}
						canWrite={canWrite}
					/>
				)}
				{activeTab === "mcp-servers" && (
					<MCPServersTab
						projectId={projectId}
						agentId={agentId}
						canWrite={canWrite}
					/>
				)}
				{activeTab === "skills" && (
					<SkillsTab
						projectId={projectId}
						agentId={agentId}
						canWrite={canWrite}
					/>
				)}
				{activeTab === "conversations" && (
					<ConversationsTab projectId={projectId} agentId={agentId} />
				)}
			</div>
		</div>
	);
}
