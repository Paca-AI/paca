import { queryOptions } from "@tanstack/react-query";
import { apiClient } from "./api-client";
import type { SuccessEnvelope } from "./api-error";
import { type ApiErrorCode, getApiErrorCode } from "./api-error";
import { sanitizeRedirect } from "./safe-redirect";

/** What a Git provider plugin reports about itself (`gitprovider.Info`). */
export interface GitProvider {
	id: string;
	display_name: string;
	icon?: string;
	hosts: string[];
	self_hosted_hosts: boolean;
	terms: {
		change_request: string;
		change_request_plural: string;
		repository: string;
	};
	capabilities: {
		multiple_identities: boolean;
		access_grants: boolean;
		/** Branches can be looked up and created through the provider. */
		branches?: boolean;
		/** The kinds of connection the provider offers, the default first. */
		identity_kinds?: GitConnectionKind[];
		/** What each kind implies; absent: every kind behaves as the provider says. */
		kinds?: { kind: GitConnectionKind; access_grants: boolean }[];
	};
}

/** How a connection exists on the platform: an App/bot, or an access token. */
export type GitConnectionKind = "app" | "bot_user" | "token";

/**
 * A connection: the credential (a GitHub App, an access token) Paca reaches a
 * Git platform with. `shared` ones are usable by every project, `project` ones
 * by `project_id` only, `agent` ones by one agent.
 */
export interface GitIdentity {
	id: string;
	provider_id: string;
	host: string;
	kind: string;
	scope: "shared" | "project" | "agent";
	project_id: string | null;
	handle: string;
	display_name: string;
	expires_at: string | null;
	/** How many linked repositories, in every project, use this connection. */
	repository_count?: number;
	created_at: string;
}

export interface GitSetupField {
	name: string;
	label: string;
	help?: string;
	secret?: boolean;
	required?: boolean;
}

/** The next thing the browser must do to continue identity setup. */
export interface GitSetupStep {
	kind: "redirect" | "form_post" | "token_form" | "done";
	url?: string;
	fields?: Record<string, string>;
	form?: GitSetupField[];
	prompt?: string;
	state?: string;
}

export interface GitSetupCompletion {
	identity: GitIdentity;
	next: GitSetupStep | null;
	return_url: string;
	done: boolean;
}

export interface BeginGitSetupPayload {
	provider_id: string;
	host?: string;
	return_url: string;
	params?: Record<string, string>;
}

export async function listGitProviders(): Promise<GitProvider[]> {
	const { data } =
		await apiClient.instance.get<SuccessEnvelope<GitProvider[]>>(
			"/git/providers",
		);
	return data.data;
}

/**
 * The kinds of connection a user can choose between when adding one (the
 * default first). A provider lists `kinds` only when setup really offers a
 * choice; otherwise there is one way to connect and nothing to pick.
 */
export function connectionKindsOf(provider: GitProvider): GitConnectionKind[] {
	const choosable = provider.capabilities.kinds?.map((k) => k.kind) ?? [];
	if (choosable.length > 0) return choosable;
	return [provider.capabilities.identity_kinds?.[0] ?? "app"];
}

/** Whether a connection of this kind must be granted access per account. */
export function kindNeedsAccessGrant(
	provider: GitProvider,
	kind: string,
): boolean {
	const profile = provider.capabilities.kinds?.find((k) => k.kind === kind);
	return profile ? profile.access_grants : provider.capabilities.access_grants;
}

export async function beginGitSetup(
	payload: BeginGitSetupPayload,
): Promise<GitSetupStep> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<GitSetupStep>>(
		"/git/identities/setup",
		payload,
	);
	return data.data;
}

export async function completeGitSetup(
	state: string,
	params: Record<string, string>,
): Promise<GitSetupCompletion> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<GitSetupCompletion>
	>("/git/identities/setup/complete", { state, params });
	return data.data;
}

export async function deleteGitIdentity(id: string): Promise<void> {
	await apiClient.instance.delete(`/git/identities/${id}`);
}

export const gitProvidersQueryOptions = queryOptions({
	queryKey: ["git", "providers"] as const,
	queryFn: listGitProviders,
	staleTime: 60 * 1000,
});

/** Root of every Git query, for refreshing them all after a change. */
export const gitQueriesKey = ["git"] as const;

