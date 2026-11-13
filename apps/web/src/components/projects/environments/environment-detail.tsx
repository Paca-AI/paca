import {
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
	AlertTriangle,
	Check,
	Copy,
	ExternalLink,
	Folder as FolderIcon,
	MoreHorizontal,
	Network,
	Play,
	Plus,
	RotateCw,
	Save,
	Server,
	Square,
	Trash2,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	EnvironmentStatusLine,
	EnvironmentStatusRing,
	type EnvironmentUsage,
	formatBytes,
	UsageRingCell,
	useEnvironmentUsage,
} from "@/components/projects/environments/environment-status-ring";
import { FolderCreateDialog } from "@/components/projects/environments/folder-create-dialog";
import { LoadMoreSentinel } from "@/components/shared/load-more-sentinel";
import { Button, buttonVariants } from "@/components/ui/button";
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
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { copyToClipboard } from "@/lib/clipboard";
import {
	addPortForward,
	deleteEnvironment,
	deleteFolder,
	deletePortForward,
	type Environment,
	type EnvironmentStatus,
	environmentConfigQueryOptions,
	environmentFoldersInfiniteQueryOptions,
	environmentPortForwardsInfiniteQueryOptions,
	environmentQueryOptions,
	portForwardUrl,
	restartEnvironment,
	startEnvironment,
	stopEnvironment,
	updateEnvironment,
} from "@/lib/environment-api";
import { notifyError } from "@/lib/notify";
import { timeAgo } from "@/lib/time-ago";

// Shared by the environment detail route
// (routes/.../projects/$projectId/environments/$environmentId/index.tsx).
// Mirrors agent-detail.tsx's tab-strip + per-tab dialog + card-list pattern
// exactly — see docs/ai-agent/environment-management.md's Frontend section.
// Environments are project-scoped only (no global-agent-style dual-scope
// branching needed here). Terminal/SSH access moved off this page's own
// tab strip and into a dedicated Connect page/route (see
// environment-connect.tsx) — reached via the "Connect" button in the
// header below — mirroring how cloud consoles (e.g. AWS EC2) give
// "connect to this resource" its own page rather than a small embedded
// tab. Port forward management, unlike terminal/SSH, stays on this page as
// its own tab — it's config about *this* environment's own row set
// (mirrors Folders), not a "how do I reach it" walkthrough like Connect.

type Tab = "overview" | "folders" | "portForwards";

const TRANSITIONAL_STATUSES: EnvironmentStatus[] = [
	"creating",
	"starting",
	"stopping",
	"deleting",
];

// ── Overview Tab ──────────────────────────────────────────────────────────────

// VitalCard surfaces one field of an environment's real, already-fetched
// spec (cpu_limit/memory_limit/disk_limit_gb) or activity data
// (last_active_at) that the API has always returned but this tab never
// showed before — the tab read like a name field with a save button
// attached, not the detail page for an actual running machine.
function VitalCard({ label, value }: { label: string; value: string }) {
	return (
		<div className="rounded-lg border border-border/60 bg-muted/30 px-3 py-2.5">
			<p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
				{label}
			</p>
			<p className="mt-0.5 text-sm font-medium font-mono tabular-nums truncate">
				{value}
			</p>
		</div>
	);
}

// formatCores renders a core count the same way the create dialog's own
// cpu_limit field is entered — a bare number, not "vCPU" repeated with
// pointless decimal noise (2, not 2.0; 0.3, not 0.300000001 from the
// usage-rate division in useEnvironmentUsage).
function formatCores(cores: number): string {
	return cores >= 10 || cores % 1 === 0 ? cores.toFixed(0) : cores.toFixed(1);
}

