import { useMutation, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { Loader2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	type ACPProvider,
	type AcpBridgeToken,
	acpBridgeStatusQueryOptions,
	generateAcpBridgeToken,
} from "@/lib/agent-api";

// Fixed commands for the setup steps that don't depend on any
// server-generated value — only the bridge run command (step 2) comes back
// from the API, since it embeds the freshly generated token.
const BRIDGE_INSTALL_COMMAND = "uv pip install paca-acp-bridge";

const SKILL_INSTALL_COMMAND =
	"curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-claude-skill.sh | bash";

const SKILL_CONTENT_URL =
	"https://github.com/Paca-AI/paca/blob/master/skills/paca/SKILL.md";

function claudeMcpConnectCommand(): string {
	return `claude mcp add paca --env PACA_API_KEY=<your-api-key> --env PACA_API_URL=${window.location.origin} -- npx -y @paca-ai/paca-mcp`;
}

// Provider-agnostic reference: the underlying MCP command/args/env every ACP
// client needs, regardless of how that client's own config format (TOML,
// JSON, CLI flags, ...) wants it expressed — only Claude Code has a known
// native "add an MCP server" command (above), so this is the fallback for
// everything else.
function genericMcpConnectCommand(): string {
	return `PACA_API_KEY=<your-api-key> PACA_API_URL=${window.location.origin} npx -y @paca-ai/paca-mcp`;
}

type CopyField = "install" | "run" | "skill" | "mcp";

function providerLabel(
	provider: ACPProvider,
	t: TFunction<"projects">,
): string {
	switch (provider) {
		case "codex":
			return t("agents.createDialog.acpProviderCodex");
		case "gemini-cli":
			return t("agents.createDialog.acpProviderGeminiCli");
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

function CommandBox({
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
			<code className="flex-1 rounded-md bg-muted px-2 py-1.5 text-xs overflow-x-auto whitespace-nowrap">
				{command}
			</code>
			<Button variant="outline" size="sm" onClick={onCopy}>
				{copied ? t("agents.acpSetup.copied") : t("agents.acpSetup.copy")}
			</Button>
		</div>
	);
}

// Four-step guide for bringing an ACP agent's local bridge online: install
// and run the bridge daemon, install the Paca skill, and connect the MCP
// server. Steps 3 and 4 adapt to the agent's ACP provider — only Claude Code
// has a known, tested skill installer and native "add MCP server" command;
// other providers get an honest fallback (link to the skill content, a
// provider-agnostic MCP command to adapt into that CLI's own config).
// Shared between the post-creation setup dialog
// (apps/web/.../agents/index.tsx) and the agent detail page's Overview tab,
// so the two stay in sync.
export function AcpBridgeSetup({
	projectId,
	agentId,
	acpProvider,
	hasToken,
	canWrite,
	onTokenGenerated,
	initialToken = null,
}: {
	projectId: string;
	agentId: string;
	acpProvider: ACPProvider;
	hasToken: boolean;
	canWrite: boolean;
	onTokenGenerated: () => void;
	// Already-generated token to show immediately — used by the post-creation
	// dialog, which generates the token as part of agent creation itself
	// rather than triggering it from inside this component.
	initialToken?: AcpBridgeToken | null;
}) {
	const { t } = useTranslation("projects");
	const [revealed, setRevealed] = useState<AcpBridgeToken | null>(initialToken);
	const [copiedField, setCopiedField] = useState<CopyField | null>(null);
	const hasAnyToken = hasToken || revealed !== null;
	const isClaudeCode = acpProvider === "claude-code";
	const mcpCommand = isClaudeCode
		? claudeMcpConnectCommand()
		: genericMcpConnectCommand();

	const { data: status } = useQuery(
		acpBridgeStatusQueryOptions(projectId, agentId, { enabled: hasAnyToken }),
	);

	const generateMutation = useMutation({
		mutationFn: () => generateAcpBridgeToken(projectId, agentId),
		onSuccess: (result) => {
			setRevealed(result);
			onTokenGenerated();
		},
	});

	const copy = (field: CopyField, text: string) => {
		navigator.clipboard
			.writeText(text)
			.then(() => {
				setCopiedField(field);
				setTimeout(() => setCopiedField((f) => (f === field ? null : f)), 2000);
			})
			.catch(() => {
				// Clipboard write can fail (permission denied, insecure context) —
				// leave copiedField untouched so the button doesn't falsely claim
				// success for a value the user may need to copy manually.
			});
	};

	return (
		<div className="space-y-5">
			<div className="flex items-center gap-1.5">
				<span
					className={`size-1.5 rounded-full ${
						status?.connected ? "bg-emerald-500" : "bg-muted-foreground/40"
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

			{/* Step 2 — run the bridge */}
			<div className="space-y-1.5">
				<StepHeading index={2} title={t("agents.acpSetup.step2Title")} />
				<p className="text-xs text-muted-foreground pl-7">
					{t("agents.acpSetup.step2Description")}
				</p>
				<div className="pl-7 space-y-2">
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
							<p className="text-xs text-amber-600">
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
								<Loader2 className="size-4 mr-1.5 animate-spin" />
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

			{/* Step 3 — install the Paca skill */}
			<div className="space-y-1.5">
				<StepHeading index={3} title={t("agents.acpSetup.step3Title")} />
				<p className="text-xs text-muted-foreground pl-7">
					{t("agents.acpSetup.step3Description")}
				</p>
				<div className="pl-7">
					{isClaudeCode ? (
						<CommandBox
							command={SKILL_INSTALL_COMMAND}
							copied={copiedField === "skill"}
							onCopy={() => copy("skill", SKILL_INSTALL_COMMAND)}
						/>
					) : (
						<p className="text-xs text-muted-foreground rounded-md bg-muted/40 px-3 py-2">
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
				</div>
			</div>

			{/* Step 4 — connect the MCP server */}
			<div className="space-y-1.5">
				<StepHeading index={4} title={t("agents.acpSetup.step4Title")} />
				<p className="text-xs text-muted-foreground pl-7">
					{t("agents.acpSetup.step4Description")}{" "}
					<Link
						to="/profile/api-keys"
						className="underline hover:text-foreground"
					>
						{t("agents.acpSetup.step4ApiKeyLink")}
					</Link>
				</p>
				{!isClaudeCode && (
					<p className="text-xs text-muted-foreground pl-7">
						{t("agents.acpSetup.step4GenericHint", {
							provider: providerLabel(acpProvider, t),
						})}
					</p>
				)}
				<div className="pl-7">
					<CommandBox
						command={mcpCommand}
						copied={copiedField === "mcp"}
						onCopy={() => copy("mcp", mcpCommand)}
					/>
				</div>
			</div>
		</div>
	);
}
