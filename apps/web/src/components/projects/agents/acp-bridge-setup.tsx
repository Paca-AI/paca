import { useMutation, useQuery } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
	type ACPProvider,
	type AcpBridgeToken,
	acpBridgeStatusQueryOptions,
	generateAcpBridgeToken,
	generateAgentMCPKey,
	generateGlobalAcpBridgeToken,
	generateGlobalAgentMCPKey,
	globalAcpBridgeStatusQueryOptions,
} from "@/lib/agent-api";
import { copyToClipboard } from "@/lib/clipboard";

// Fixed commands for the setup steps that don't depend on any
// server-generated value — only the bridge run command comes back from the
// API, since it embeds the freshly generated token. Installs a prebuilt
// binary (see scripts/install-acp-bridge.sh, uploaded to every GitHub
// release alongside the binaries themselves) — no Go toolchain, Python, or
// uv required on the end user's machine, only Node.js for the `npx`-launched
// ACP CLI itself.
const BRIDGE_INSTALL_COMMAND =
	"curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/install-acp-bridge.sh | bash";

// Claude Code's CLI needs its own one-time interactive login, separate from
// (and prior to) authenticating the bridge to Paca — the bridge invokes
// `claude` non-interactively, so the CLI must already hold valid credentials
// before the bridge ever starts, or the process it spawns has nothing to
// authenticate with.
const CLAUDE_SETUP_TOKEN_COMMAND = "claude setup-token";

// `claude setup-token` prints a token to the terminal and does not save it
// anywhere itself — non-interactive/headless auth (what the bridge needs)
// only picks it up once it's exported as CLAUDE_CODE_OAUTH_TOKEN. See
// https://platform.claude.com/docs/en/authentication#generate-a-long-lived-token.
const CLAUDE_OAUTH_EXPORT_COMMAND =
	"export CLAUDE_CODE_OAUTH_TOKEN=<token-from-above>";

// Codex and Gemini CLI don't have an equivalent "generate a token" command —
// both read their API key straight from an env var — so unlike Claude Code
// above, this is the only auth step they need before the bridge starts.
// Returns null for "custom" (unknown provider, no guidance possible) and for
// "goose" and "opencode": Goose's own setup is `goose configure`
// (interactive) or one of several possible underlying-provider env vars
// depending on what the user picks inside it, neither of which fits this
// single copy-paste-able non-interactive export line the way Codex/Gemini
// CLI's single env var does — showing one anyway would risk being actively
// wrong. Goose users get the generic guidance in the bridge's own README
// instead (OpenCode likewise: `opencode auth login` / `opencode providers`
// is interactive and its credentials depend on which model provider the
// user picks).
function localAuthExportCommand(provider: ACPProvider): string | null {
	switch (provider) {
		case "codex":
			return "export OPENAI_API_KEY=<your-api-key>";
		case "gemini-cli":
			return "export GEMINI_API_KEY=<your-api-key>";
		default:
			return null;
	}
}

// PACA_API_URL is required here, not just a nice-to-have — the installer
// fetches bundled skill content from this instance's own API rather than
// GitHub (so it always matches the exact version running here), and this
// command is never run from a local clone of the Paca repo. apiKey is
// optional (the script's own endpoints are publicly readable) but sent when
// available so a locked-down deployment doesn't 401.
//
// The env vars are attached to `bash`, not `curl` — in a `VAR=val cmd1 |
// cmd2` pipeline, VAR is only exported into cmd1's environment, not cmd2's.
// Putting them before `curl` here would leave the piped script (which runs
// *inside* bash) unable to see either one, so it'd fall back to its own
// interactive prompt for both — even though the values were right there on
// the command line.
function skillInstallCommand(apiKey?: string): string {
	const envVars = [`PACA_API_URL=${window.location.origin}`];
	if (apiKey) envVars.push(`PACA_API_KEY=${apiKey}`);
	return `curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-paca-skills.sh | ${envVars.join(" ")} bash`;
}

const SKILL_CONTENT_URL =
	"https://github.com/Paca-AI/paca/blob/master/skills/paca/SKILL.md";

