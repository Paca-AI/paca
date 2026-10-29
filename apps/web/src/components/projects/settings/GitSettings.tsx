import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { GitBranch, Link2, Loader2, Lock, Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { CopyChip, EmptyState, Section } from "@/components/git/git-ui";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { listAgents } from "@/lib/agent-api";
import {
	beginGitAccess,
	followGitStep,
	GIT_PERMISSIONS,
	type GitIdentity,
	type GitPermission,
	type GitProjectSettings,
	type GitProvider,
	type GitRepository,
	getApiErrorCodeSafe,
	gitConnectionsKey,
	gitConnectionsQueryOptions,
	gitErrorKey,
	gitProjectProvidersQueryOptions,
	gitProviderMessage,
	gitRepositoriesKey,
	gitRepositoriesQueryOptions,
	gitSettingsKey,
	gitSettingsQueryOptions,
	kindNeedsAccessGrant,
	linkGitRepository,
	reconnectGitRepository,
	registerGitWebhook,
	rememberGitFlow,
	repoOwnerOf,
	unlinkGitRepository,
	updateGitRepository,
	updateGitSettings,
} from "@/lib/git-api";
import { GitConnections } from "./GitConnections";

interface GitSettingsProps {
	projectId: string;
	/** git:write: may link and unlink repositories and change the automation. */
	canWrite: boolean;
}

const pendingKey = (projectId: string) => `paca.git.pendingLink.${projectId}`;

function readPending(
	projectId: string,
): { input: string; providerId: string } | null {
	try {
		const raw = window.sessionStorage.getItem(pendingKey(projectId));
		if (!raw) return null;
		window.sessionStorage.removeItem(pendingKey(projectId));
		const v = JSON.parse(raw) as { input?: unknown; providerId?: unknown };
		return typeof v.input === "string" && typeof v.providerId === "string"
			? { input: v.input, providerId: v.providerId }
			: null;
	} catch {
		return null;
	}
}

function writePending(projectId: string, input: string, providerId: string) {
	try {
		window.sessionStorage.setItem(
			pendingKey(projectId),
			JSON.stringify({ input, providerId }),
		);
	} catch {
		// Without storage the user just types the repository again.
	}
}

/**
 * The project's Git settings, in the order a user does things: link
 * repositories (the form is open while there are none), pick the agent that
 * answers, then, folded away, the rarely changed limits and the words to type
 * on the platform. Nothing but the next step is in front of the user.
 */
export function GitSettings({ projectId, canWrite }: GitSettingsProps) {
	const { t } = useTranslation("projects");
	const providers = useQuery(gitProjectProvidersQueryOptions(projectId));
	const repos = useQuery(gitRepositoriesQueryOptions(projectId));
	const connections = useQuery(gitConnectionsQueryOptions(projectId));
	const settings = useQuery(gitSettingsQueryOptions(projectId));
	const agents = useQuery({
		queryKey: ["git", "projects", projectId, "responder-candidates"] as const,
		queryFn: () => listAgents(projectId),
	});

	if (providers.isLoading || repos.isLoading) {
		return <Skeleton className="h-60 w-full" />;
	}
	const linked: GitRepository[] = repos.data ?? [];
	const list = providers.data ?? [];
	const agentList = agents.data?.items ?? [];
	const handle = linked.find((r) => r.identity_handle)?.identity_handle ?? "";
	const bot = handle ? `@${handle}` : "@bot";

	return (
		<div className="flex max-w-3xl flex-col gap-8">
			<div>
				<h2 className="flex items-center gap-2 text-lg font-semibold">
					<GitBranch className="size-4 text-primary" />
					{t("project.git.title")}
				</h2>
				<p className="mt-1 text-sm text-muted-foreground">
					{t("project.git.description")}
				</p>
			</div>

			<GitConnections projectId={projectId} providers={list} />

			<Repositories
				projectId={projectId}
				canWrite={canWrite}
				providers={list}
				connections={connections.data ?? []}
				linked={linked}
			/>

			<Automation
				projectId={projectId}
				canWrite={canWrite}
				locked={linked.length === 0}
				settings={settings.data}
				agentList={agentList}
			/>

			{linked.length > 0 ? (
				<HowToUse bot={bot} agentHandle={agentList[0]?.handle ?? "agent"} />
			) : null}
		</div>
	);
}

// ── Repositories ─────────────────────────────────────────────────────────────

function Repositories({
	projectId,
	canWrite,
	providers,
	connections,
	linked,
}: {
	projectId: string;
	canWrite: boolean;
	providers: GitProvider[];
	connections: GitIdentity[];
	linked: GitRepository[];
}) {
	const { t } = useTranslation("projects");
	// Linking goes through a connection the project can use, so git:write is enough.
	const canLink = canWrite;
	const queryClient = useQueryClient();
	const [input, setInput] = useState("");
	const [description, setDescription] = useState("");
	const [providerId, setProviderId] = useState("");
	const [identityId, setIdentityId] = useState("");
	const [errorKey, setErrorKey] = useState<string | null>(null);
	const [errorDetail, setErrorDetail] = useState<string | null>(null);
	const [needsAccess, setNeedsAccess] = useState(false);
	const [resumed, setResumed] = useState<string | null>(null);
	const [confirming, setConfirming] = useState<string | null>(null);
	const [formOpen, setFormOpen] = useState(false);

	const provider: GitProvider | undefined =
		providers.find((p) => p.id === providerId) ?? providers[0];
	// The connections this platform can be reached with, and the ones that need
	// to be installed on each owner's account before they can see a repository.
	const options = connections.filter((c) => c.provider_id === provider?.id);
	const grantable = provider
		? options.filter((c) => kindNeedsAccessGrant(provider, c.kind))
		: [];
	const chosen = options.some((c) => c.id === identityId) ? identityId : "";
	const accessTarget =
		chosen && grantable.some((c) => c.id === chosen)
			? chosen
			: grantable.length === 1
				? grantable[0].id
				: "";

	// Back from the platform: restore what the user was linking.
	useEffect(() => {
		const pending = readPending(projectId);
		if (pending) {
			setInput(pending.input);
			setProviderId(pending.providerId);
			setResumed(pending.input);
			setFormOpen(true);
		}
	}, [projectId]);

	const link = useMutation({
		mutationFn: () =>
			linkGitRepository(projectId, {
				provider_id: provider?.id ?? "",
				path_or_url: input.trim(),
				...(chosen ? { identity_id: chosen } : {}),
				...(description.trim() ? { description: description.trim() } : {}),
			}),
		onSuccess: () => {
			setInput("");
			setDescription("");
			setErrorKey(null);
			setErrorDetail(null);
			setNeedsAccess(false);
			setResumed(null);
			setFormOpen(false);
			void queryClient.invalidateQueries({
				queryKey: gitRepositoriesKey(projectId),
			});
		},
		onError: (err: unknown) => {
			setResumed(null);
			if (getApiErrorCodeSafe(err) === "GIT_ACCESS_NOT_GRANTED") {
				setNeedsAccess(true);
				setErrorKey(null);
				setErrorDetail(null);
				return;
			}
			setNeedsAccess(false);
			setErrorKey(gitErrorKey(err));
			setErrorDetail(gitProviderMessage(err));
		},
	});

	const grant = useMutation({
		mutationFn: () =>
			beginGitAccess(projectId, {
				provider_id: provider?.id ?? "",
				owner: repoOwnerOf(input),
				...(accessTarget ? { identity_id: accessTarget } : {}),
				return_url: `${window.location.origin}/projects/${projectId}/settings#git`,
			}),
		onSuccess: (step) => {
			writePending(projectId, input, provider?.id ?? "");
			rememberGitFlow(step.state, { kind: "access", projectId });
			// Leaves the page; the platform returns to /git/setup/callback.
			if (!followGitStep(step)) setErrorKey("generic");
		},
		onError: (err: unknown) => setErrorKey(gitErrorKey(err)),
	});

	const unlink = useMutation({
		mutationFn: (id: string) => unlinkGitRepository(projectId, id),
		onSuccess: () => {
			setConfirming(null);
			void queryClient.invalidateQueries({
				queryKey: gitRepositoriesKey(projectId),
			});
		},
		onError: (err: unknown) => {
			setConfirming(null);
			setErrorKey(gitErrorKey(err));
		},
	});

	const owner = repoOwnerOf(input);
	const showForm =
		canLink &&
		providers.length > 0 &&
		connections.length > 0 &&
		(formOpen || linked.length === 0);

	const form = showForm ? (
		<form
			className="flex flex-col gap-3 rounded-xl border border-border/60 bg-card p-4"
			onSubmit={(e) => {
				e.preventDefault();
				setErrorKey(null);
				setNeedsAccess(false);
				link.mutate();
			}}
		>
			{providers.length > 1 ? (
				<div className="flex flex-col gap-1.5">
					<Label htmlFor="git-link-provider">
						{t("project.git.link.platform")}
					</Label>
					<select
						id="git-link-provider"
						className="h-9 rounded-md border border-input bg-background px-3 text-sm"
						value={provider?.id ?? ""}
						onChange={(e) => setProviderId(e.target.value)}
					>
						{providers.map((p) => (
							<option key={p.id} value={p.id}>
								{p.display_name}
							</option>
						))}
					</select>
				</div>
			) : null}
			{options.length === 0 ? (
				<p role="status" className="text-sm text-muted-foreground">
					{t("project.git.link.noConnection", {
						provider: provider?.display_name ?? "",
					})}
				</p>
			) : options.length > 1 ? (
				<div className="flex flex-col gap-1.5">
					<Label htmlFor="git-link-connection">
						{t("project.git.link.connection")}
					</Label>
					<select
						id="git-link-connection"
						className="h-9 rounded-md border border-input bg-background px-3 text-sm"
						value={chosen}
						onChange={(e) => setIdentityId(e.target.value)}
					>
						<option value="">{t("project.git.link.anyConnection")}</option>
						{options.map((c) => (
							<option key={c.id} value={c.id}>
								{c.display_name || c.handle} ({c.host},{" "}
								{t(`project.git.connections.scope.${c.scope}` as never)})
							</option>
						))}
					</select>
					<p className="text-xs text-muted-foreground">
						{t("project.git.link.connectionHelp")}
					</p>
				</div>
			) : null}
			<div className="flex flex-col gap-1.5">
				<Label htmlFor="git-link-input">
					{provider?.terms?.repository ?? t("project.git.link.repository")}
				</Label>
				<Input
					id="git-link-input"
					value={input}
					placeholder={t("project.git.link.placeholder")}
					autoComplete="off"
					onChange={(e) => setInput(e.target.value)}
				/>
				<p className="text-xs text-muted-foreground">
					{t("project.git.link.help")}
				</p>
			</div>

			<div className="flex flex-col gap-1.5">
				<Label htmlFor="git-link-description">
					{t("project.git.link.description")}
				</Label>
				<Textarea
					id="git-link-description"
					value={description}
					rows={2}
					maxLength={1000}
					placeholder={t("project.git.link.descriptionPlaceholder")}
					onChange={(e) => setDescription(e.target.value)}
				/>
				<p className="text-xs text-muted-foreground">
					{t("project.git.link.descriptionHelp")}
				</p>
			</div>

			{resumed ? (
				<p role="status" className="text-sm text-muted-foreground">
					{t("project.git.resumed", { repository: resumed })}
				</p>
			) : null}

			{needsAccess ? (
				<div
					role="alert"
					className="flex flex-col gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 px-3 py-3 text-sm"
				>
					<p className="font-medium">{t("project.git.access.title")}</p>
					<p className="text-muted-foreground">
						{owner
							? t("project.git.access.description", {
									provider: provider?.display_name ?? "",
									owner,
								})
							: t("project.git.access.descriptionNoOwner", {
									provider: provider?.display_name ?? "",
								})}
					</p>
					{grantable.length > 1 && !chosen ? (
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="git-access-connection">
								{t("project.git.access.connection")}
							</Label>
							<select
								id="git-access-connection"
								className="h-9 rounded-md border border-input bg-background px-3 text-sm"
								value={accessTarget}
								onChange={(e) => setIdentityId(e.target.value)}
							>
								<option value="" disabled>
									{t("project.git.access.choose")}
								</option>
								{grantable.map((c) => (
									<option key={c.id} value={c.id}>
										{c.display_name || c.handle}
									</option>
								))}
							</select>
						</div>
					) : null}
					<div>
						<Button
							type="button"
							size="sm"
							disabled={grant.isPending || !accessTarget}
							onClick={() => grant.mutate()}
						>
							{grant.isPending ? (
								<Loader2 className="size-4 animate-spin" />
							) : null}
							{owner
								? t("project.git.access.button", { owner })
								: t("project.git.access.buttonNoOwner")}
						</Button>
					</div>
				</div>
			) : null}

			{errorKey ? (
				<div role="alert" className="flex flex-col gap-1 text-sm">
					<p className="text-destructive">
						{t(`project.git.errors.${errorKey}` as never)}
					</p>
					<ProviderDetail message={errorDetail} />
				</div>
			) : null}

			<div className="flex gap-2">
				<Button
					type="submit"
					disabled={link.isPending || !input.trim() || !provider}
				>
					{link.isPending ? (
						<Loader2 className="size-4 animate-spin" />
					) : (
						<Link2 className="size-4" />
					)}
					{t("project.git.link.button")}
				</Button>
				{linked.length > 0 ? (
					<Button
						type="button"
						variant="ghost"
						onClick={() => {
							setFormOpen(false);
							setNeedsAccess(false);
							setErrorKey(null);
						}}
					>
						{t("project.git.link.cancel")}
					</Button>
				) : null}
			</div>
		</form>
	) : null;

	return (
		<Section
			id="git-repositories"
			title={t("project.git.repositories.title")}
			description={t("project.git.repositories.description")}
			action={
				canLink && providers.length > 0 && linked.length > 0 && !formOpen ? (
					<Button
						type="button"
						size="sm"
						variant="outline"
						onClick={() => setFormOpen(true)}
					>
						<Plus className="size-4" />
						{t("project.git.link.open")}
					</Button>
				) : undefined
			}
		>
			{linked.length === 0 ? (
				<EmptyState
					icon={GitBranch}
					title={t("project.git.empty.title")}
					description={
						providers.length === 0
							? t("project.git.noProviders")
							: connections.length === 0
								? t("project.git.empty.noConnection")
								: canLink
									? t("project.git.empty.description")
									: t("project.git.empty.readOnly")
					}
				/>
			) : (
				<ul className="flex flex-col divide-y divide-border/60 rounded-xl border border-border/60">
					{linked.map((r) => (
						<li
							key={r.id}
							className="flex flex-wrap items-center justify-between gap-3 px-4 py-3"
						>
							<div className="min-w-0">
								<p className="truncate text-sm font-medium">{r.full_path}</p>
								<p className="mt-0.5 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
									<span>{r.host}</span>
									{r.default_branch ? (
										<Badge variant="secondary">{r.default_branch}</Badge>
									) : null}
									{r.identity_handle ? (
										<span className="flex items-center gap-1">
											{t("project.git.repositories.mention")}
											<CopyChip value={`@${r.identity_handle}`} />
										</span>
									) : null}
								</p>
								<RepositoryDescription
									projectId={projectId}
									repository={r}
									canWrite={canWrite}
								/>
							</div>
							{!r.identity_id ? (
								<ReconnectNotice
									projectId={projectId}
									repository={r}
									canWrite={canWrite}
									options={connections.filter(
										(c) =>
											c.provider_id === r.provider_id &&
											c.host.toLowerCase() === r.host.toLowerCase(),
									)}
								/>
							) : null}
							{r.webhook_url ? (
								<WebhookNotice
									projectId={projectId}
									repository={r}
									canWrite={canWrite}
								/>
							) : null}
							{canWrite ? (
								confirming === r.id ? (
									<div className="flex gap-2">
										<Button
											type="button"
											size="sm"
											variant="destructive"
											disabled={unlink.isPending}
											onClick={() => unlink.mutate(r.id)}
										>
											{t("project.git.unlink.confirm")}
										</Button>
										<Button
											type="button"
											size="sm"
											variant="outline"
											onClick={() => setConfirming(null)}
										>
											{t("project.git.unlink.cancel")}
										</Button>
									</div>
								) : (
									<Button
										type="button"
										size="sm"
										variant="ghost"
										onClick={() => setConfirming(r.id)}
									>
										<Trash2 className="size-4" />
										{t("project.git.unlink.button")}
									</Button>
								)
							) : null}
						</li>
					))}
				</ul>
			)}
			{form}
		</Section>
	);
}

/**
 * What a repository is for, shown under its name; with git:write it can be
 * edited in place. Agents read it to pick the right repository.
 */
function RepositoryDescription({
	projectId,
	repository,
	canWrite,
}: {
	projectId: string;
	repository: GitRepository;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const [editing, setEditing] = useState(false);
	const [value, setValue] = useState("");
	const [errorKey, setErrorKey] = useState<string | null>(null);
	const current = repository.description ?? "";

	const save = useMutation({
		mutationFn: () =>
			updateGitRepository(projectId, repository.id, {
				description: value.trim(),
			}),
		onSuccess: () => {
			setEditing(false);
			setErrorKey(null);
			void queryClient.invalidateQueries({
				queryKey: gitRepositoriesKey(projectId),
			});
		},
		onError: (err: unknown) => setErrorKey(gitErrorKey(err)),
	});

	if (editing) {
		return (
			<form
				className="mt-2 flex flex-col gap-2"
				onSubmit={(e) => {
					e.preventDefault();
					setErrorKey(null);
					save.mutate();
				}}
			>
				<Textarea
					value={value}
					rows={2}
					maxLength={1000}
					autoFocus
					aria-label={t("project.git.repositories.descriptionLabel")}
					placeholder={t("project.git.link.descriptionPlaceholder")}
					onChange={(e) => setValue(e.target.value)}
				/>
				{errorKey ? (
					<p role="alert" className="text-sm text-destructive">
						{t(`project.git.errors.${errorKey}` as never)}
					</p>
				) : null}
				<div className="flex gap-2">
					<Button type="submit" size="sm" disabled={save.isPending}>
						{save.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : null}
						{t("project.git.repositories.saveDescription")}
					</Button>
					<Button
						type="button"
						size="sm"
						variant="outline"
						onClick={() => {
							setEditing(false);
							setErrorKey(null);
						}}
					>
						{t("project.git.repositories.cancelDescription")}
					</Button>
				</div>
			</form>
		);
	}

	return (
		<div className="mt-1 flex flex-wrap items-start gap-2 text-xs">
			{current ? (
				<p className="min-w-0 whitespace-pre-line break-words text-muted-foreground">
					{current}
				</p>
			) : canWrite ? null : (
				<p className="italic text-muted-foreground">
					{t("project.git.repositories.noDescription")}
				</p>
			)}
			{canWrite ? (
				<Button
					type="button"
					size="sm"
					variant="link"
					className="h-auto p-0 text-xs"
					onClick={() => {
						setValue(current);
						setEditing(true);
					}}
				>
					{current
						? t("project.git.repositories.editDescription")
						: t("project.git.repositories.addDescription")}
				</Button>
			) : null}
		</div>
	);
}

/**
 * A repository whose connection was deleted: it stays linked but has no
 * credential, so nothing can use it. Offers to point it at another connection
 * that can see it, the project's only one at once or the user's choice among
 * several.
 */
function ReconnectNotice({
	projectId,
	repository,
	canWrite,
	options,
}: {
	projectId: string;
	repository: GitRepository;
	canWrite: boolean;
	options: GitIdentity[];
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const [choice, setChoice] = useState("");
	const [errorKey, setErrorKey] = useState<string | null>(null);
	const [errorDetail, setErrorDetail] = useState<string | null>(null);
	const chosen = options.some((c) => c.id === choice) ? choice : "";
	const reconnect = useMutation({
		mutationFn: () =>
			reconnectGitRepository(projectId, repository.id, chosen || undefined),
		onSuccess: () => {
			setErrorKey(null);
			setErrorDetail(null);
			void queryClient.invalidateQueries({
				queryKey: gitRepositoriesKey(projectId),
			});
			void queryClient.invalidateQueries({
				queryKey: gitConnectionsKey(projectId),
			});
		},
		onError: (err: unknown) => {
			setErrorKey(gitErrorKey(err));
			setErrorDetail(gitProviderMessage(err));
		},
	});
	return (
		<div
			role="status"
			className="flex basis-full flex-col gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 px-3 py-3 text-sm"
		>
			<p className="font-medium">{t("project.git.reconnect.title")}</p>
			<p className="text-muted-foreground">
				{options.length === 0
					? t("project.git.reconnect.noConnection")
					: t("project.git.reconnect.description")}
			</p>
			{canWrite && options.length > 1 ? (
				<select
					aria-label={t("project.git.link.connection")}
					className="h-9 max-w-xs rounded-md border border-input bg-background px-3 text-sm"
					value={chosen}
					onChange={(e) => setChoice(e.target.value)}
				>
					<option value="">{t("project.git.link.anyConnection")}</option>
					{options.map((c) => (
						<option key={c.id} value={c.id}>
							{c.display_name || c.handle}
						</option>
					))}
				</select>
			) : null}
			{errorKey ? (
				<div role="alert" className="flex flex-col gap-1">
					<p className="text-destructive">
						{t(`project.git.errors.${errorKey}` as never)}
					</p>
					<ProviderDetail message={errorDetail} />
				</div>
			) : null}
			{canWrite && options.length > 0 ? (
				<div>
					<Button
						type="button"
						size="sm"
						variant="outline"
						disabled={reconnect.isPending}
						onClick={() => reconnect.mutate()}
					>
						{reconnect.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : null}
						{t("project.git.reconnect.button")}
					</Button>
				</div>
			) : null}
		</div>
	);
}

/**
 * What the platform itself said about a failure, under the generic message.
 * Untrusted free text from a plugin: rendered as plain text only.
 */
function ProviderDetail({ message }: { message: string | null }) {
	const { t } = useTranslation("projects");
	if (!message) return null;
	return (
		<p className="break-words text-xs text-muted-foreground">
			{t("project.git.errors.providerDetail", { message })}
		</p>
	);
}

/**
 * A repository whose platform has not been told where to send events: nothing
 * reaches Paca from it until a webhook exists. Paca registers it itself when
 * the connection may; otherwise this says what is missing and offers a retry
 * for when the token has since been given the right.
 */
function WebhookNotice({
	projectId,
	repository,
	canWrite,
}: {
	projectId: string;
	repository: GitRepository;
	canWrite: boolean;
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const [failed, setFailed] = useState(false);
	const [detail, setDetail] = useState<string | null>(null);
	// Nobody has tried yet (a repository carried over from an earlier plugin
	// version): nothing failed, so there is no cause to guess at.
	const neverTried = repository.webhook_attempted === false && !failed;
	const retry = useMutation({
		mutationFn: () => registerGitWebhook(projectId, repository.id),
		onSuccess: () => {
			setFailed(false);
			setDetail(null);
			void queryClient.invalidateQueries({
				queryKey: gitRepositoriesKey(projectId),
			});
		},
		onError: (err: unknown) => {
			setFailed(true);
			setDetail(gitProviderMessage(err));
		},
	});
	return (
		<div
			role="status"
			className="flex basis-full flex-col gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 px-3 py-3 text-sm"
		>
			<p className="font-medium">{t("project.git.webhook.title")}</p>
			<p className="text-muted-foreground">
				{t(
					neverTried
						? "project.git.webhook.descriptionNotTried"
						: "project.git.webhook.description",
				)}
			</p>
			{/* The usual cause, until the platform has said otherwise. */}
			{neverTried || (failed && detail) ? null : (
				<p className="text-muted-foreground">
					{t("project.git.webhook.permissionHint")}
				</p>
			)}
			{failed ? (
				<div role="alert" className="flex flex-col gap-1">
					<p className="text-destructive">{t("project.git.webhook.failed")}</p>
					<ProviderDetail message={detail} />
				</div>
			) : null}
			{canWrite ? (
				<div>
					<Button
						type="button"
						size="sm"
						variant="outline"
						disabled={retry.isPending}
						onClick={() => retry.mutate()}
					>
						{retry.isPending ? (
							<Loader2 className="size-4 animate-spin" />
						) : null}
						{t(
							neverTried
								? "project.git.webhook.register"
								: "project.git.webhook.retry",
						)}
					</Button>
				</div>
			) : null}
		</div>
	);
}

// ── Automation ───────────────────────────────────────────────────────────────

function Automation({
	projectId,
	canWrite,
	locked,
	settings,
	agentList,
}: {
	projectId: string;
	canWrite: boolean;
	locked: boolean;
	settings: GitProjectSettings | undefined;
	agentList: { id: string; name: string }[];
}) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const [draft, setDraft] = useState<GitProjectSettings | null>(null);
	const [errorKey, setErrorKey] = useState<string | null>(null);
	const [saved, setSaved] = useState(false);

	useEffect(() => {
		if (settings) setDraft(settings);
	}, [settings]);

	const save = useMutation({
		mutationFn: (next: GitProjectSettings) =>
			updateGitSettings(projectId, next),
		onSuccess: (next) => {
			queryClient.setQueryData(gitSettingsKey(projectId), next);
			setDraft(next);
			setErrorKey(null);
			setSaved(true);
		},
		onError: (err: unknown) => {
			setSaved(false);
			setErrorKey(gitErrorKey(err));
		},
	});

	if (!draft) return <Skeleton className="h-32 w-full" />;
	const disabled = locked || !canWrite;
	const dirty = JSON.stringify(draft) !== JSON.stringify(settings ?? draft);
	const change = (patch: Partial<GitProjectSettings>) => {
		setSaved(false);
		setDraft({ ...draft, ...patch });
	};

	return (
		<Section
			id="git-automation"
			title={t("project.git.automation.title")}
			description={t("project.git.automation.description")}
		>
			{locked ? (
				<p className="flex items-center gap-2 rounded-lg bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
					<Lock className="size-4 shrink-0" aria-hidden="true" />
					{t("project.git.automation.locked")}
				</p>
			) : null}
			<form
				className="flex flex-col gap-5 rounded-xl border border-border/60 bg-card p-4"
				onSubmit={(e) => {
					e.preventDefault();
					save.mutate(draft);
				}}
			>
				<div className="flex flex-col gap-1.5">
					<Label htmlFor="git-responder">
						{t("project.git.automation.responder.label")}
					</Label>
					<select
						id="git-responder"
						className="h-9 rounded-md border border-input bg-background px-3 text-sm disabled:opacity-60"
						disabled={disabled}
						value={draft.default_responder_agent_id ?? ""}
						onChange={(e) =>
							change({ default_responder_agent_id: e.target.value || null })
						}
					>
						<option value="">
							{t("project.git.automation.responder.none")}
						</option>
						{agentList.map((a) => (
							<option key={a.id} value={a.id}>
								{a.name}
							</option>
						))}
					</select>
					<p className="text-xs text-muted-foreground">
						{t("project.git.automation.responder.help")}
					</p>
					{!locked && !draft.default_responder_agent_id ? (
						<p
							role="status"
							className="text-xs font-medium text-amber-600 dark:text-amber-400"
						>
							{t("project.git.automation.responder.warning")}
						</p>
					) : null}
				</div>

				<details className="group rounded-lg border border-border/60 px-3 py-2">
					<summary className="cursor-pointer text-sm font-medium">
						{t("project.git.automation.advanced")}
					</summary>
					<div className="mt-4 flex flex-col gap-5">
						<div className="flex flex-col gap-1.5">
							<Label htmlFor="git-min-permission">
								{t("project.git.automation.minPermission.label")}
							</Label>
							<select
								id="git-min-permission"
								className="h-9 rounded-md border border-input bg-background px-3 text-sm disabled:opacity-60"
								disabled={disabled}
								value={draft.min_actor_permission}
								onChange={(e) =>
									change({
										min_actor_permission: e.target.value as GitPermission,
									})
								}
							>
								{GIT_PERMISSIONS.map((p) => (
									<option key={p} value={p}>
										{t(`project.git.automation.permission.${p}` as never)}
									</option>
								))}
							</select>
							<p className="text-xs text-muted-foreground">
								{t("project.git.automation.minPermission.help")}
							</p>
						</div>

						<div className="flex items-center justify-between gap-3">
							<div>
								<Label htmlFor="git-paused">
									{t("project.git.automation.paused.label")}
								</Label>
								<p className="text-xs text-muted-foreground">
									{t("project.git.automation.paused.help")}
								</p>
							</div>
							<Switch
								id="git-paused"
								checked={draft.paused}
								disabled={disabled}
								onCheckedChange={(v) => change({ paused: v })}
							/>
						</div>
					</div>
				</details>

				{errorKey ? (
					<p role="alert" className="text-sm text-destructive">
						{t(`project.git.errors.${errorKey}` as never)}
					</p>
				) : null}
				{saved ? (
					<p role="status" className="text-sm text-muted-foreground">
						{t("project.git.automation.saved")}
					</p>
				) : null}

				{!disabled ? (
					<div>
						<Button type="submit" disabled={!dirty || save.isPending}>
							{save.isPending ? (
								<Loader2 className="size-4 animate-spin" />
							) : null}
							{t("project.git.automation.save")}
						</Button>
					</div>
				) : null}
			</form>
		</Section>
	);
}

// ── How to use it ────────────────────────────────────────────────────────────

function HowToUse({ bot, agentHandle }: { bot: string; agentHandle: string }) {
	const { t } = useTranslation("projects");
	const items = [
		{
			id: "mention",
			title: t("project.git.usage.mention.title"),
			description: t("project.git.usage.mention.description"),
			example: `${bot} review this pull request`,
		},
		{
			id: "pick",
			title: t("project.git.usage.pick.title"),
			description: t("project.git.usage.pick.description"),
			example: `${bot} agent:${agentHandle} fix the failing test`,
		},
		{
			id: "label",
			title: t("project.git.usage.label.title"),
			description: t("project.git.usage.label.description"),
			example: "paca",
		},
	];
	return (
		<details className="group rounded-xl border border-border/60 px-4 py-3">
			<summary className="cursor-pointer text-sm font-semibold">
				{t("project.git.usage.title")}
			</summary>
			<div className="mt-3">
				<p className="mb-3 text-sm text-muted-foreground">
					{t("project.git.usage.description")}
				</p>
				<ul className="flex flex-col divide-y divide-border/60">
					{items.map((i) => (
						<li
							key={i.id}
							className="flex flex-wrap items-center justify-between gap-3 py-3 first:pt-0 last:pb-0"
						>
							<div className="min-w-0 flex-1">
								<p className="text-sm font-medium">{i.title}</p>
								<p className="text-sm text-muted-foreground">{i.description}</p>
							</div>
							<CopyChip value={i.example} />
						</li>
					))}
				</ul>
				<p className="mt-4 border-t border-border/60 pt-3 text-sm text-muted-foreground">
					{t("project.git.usage.automatic")}
				</p>
			</div>
		</details>
	);
}