function OverviewTab({
	environment,
	projectId,
	canWrite,
	onGoToPortForwards,
	usage,
}: {
	environment: Environment;
	projectId: string;
	canWrite: boolean;
	onGoToPortForwards: () => void;
	// Lifted to EnvironmentDetailView and shared with the header's
	// EnvironmentStatusRing/Line rather than fetched here — see that
	// component's own doc comment on why a single subscription per page,
	// not one per consumer, is worth the prop.
	usage: EnvironmentUsage;
}) {
	const { t } = useTranslation("projects");
	const { t: tCommon } = useTranslation("common");
	const qc = useQueryClient();
	const [name, setName] = useState(environment.name);
	const [idleTimeout, setIdleTimeout] = useState(
		String(environment.idle_timeout_minutes),
	);

	const envKey = environmentQueryOptions(projectId, environment.id).queryKey;
	const listKey = ["projects", projectId, "environments"];

	const isDirty =
		name !== environment.name ||
		idleTimeout !== String(environment.idle_timeout_minutes);

	const saveErrorMessage = (err: unknown) =>
		getApiErrorCode(err) === ApiErrorCode.EnvironmentNameInvalid
			? t("environments.detail.overview.nameRequired")
			: t("environments.detail.overview.saveFailed");

	const saveMutation = useMutation({
		meta: { success: "updated", subject: "environmentSettings" },
		onError: (err) => notifyError(err, saveErrorMessage(err)),
		mutationFn: () =>
			updateEnvironment(projectId, environment.id, {
				name: name.trim(),
				idle_timeout_minutes: Number(idleTimeout),
			}),
		onSuccess: (updated) => {
			qc.setQueryData(envKey, updated);
			qc.invalidateQueries({ queryKey: listKey });
		},
	});

	const idleTimeoutNumber = Number(idleTimeout);
	const canSave =
		isDirty &&
		!!name.trim() &&
		Number.isInteger(idleTimeoutNumber) &&
		idleTimeoutNumber > 0 &&
		!saveMutation.isPending;

	return (
		<div className="space-y-6">
			{environment.error_message && (
				<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
					{environment.error_message}
				</p>
			)}

			<div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
				<UsageRingCell
					label={t("environments.detail.overview.vitals.cpu")}
					valueText={
						usage.cpuCoresUsed !== null
							? `${formatCores(usage.cpuCoresUsed)} / ${formatCores(usage.cpuLimitCores)} vCPU`
							: null
					}
					limitText={`${formatCores(usage.cpuLimitCores)} vCPU`}
					fraction={usage.cpuFraction}
				/>
				<UsageRingCell
					label={t("environments.detail.overview.vitals.memory")}
					valueText={
						usage.memoryFraction !== null
							? `${formatBytes(usage.memoryUsedBytes)} / ${formatBytes(usage.memoryLimitBytes)}`
							: null
					}
					limitText={environment.memory_limit}
					fraction={usage.memoryFraction}
				/>
				<UsageRingCell
					label={t("environments.detail.overview.vitals.disk")}
					valueText={
						usage.diskFraction !== null
							? `${formatBytes(usage.diskUsedBytes)} / ${formatBytes(usage.diskLimitBytes)}`
							: null
					}
					limitText={t("environments.detail.overview.vitals.diskValue", {
						value: environment.disk_limit_gb,
					})}
					fraction={usage.diskFraction}
				/>
				<VitalCard
					label={t("environments.detail.overview.vitals.lastActive")}
					value={timeAgo(environment.last_active_at, tCommon)}
				/>
			</div>

			<Separator />

			<div className="space-y-4 max-w-2xl">
				<p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
					{t("environments.detail.overview.configuration")}
				</p>

				<div className="space-y-1.5">
					<Label>{t("environments.detail.overview.nameLabel")}</Label>
					<Input
						value={name}
						onChange={(e) => setName(e.target.value)}
						disabled={!canWrite}
					/>
				</div>

				<div className="grid grid-cols-2 gap-3">
					<div className="space-y-1.5">
						<Label>{t("environments.detail.overview.backendLabel")}</Label>
						<p className="text-sm text-muted-foreground">
							{environment.backend}
						</p>
					</div>
					<div className="space-y-1.5">
						<Label>{t("environments.detail.overview.imageLabel")}</Label>
						<p className="text-sm text-muted-foreground font-mono truncate">
							{environment.image ??
								t("environments.detail.overview.defaultImage")}
						</p>
					</div>
					<div className="space-y-1.5">
						<Label>
							{t("environments.detail.overview.dockerEnabledLabel")}
						</Label>
						<p className="text-sm text-muted-foreground">
							{environment.docker_enabled
								? t("environments.detail.overview.dockerEnabledOn")
								: t("environments.detail.overview.dockerEnabledOff")}
						</p>
					</div>
				</div>

				<div className="space-y-1.5">
					<Label>{t("environments.detail.overview.idleTimeoutLabel")}</Label>
					<Input
						type="number"
						min={1}
						value={idleTimeout}
						onChange={(e) => setIdleTimeout(e.target.value)}
						disabled={!canWrite}
						className="max-w-32"
					/>
					<p className="text-xs text-muted-foreground">
						{t("environments.detail.overview.idleTimeoutHint")}
					</p>
				</div>
			</div>

			{environment.ports_pending_restart && (
				<div className="flex items-center gap-2 rounded-md border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning">
					<AlertTriangle className="size-4 shrink-0" />
					<span className="flex-1">
						{t("environments.detail.overview.portsPendingRestart")}
					</span>
					<button
						type="button"
						onClick={onGoToPortForwards}
						className="underline underline-offset-2 whitespace-nowrap"
					>
						{t("environments.detail.overview.portsPendingRestartLink")}
					</button>
				</div>
			)}

			{canWrite && (
				<div className="flex items-center gap-3">
					<Button onClick={() => saveMutation.mutate()} disabled={!canSave}>
						{saveMutation.isPending ? (
							<Spinner aria-hidden="true" className="mr-2" />
						) : (
							<Save className="size-4 mr-2" />
						)}
						{t("environments.detail.overview.saveChanges")}
					</Button>
				</div>
			)}
		</div>
	);
}