// The installer has a known integration for these providers — Claude Code
// and Gemini CLI both get their own global slash-command directory
// (~/.claude/commands, ~/.gemini/commands), and Codex reads the installer's
// project-scoped AGENTS.md output automatically since the ACP bridge runs
// it from the project's own working directory. A custom ACP server's
// command format is unknown, so it falls back to the raw-content link.
function supportsSkillInstaller(provider: ACPProvider): boolean {
	return provider !== "custom";
}

// mcpEnvPairs builds the PACA_* environment variables every ACP client's MCP
// config needs: the agent's own key (not the human's) plus PACA_AGENT_ID so
// paca-mcp authenticates and attributes tool calls as this agent (see
// apps/mcp's "Agent Mode"). PACA_PROJECT_ID pins tool calls to a single
// project — included for project-scoped agents; omitted for a global agent,
// which runs "unpinned" across every project it's invited into.
function mcpEnvPairs(
	agentId: string,
	projectId: string | undefined,
	apiKey: string,
): [string, string][] {
	const pairs: [string, string][] = [
		["PACA_API_KEY", apiKey],
		["PACA_API_URL", window.location.origin],
		["PACA_AGENT_ID", agentId],
	];
	if (projectId) pairs.push(["PACA_PROJECT_ID", projectId]);
	return pairs;
}

function claudeMcpConnectCommand(
	agentId: string,
	projectId: string | undefined,
	apiKey: string,
): string {
	const envFlags = mcpEnvPairs(agentId, projectId, apiKey)
		.map(([key, value]) => `--env ${key}=${value}`)
		.join(" ");
	return `claude mcp add paca ${envFlags} -- npx -y @paca-ai/paca-mcp`;
}

// Provider-agnostic reference: the underlying MCP command/args/env every ACP
// client needs, regardless of how that client's own config format (TOML,
// JSON, CLI flags, ...) wants it expressed — only Claude Code has a known
// native "add an MCP server" command (above), so this is the fallback for
// everything else.
function genericMcpConnectCommand(
	agentId: string,
	projectId: string | undefined,
	apiKey: string,
): string {
	const envVars = mcpEnvPairs(agentId, projectId, apiKey)
		.map(([key, value]) => `${key}=${value}`)
		.join(" ");
	return `${envVars} npx -y @paca-ai/paca-mcp`;
}

// Codex and Gemini CLI each have a real non-interactive "add MCP server"
// command (syntax verified against codex-cli and gemini-cli 0.46.0) — like
// Claude Code, running paca-mcp by hand does nothing for them; it has to be
// registered in their own config. Gemini's default scope is "project", so
// -s user is explicit (the bridge may run from any directory), and `--`
// keeps npx's own -y from being parsed as a gemini flag.
function codexMcpConnectCommand(
	agentId: string,
	projectId: string | undefined,
	apiKey: string,
): string {
	const envFlags = mcpEnvPairs(agentId, projectId, apiKey)
		.map(([key, value]) => `--env ${key}=${value}`)
		.join(" ");
	return `codex mcp add paca ${envFlags} -- npx -y @paca-ai/paca-mcp`;
}

function geminiMcpConnectCommand(
	agentId: string,
	projectId: string | undefined,
	apiKey: string,
): string {
	const envFlags = mcpEnvPairs(agentId, projectId, apiKey)
		.map(([key, value]) => `-e ${key}=${value}`)
		.join(" ");
	return `gemini mcp add -s user ${envFlags} paca npx -- -y @paca-ai/paca-mcp`;
}

// Goose's only persistent, non-interactive MCP registration is an entry in
// the `extensions` section of ~/.config/goose/config.yaml (format per
// Goose's own docs; UNVERIFIED — not exercised against a real goose install). Values are
// JSON-quoted, which is valid YAML flow-scalar syntax.
function gooseMcpConfig(
	agentId: string,
	projectId: string | undefined,
	apiKey: string,
): string {
	const envs = mcpEnvPairs(agentId, projectId, apiKey)
		.map(([key, value]) => `      ${key}: ${JSON.stringify(value)}`)
		.join("\n");
	return [
		"extensions:",
		"  paca:",
		"    name: Paca",
		"    type: stdio",
		"    cmd: npx",
		'    args: [-y, "@paca-ai/paca-mcp"]',
		"    enabled: true",
		"    timeout: 300",
		"    envs:",
		envs,
	].join("\n");
}

