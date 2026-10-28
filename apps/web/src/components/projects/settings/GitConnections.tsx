import { useQuery } from "@tanstack/react-query";
import { KeyRound, Plug, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { DeleteGitIdentityDialog } from "@/components/git/DeleteGitIdentityDialog";
import { GitSetupDialog } from "@/components/git/GitSetupDialog";
import { CopyChip, EmptyState, Section } from "@/components/git/git-ui";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useEffectiveActions } from "@/hooks/use-effective-actions";
import { usePermissions } from "@/hooks/use-permissions";
import { formatDate } from "@/lib/format-date";
import {
	deleteGitIdentity,
	deleteProjectGitConnection,
	type GitIdentity,
	type GitProvider,
	type GitRepository,
	gitConnectionsQueryOptions,
	gitQueriesKey,
	gitRepositoriesQueryOptions,
	projectGitSettingsPath,
} from "@/lib/git-api";

interface GitConnectionsProps {
	projectId: string;
	providers: GitProvider[];
}

/**
 * The credentials (GitHub Apps, access tokens) Paca reaches the project's
 * repositories with. A project can have several, from different organizations
 * or platforms; each is either this project's own or shared by every project.
 * Creating one is the only place a Git platform is set up.
 */
export function GitConnections({ projectId, providers }: GitConnectionsProps) {
	const { t } = useTranslation("projects");
	const { hasPermission } = usePermissions();
	const project = useEffectiveActions(projectId);
	const has = (action: string) => hasPermission(action) || project.has(action);
	// The router gates: a project's own connection takes git:write and
	// git.connections:manage; a workspace-wide one takes git.identities:manage.
	const canProject = has("git:write") && has("git.connections:manage");
	const canWorkspace = hasPermission("git.identities:manage");
	const canAdd = canProject || canWorkspace;

	const connections = useQuery(gitConnectionsQueryOptions(projectId));
	const repositories = useQuery(gitRepositoriesQueryOptions(projectId));
	const [adding, setAdding] = useState<GitProvider | null>(null);
	const [removing, setRemoving] = useState<GitIdentity | null>(null);

	if (connections.isLoading) return <Skeleton className="h-24 w-full" />;
	const list = connections.data ?? [];
	const canRemove = (c: GitIdentity) =>
		c.scope === "shared" ? canWorkspace : canProject;

	const addButtons =
		canAdd && providers.length > 0 ? (
			<div className="flex flex-wrap gap-2">
				{providers.map((p) => (
					<Button
						key={p.id}
						type="button"
						size="sm"
						variant={list.length === 0 ? "default" : "outline"}
						onClick={() => setAdding(p)}
					>
						<Plus className="size-4" />
						{t("project.git.connections.add", { provider: p.display_name })}
					</Button>
				))}
			</div>
		) : null;

	return (
		<Section
			id="git-connections"
			title={t("project.git.connections.title")}
			description={t("project.git.connections.description")}
			action={list.length > 0 ? addButtons : undefined}
		>
			{connections.isError ? (
				<p role="alert" className="text-sm text-destructive">
					{t("project.git.errors.generic")}
				</p>
			) : list.length === 0 ? (
				<EmptyState
					icon={Plug}
					title={t("project.git.connections.empty.title")}
					description={
						providers.length === 0
							? t("project.git.noProviders")
							: canAdd
								? t("project.git.connections.empty.description")
								: t("project.git.connections.empty.noPermission")
					}
				>
					{addButtons}
				</EmptyState>
			) : (
				<ul className="flex flex-col divide-y divide-border/60 rounded-xl border border-border/60">
					{list.map((c) => (
						<ConnectionRow
							key={c.id}
							connection={c}
							provider={providers.find((p) => p.id === c.provider_id)}
							onRemove={canRemove(c) ? () => setRemoving(c) : undefined}
						/>
					))}
				</ul>
			)}

			{adding ? (
				<GitSetupDialog
					provider={adding}
					host={adding.hosts[0] ?? ""}
					open
					project={{ id: projectId, canProject, canWorkspace }}
					returnPath={projectGitSettingsPath(projectId)}
					onOpenChange={(open) => {
						if (!open) setAdding(null);
					}}
				/>
			) : null}
			{removing ? (
				<DeleteGitIdentityDialog
					identity={removing}
					open
					remove={() =>
						removing.scope === "shared"
							? deleteGitIdentity(removing.id)
							: deleteProjectGitConnection(projectId, removing.id)
					}
					usage={usageOf(removing, repositories.data ?? [])}
					invalidateKey={gitQueriesKey}
					onOpenChange={(open) => {
						if (!open) setRemoving(null);
					}}
				/>
			) : null}
		</Section>
	);
}

/**
 * What deleting a connection would leave without a credential: this project's
 * repositories by name, and a count of the ones in other projects (a shared
 * connection serves every project).
 */
function usageOf(
	connection: GitIdentity,
	repositories: GitRepository[],
): { names: string[]; others: number } {
	const names = repositories
		.filter((r) => r.identity_id === connection.id)
		.map((r) => r.full_path);
	return {
		names,
		others: Math.max(0, (connection.repository_count ?? 0) - names.length),
	};
}

function ConnectionRow({
	connection: c,
	provider,
	onRemove,
}: {
	connection: GitIdentity;
	provider: GitProvider | undefined;
	onRemove?: () => void;
}) {
	const { t } = useTranslation("projects");
	const { t: tAdmin } = useTranslation("admin");
	const name = c.display_name || c.handle;
	const expired = c.expires_at && new Date(c.expires_at).getTime() < Date.now();
	const meta = [
		`${provider?.display_name ?? c.provider_id} · ${c.host}`,
		c.expires_at && !expired
			? t("project.git.connections.expires", { date: formatDate(c.expires_at) })
			: null,
	].filter(Boolean);
	return (
		<li className="flex items-start gap-3 px-4 py-3.5">
			<span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
				<KeyRound className="size-4" aria-hidden="true" />
			</span>
			<div className="min-w-0 flex-1">
				<div className="flex flex-wrap items-center gap-x-2 gap-y-1">
					<p className="min-w-0 max-w-full truncate text-sm font-medium">
						{name}
					</p>
					<Badge variant="secondary">
						{tAdmin(`git.setup.kind.${c.kind}.title` as never)}
					</Badge>
					<Badge variant={c.scope === "shared" ? "default" : "outline"}>
						{t(`project.git.connections.scope.${c.scope}` as never)}
					</Badge>
					{expired ? (
						<Badge variant="destructive">
							{t("project.git.connections.expired")}
						</Badge>
					) : null}
				</div>
				<p className="mt-1 truncate text-xs text-muted-foreground">
					{meta.join(" · ")}
				</p>
				{c.handle ? (
					<div className="mt-2 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
						<span className="shrink-0">
							{t("project.git.repositories.mention")}
						</span>
						<CopyChip value={`@${c.handle}`} />
					</div>
				) : null}
			</div>
			{onRemove ? (
				<Button
					type="button"
					size="icon-sm"
					variant="ghost"
					className="shrink-0 text-muted-foreground hover:text-destructive"
					aria-label={t("project.git.connections.remove")}
					title={t("project.git.connections.remove")}
					onClick={onRemove}
				>
					<Trash2 className="size-4" />
				</Button>
			) : null}
		</li>
	);
}
