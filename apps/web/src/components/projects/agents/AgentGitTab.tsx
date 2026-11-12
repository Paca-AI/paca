import { useMutation, useQuery } from "@tanstack/react-query";
import { GitBranch, Plug, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { DeleteGitIdentityDialog } from "@/components/git/DeleteGitIdentityDialog";
import { GitSetupDialog } from "@/components/git/GitSetupDialog";
import { CopyChip, EmptyState, Section } from "@/components/git/git-ui";
import { AgentGitTriggers } from "@/components/projects/agents/AgentGitTriggers";
import {
	Accordion,
	AccordionContent,
	AccordionItem,
	AccordionTrigger,
} from "@/components/ui/accordion";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	agentGitIdentitiesKey,
	agentGitIdentitiesQueryOptions,
	beginAgentGitAccess,
	beginAgentGitSetup,
	deleteAgentGitIdentity,
	followGitStep,
	forgetAgentInstall,
	type GitIdentity,
	type GitProvider,
	gitErrorKey,
	gitProjectProvidersQueryOptions,
	rememberAgentInstall,
	rememberGitFlow,
} from "@/lib/git-api";

interface AgentGitTabProps {
	projectId: string;
	agentId: string;
	/** agents:write on this agent. */
	canWrite: boolean;
}

/**
 * An agent's Git identity. By default an agent acts through the workspace's
 * shared identity; here it can get its own (so its commits, pull requests and
 * comments appear under a separate bot) and install that identity on the
 * accounts and organizations whose repositories it works on.
 *
 * Creating or removing an identity creates or removes an app on the Git
 * platform, which needs the workspace-level git.identities:manage. Installing
 * an existing one on another account needs the agent's agents:write and the
 * project's git:write. What the platform lets a user install is the platform's
 * own check.
 */
export function AgentGitTab({
	projectId,
	agentId,
	canWrite,
}: AgentGitTabProps) {
	const { t } = useTranslation("projects");
	const { hasPermission } = usePermissions();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const canManage = hasPermission("git.identities:manage");
	const canInstall =
		canWrite &&
		(hasPermission("git:write") || hasProjectPermission("git:write"));

	const providers = useQuery(gitProjectProvidersQueryOptions(projectId));
	const identities = useQuery(
		agentGitIdentitiesQueryOptions(projectId, agentId),
	);

	const [creating, setCreating] = useState<GitProvider | null>(null);
	const [removing, setRemoving] = useState<GitIdentity | null>(null);

	if (providers.isLoading || identities.isLoading) {
		return <Skeleton className="h-40 w-full" />;
	}
	const list = providers.data ?? [];
	const own = (providerId: string) =>
		(identities.data ?? []).find((i) => i.provider_id === providerId);

	return (
		<div className="flex max-w-3xl flex-col gap-6">
			<div>
				<h2 className="flex items-center gap-2 text-lg font-semibold">
					<GitBranch className="size-4 text-primary" />
					{t("agents.detail.git.title")}
				</h2>
				<p className="mt-1 text-sm text-muted-foreground">
					{t("agents.detail.git.description")}
				</p>
			</div>

			{providers.isError ? (
				<p role="alert" className="text-sm text-muted-foreground">
					{t("agents.detail.git.providersUnavailable")}
				</p>
			) : list.length === 0 ? (
				<EmptyState
					icon={Plug}
					title={t("agents.detail.git.noProviders.title")}
					description={t("agents.detail.git.noProviders.description")}
				/>
			) : (
				<Section
					id="git-agent-identity"
					title={t("agents.detail.git.identity.title")}
					description={t("agents.detail.git.identity.description")}
				>
					{list.map((provider) => {
						const identity = own(provider.id);
						return (
							<div
								key={provider.id}
								className="flex flex-col gap-4 rounded-lg border border-border/60 p-4"
							>
								<div className="flex items-start justify-between gap-3">
									<div className="min-w-0">
										<div className="flex flex-wrap items-center gap-2">
											<h3 className="truncate text-sm font-semibold">
												{provider.display_name}
											</h3>
											<Badge variant={identity ? "default" : "secondary"}>
												{identity
													? t("agents.detail.git.ownIdentity")
													: t("agents.detail.git.usesShared")}
											</Badge>
										</div>
										{identity ? (
											<div className="mt-2 flex flex-col gap-1.5 text-sm text-muted-foreground">
												<p>{t("agents.detail.git.own.description")}</p>
												{identity.handle ? (
													<p className="flex flex-wrap items-center gap-2">
														{t("agents.detail.git.own.mention")}
														<CopyChip
															value={`@${identity.handle}`}
															label={identity.display_name || identity.handle}
														/>
													</p>
												) : null}
											</div>
										) : (
											<p className="mt-2 text-sm text-muted-foreground">
												{t("agents.detail.git.shared.description")}
											</p>
										)}
									</div>
									{identity && canManage ? (
										<Button
											type="button"
											variant="outline"
											size="sm"
											onClick={() => setRemoving(identity)}
										>
											<Trash2 className="size-4" />
											{t("agents.detail.git.remove")}
										</Button>
									) : null}
								</div>

								{identity ? null : !provider.capabilities
										.multiple_identities ? (
									<p className="text-xs text-muted-foreground">
										{t("agents.detail.git.singleIdentity")}
									</p>
								) : canManage ? (
									<div className="flex flex-col items-start gap-1.5 rounded-md bg-muted/40 p-3">
										<p className="text-sm font-medium">
											{t("agents.detail.git.create.title")}
										</p>
										<p className="text-sm text-muted-foreground">
											{t("agents.detail.git.create.description")}
										</p>
										<Button
											type="button"
											size="sm"
											disabled={!canWrite}
											onClick={() => setCreating(provider)}
										>
											{t("agents.detail.git.create.button")}
										</Button>
									</div>
								) : (
									<p className="rounded-md bg-muted/40 p-3 text-sm text-muted-foreground">
										{t("agents.detail.git.askAdmin")}
									</p>
								)}

								{identity &&
								provider.capabilities.access_grants &&
								canInstall ? (
									<InstallOnAccount
										projectId={projectId}
										agentId={agentId}
										provider={provider}
									/>
								) : null}
							</div>
						);
					})}
				</Section>
			)}

			{list.length > 0 ? (
				<AgentGitTriggers
					projectId={projectId}
					agentId={agentId}
					canWrite={canWrite}
				/>
			) : null}

			{list.length > 0 ? (
				<p className="text-xs text-muted-foreground">
					{t("agents.detail.git.permissions")}
				</p>
			) : null}

			{creating ? (
				<GitSetupDialog
					provider={creating}
					host={creating.hosts[0] ?? ""}
					open
					onOpenChange={(open) => {
						if (!open) setCreating(null);
					}}
					begin={async (payload) => {
						// The App is installed right after it is created.
						rememberAgentInstall({ projectId, agentId });
						try {
							return await beginAgentGitSetup(projectId, agentId, payload);
						} catch (err) {
							forgetAgentInstall();
							throw err;
						}
					}}
					returnPath={`/projects/${projectId}/agents/${agentId}#git`}
					invalidateKey={agentGitIdentitiesKey(projectId, agentId)}
				/>
			) : null}
			{removing ? (
				<DeleteGitIdentityDialog
					identity={removing}
					open
					onOpenChange={(open) => {
						if (!open) setRemoving(null);
					}}
					remove={() =>
						deleteAgentGitIdentity(projectId, agentId, removing.provider_id)
					}
					invalidateKey={agentGitIdentitiesKey(projectId, agentId)}
				/>
			) : null}
		</div>
	);
}