// OpenCode has no non-interactive "add MCP server" command (`opencode mcp
// add` is a prompt wizard) — its MCP servers live in the `mcp` section of
// opencode.json, and running paca-mcp by hand in a terminal does nothing
// for it, so show the config block to merge into
// ~/.config/opencode/opencode.json instead of the generic command above.
function opencodeMcpConfig(
	agentId: string,
	projectId: string | undefined,
	apiKey: string,
): string {
	return JSON.stringify(
		{
			mcp: {
				paca: {
					type: "local",
					command: ["npx", "-y", "@paca-ai/paca-mcp"],
					enabled: true,
					environment: Object.fromEntries(
						mcpEnvPairs(agentId, projectId, apiKey),
					),
				},
			},
		},
		null,
		2,
	);
}

// Picks the per-provider MCP registration step (a command or a config block).
function buildMcpConnectCommand(
	provider: ACPProvider,
	agentId: string,
	projectId: string | undefined,
	apiKey: string,
): string {
	switch (provider) {
		case "claude-code":
			return claudeMcpConnectCommand(agentId, projectId, apiKey);
		case "codex":
			return codexMcpConnectCommand(agentId, projectId, apiKey);
		case "gemini-cli":
			return geminiMcpConnectCommand(agentId, projectId, apiKey);
		case "opencode":
			return opencodeMcpConfig(agentId, projectId, apiKey);
		case "goose":
			return gooseMcpConfig(agentId, projectId, apiKey);
		default:
			return genericMcpConnectCommand(agentId, projectId, apiKey);
	}
}

type CopyField =
	| "install"
	| "run"
	| "skill"
	| "mcp"
	| "claudeSetupToken"
	| "claudeOauthExport"
	| "localAuthExport";

function providerLabel(
	provider: ACPProvider,
	t: TFunction<"projects">,
): string {
	switch (provider) {
		case "codex":
			return t("agents.createDialog.acpProviderCodex");
		case "gemini-cli":
			return t("agents.createDialog.acpProviderGeminiCli");
		case "goose":
			return t("agents.createDialog.acpProviderGoose");
		case "opencode":
			return t("agents.createDialog.acpProviderOpenCode");
		case "custom":
			return t("agents.acpSetup.customProviderLabel");
		default:
			return t("agents.createDialog.acpProviderClaudeCode");
	}
}

function StepHeading({ index, title }: { index: number; title: string }) {
	return (
		<div className="flex items-center gap-2">
			<span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-primary/10 text-[10px] font-semibold text-primary">
				{index}
			</span>
			<p className="text-sm font-medium">{title}</p>
		</div>
	);
}

// Exported so other setup flows needing the same "command + copy button"
// treatment (e.g. create-agent-dialog.tsx's provider_cli login-command
// hint) render it identically instead of hand-rolling a lookalike.
export function CommandBox({
	command,
	copied,
	onCopy,
}: {
	command: string;
	copied: boolean;
	onCopy: () => void;
}) {
	const { t } = useTranslation("projects");
	return (
		<div className="flex items-center gap-2">
			<code className="flex-1 rounded-md bg-muted px-2 py-1.5 text-xs overflow-x-auto whitespace-pre">
				{command}
			</code>
			<Button variant="outline" size="sm" onClick={onCopy}>
				{copied ? t("agents.acpSetup.copied") : t("agents.acpSetup.copy")}
			</Button>
		</div>
	);
}