/** Where the browser lands when a return URL is missing or not ours. */
export const GIT_FALLBACK_PATH = "/home";

/** A project's settings page, on its Git section. */
export const projectGitSettingsPath = (projectId: string) =>
	`/projects/${projectId}/settings#git`;

/**
 * Turns the `return_url` the API reports into a same-origin path the router
 * can navigate to. The API only accepts a return URL on its own origin, but
 * the browser does not rely on that: anything that is not a same-origin path
 * falls back to the home page.
 */
export function returnPathOf(
	returnUrl: string | null | undefined,
	origin: string = window.location.origin,
): string {
	if (!returnUrl) return GIT_FALLBACK_PATH;
	try {
		const u = new URL(returnUrl, origin);
		if (u.origin !== origin) return GIT_FALLBACK_PATH;
		return (
			sanitizeRedirect(`${u.pathname}${u.search}${u.hash}`, origin) ??
			GIT_FALLBACK_PATH
		);
	} catch {
		return GIT_FALLBACK_PATH;
	}
}

/**
 * Sends the browser through a redirect or form-post step. A form post is
 * how GitHub's App manifest flow starts: the manifest travels as a form field.
 * Returns false for steps that need no navigation (token form, done).
 */
export function followGitStep(step: GitSetupStep): boolean {
	if (!step.url) return false;
	if (step.kind === "redirect") {
		window.location.assign(step.url);
		return true;
	}
	if (step.kind === "form_post") {
		const form = document.createElement("form");
		form.method = "POST";
		form.action = step.url;
		for (const [name, value] of Object.entries(step.fields ?? {})) {
			const input = document.createElement("input");
			input.type = "hidden";
			input.name = name;
			input.value = value;
			form.appendChild(input);
		}
		document.body.appendChild(form);
		form.submit();
		return true;
	}
	return false;
}

/** A repository linked to a project. */
export interface GitRepository {
	id: string;
	provider_id: string;
	host: string;
	full_path: string;
	default_branch: string;
	clone_url: string;
	/** What the repository is for (at most 1000 characters); empty when unset. */
	description: string;
	identity_id: string | null;
	/** What to @mention to reach the bot serving the repository; empty when unknown. */
	identity_handle: string;
	/**
	 * Set when the platform must be told by hand where to deliver events for
	 * this repository: its connection needs a webhook per repository and none
	 * could be registered automatically.
	 */
	webhook_url?: string;
	/**
	 * False when Paca never tried to register this repository's webhook (for
	 * example it was carried over from an earlier version of a provider plugin),
	 * as opposed to having tried and failed.
	 */
	webhook_attempted?: boolean;
	created_at: string;
}

export interface GitAccessCompletion {
	owner: string;
	return_url: string;
}

const projectGit = (projectId: string) => `/projects/${projectId}/git`;

export async function listProjectGitProviders(
	projectId: string,
): Promise<GitProvider[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<GitProvider[]>>(
		`${projectGit(projectId)}/providers`,
	);
	return data.data;
}

export async function listGitRepositories(
	projectId: string,
): Promise<GitRepository[]> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<GitRepository[]>
	>(`${projectGit(projectId)}/repositories`);
	return data.data;
}

export async function linkGitRepository(
	projectId: string,
	payload: {
		provider_id: string;
		host?: string;
		path_or_url: string;
		/** The connection to link through; empty: any the project can use. */
		identity_id?: string;
		description?: string;
	},
): Promise<GitRepository> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<GitRepository>
	>(`${projectGit(projectId)}/repositories`, payload);
	return data.data;
}

export async function updateGitRepository(
	projectId: string,
	repositoryId: string,
	payload: { description: string },
): Promise<GitRepository> {
	const { data } = await apiClient.instance.patch<
		SuccessEnvelope<GitRepository>
	>(`${projectGit(projectId)}/repositories/${repositoryId}`, payload);
	return data.data;
}

export async function unlinkGitRepository(
	projectId: string,
	repositoryId: string,
): Promise<void> {
	await apiClient.instance.delete(
		`${projectGit(projectId)}/repositories/${repositoryId}`,
	);
}

/**
 * Points a repository whose connection was deleted at a connection again. It
 * must see the repository; empty `identityId` lets Paca try every connection
 * the project can use.
 */