// ── Folders Tab ───────────────────────────────────────────────────────────────

function FoldersTab({
	projectId,
	environmentId,
	environmentStatus,
	canWrite,
}: {
	projectId: string;
	environmentId: string;
	environmentStatus: EnvironmentStatus;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const foldersQuery = useInfiniteQuery(
		environmentFoldersInfiniteQueryOptions(projectId, environmentId),
	);
	const folders = foldersQuery.data?.pages.flatMap((p) => p.items) ?? [];
	const folderCount =
		foldersQuery.data?.pages[0]?.total_count ?? folders.length;
	const [addOpen, setAddOpen] = useState(false);
	// Prefix of every folder list (any search) plus the environment itself.
	const foldersKey = environmentQueryOptions(projectId, environmentId).queryKey;

	const deleteMutation = useMutation({
		meta: { success: "removed", subject: "folder" },
		onError: (err) =>
			notifyError(err, t("environments.detail.folders.deleteFailed")),
		mutationFn: (folderId: string) =>
			deleteFolder(projectId, environmentId, folderId),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: foldersKey });
			qc.invalidateQueries({
				queryKey: environmentQueryOptions(projectId, environmentId).queryKey,
			});
		},
	});

	return (
		<div className="space-y-4">
			<div className="flex items-center justify-between">
				<p className="text-sm text-muted-foreground">
					{t("environments.detail.folders.count", { count: folderCount })}
				</p>
				{canWrite && (
					<Button size="sm" onClick={() => setAddOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("environments.detail.folders.addFolder")}
					</Button>
				)}
			</div>

			{folders.length === 0 ? (
				<div className="flex flex-col items-center justify-center gap-3 py-14 rounded-xl border border-dashed border-border">
					<FolderIcon className="size-8 text-muted-foreground/40" />
					<p className="text-sm text-muted-foreground">
						{t("environments.detail.folders.empty.title")}
					</p>
					{canWrite && (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setAddOpen(true)}
						>
							<Plus className="size-3.5 mr-1" />
							{t("environments.detail.folders.empty.addFirstFolder")}
						</Button>
					)}
				</div>
			) : (
				<div className="space-y-2">
					{folders.map((f) => (
						<div
							key={f.id}
							className="flex items-center justify-between gap-3 rounded-lg border border-border/60 bg-card px-4 py-3"
						>
							<div className="flex items-center gap-3 min-w-0">
								<FolderIcon className="size-4 text-muted-foreground shrink-0" />
								<p className="text-sm font-medium font-mono truncate min-w-0">
									{f.path}
								</p>
							</div>
							{canWrite && (
								<Button
									variant="ghost"
									size="icon"
									className="size-7 text-muted-foreground hover:text-destructive shrink-0"
									onClick={() => deleteMutation.mutate(f.id)}
									disabled={deleteMutation.isPending}
								>
									<Trash2 className="size-3.5" />
								</Button>
							)}
						</div>
					))}
					<LoadMoreSentinel
						hasMore={foldersQuery.hasNextPage}
						isLoadingMore={foldersQuery.isFetchingNextPage}
						isError={foldersQuery.isFetchNextPageError}
						onLoadMore={() => void foldersQuery.fetchNextPage()}
					/>
				</div>
			)}

			<FolderCreateDialog
				projectId={projectId}
				environmentId={environmentId}
				environmentStatus={environmentStatus}
				open={addOpen}
				onOpenChange={setAddOpen}
			/>
		</div>
	);
}