/**
 * Installs the agent's identity on an account or organization, so it can reach
 * that owner's repositories. The platform's install page opens (pre-selecting
 * the owner when one is typed) and returns to the Paca callback, which brings
 * the user back to this tab.
 */
function InstallOnAccount({
	projectId,
	agentId,
	provider,
}: {
	projectId: string;
	agentId: string;
	provider: GitProvider;
}) {
	const { t } = useTranslation("projects");
	const [owner, setOwner] = useState("");
	const [errorKey, setErrorKey] = useState<string | null>(null);

	const install = useMutation({
		mutationFn: () =>
			beginAgentGitAccess(projectId, agentId, {
				provider_id: provider.id,
				owner: owner.trim(),
				return_url: `${window.location.origin}/projects/${projectId}/agents/${agentId}#git`,
			}),
		onSuccess: (step) => {
			rememberGitFlow(step.state, { kind: "access", projectId });
			// Leaves the page; the platform returns to /git/setup/callback.
			if (!followGitStep(step)) setErrorKey("generic");
		},
		onError: (err: unknown) => setErrorKey(gitErrorKey(err)),
	});

	return (
		<Accordion className="rounded-md border border-border/60 p-3">
			<AccordionItem value="item" className="border-b-0">
				<AccordionTrigger className="py-0 text-sm font-medium">
					{t("agents.detail.git.install.title")}
				</AccordionTrigger>
				<AccordionContent keepMounted className="pb-0 pt-0">
					<form
						className="mt-3 flex flex-col gap-2"
						onSubmit={(e) => {
							e.preventDefault();
							setErrorKey(null);
							install.mutate();
						}}
					>
						<Label htmlFor={`git-agent-owner-${provider.id}`}>
							{t("agents.detail.git.install.label")}
						</Label>
						<div className="flex gap-2">
							<Input
								id={`git-agent-owner-${provider.id}`}
								value={owner}
								autoComplete="off"
								placeholder={t("agents.detail.git.install.placeholder")}
								onChange={(e) => setOwner(e.target.value)}
							/>
							<Button
								type="submit"
								variant="outline"
								disabled={install.isPending}
							>
								{install.isPending ? <Spinner aria-hidden="true" /> : null}
								{t("agents.detail.git.install.button")}
							</Button>
						</div>
						<p className="text-xs text-muted-foreground">
							{t("agents.detail.git.install.help")}
						</p>
						{errorKey ? (
							<p role="alert" className="text-sm text-destructive">
								{t(`project.git.errors.${errorKey}` as never)}
							</p>
						) : null}
					</form>
				</AccordionContent>
			</AccordionItem>
		</Accordion>
	);
}