export async function reconnectGitRepository(
	projectId: string,
	repositoryId: string,
	identityId?: string,
): Promise<GitRepository> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<GitRepository>
	>(`${projectGit(projectId)}/repositories/${repositoryId}/reconnect`, {
		...(identityId ? { identity_id: identityId } : {}),
	});
	return data.data;
}

/** Retries registering a linked repository's webhook on the platform. */
export async function registerGitWebhook(
	projectId: string,
	repositoryId: string,
): Promise<GitRepository> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<GitRepository>
	>(`${projectGit(projectId)}/repositories/${repositoryId}/webhook`);
	return data.data;
}

/** Starts granting a connection access to a repository owner's account. */
export async function beginGitAccess(
	projectId: string,
	payload: {
		provider_id: string;
		host?: string;
		owner: string;
		return_url: string;
		/** The connection to grant; required when the project has several. */
		identity_id?: string;
	},
): Promise<GitSetupStep> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<GitSetupStep>>(
		`${projectGit(projectId)}/access/begin`,
		payload,
	);
	return data.data;
}

export async function completeGitAccess(
	projectId: string,
	state: string,
	params: Record<string, string>,
): Promise<GitAccessCompletion> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<GitAccessCompletion>
	>(`${projectGit(projectId)}/access/complete`, { state, params });
	return data.data;
}

export const gitProjectProvidersQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: ["git", "projects", projectId, "providers"] as const,
		queryFn: () => listProjectGitProviders(projectId),
		staleTime: 60 * 1000,
	});

// ── A project's connections ──────────────────────────────────────────────────

/** The workspace's connections and the project's own. */
export async function listGitConnections(
	projectId: string,
): Promise<GitIdentity[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<GitIdentity[]>>(
		`${projectGit(projectId)}/connections`,
	);
	return data.data;
}

/** Starts a connection only this project can use. */
export async function beginProjectGitSetup(
	projectId: string,
	payload: BeginGitSetupPayload,
): Promise<GitSetupStep> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<GitSetupStep>>(
		`${projectGit(projectId)}/connections/setup`,
		payload,
	);
	return data.data;
}

export async function completeProjectGitSetup(
	projectId: string,
	state: string,
	params: Record<string, string>,
): Promise<GitSetupCompletion> {
	const { data } = await apiClient.instance.post<
		SuccessEnvelope<GitSetupCompletion>
	>(`${projectGit(projectId)}/connections/setup/complete`, { state, params });
	return data.data;
}

/** Removes a connection that belongs to this project. */
export async function deleteProjectGitConnection(
	projectId: string,
	id: string,
): Promise<void> {
	await apiClient.instance.delete(`${projectGit(projectId)}/connections/${id}`);
}

export const gitConnectionsKey = (projectId: string) =>
	["git", "projects", projectId, "connections"] as const;

export const gitConnectionsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: gitConnectionsKey(projectId),
		queryFn: () => listGitConnections(projectId),
	});

export const gitRepositoriesKey = (projectId: string) =>
	["git", "projects", projectId, "repositories"] as const;

export const gitRepositoriesQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: gitRepositoriesKey(projectId),
		queryFn: () => listGitRepositories(projectId),
	});

/**
 * The account or organization a repository reference points at, to pre-select
 * it on the platform's install page: "owner/name", an https URL
 * ("https://host/owner/name[.git]") or an ssh remote ("git@host:owner/name").
 * Empty when it cannot tell, in which case the platform asks the user.
 */
export function repoOwnerOf(input: string): string {
	let s = input.trim();
	if (!s) return "";
	if (s.startsWith("git@")) {
		s = s.slice(s.indexOf(":") + 1);
	} else if (s.includes("://")) {
		try {
			s = new URL(s).pathname;
		} catch {
			return "";
		}
	} else if (/^[\w.-]+\.[a-z]{2,}\//i.test(s)) {
		s = s.slice(s.indexOf("/") + 1); // "github.com/owner/name"
	}
	const parts = s.split("/").filter(Boolean);
	return parts.length >= 2 ? parts[0] : "";
}

/**
 * A browser-local note of which flow started a platform round trip, keyed by
 * its state, so the one callback page can complete the right kind (identity
 * setup, or a project's access grant). It only routes the call: the server
 * authorizes and validates every completion on its own.
 */
export type GitFlowHint =
	| { kind: "identity" }
	| { kind: "project-identity"; projectId: string }
	| { kind: "access"; projectId: string };