// ── Port Forwards Tab ────────────────────────────────────────────────────────
// Small, self-contained duplicate of the same CommandBox pattern
// environment-connect.tsx already established for its own SSH tab — kept
// local rather than shared across features for two components this size.

function CommandBox({ command }: { command: string }) {
	const { t } = useTranslation("projects");
	const [copied, setCopied] = useState(false);

	const copy = () => {
		// Best-effort: on failure the command is still selectable/copyable by
		// hand from the <code> block itself.
		void copyToClipboard(command).then((ok) => {
			if (!ok) return;
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
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

function AddPortForwardDialog({
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
	const [containerPort, setContainerPort] = useState("");

	const addMutation = useMutation({
		meta: { silent: true },
		mutationFn: () =>
			addPortForward(projectId, environmentId, {
				label: label.trim() || undefined,
				container_port: Number(containerPort),
			}),
		onSuccess: () => {
			qc.invalidateQueries({
				queryKey: environmentPortForwardsInfiniteQueryOptions(
					projectId,
					environmentId,
				).queryKey,
			});
			qc.invalidateQueries({
				queryKey: environmentQueryOptions(projectId, environmentId).queryKey,
			});
			onOpenChange(false);
			setLabel("");
			setContainerPort("");
		},
	});

	const portNum = Number(containerPort);
	const isValidPort =
		containerPort.trim() !== "" &&
		Number.isInteger(portNum) &&
		portNum >= 1 &&
		portNum <= 65535;

	// isValidPort already blocks an out-of-range port, so
	// EnvironmentPortForwardContainerPortInvalid mainly guards a stale check;
	// EnvironmentPortForwardContainerPortTaken is the genuinely common case —
	// the same port forwarded twice.
	const addErrorMessage = addMutation.isError
		? (() => {
				switch (getApiErrorCode(addMutation.error)) {
					case ApiErrorCode.EnvironmentPortForwardContainerPortTaken:
						return t("environments.detail.portForwards.addDialog.portTaken");
					case ApiErrorCode.EnvironmentPortForwardContainerPortInvalid:
						return t(
							"environments.detail.portForwards.addDialog.portOutOfRange",
						);
					default:
						return t("environments.detail.portForwards.addDialog.addFailed");
				}
			})()
		: null;

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent className="sm:max-w-lg">
				<DialogHeader>
					<DialogTitle className="flex items-center gap-2">
						<Network className="size-4 text-primary" />
						{t("environments.detail.portForwards.addDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("environments.detail.portForwards.addDialog.description")}
					</DialogDescription>
				</DialogHeader>
				<div className="space-y-4 py-2">
					<div className="space-y-1.5">
						<Label>
							{t("environments.detail.portForwards.addDialog.portLabel")}
						</Label>
						<Input
							type="number"
							min={1}
							max={65535}
							placeholder="3000"
							value={containerPort}
							onChange={(e) => setContainerPort(e.target.value)}
							className="max-w-32 font-mono"
						/>
						<p className="text-xs text-muted-foreground">
							{t("environments.detail.portForwards.addDialog.portHint")}
						</p>
					</div>
					<div className="space-y-1.5">
						<Label>
							{t("environments.detail.portForwards.addDialog.labelLabel")}
						</Label>
						<Input
							placeholder={t(
								"environments.detail.portForwards.addDialog.labelPlaceholder",
							)}
							value={label}
							onChange={(e) => setLabel(e.target.value)}
						/>
					</div>
					{addErrorMessage && (
						<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
							{addErrorMessage}
						</p>
					)}
				</div>
				<DialogFooter>
					<Button variant="outline" onClick={() => onOpenChange(false)}>
						{t("environments.detail.portForwards.addDialog.cancel")}
					</Button>
					<Button
						onClick={() => addMutation.mutate()}
						disabled={!isValidPort || addMutation.isPending}
					>
						{addMutation.isPending ? (
							<Spinner aria-hidden="true" />
						) : (
							t("environments.detail.portForwards.addDialog.add")
						)}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function RestartEnvironmentDialog({
	projectId,
	environment,
	open,
	onOpenChange,
}: {
	projectId: string;
	environment: Environment;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();

	const restartMutation = useMutation({
		meta: { silent: true },
		mutationFn: () => restartEnvironment(projectId, environment.id),
		onSuccess: (updated) => {
			qc.setQueryData(
				environmentQueryOptions(projectId, environment.id).queryKey,
				updated,
			);
			qc.invalidateQueries({
				queryKey: environmentPortForwardsInfiniteQueryOptions(
					projectId,
					environment.id,
				).queryKey,
			});
			onOpenChange(false);
		},
	});

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>
						{t("environments.detail.portForwards.restartDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("environments.detail.portForwards.restartDialog.description")}
					</DialogDescription>
				</DialogHeader>
				{restartMutation.isError && (
					<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
						{getApiErrorCode(restartMutation.error) ===
						ApiErrorCode.EnvironmentBusy
							? t("environments.detail.busy")
							: t(
									"environments.detail.portForwards.restartDialog.restartFailed",
								)}
					</p>
				)}
				<DialogFooter>
					<Button
						variant="outline"
						onClick={() => onOpenChange(false)}
						disabled={restartMutation.isPending}
					>
						{t("environments.detail.portForwards.restartDialog.cancel")}
					</Button>
					<Button
						onClick={() => restartMutation.mutate()}
						disabled={restartMutation.isPending}
					>
						{restartMutation.isPending ? (
							<Spinner aria-hidden="true" className="mr-2" />
						) : (
							<RotateCw className="size-4 mr-2" />
						)}
						{t("environments.detail.portForwards.restartDialog.confirm")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function PortForwardsTab({
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
	const { data: config } = useQuery(environmentConfigQueryOptions());
	const forwardsQuery = useInfiniteQuery(
		environmentPortForwardsInfiniteQueryOptions(projectId, environment.id),
	);
	const forwards = forwardsQuery.data?.pages.flatMap((p) => p.items) ?? [];
	const forwardCount =
		forwardsQuery.data?.pages[0]?.total_count ?? forwards.length;
	const [addOpen, setAddOpen] = useState(false);
	const [restartOpen, setRestartOpen] = useState(false);
	const host = config?.port_forward_host || null;
	const isRunning = environment.status === "running";

	const forwardsKey = environmentPortForwardsInfiniteQueryOptions(
		projectId,
		environment.id,
	).queryKey;

	const deleteMutation = useMutation({
		meta: { success: "removed", subject: "portForward" },
		onError: (err) =>
			notifyError(err, t("environments.detail.portForwards.deleteFailed")),
		mutationFn: (portForwardId: string) =>
			deletePortForward(projectId, environment.id, portForwardId),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: forwardsKey });
			qc.invalidateQueries({
				queryKey: environmentQueryOptions(projectId, environment.id).queryKey,
			});
		},
	});

	return (
		<div className="space-y-4">
			<p className="text-sm text-muted-foreground">
				{t("environments.detail.portForwards.description")}
			</p>

			{environment.ports_pending_restart && (
				<div className="flex items-center gap-2 rounded-md border border-warning/30 bg-warning/10 px-3 py-2 text-sm text-warning">
					<AlertTriangle className="size-4 shrink-0" />
					<span className="flex-1">
						{isRunning
							? t("environments.detail.portForwards.pendingRestartRunning")
							: t("environments.detail.portForwards.pendingRestartStopped")}
					</span>
					{isRunning && canWrite && (
						<Button
							size="sm"
							variant="outline"
							className="shrink-0"
							onClick={() => setRestartOpen(true)}
						>
							<RotateCw className="size-3.5 mr-1.5" />
							{t("environments.detail.portForwards.restart")}
						</Button>
					)}
				</div>
			)}

			<div className="flex items-center justify-between">
				<p className="text-sm text-muted-foreground">
					{t("environments.detail.portForwards.count", {
						count: forwardCount,
					})}
				</p>
				{canWrite && (
					<Button size="sm" variant="outline" onClick={() => setAddOpen(true)}>
						<Plus className="size-4 mr-1.5" />
						{t("environments.detail.portForwards.add")}
					</Button>
				)}
			</div>

			{forwards.length === 0 ? (
				<div className="flex flex-col items-center justify-center gap-3 py-10 rounded-xl border border-dashed border-border">
					<Network className="size-7 text-muted-foreground/40" />
					<p className="text-sm text-muted-foreground">
						{t("environments.detail.portForwards.empty.title")}
					</p>
					{canWrite && (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setAddOpen(true)}
						>
							<Plus className="size-3.5 mr-1" />
							{t("environments.detail.portForwards.empty.addFirst")}
						</Button>
					)}
				</div>
			) : (
				<div className="space-y-2">
					{forwards.map((pf) => (
						<div
							key={pf.id}
							className="flex flex-col gap-3 rounded-lg border border-border/60 bg-card px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
						>
							<div className="flex items-center gap-3 min-w-0 flex-1">
								<Network className="size-4 text-muted-foreground shrink-0" />
								<div className="min-w-0 flex-1">
									<Link
										to="/projects/$projectId/environments/$environmentId/port-forwards/$portForwardId"
										params={{
											projectId,
											environmentId: environment.id,
											portForwardId: pf.id,
										}}
										className="text-sm font-medium truncate hover:underline block"
									>
										{pf.label}
									</Link>
									<p className="text-xs text-muted-foreground font-mono">
										{t("environments.detail.portForwards.containerPort", {
											port: pf.container_port,
										})}
									</p>
								</div>
							</div>
							<div className="flex items-center gap-2 shrink-0">
								{pf.host_port !== null ? (
									<>
										<div className="w-full sm:w-80">
											<CommandBox
												command={`${host ?? "<host>"}:${pf.host_port}`}
											/>
										</div>
										{host && (
											<Button
												variant="ghost"
												size="icon"
												className="size-7 text-muted-foreground shrink-0"
												title={t("environments.detail.portForwards.open")}
												onClick={() => {
													if (pf.host_port === null) return;
													window.open(
														portForwardUrl(host, pf.host_port),
														"_blank",
														"noopener,noreferrer",
													);
												}}
											>
												<ExternalLink className="size-3.5" />
											</Button>
										)}
									</>
								) : (
									<span className="text-xs text-muted-foreground">
										{t("environments.detail.portForwards.unassigned")}
									</span>
								)}
								{canWrite && (
									<Button
										variant="ghost"
										size="icon"
										className="size-7 text-muted-foreground hover:text-destructive shrink-0"
										onClick={() => deleteMutation.mutate(pf.id)}
										disabled={deleteMutation.isPending}
									>
										<Trash2 className="size-3.5" />
									</Button>
								)}
							</div>
						</div>
					))}
					<LoadMoreSentinel
						hasMore={forwardsQuery.hasNextPage}
						isLoadingMore={forwardsQuery.isFetchingNextPage}
						isError={forwardsQuery.isFetchNextPageError}
						onLoadMore={() => void forwardsQuery.fetchNextPage()}
					/>
				</div>
			)}

			<AddPortForwardDialog
				projectId={projectId}
				environmentId={environment.id}
				open={addOpen}
				onOpenChange={setAddOpen}
			/>
			<RestartEnvironmentDialog
				projectId={projectId}
				environment={environment}
				open={restartOpen}
				onOpenChange={setRestartOpen}
			/>
		</div>
	);
}

// ── Page ──────────────────────────────────────────────────────────────────────

const TABS = [
	{
		id: "overview",
		labelKey: "environments.detail.tabs.overview",
		icon: Server,
	},
	{
		id: "folders",
		labelKey: "environments.detail.tabs.folders",
		icon: FolderIcon,
	},
	{
		id: "portForwards",
		labelKey: "environments.detail.tabs.portForwards",
		icon: Network,
	},
] as const satisfies {
	id: Tab;
	labelKey: string;
	icon: React.ComponentType<{ className?: string }>;
}[];

export function EnvironmentDetailView({
	projectId,
	environmentId,
}: {
	projectId: string;
	environmentId: string;
}) {
	const { t } = useTranslation("projects");
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canWrite = hasProjectPermission("environments:write");
	const qc = useQueryClient();
	const navigate = useNavigate();

	const { data: environment } = useQuery(
		environmentQueryOptions(projectId, environmentId),
	);

	const [activeTab, setActiveTab] = useState<Tab>(() => {
		const hash = window.location.hash.slice(1);
		if (hash && TABS.map((tab) => tab.id).includes(hash as Tab)) {
			return hash as Tab;
		}
		return "overview";
	});
	const [confirmDelete, setConfirmDelete] = useState(false);

	useEffect(() => {
		const handleHashChange = () => {
			const hash = window.location.hash.slice(1);
			if (hash && TABS.map((tab) => tab.id).includes(hash as Tab)) {
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

	const envKey = environmentQueryOptions(projectId, environmentId).queryKey;
	const listKey = ["projects", projectId, "environments"];

	// Start/Stop/Delete all live in the header next to Connect (see the
	// return below) rather than inline in OverviewTab — lifted up here so
	// they're reachable from every tab, not just Overview.
	const actionErrorMessage = (err: unknown) =>
		getApiErrorCode(err) === ApiErrorCode.EnvironmentBusy
			? t("environments.detail.busy")
			: t("environments.detail.actionFailed");

	const startMutation = useMutation({
		meta: { success: "started", subject: "environment" },
		onError: (err) => notifyError(err, actionErrorMessage(err)),
		mutationFn: () => startEnvironment(projectId, environmentId),
		onSuccess: (updated) => {
			qc.setQueryData(envKey, updated);
			qc.invalidateQueries({ queryKey: listKey });
		},
	});

	const stopMutation = useMutation({
		meta: { success: "stopped", subject: "environment" },
		onError: (err) => notifyError(err, actionErrorMessage(err)),
		mutationFn: () => stopEnvironment(projectId, environmentId),
		onSuccess: (updated) => {
			qc.setQueryData(envKey, updated);
			qc.invalidateQueries({ queryKey: listKey });
		},
	});

	const deleteMutation = useMutation({
		meta: { success: "deleted", subject: "environment", silent: true },
		mutationFn: () => deleteEnvironment(projectId, environmentId),
		onSuccess: () => {
			qc.invalidateQueries({ queryKey: listKey });
			navigate({
				to: "/projects/$projectId/environments",
				params: { projectId },
				search: { create: false },
			});
		},
	});

	// Called unconditionally (before the `!environment` early return below)
	// and shared with OverviewTab as a prop — see this hook's own doc
	// comment on why one subscription per page, not one per consumer,
	// matters here.
	const usage = useEnvironmentUsage(projectId, environmentId, environment);

	if (!environment) {
		return (
			<div className="flex flex-col gap-4 p-6">
				<Skeleton className="h-16 w-full rounded-xl" />
				<Skeleton className="h-64 w-full rounded-xl" />
			</div>
		);
	}

	const isTransitioning = TRANSITIONAL_STATUSES.includes(environment.status);
	const canStop = !isTransitioning && environment.status === "running";
	const canStart =
		!isTransitioning &&
		(environment.status === "stopped" ||
			environment.status === "suspended" ||
			environment.status === "error");
	return (
		<div className="flex flex-col flex-1 min-h-0">
			{/* Environment header */}
			<div className="border-b border-border/50 px-6 py-5 shrink-0">
				<div className="flex items-center justify-between gap-4">
					<div className="flex items-center gap-4">
						<EnvironmentStatusRing
							environment={environment}
							size={52}
							hasActiveSshSession={usage.hasActiveSshSession}
						/>
						<div>
							<h1 className="text-lg font-semibold">{environment.name}</h1>
							<span className="text-sm text-muted-foreground font-mono">
								{environment.slug}
							</span>
							<div className="mt-1">
								<EnvironmentStatusLine
									environment={environment}
									hasActiveSshSession={usage.hasActiveSshSession}
									showDot={environment.status === "running"}
								/>
							</div>
						</div>
					</div>
					<div className="flex items-center gap-2 shrink-0">
						{canWrite && canStart && (
							<Button
								variant="outline"
								onClick={() => startMutation.mutate()}
								disabled={startMutation.isPending}
							>
								{startMutation.isPending ? (
									<Spinner aria-hidden="true" className="size-3.5 mr-1.5" />
								) : (
									<Play className="size-3.5 mr-1.5" />
								)}
								{t("environments.detail.overview.start")}
							</Button>
						)}
						<Link
							to="/projects/$projectId/environments/$environmentId/connect"
							params={{ projectId, environmentId }}
							className={buttonVariants({ variant: "outline" })}
						>
							<ExternalLink className="size-3.5 mr-1.5" />
							{t("environments.detail.overview.connect")}
						</Link>
						{canWrite && (
							<DropdownMenu>
								<DropdownMenuTrigger
									className={buttonVariants({
										variant: "outline",
										size: "icon",
									})}
								>
									<MoreHorizontal className="size-4" />
								</DropdownMenuTrigger>
								<DropdownMenuContent align="end" className="w-40">
									{canStop && (
										<DropdownMenuItem
											onClick={() => stopMutation.mutate()}
											disabled={stopMutation.isPending}
										>
											{stopMutation.isPending ? (
												<Spinner aria-hidden="true" className="size-3.5 mr-2" />
											) : (
												<Square className="size-3.5 mr-2" />
											)}
											{t("environments.detail.overview.stop")}
										</DropdownMenuItem>
									)}
									<DropdownMenuItem
										className="text-destructive focus:text-destructive"
										onClick={() => setConfirmDelete(true)}
									>
										<Trash2 className="size-3.5 mr-2" />
										{t("environments.detail.overview.delete")}
									</DropdownMenuItem>
								</DropdownMenuContent>
							</DropdownMenu>
						)}
					</div>
				</div>
			</div>

			<Tabs
				value={activeTab}
				onValueChange={(v) => handleTabChange(v as Tab)}
				className="min-h-0 flex-1 gap-0"
			>
				<div className="border-b border-border/50 px-6 shrink-0 overflow-x-auto">
					<TabsList
						variant="line"
						className="h-10 w-max min-w-full justify-start"
					>
						{TABS.map((tab) => {
							const Icon = tab.icon;
							return (
								<TabsTrigger
									key={tab.id}
									value={tab.id}
									className="flex-none px-3"
								>
									<Icon />
									{t(tab.labelKey)}
								</TabsTrigger>
							);
						})}
					</TabsList>
				</div>

				<TabsContent value="overview" className="flex-1 overflow-auto p-6">
					<OverviewTab
						environment={environment}
						projectId={projectId}
						canWrite={canWrite}
						onGoToPortForwards={() => handleTabChange("portForwards")}
						usage={usage}
					/>
				</TabsContent>
				<TabsContent value="folders" className="flex-1 overflow-auto p-6">
					<FoldersTab
						projectId={projectId}
						environmentId={environmentId}
						environmentStatus={environment.status}
						canWrite={canWrite}
					/>
				</TabsContent>
				<TabsContent value="portForwards" className="flex-1 overflow-auto p-6">
					<PortForwardsTab
						projectId={projectId}
						environment={environment}
						canWrite={canWrite}
					/>
				</TabsContent>
			</Tabs>

			<Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
				<DialogContent className="max-w-sm">
					<DialogHeader>
						<DialogTitle>
							{t("environments.detail.overview.deleteDialog.title", {
								name: environment.name,
							})}
						</DialogTitle>
						<DialogDescription>
							{t("environments.detail.overview.deleteDialog.description")}
						</DialogDescription>
					</DialogHeader>
					{deleteMutation.isError && (
						<p className="text-sm text-destructive rounded-md bg-destructive/10 px-3 py-2">
							{t("environments.detail.overview.deleteDialog.deleteFailed")}
						</p>
					)}
					<DialogFooter>
						<Button
							variant="outline"
							onClick={() => setConfirmDelete(false)}
							disabled={deleteMutation.isPending}
						>
							{t("environments.detail.overview.deleteDialog.cancel")}
						</Button>
						<Button
							variant="destructive"
							onClick={() => deleteMutation.mutate()}
							disabled={deleteMutation.isPending}
						>
							{deleteMutation.isPending ? (
								<Spinner aria-hidden="true" />
							) : (
								t("environments.detail.overview.deleteDialog.delete")
							)}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</div>
	);
}
