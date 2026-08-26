import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import {
	Check,
	ChevronLeft,
	Copy,
	ExternalLink,
	KeyRound,
	Loader2,
	Play,
	Plus,
	TerminalSquare,
	Trash2,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	EnvironmentStatusLine,
	EnvironmentStatusRing,
	useEnvironmentUsage,
} from "@/components/projects/environments/environment-status-ring";
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
import { Skeleton } from "@/components/ui/skeleton";
import { Textarea } from "@/components/ui/textarea";
import {
	addSSHKey,
	deleteSSHKey,
	type Environment,
	environmentConfigQueryOptions,
	environmentQueryOptions,
	environmentSSHKeysQueryOptions,
	startEnvironment,
} from "@/lib/environment-api";

// The environment "Connect" page — a dedicated route
// (routes/.../environments/$environmentId/connect.tsx), not a tab on
// environment-detail.tsx, reached via that page's own "Connect" button.
// Two tabs, mirroring the well-known "connect to instance" pattern from
// cloud consoles (e.g. AWS EC2's Connect page): connecting via the
// in-browser terminal (opens full-page in a new tab), and connecting via a
// real `ssh` client from the user's own machine. Port forward management
// lives on environment-detail.tsx instead — see that file's own doc
// comment.

type ConnectTab = "web-app" | "ssh";

// ── Shared bits (CommandBox / StepHeading) ─────────────────────────────────────
// Small, self-contained duplicates of the same pattern
// agents/acp-bridge-setup.tsx already established for its own "connect your
// CLI agent" numbered-step guide — kept local rather than shared across
// features for two components this size.

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

function CommandBox({ command }: { command: string }) {
	const { t } = useTranslation("projects");
	const [copied, setCopied] = useState(false);

	const copy = () => {
		navigator.clipboard
			.writeText(command)
			.then(() => {
				setCopied(true);
				setTimeout(() => setCopied(false), 2000);
			})
			.catch(() => {
				// Best-effort — clipboard write can fail (permission denied,
				// insecure context); the command is still selectable/copyable
				// by hand from the <code> block itself.
			});
	};

	return (
		<div className="flex items-center gap-2 min-w-0">
			<code className="flex-1 min-w-0 rounded-md bg-muted px-2 py-1.5 text-xs overflow-x-auto whitespace-nowrap select-all">
				{command}
			</code>
			<Button variant="outline" size="sm" className="shrink-0" onClick={copy}>
				{copied ? (
					<>
						<Check className="size-3.5 mr-1.5" />
						{t("environments.connect.copied")}
					</>
				) : (
					<>
						<Copy className="size-3.5 mr-1.5" />
						{t("environments.connect.copy")}
					</>
				)}
			</Button>
		</div>
	);
}

// ── SSH key management (moved here from the old standalone SSH Keys tab) ──────