const FLOW_PREFIX = "paca.git.flow.";

export function rememberGitFlow(state: string | undefined, hint: GitFlowHint) {
	if (!state) return;
	try {
		window.sessionStorage.setItem(FLOW_PREFIX + state, JSON.stringify(hint));
	} catch {
		// Storage can be unavailable (private windows); the callback then
		// treats the state as an identity setup.
	}
}

export function readGitFlow(state: string): GitFlowHint {
	try {
		const raw = window.sessionStorage.getItem(FLOW_PREFIX + state);
		if (raw) {
			const v = JSON.parse(raw) as GitFlowHint;
			if (
				(v.kind === "access" || v.kind === "project-identity") &&
				typeof v.projectId === "string"
			) {
				return v;
			}
		}
	} catch {
		// fall through
	}
	return { kind: "identity" };
}

/**
 * An agent's identity is created for one project, so its App is installed
 * right after it is created. The agent tab notes which agent started the
 * setup; the callback page picks the note up once the App exists and goes
 * straight on to the platform's install page. The note only routes the call:
 * the API authorizes the install itself.
 */
const AGENT_INSTALL_KEY = "paca.git.agentInstall";

export interface AgentInstallNote {
	projectId: string;
	agentId: string;
}

export function rememberAgentInstall(note: AgentInstallNote) {
	try {
		window.sessionStorage.setItem(AGENT_INSTALL_KEY, JSON.stringify(note));
	} catch {
		// Without storage the user installs from the agent tab afterwards.
	}
}

export function forgetAgentInstall() {
	try {
		window.sessionStorage.removeItem(AGENT_INSTALL_KEY);
	} catch {
		// nothing to clean up
	}
}

/** Reads and clears the note, so one setup installs once. */
export function takeAgentInstall(): AgentInstallNote | null {
	try {
		const raw = window.sessionStorage.getItem(AGENT_INSTALL_KEY);
		window.sessionStorage.removeItem(AGENT_INSTALL_KEY);
		if (!raw) return null;
		const v = JSON.parse(raw) as Partial<AgentInstallNote>;
		return typeof v.projectId === "string" && typeof v.agentId === "string"
			? { projectId: v.projectId, agentId: v.agentId }
			: null;
	} catch {
		return null;
	}
}

export function forgetGitFlow(state: string) {
	try {
		window.sessionStorage.removeItem(FLOW_PREFIX + state);
	} catch {
		// nothing to clean up
	}
}

// ── An agent's own identity ──────────────────────────────────────────────────

const agentGit = (projectId: string, agentId: string) =>
	`/projects/${projectId}/agents/${agentId}/git/identity`;

export const agentGitIdentitiesKey = (projectId: string, agentId: string) =>
	["git", "projects", projectId, "agents", agentId, "identity"] as const;

export async function listAgentGitIdentities(
	projectId: string,
	agentId: string,
): Promise<GitIdentity[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<GitIdentity[]>>(
		agentGit(projectId, agentId),
	);
	return data.data;
}

export const agentGitIdentitiesQueryOptions = (
	projectId: string,
	agentId: string,
) =>
	queryOptions({
		queryKey: agentGitIdentitiesKey(projectId, agentId),
		queryFn: () => listAgentGitIdentities(projectId, agentId),
	});

/** Starts creating the agent's own identity (same steps as the shared one). */
export async function beginAgentGitSetup(
	projectId: string,
	agentId: string,
	payload: BeginGitSetupPayload,
): Promise<GitSetupStep> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<GitSetupStep>>(
		`${agentGit(projectId, agentId)}/setup`,
		payload,
	);
	return data.data;
}

export async function deleteAgentGitIdentity(
	projectId: string,
	agentId: string,
	providerId: string,
): Promise<void> {
	await apiClient.instance.delete(agentGit(projectId, agentId), {
		params: { provider_id: providerId },
	});
}

/** Installs the agent's own identity on one more account or organization. */
export async function beginAgentGitAccess(
	projectId: string,
	agentId: string,
	payload: { provider_id: string; owner: string; return_url: string },
): Promise<GitSetupStep> {
	const { data } = await apiClient.instance.post<SuccessEnvelope<GitSetupStep>>(
		`${agentGit(projectId, agentId)}/access/begin`,
		payload,
	);
	return data.data;
}