// Three-step guide for bringing an ACP agent's local bridge online, in the
// order the CLI actually needs them: install the bridge daemon software;
// install Paca's skill and connect the MCP server (one step — both need the
// agent's own MCP key, so there's one generate action for both, though the
// resulting commands are shown separately rather than chained with `&&`,
// which got too long to read/copy comfortably); and only then start the
// bridge — starting it earlier would launch the CLI before its skill/MCP
// config exists to load. For Claude Code, the last step also surfaces the
// one-time `claude setup-token` login *before* the run command — the bridge
// invokes `claude` non-interactively, so the CLI needs valid credentials
// already in place when the bridge starts, not after.
//
// The middle step adapts to the agent's ACP provider — the skill installer
// covers every provider except "custom" (see supportsSkillInstaller); only
// Claude Code has a native "add MCP server" command, so other providers get
// a provider-agnostic MCP command to adapt into that CLI's own config.
//
// Note: translation keys below (step1/step2/step3/step4) are numbered by
// original authoring order, not by the display order rendered here — step2
// (run the bridge) renders last; step3/step4's title/description were
// retired in favor of a combined stepSkillMcp* pair, but their
// NotAvailable/ViewContent/GenericHint/generate-button strings are still
// used by the merged step. Renumbering every key was skipped to keep this
// change scoped; only the StepHeading index props reflect the real order.
//
// Shared between the post-creation setup dialog
// (apps/web/.../agents/index.tsx) and the agent detail page's Overview tab,
// so the two stay in sync.
export function AcpBridgeSetup({
	projectId,
	agentId,
	acpProvider,
	hasToken,
	hasKey,
	canWrite,
	onTokenGenerated,
	initialToken = null,
	initialKey = null,
}: {
	/** Absent for a global agent (managed from /admin/agents, no project). */
	projectId?: string;
	agentId: string;
	acpProvider: ACPProvider;
	hasToken: boolean;
	/** Whether this agent already has an MCP API key (agent.has_mcp_api_key). */
	hasKey: boolean;
	canWrite: boolean;
	onTokenGenerated: () => void;
	// Already-generated token to show immediately — used by the post-creation
	// dialog, which generates the token as part of agent creation itself
	// rather than triggering it from inside this component.
	initialToken?: AcpBridgeToken | null;
	// Already-generated MCP key, same rationale as initialToken above.
	initialKey?: string | null;
}) {
	const { t } = useTranslation("projects");
	const [revealed, setRevealed] = useState<AcpBridgeToken | null>(initialToken);
	const [generatedKey, setGeneratedKey] = useState<string | null>(initialKey);
	const [copiedField, setCopiedField] = useState<CopyField | null>(null);
	const hasAnyToken = hasToken || revealed !== null;
	const hasAnyKey = hasKey || generatedKey !== null;
	const isClaudeCode = acpProvider === "claude-code";
	const localAuthExport = isClaudeCode
		? null
		: localAuthExportCommand(acpProvider);
	const canAutoInstallSkill = supportsSkillInstaller(acpProvider);
	// Both need the same freshly generated key, so neither is shown until it
	// exists — but shown as two separate commands rather than one chained
	// with `&&`, since a single combined line got too long to read/copy
	// comfortably. One key, one generate action, two commands.
	const skillCommand =
		generatedKey && canAutoInstallSkill
			? skillInstallCommand(generatedKey)
			: null;
	const mcpCommand = generatedKey
		? buildMcpConnectCommand(acpProvider, agentId, projectId, generatedKey)
		: null;
	// Providers whose MCP step is a config-file block (not a command): which
	// file, and the name of the top-level section to merge into.
	const mcpConfigFile =
		acpProvider === "opencode"
			? { file: "~/.config/opencode/opencode.json", section: "mcp" }
			: acpProvider === "goose"
				? { file: "~/.config/goose/config.yaml", section: "extensions" }
				: null;

	const { data: status } = useQuery(
		projectId
			? acpBridgeStatusQueryOptions(projectId, agentId, {
					enabled: hasAnyToken,
				})
			: globalAcpBridgeStatusQueryOptions(agentId, { enabled: hasAnyToken }),
	);

	const generateMutation = useMutation({
		meta: { silent: true },
		mutationFn: () =>
			projectId
				? generateAcpBridgeToken(projectId, agentId)
				: generateGlobalAcpBridgeToken(agentId),
		onSuccess: (result) => {
			setRevealed(result);
			onTokenGenerated();
		},
	});

	const generateKeyMutation = useMutation({
		meta: { silent: true },
		mutationFn: () =>
			projectId
				? generateAgentMCPKey(projectId, agentId)
				: generateGlobalAgentMCPKey(agentId),
		onSuccess: (result) => {
			setGeneratedKey(result.token);
		},
	});

	const copy = (field: CopyField, text: string) => {
		// On failure leave copiedField untouched so the button doesn't falsely
		// claim success for a value the user may need to copy manually.
		void copyToClipboard(text).then((ok) => {
			if (!ok) return;
			setCopiedField(field);
			setTimeout(() => setCopiedField((f) => (f === field ? null : f)), 2000);
		});
	};

	return (
		<div className="space-y-5">
			<div className="flex items-center gap-1.5">
				<span
					className={`size-1.5 rounded-full ${
						status?.connected ? "bg-success" : "bg-muted-foreground/40"
					}`}
				/>
				<span className="text-xs text-muted-foreground">
					{status?.connected
						? t("agents.acpSetup.statusConnected")
						: t("agents.acpSetup.statusDisconnected")}
				</span>
			</div>

			{/* Step 1 — install the bridge */}
			<div className="space-y-1.5">
				<StepHeading index={1} title={t("agents.acpSetup.step1Title")} />
				<p className="text-xs text-muted-foreground pl-7">
					{t("agents.acpSetup.step1Description")}
				</p>
				<div className="pl-7">
					<CommandBox
						command={BRIDGE_INSTALL_COMMAND}
						copied={copiedField === "install"}
						onCopy={() => copy("install", BRIDGE_INSTALL_COMMAND)}
					/>
				</div>
			</div>

			{/* Step 2 — install the Paca skill & connect the MCP server: one
			    generate action for both, since they need the same key, but
			    shown as two separate commands (a single chained `&&` line got
			    too long to read/copy comfortably) */}
			<div className="space-y-1.5">
				<StepHeading index={2} title={t("agents.acpSetup.stepSkillMcpTitle")} />
				<p className="text-xs text-muted-foreground pl-7">
					{t("agents.acpSetup.stepSkillMcpDescription")}
				</p>
				{!canAutoInstallSkill && (
					<p className="text-xs text-muted-foreground rounded-md bg-muted/40 px-3 py-2 mx-7">
						{t("agents.acpSetup.step3NotAvailable", {
							provider: providerLabel(acpProvider, t),
						})}{" "}
						<a
							href={SKILL_CONTENT_URL}
							target="_blank"
							rel="noopener noreferrer"
							className="underline hover:text-foreground"
						>
							{t("agents.acpSetup.step3ViewContent")}
						</a>
					</p>
				)}
				{!isClaudeCode && (
					<p className="text-xs text-muted-foreground pl-7">
						{t("agents.acpSetup.step4GenericHint", {
							provider: providerLabel(acpProvider, t),
						})}
					</p>
				)}
				<div className="pl-7 space-y-2">
					{!hasAnyKey && (
						<p className="text-xs text-muted-foreground">
							{t("agents.acpSetup.step4NoKeyYet")}
						</p>
					)}
					{generateKeyMutation.isError && (
						<p className="text-xs text-destructive">
							{t("agents.acpSetup.step4GenerateFailed")}
						</p>
					)}
					{(skillCommand || mcpCommand) && (
						<div className="space-y-2">
							<p className="text-xs text-warning">
								{t("agents.acpSetup.step4KeyWarning")}
							</p>
							{skillCommand && (
								<div className="space-y-1">
									<p className="text-xs text-muted-foreground">
										{t("agents.acpSetup.stepSkillMcpSkillLabel")}
									</p>
									<CommandBox
										command={skillCommand}
										copied={copiedField === "skill"}
										onCopy={() => copy("skill", skillCommand)}
									/>
								</div>
							)}
							{mcpCommand && (
								<div className="space-y-1">
									<p className="text-xs text-muted-foreground">
										{mcpConfigFile
											? t(
													"agents.acpSetup.stepSkillMcpConnectLabelConfig",
													mcpConfigFile,
												)
											: t("agents.acpSetup.stepSkillMcpConnectLabel")}
									</p>
									<CommandBox
										command={mcpCommand}
										copied={copiedField === "mcp"}
										onCopy={() => copy("mcp", mcpCommand)}
									/>
								</div>
							)}
						</div>
					)}
					<Button
						variant="outline"
						size="sm"
						onClick={() => generateKeyMutation.mutate()}
						disabled={!canWrite || generateKeyMutation.isPending}
					>
						{generateKeyMutation.isPending ? (
							<>
								<Spinner aria-hidden="true" className="mr-1.5" />
								{t("agents.acpSetup.step4Generating")}
							</>
						) : hasAnyKey ? (
							t("agents.acpSetup.step4RegenerateButton")
						) : (
							t("agents.acpSetup.step4GenerateButton")
						)}
					</Button>
				</div>
			</div>

			{/* Step 3 — run the bridge (last: it starts the CLI process that
			    depends on the skill/MCP config from the step above) */}
			<div className="space-y-1.5">
				<StepHeading index={3} title={t("agents.acpSetup.step2Title")} />
				<p className="text-xs text-muted-foreground pl-7">
					{t("agents.acpSetup.step2Description")}
				</p>
				<div className="pl-7 space-y-2">
					{isClaudeCode ? (
						<div className="space-y-1.5 pb-1">
							<p className="text-xs text-muted-foreground">
								{t("agents.acpSetup.claudeSetupTokenHint")}
							</p>
							<CommandBox
								command={CLAUDE_SETUP_TOKEN_COMMAND}
								copied={copiedField === "claudeSetupToken"}
								onCopy={() =>
									copy("claudeSetupToken", CLAUDE_SETUP_TOKEN_COMMAND)
								}
							/>
							<p className="text-xs text-muted-foreground">
								{t("agents.acpSetup.claudeOauthExportHint")}
							</p>
							<CommandBox
								command={CLAUDE_OAUTH_EXPORT_COMMAND}
								copied={copiedField === "claudeOauthExport"}
								onCopy={() =>
									copy("claudeOauthExport", CLAUDE_OAUTH_EXPORT_COMMAND)
								}
							/>
						</div>
					) : (
						localAuthExport && (
							<div className="space-y-1.5 pb-1">
								<p className="text-xs text-muted-foreground">
									{t("agents.acpSetup.localAuthExportHint", {
										provider: providerLabel(acpProvider, t),
									})}
								</p>
								<CommandBox
									command={localAuthExport}
									copied={copiedField === "localAuthExport"}
									onCopy={() => copy("localAuthExport", localAuthExport)}
								/>
							</div>
						)
					)}
					{!hasAnyToken && (
						<p className="text-xs text-muted-foreground">
							{t("agents.acpSetup.noTokenYet")}
						</p>
					)}
					{generateMutation.isError && (
						<p className="text-xs text-destructive">
							{t("agents.acpSetup.generateFailed")}
						</p>
					)}
					{revealed && (
						<div className="space-y-1.5">
							<p className="text-xs text-warning">
								{t("agents.acpSetup.tokenWarning")}
							</p>
							<CommandBox
								command={revealed.run_command}
								copied={copiedField === "run"}
								onCopy={() => copy("run", revealed.run_command)}
							/>
						</div>
					)}
					<Button
						variant="outline"
						size="sm"
						onClick={() => generateMutation.mutate()}
						disabled={!canWrite || generateMutation.isPending}
					>
						{generateMutation.isPending ? (
							<>
								<Spinner aria-hidden="true" className="mr-1.5" />
								{t("agents.acpSetup.generating")}
							</>
						) : hasAnyToken ? (
							t("agents.acpSetup.regenerateToken")
						) : (
							t("agents.acpSetup.generateToken")
						)}
					</Button>
				</div>
			</div>
		</div>
	);
}