function AddSSHKeyDialog({
	projectId,
	environmentId,
	open,
	onOpenChange,
}: {
	projectId: string;
	environmentId: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [label, setLabel] = useState("");
	const [publicKey, setPublicKey] = useState("");

	const addMutation = useMutation({
		mutationFn: () =>
			addSSHKey(projectId, environmentId, {
				label: label.trim(),
				public_key: publicKey.trim(),
			}),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: environmentSSHKeysQueryOptions(projectId, environmentId)
					.queryKey,
			});
			onOpenChange(false);
			setLabel("");
			setPublicKey("");
		},
	});

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-xl">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						<KeyRound className="size-4 text-primary" />
						{t("environments.detail.sshKeys.addDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("environments.detail.sshKeys.addDialog.description")}
					</DialogDescription>
				</DialogHeader>
				<div className="space-y-4 py-2 min-w-0">
					<div className="space-y-1.5 rounded-md bg-muted/50 p-3">
						<p className="text-xs text-muted-foreground">
							{t("environments.detail.sshKeys.addDialog.generateHint")}
						</p>
						<CommandBox command='ssh-keygen -t ed25519 -C "your_email@example.com"' />
						<p className="text-xs text-muted-foreground">
							{t("environments.detail.sshKeys.addDialog.generateFollowup")}
						</p>
					</div>
					<div className="space-y-1.5">
						<Label>
							{t("environments.detail.sshKeys.addDialog.labelLabel")}
						</Label>
						<Input
							placeholder={t(
								"environments.detail.sshKeys.addDialog.labelPlaceholder",
							)}
							value={label}
							onChange={(e) => setLabel(e.target.value)}
						/>
					</div>
					<div className="space-y-1.5">
						<Label>
							{t("environments.detail.sshKeys.addDialog.publicKeyLabel")}
						</Label>
						<Textarea
							placeholder="ssh-ed25519 AAAA... user@host"
							className="font-mono text-xs"
							rows={4}
							value={publicKey}
							onChange={(e) => setPublicKey(e.target.value)}
						/>
					</div>
					{addMutation.isError && (
						<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
							{t("environments.detail.sshKeys.addDialog.addFailed")}
						</p>
					)}
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						{t("environments.detail.sshKeys.addDialog.cancel")}
					</Button>
					<Button
						onClick={() => addMutation.mutate()}
						disabled={
							!label.trim() || !publicKey.trim() || addMutation.isPending
						}
					>
						{addMutation.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : (
							t("environments.detail.sshKeys.addDialog.addKey")
						)}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function SSHKeysManager({
	projectId,
	environmentId,
	canWrite,
}: {
	projectId: string;
	environmentId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const { data: keys = [] } = useQuery(
		environmentSSHKeysQueryOptions(projectId, environmentId),
	);
	const [addOpen, setAddOpen] = useState(false);
	const keysKey = environmentSSHKeysQueryOptions(
		projectId,
		environmentId,
	).queryKey;

	const deleteMutation = useMutation({
		mutationFn: (keyId: string) =>
			deleteSSHKey(projectId, environmentId, keyId),
		onSuccess: () => qc.invalidateQueries({ queryKey: keysKey }),
	});

	return (
		<div className="space-y-3">
			<div className="flex items-center justify-between">
				<p className="text-sm text-muted-foreground">
					{t("environments.detail.sshKeys.count", { count: keys.length })}
				</p>
				{canWrite && (
					<Button size="sm" variant="outline" onClick={() => setAddOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("environments.detail.sshKeys.addKey")}
					</Button>
				)}
			</div>

			{keys.length === 0 ? (
				<div className="flex flex-col items-center justify-center gap-3 py-10 rounded-xl border border-dashed border-border">
					<KeyRound className="size-7 text-muted-foreground/40" />
					<p className="text-sm text-muted-foreground">
						{t("environments.detail.sshKeys.empty.title")}
					</p>
					{canWrite && (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setAddOpen(true)}
						>
							<Plus className="size-3.5 mr-1" />
							{t("environments.detail.sshKeys.empty.addFirst")}
						</Button>
					)}
				</div>
			) : (
				<div className="space-y-2">
					{keys.map((k) => (
						<div
							key={k.id}
							className="flex items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-4 py-3"
						>
							<div className="flex items-center gap-3 min-w-0">
								<KeyRound className="size-4 text-muted-foreground shrink-0" />
								<div className="min-w-0">
									<p className="text-sm font-medium truncate">{k.label}</p>
									<p className="text-xs text-muted-foreground font-mono truncate">
										{k.fingerprint}
									</p>
								</div>
							</div>
							{canWrite && (
								<Button
									variant="ghost"
									size="icon"
									className="size-7 text-muted-foreground hover:text-destructive shrink-0"
									onClick={() => deleteMutation.mutate(k.id)}
									disabled={deleteMutation.isPending}
								>
									<Trash2 className="size-3.5" />
								</Button>
							)}
						</div>
					))}
				</div>
			)}

			{deleteMutation.isError && (
				<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
					{t("environments.detail.sshKeys.deleteFailed")}
				</p>
			)}

			<AddSSHKeyDialog
				projectId={projectId}
				environmentId={environmentId}
				open={addOpen}
				onOpenChange={setAddOpen}
			/>
		</div>
	);
}

// ── Web App tab ─────────────────────────────────────────────────────────────────

function WebAppConnectTab({
	projectId,
	environment,
	canWrite,
}: {
	projectId: string;
	environment: Environment;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const isRunning = environment.status === "running";

	const startMutation = useMutation({
		mutationFn: () => startEnvironment(projectId, environment.id),
		onSuccess: (updated) => {
			qc.setQueryData(
				environmentQueryOptions(projectId, environment.id).queryKey,
				updated,
			);
		},
	});

	return (
		<div className="space-y-4 max-w-2xl">
			<p className="text-sm text-muted-foreground">
				{t("environments.connect.webApp.description")}
			</p>
			{isRunning ? (
				canWrite ? (
					<Link
						to="/projects/$projectId/environments/$environmentId/terminal"
						params={{ projectId, environmentId: environment.id }}
						target="_blank"
						rel="noopener noreferrer"
						className={buttonVariants({ size: "lg" })}
					>
						<ExternalLink className="size-4 mr-2" />
						{t("environments.connect.webApp.connect")}
					</Link>
				) : (
					// The terminal ticket endpoint requires agents:write (see
					// router.go) — a read-only member who followed this link
					// would only hit a 403 minting the ticket, so the link
					// itself is hidden rather than shown-then-failing.
					<p className="text-sm text-muted-foreground">
						{t("environments.connect.webApp.readOnly")}
					</p>
				)
			) : (
				<div className="space-y-3">
					<p className="text-sm text-amber-600">
						{t("environments.connect.webApp.notRunning")}
					</p>
					{canWrite && (
						<Button
							onClick={() => startMutation.mutate()}
							disabled={startMutation.isPending}
						>
							{startMutation.isPending ? (
								<Loader2 className="size-4 mr-2 animate-spin" />
							) : (
								<Play className="size-4 mr-2" />
							)}
							{t("environments.detail.overview.start")}
						</Button>
					)}
				</div>
			)}
			<p className="text-xs text-muted-foreground">
				{t("environments.connect.webApp.hint")}
			</p>
		</div>
	);
}

// ── SSH tab ───────────────────────────────────────────────────────────────────

function SSHConnectTab({
	projectId,
	environment,
	canWrite,
}: {
	projectId: string;
	environment: Environment;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const { data: config } = useQuery(environmentConfigQueryOptions());
	const sshPort = environment.ssh_port;
	// Always the same deployment-wide host every environment's SSH port is
	// published on — a native Docker -p binding lands on this one Docker
	// host's own address; a Kubernetes NodePort Service is reachable at
	// any node's address — never a per-environment hostname. SSH has no
	// equivalent of HTTP's Host header/TLS SNI to route on, so every
	// environment's `ssh` command shares the exact same host and is
	// disambiguated only by -p.
	const host = config?.ssh_bastion_host || null;

	return (
		<div className="space-y-6">
			<div className="space-y-3">
				<StepHeading
					index={1}
					title={t("environments.connect.ssh.step1Title")}
				/>
				<p className="text-sm text-muted-foreground pl-7 max-w-2xl">
					{t("environments.connect.ssh.step1Description")}
				</p>
				<div className="pl-7">
					<SSHKeysManager
						projectId={projectId}
						environmentId={environment.id}
						canWrite={canWrite}
					/>
				</div>
			</div>

			<div className="space-y-3">
				<StepHeading
					index={2}
					title={t("environments.connect.ssh.step2Title")}
				/>
				<p className="text-sm text-muted-foreground pl-7 max-w-2xl">
					{t("environments.connect.ssh.step2Description")}
				</p>
				<div className="pl-7 space-y-2 max-w-2xl">
					{sshPort === null ? (
						<p className="text-sm text-muted-foreground">
							{t("environments.detail.sshKeys.connectUnavailable")}
						</p>
					) : (
						<>
							<CommandBox
								command={`ssh -i ~/.ssh/id_ed25519 -p ${sshPort} root@${host ?? "<bastion-host>"}`}
							/>
							{!host && (
								<p className="text-xs text-muted-foreground">
									{t("environments.detail.sshKeys.connectHint")}
								</p>
							)}
							{environment.status !== "running" && (
								<p className="text-xs text-amber-600">
									{t("environments.connect.ssh.notRunning")}
								</p>
							)}
						</>
					)}
				</div>
			</div>
		</div>
	);
}

// ── Page ──────────────────────────────────────────────────────────────────────

const CONNECT_TABS = [
	{
		id: "web-app",
		labelKey: "environments.connect.tabs.webApp",
		icon: TerminalSquare,
	},
	{
		id: "ssh",
		labelKey: "environments.connect.tabs.ssh",
		icon: KeyRound,
	},
] as const satisfies {
	id: ConnectTab;
	labelKey: string;
	icon: React.ComponentType<{ className?: string }>;
}[];

export function EnvironmentConnectView({
	projectId,
	environmentId,
	canWrite,
}: {
	projectId: string;
	environmentId: string;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const { data: environment } = useQuery(
		environmentQueryOptions(projectId, environmentId),
	);
	const [activeTab, setActiveTab] = useState<ConnectTab>("web-app");
	// Called unconditionally (before the `!environment` early return below)
	// purely for its hasActiveSshSession signal — see
	// EnvironmentStatusRing's doc comment on why the header needs this
	// rather than depleting its idle ring while a real SSH session is open.
	const usage = useEnvironmentUsage(projectId, environmentId, environment);

	if (!environment) {
		return (
			<div className="flex flex-col gap-4 p-6">
				<Skeleton className="h-16 w-full rounded-xl" />
				<Skeleton className="h-64 w-full rounded-xl" />
			</div>
		);
	}

	return (
		<div className="flex flex-col flex-1 min-h-0">
			<div className="border-b border-border/50 px-6 py-5 shrink-0 space-y-3">
				<Link
					to="/projects/$projectId/environments/$environmentId"
					params={{ projectId, environmentId }}
					className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
				>
					<ChevronLeft className="size-4" />
					{environment.name}
				</Link>
				<div className="flex items-center gap-4">
					<EnvironmentStatusRing
						environment={environment}
						size={52}
						hasActiveSshSession={usage.hasActiveSshSession}
					/>
					<div>
						<h1 className="text-lg font-semibold">
							{t("environments.connect.title", { name: environment.name })}
						</h1>
						<div className="mt-0.5">
							<EnvironmentStatusLine
								environment={environment}
								hasActiveSshSession={usage.hasActiveSshSession}
							/>
						</div>
					</div>
				</div>
			</div>

			<div className="border-b border-border/50 px-6 shrink-0">
				<div className="flex items-center gap-1 -mb-px">
					{CONNECT_TABS.map((tab) => {
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

			<div className="flex-1 overflow-auto p-6">
				{activeTab === "web-app" && (
					<WebAppConnectTab
						projectId={projectId}
						environment={environment}
						canWrite={canWrite}
					/>
				)}
				{activeTab === "ssh" && (
					<SSHConnectTab
						projectId={projectId}
						environment={environment}
						canWrite={canWrite}
					/>
				)}
			</div>
		</div>
	);
}
