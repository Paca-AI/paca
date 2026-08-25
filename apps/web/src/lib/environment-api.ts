import { type Query, queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";

// Mirrors agent-api.ts exactly: types, REST functions, and queryOptions(...)
// factories — see docs/ai-agent/environment-management.md for the full
// design. Base path `/projects/{projectId}/environments`, under the existing
// `/api/v1` prefix api-client.ts already applies.

// ── Shapes ────────────────────────────────────────────────────────────────────

export type EnvironmentStatus =
	| "creating"
	| "running"
	| "stopping"
	| "stopped"
	| "suspended"
	| "error"
	| "deleting";

export type EnvironmentBackend = "docker" | "kubernetes";

// EnvironmentFolder is identified purely by path — a name/repo-clone-URL/
// branch used to exist here but were dropped before ever shipping (the
// repo-clone machinery was never fully wired to a real credential
// source).
export interface EnvironmentFolder {
	id: string;
	path: string;
	created_at: string;
}

// EnvironmentBrowseEntry is one immediate child of a directory listed via
// browseEnvironment — a live read against the environment's own running
// container/Pod filesystem, not a persisted row.
export interface EnvironmentBrowseEntry {
	name: string;
	is_dir: boolean;
}

export interface Environment {
	id: string;
	project_id: string;
	name: string;
	slug: string;
	// null until agent-runner's own SSH-routing feature assigns one (or
	// permanently null on a deployment that never configured it) — see
	// docs/ai-agent/environment-management.md's "Terminal / SSH Access"
	// section. Published as a native Docker -p binding or a Kubernetes
	// NodePort Service entry directly on the environment's own
	// container/Pod, never relayed through agent-runner's own process.
	ssh_port: number | null;
	status: EnvironmentStatus;
	backend: EnvironmentBackend;
	image: string | null;
	cpu_limit: string;
	memory_limit: string;
	disk_limit_gb: number;
	idle_timeout_minutes: number;
	last_active_at: string;
	error_message?: string | null;
	// True whenever a port forward has been added/removed since the
	// environment's backing container/Pod last had its full port-mapping
	// set applied — see docs/ai-agent/environment-management.md's "Port
	// Forwarding" section. When true, show a "restart required" prompt
	// (see restartEnvironment below).
	ports_pending_restart: boolean;
	created_at: string;
	updated_at: string;
	folders: EnvironmentFolder[];
}

// EnvironmentStats is one message on the live-usage WebSocket
// (environment-status-ring.tsx's useEnvironmentUsage connects to
// getStatsTicket's ws_url) — a point-in-time snapshot, not a persisted
// field on Environment above. cpu_usage_usec is cumulative since the
// container/Pod started, not an instantaneous rate — useEnvironmentUsage
// derives a rate from two successive messages.
// cpu_limit_millicores/memory_limit_bytes are 0 when the backend reports
// no enforced limit — fall back to the parent Environment's own
// cpu_limit/memory_limit in that case.
export interface EnvironmentStats {
	cpu_usage_usec: number;
	cpu_limit_millicores: number;
	memory_used_bytes: number;
	memory_limit_bytes: number;
	disk_used_bytes: number;
}

export interface EnvironmentSSHKey {
	id: string;
	label: string;
	public_key: string;
	fingerprint: string;
	created_at: string;
}

// EnvironmentPortForward is a user-added mapping exposing one container
// port on a dedicated external port — see
// docs/ai-agent/environment-management.md's "Port Forwarding" section.
// host_port is null until agent-runner assigns one (either the
// environment isn't running yet, or port forwarding isn't configured on
// this deployment); even once assigned, it isn't necessarily *live* yet —
// see Environment.ports_pending_restart.
export interface EnvironmentPortForward {
	id: string;
	label: string;
	container_port: number;
	host_port: number | null;
	created_at: string;
}

// Statuses an environment only passes through transiently on its way to a
// stable state — while in one of these, environmentQueryOptions polls (see
// below) instead of waiting for a manual refresh or a socket event (no
// realtime wiring exists for environments in Phase 1).
const TRANSITIONAL_ENVIRONMENT_STATUSES = new Set<EnvironmentStatus>([
	"creating",
	"stopping",
	"deleting",
]);

const ENVIRONMENT_POLL_INTERVAL_MS = 2_000;

// ── Environments ──────────────────────────────────────────────────────────────

export async function listEnvironments(
	projectId: string,
): Promise<Environment[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ environments: Environment[] }>
	>(`/projects/${projectId}/environments`);
	return data.data.environments;
}