// ── A task's pull/merge requests and branches ────────────────────────────────

/** The state of a pull/merge request's checks; absent when it has none. */
export interface GitCISummary {
	state: "passing" | "failing" | "pending";
	total: number;
	passed: number;
	failed: number;
	pending: number;
	/** Names of failing checks (a few). */
	failing: string[];
}

export interface GitTaskChangeRequest {
	repository_id: string;
	repository: string;
	provider_id: string;
	number: string;
	title: string;
	state: "open" | "closed" | "merged";
	draft: boolean;
	head_ref: string;
	base_ref: string;
	author: string;
	/** The request's page on the platform; empty when it is not known. */
	url: string;
	merged_at: string | null;
	updated_at: string;
	ci: GitCISummary | null;
}

export interface GitTaskBranch {
	repository_id: string;
	repository: string;
	provider_id: string;
	branch: string;
}

export interface GitTaskLinks {
	change_requests: GitTaskChangeRequest[];
	branches: GitTaskBranch[];
}

export const taskGitKey = (projectId: string, taskId: string) =>
	["git", "projects", projectId, "tasks", taskId] as const;

export async function getTaskGit(
	projectId: string,
	taskId: string,
): Promise<GitTaskLinks> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<GitTaskLinks>>(
		`/projects/${projectId}/tasks/${taskId}/git`,
	);
	return data.data;
}

const taskGit = (projectId: string, taskId: string) =>
	`/projects/${projectId}/tasks/${taskId}/git`;

export async function linkTaskChangeRequest(
	projectId: string,
	taskId: string,
	payload: { repository_id: string; number: string },
): Promise<void> {
	await apiClient.instance.post(
		`${taskGit(projectId, taskId)}/change-requests`,
		payload,
	);
}

export async function unlinkTaskChangeRequest(
	projectId: string,
	taskId: string,
	repositoryId: string,
	number: string,
): Promise<void> {
	await apiClient.instance.delete(
		`${taskGit(projectId, taskId)}/change-requests/${encodeURIComponent(number)}`,
		{ params: { repository_id: repositoryId } },
	);
}

export async function linkTaskBranch(
	projectId: string,
	taskId: string,
	payload: { repository_id: string; branch: string },
): Promise<void> {
	await apiClient.instance.post(
		`${taskGit(projectId, taskId)}/branches`,
		payload,
	);
}

/** Creates the branch on the platform and links it to the task. */
export async function createTaskBranch(
	projectId: string,
	taskId: string,
	payload: { repository_id: string; branch: string; from?: string },
): Promise<void> {
	await apiClient.instance.post(
		`${taskGit(projectId, taskId)}/branches/new`,
		payload,
	);
}

export async function unlinkTaskBranch(
	projectId: string,
	taskId: string,
	repositoryId: string,
	branch: string,
): Promise<void> {
	await apiClient.instance.delete(`${taskGit(projectId, taskId)}/branches`, {
		params: { repository_id: repositoryId, branch },
	});
}

export const taskGitQueryOptions = (projectId: string, taskId: string) =>
	queryOptions({
		queryKey: taskGitKey(projectId, taskId),
		queryFn: () => getTaskGit(projectId, taskId),
		staleTime: 30 * 1000,
	});

// ── Workflow: who may start agents, triggers and runs ────────────────────────

/** Repository permission levels, lowest first, as the API names them. */
export const GIT_PERMISSIONS = [
	"read",
	"triage",
	"write",
	"maintain",
	"admin",
] as const;
export type GitPermission = (typeof GIT_PERMISSIONS)[number];

export interface GitProjectSettings {
	default_responder_agent_id: string | null;
	paused: boolean;
	min_actor_permission: GitPermission;
}

export type GitTriggerKind =
	| "mention"
	| "own_cr_feedback"
	| "ci_failure"
	| "review_request"
	| "assignment"
	| "label"
	| "auto_review";

/** Filters of the auto_review trigger; an empty list matches everything. */
export interface GitAutoReviewConfig {
	branches?: string[];
	authors?: string[];
}

export interface GitTrigger {
	kind: GitTriggerKind;
	enabled: boolean;
	/** False while the agent still has the trigger's default. */
	explicit: boolean;
	/** Whether any installed provider can deliver this trigger. */
	supported: boolean;
	config: GitAutoReviewConfig;
}