export async function getEnvironment(
	projectId: string,
	environmentId: string,
): Promise<Environment> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<Environment>>(
		`/projects/${projectId}/environments/${environmentId}`,
	);
	return data.data;
}

export async function createEnvironment(
	projectId: string,
	payload: {
		name: string;
		image?: string;
		cpu_limit?: string;
		memory_limit?: string;
		disk_limit_gb?: number;
	},
): Promise<Environment> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Environment>>(
		`/projects/${projectId}/environments`,
		payload,
	);
	return data.data;
}

export async function updateEnvironment(
	projectId: string,
	environmentId: string,
	payload: { name?: string; idle_timeout_minutes?: number },
): Promise<Environment> {
	const { data } = await apiClient.instance.patch<SuccessEnvelope<Environment>>(
		`/projects/${projectId}/environments/${environmentId}`,
		payload,
	);
	return data.data;
}

export async function deleteEnvironment(
	projectId: string,
	environmentId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/environments/${environmentId}`,
	);
}

export async function startEnvironment(
	projectId: string,
	environmentId: string,
): Promise<Environment> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Environment>>(
		`/projects/${projectId}/environments/${environmentId}/start`,
	);
	return data.data;
}

export async function stopEnvironment(
	projectId: string,
	environmentId: string,
): Promise<Environment> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Environment>>(
		`/projects/${projectId}/environments/${environmentId}/stop`,
	);
	return data.data;
}

// restartEnvironment applies any pending port-forward changes to a
// currently-running environment's backing container/Pod — the explicit
// user-facing action for Environment.ports_pending_restart. Only valid
// against a running environment; a stopped one applies its pending
// changes automatically on its next startEnvironment call instead.
export async function restartEnvironment(
	projectId: string,
	environmentId: string,
): Promise<Environment> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<Environment>>(
		`/projects/${projectId}/environments/${environmentId}/restart`,
	);
	return data.data;
}

// heartbeatEnvironment refreshes an environment's idle timer — pinged
// periodically (see environment-terminal.tsx's CONVERSATION_HEARTBEAT_INTERVAL_MS
// sibling below) while a browser terminal session is open against it, the
// same "keep the sandbox alive while a tab is actively using it" pattern
// heartbeatConversation already establishes in agent-api.ts.
export async function heartbeatEnvironment(
	projectId: string,
	environmentId: string,
): Promise<void> {
	await apiClient.instance.post(
		`/projects/${projectId}/environments/${environmentId}/heartbeat`,
	);
}

export const ENVIRONMENT_HEARTBEAT_INTERVAL_MS = 30_000;

// ── Folders ───────────────────────────────────────────────────────────────────

export async function listFolders(
	projectId: string,
	environmentId: string,
): Promise<EnvironmentFolder[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ folders: EnvironmentFolder[] }>
	>(`/projects/${projectId}/environments/${environmentId}/folders`);
	return data.data.folders;
}

export async function addFolder(
	projectId: string,
	environmentId: string,
	payload: { path: string },
): Promise<EnvironmentFolder> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<EnvironmentFolder>
	>(`/projects/${projectId}/environments/${environmentId}/folders`, payload);
	return data.data;
}

export async function deleteFolder(
	projectId: string,
	environmentId: string,
	folderId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/environments/${environmentId}/folders/${folderId}`,
	);
}

// browseEnvironment lists the immediate children of path (defaults
// server-side to the environment's fixed folders root when omitted)
// inside a running environment's own container/Pod — used by
// FolderCreateDialog's "browse instead of typing blind" affordance.
// Requires the environment to be running (a 409/error otherwise — see
// ErrEnvironmentNotRunning on the backend).
export async function browseEnvironment(
	projectId: string,
	environmentId: string,
	path?: string,
): Promise<{ path: string; entries: EnvironmentBrowseEntry[] }> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ path: string; entries: EnvironmentBrowseEntry[] }>
	>(`/projects/${projectId}/environments/${environmentId}/browse`, {
		params: path ? { path } : undefined,
	});
	return data.data;
}