export const gitSettingsKey = (projectId: string) =>
	["git", "projects", projectId, "settings"] as const;

export const agentGitTriggersKey = (projectId: string, agentId: string) =>
	["git", "projects", projectId, "agents", agentId, "triggers"] as const;

export async function getGitSettings(
	projectId: string,
): Promise<GitProjectSettings> {
	const { data } = await apiClient.instance.get<
		SuccessEnvelope<GitProjectSettings>
	>(`${projectGit(projectId)}/settings`);
	return data.data;
}

export async function updateGitSettings(
	projectId: string,
	settings: GitProjectSettings,
): Promise<GitProjectSettings> {
	const { data } = await apiClient.instance.put<
		SuccessEnvelope<GitProjectSettings>
	>(`${projectGit(projectId)}/settings`, settings);
	return data.data;
}

export const gitSettingsQueryOptions = (projectId: string) =>
	queryOptions({
		queryKey: gitSettingsKey(projectId),
		queryFn: () => getGitSettings(projectId),
	});

export async function listAgentGitTriggers(
	projectId: string,
	agentId: string,
): Promise<GitTrigger[]> {
	const { data } = await apiClient.instance.get<SuccessEnvelope<GitTrigger[]>>(
		`/projects/${projectId}/agents/${agentId}/git/triggers`,
	);
	return data.data;
}

export async function setAgentGitTrigger(
	projectId: string,
	agentId: string,
	kind: GitTriggerKind,
	payload: { enabled: boolean; config?: GitAutoReviewConfig },
): Promise<GitTrigger[]> {
	const { data } = await apiClient.instance.put<SuccessEnvelope<GitTrigger[]>>(
		`/projects/${projectId}/agents/${agentId}/git/triggers/${kind}`,
		payload,
	);
	return data.data;
}

export const agentGitTriggersQueryOptions = (
	projectId: string,
	agentId: string,
) =>
	queryOptions({
		queryKey: agentGitTriggersKey(projectId, agentId),
		queryFn: () => listAgentGitTriggers(projectId, agentId),
	});

const SETUP_ERROR_KEYS: Partial<Record<ApiErrorCode, string>> = {
	GIT_SETUP_INVALID: "setupInvalid",
	GIT_IDENTITY_EXISTS: "identityExists",
	GIT_PUBLIC_URL_REQUIRED: "publicUrlRequired",
	GIT_PROVIDER_UNAVAILABLE: "providerUnavailable",
	GIT_PROVIDER_REJECTED: "providerRejected",
	GIT_NOT_FOUND: "notFound",
	GIT_ACCESS_NOT_GRANTED: "accessNotGranted",
	GIT_REPOSITORY_NOT_VISIBLE: "notVisible",
	GIT_NO_IDENTITY: "noIdentity",
	GIT_ALREADY_LINKED: "alreadyLinked",
	GIT_IDENTITY_REQUIRED: "identityRequired",
	GIT_DESCRIPTION_TOO_LONG: "descriptionTooLong",
};

/** The API's error code for a failed call, as a plain string (null when absent). */
export function getApiErrorCodeSafe(err: unknown): string | null {
	const code = (err as { response?: { data?: { error_code?: unknown } } })
		?.response?.data?.error_code;
	return typeof code === "string" ? code : null;
}

/** Key under `git.errors.*` (admin namespace) for a failed Git call. */
export function gitErrorKey(err: unknown): string {
	const code = getApiErrorCode(err);
	return (code && SETUP_ERROR_KEYS[code]) || "generic";
}

const MAX_PROVIDER_MESSAGE = 500;

/**
 * What the platform itself said about a failed Git call (the API's
 * `error_details.provider_message`), for showing under the generic message.
 * It is untrusted free text: render it as text only, never as HTML. Null when
 * the platform said nothing.
 */
export function gitProviderMessage(err: unknown): string | null {
	const msg = (
		err as {
			response?: { data?: { error_details?: { provider_message?: unknown } } };
		}
	)?.response?.data?.error_details?.provider_message;
	if (typeof msg !== "string") return null;
	const text = msg.trim();
	if (!text) return null;
	return text.length > MAX_PROVIDER_MESSAGE
		? `${text.slice(0, MAX_PROVIDER_MESSAGE)}…`
		: text;
}