// ── SSH Keys ──────────────────────────────────────────────────────────────────

export async function listSSHKeys(
	projectId: string,
	environmentId: string,
): Promise<EnvironmentSSHKey[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ ssh_keys: EnvironmentSSHKey[] }>
	>(`/projects/${projectId}/environments/${environmentId}/ssh-keys`);
	return data.data.ssh_keys;
}

export async function addSSHKey(
	projectId: string,
	environmentId: string,
	payload: { label: string; public_key: string },
): Promise<EnvironmentSSHKey> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<EnvironmentSSHKey>
	>(`/projects/${projectId}/environments/${environmentId}/ssh-keys`, payload);
	return data.data;
}

export async function deleteSSHKey(
	projectId: string,
	environmentId: string,
	keyId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/environments/${environmentId}/ssh-keys/${keyId}`,
	);
}

// ── Port Forwards ─────────────────────────────────────────────────────────────

export async function listPortForwards(
	projectId: string,
	environmentId: string,
): Promise<EnvironmentPortForward[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<{ port_forwards: EnvironmentPortForward[] }>
	>(`/projects/${projectId}/environments/${environmentId}/port-forwards`);
	return data.data.port_forwards;
}

export async function addPortForward(
	projectId: string,
	environmentId: string,
	payload: { label?: string; container_port: number },
): Promise<EnvironmentPortForward> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<EnvironmentPortForward>
	>(
		`/projects/${projectId}/environments/${environmentId}/port-forwards`,
		payload,
	);
	return data.data;
}

export async function deletePortForward(
	projectId: string,
	environmentId: string,
	portForwardId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`/projects/${projectId}/environments/${environmentId}/port-forwards/${portForwardId}`,
	);
}

// ── Environment tickets (terminal + stats WebSockets) ───────────────────────────
//
// Both endpoints below mint a short-lived signed ticket one of
// agent-runner's browser-facing WebSocket endpoints verifies on its own —
// see services/api's EnvironmentHandler.TerminalTicket/StatsTicket for the
// full contract. resolveWsUrl below is shared by both the terminal
// (environment-terminal.tsx) and the live-usage stream
// (environment-status-ring.tsx)'s own WebSocket connection setup.

export interface EnvironmentTicket {
	ticket: string;
	ws_url: string;
}

export async function getTerminalTicket(
	projectId: string,
	environmentId: string,
): Promise<EnvironmentTicket> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<EnvironmentTicket>
	>(`/projects/${projectId}/environments/${environmentId}/terminal-ticket`);
	return data.data;
}

export async function getStatsTicket(
	projectId: string,
	environmentId: string,
): Promise<EnvironmentTicket> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<EnvironmentTicket>
	>(`/projects/${projectId}/environments/${environmentId}/stats-ticket`);
	return data.data;
}

/**
 * Resolves a ticket response's `ws_url` against the current origin — the
 * same "treat window.location as the connection's base" convention
 * socket-client.ts uses for its Socket.IO origin. Already-absolute
 * ws(s):// URLs (the expected shape) pass through untouched; a bare path is
 * resolved against the page's own host, swapping http(s) for the matching
 * ws(s) scheme.
 */
export function resolveWsUrl(wsUrl: string): string {
	if (/^wss?:\/\//i.test(wsUrl)) return wsUrl;
	const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
	const path = wsUrl.startsWith("/") ? wsUrl : `/${wsUrl}`;
	return `${proto}//${window.location.host}${path}`;
}

// ── Deployment config ────────────────────────────────────────────────────────

// EnvironmentDeploymentConfig is public, deployment-wide (not per-project)
// config the Connect page needs to show a real `ssh` command / port-
// forward address instead of a placeholder host — see
// docs/ai-agent/environment-management.md's "Terminal / SSH Access" and
// "Port Forwarding" sections. Either field is "" when the operator hasn't
// configured that feature on this deployment.
export interface EnvironmentDeploymentConfig {
	ssh_bastion_host: string;
	port_forward_host: string;
}

export async function getEnvironmentConfig(): Promise<EnvironmentDeploymentConfig> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<EnvironmentDeploymentConfig>
	>("/environments/config");
	return data.data;
}

// Not project- or environment-scoped (the same deployment-wide values for
// everyone), so one shared query key/cache entry — mirrors how
// currentUserQueryOptions/branding-style global config is fetched
// elsewhere in this app. staleTime: Infinity since this can only change by
// redeploying the backend, never during a session.
export const environmentConfigQueryOptions = () =>
	queryOptions({
		queryKey: ["environments", "config"],
		queryFn: getEnvironmentConfig,
		staleTime: Number.POSITIVE_INFINITY,
	});

// ── Query Options ─────────────────────────────────────────────────────────────

export const environmentsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["projects", projectId, "environments"],
		queryFn: () => listEnvironments(projectId),
	});

// Polls every ENVIRONMENT_POLL_INTERVAL_MS while the environment is in a
// transitional status (creating/stopping/deleting) — no realtime socket
// wiring exists for environment status changes in Phase 1, so this is the
// only way the UI learns a transition finished. Stops polling once the
// status settles into a stable one (running/stopped/suspended/error).
export const environmentQueryOptions = (
	projectId: string,
	environmentId: string,
) =>
	queryOptions({
		queryKey: ["projects", projectId, "environments", environmentId],
		queryFn: () => getEnvironment(projectId, environmentId),
		refetchInterval: (query: Query<Environment>) => {
			const status = query.state.data?.status;
			return status && TRANSITIONAL_ENVIRONMENT_STATUSES.has(status)
				? ENVIRONMENT_POLL_INTERVAL_MS
				: false;
		},
	});

// environmentBrowseQueryOptions is keyed by path (undefined path collapses
// to "" so the root listing has a stable key) — FolderCreateDialog issues
// one of these per directory it navigates into. staleTime: 0 (the
// default) is deliberate: the container's own filesystem can change under
// an agent's feet between browses, unlike everything else in this file.
export const environmentBrowseQueryOptions = (
	projectId: string,
	environmentId: string,
	path?: string,
) =>
	queryOptions({
		queryKey: [
			"projects",
			projectId,
			"environments",
			environmentId,
			"browse",
			path ?? "",
		],
		queryFn: () => browseEnvironment(projectId, environmentId, path),
	});

export const environmentFoldersQueryOptions = (
	projectId: string,
	environmentId: string,
) =>
	queryOptions({
		queryKey: ["projects", projectId, "environments", environmentId, "folders"],
		queryFn: () => listFolders(projectId, environmentId),
	});

export const environmentSSHKeysQueryOptions = (
	projectId: string,
	environmentId: string,
) =>
	queryOptions({
		queryKey: [
			"projects",
			projectId,
			"environments",
			environmentId,
			"ssh-keys",
		],
		queryFn: () => listSSHKeys(projectId, environmentId),
	});

export const environmentPortForwardsQueryOptions = (
	projectId: string,
	environmentId: string,
) =>
	queryOptions({
		queryKey: [
			"projects",
			projectId,
			"environments",
			environmentId,
			"port-forwards",
		],
		queryFn: () => listPortForwards(projectId, environmentId),
	});

// ── Helpers ───────────────────────────────────────────────────────────────────

// Colors only — unlike agent-api.ts's CONVERSATION_STATUS_LABELS, the label
// text itself is looked up via i18n (`environments.status.<status>`, see
// environment-detail.tsx) rather than hardcoded here, so every status string
// shown to a user goes through translation instead of staying English-only.
export const ENVIRONMENT_STATUS_COLORS: Record<EnvironmentStatus, string> = {
	creating: "text-amber-500",
	running: "text-emerald-500",
	stopping: "text-amber-500",
	stopped: "text-muted-foreground",
	suspended: "text-muted-foreground",
	error: "text-destructive",
	deleting: "text-amber-500",
};
