import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Section } from "@/components/git/git-ui";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import {
	agentGitTriggersKey,
	agentGitTriggersQueryOptions,
	type GitAutoReviewConfig,
	type GitTrigger,
	gitErrorKey,
	setAgentGitTrigger,
} from "@/lib/git-api";

interface AgentGitTriggersProps {
	projectId: string;
	agentId: string;
	/** agents:write on this agent. */
	canWrite: boolean;
}

const list = (s: string): string[] =>
	s
		.split(",")
		.map((v) => v.trim())
		.filter(Boolean);

const GROUPS: { id: string; kinds: GitTrigger["kind"][] }[] = [
	{
		id: "requests",
		kinds: ["mention", "label", "review_request", "assignment"],
	},
	{ id: "followUp", kinds: ["own_cr_feedback", "ci_failure"] },
	{ id: "proactive", kinds: ["auto_review"] },
];

/**
 * Which Git events start this agent, grouped by what they are for: answering
 * requests, following up on its own pull requests, and reviewing on its own.
 * Triggers no installed provider can deliver stay visible but switched off, so
 * the user can see what the agent could do and why it is unavailable.
 */
export function AgentGitTriggers({
	projectId,
	agentId,
	canWrite,
}: AgentGitTriggersProps) {
	const { t } = useTranslation("projects");
	const queryClient = useQueryClient();
	const triggers = useQuery(agentGitTriggersQueryOptions(projectId, agentId));
	const [errorKey, setErrorKey] = useState<string | null>(null);

	const set = useMutation({
		mutationFn: (v: {
			kind: GitTrigger["kind"];
			enabled: boolean;
			config?: GitAutoReviewConfig;
		}) =>
			setAgentGitTrigger(projectId, agentId, v.kind, {
				enabled: v.enabled,
				config: v.config,
			}),
		onSuccess: (next) => {
			setErrorKey(null);
			queryClient.setQueryData(agentGitTriggersKey(projectId, agentId), next);
		},
		onError: (err: unknown) => setErrorKey(gitErrorKey(err)),
	});

	if (triggers.isLoading) return <Skeleton className="h-32 w-full" />;
	const all = triggers.data ?? [];
	if (triggers.isError || all.length === 0) return null;
	const byKind = new Map(all.map((x) => [x.kind, x]));

	return (
		<Section
			id="git-agent-triggers"
			title={t("agents.detail.git.triggers.title")}
			description={t("agents.detail.git.triggers.description")}
		>
			{GROUPS.map((group) => {
				const rows = group.kinds
					.map((k) => byKind.get(k))
					.filter((x): x is GitTrigger => Boolean(x));
				if (rows.length === 0) return null;
				return (
					<div
						key={group.id}
						className="rounded-lg border border-border/60 p-4"
					>
						<h3 className="text-sm font-semibold">
							{t(
								`agents.detail.git.triggers.groups.${group.id}.title` as never,
							)}
						</h3>
						<p className="text-xs text-muted-foreground">
							{t(
								`agents.detail.git.triggers.groups.${group.id}.description` as never,
							)}
						</p>
						<ul className="mt-2 flex flex-col divide-y divide-border/60">
							{rows.map((trigger) => (
								<li key={trigger.kind} className="flex flex-col gap-2 py-2.5">
									<div className="flex items-center justify-between gap-3">
										<div className="min-w-0">
											<Label htmlFor={`git-trigger-${trigger.kind}`}>
												{t(
													`project.git.triggerKinds.${trigger.kind}.label` as never,
												)}
											</Label>
											<p className="text-xs text-muted-foreground">
												{t(
													`project.git.triggerKinds.${trigger.kind}.description` as never,
												)}
											</p>
											{trigger.supported ? null : (
												<p className="mt-0.5 text-xs text-warning">
													{t("agents.detail.git.triggers.unsupported")}
												</p>
											)}
										</div>
										<Switch
											id={`git-trigger-${trigger.kind}`}
											checked={trigger.supported && trigger.enabled}
											disabled={
												!canWrite || !trigger.supported || set.isPending
											}
											onCheckedChange={(enabled) =>
												set.mutate({
													kind: trigger.kind,
													enabled,
													config: trigger.explicit ? trigger.config : undefined,
												})
											}
										/>
									</div>
									{trigger.kind === "auto_review" &&
									trigger.supported &&
									trigger.enabled ? (
										<AutoReviewFilters
											trigger={trigger}
											canWrite={canWrite}
											pending={set.isPending}
											onSave={(config) =>
												set.mutate({
													kind: trigger.kind,
													enabled: true,
													config,
												})
											}
										/>
									) : null}
								</li>
							))}
						</ul>
					</div>
				);
			})}
			{errorKey ? (
				<p role="alert" className="text-sm text-destructive">
					{t(`project.git.errors.${errorKey}` as never)}
				</p>
			) : null}
		</Section>
	);
}

function AutoReviewFilters({
	trigger,
	canWrite,
	pending,
	onSave,
}: {
	trigger: GitTrigger;
	canWrite: boolean;
	pending: boolean;
	onSave: (config: GitAutoReviewConfig) => void;
}) {
	const { t } = useTranslation("projects");
	const [branches, setBranches] = useState("");
	const [authors, setAuthors] = useState("");

	useEffect(() => {
		setBranches((trigger.config.branches ?? []).join(", "));
		setAuthors((trigger.config.authors ?? []).join(", "));
	}, [trigger.config]);

	return (
		<form
			className="grid gap-2 sm:grid-cols-2"
			onSubmit={(e) => {
				e.preventDefault();
				onSave({ branches: list(branches), authors: list(authors) });
			}}
		>
			<div className="flex flex-col gap-1">
				<Label htmlFor="git-auto-branches">
					{t("agents.detail.git.triggers.filters.branches")}
				</Label>
				<Input
					id="git-auto-branches"
					value={branches}
					disabled={!canWrite}
					placeholder="main, release"
					onChange={(e) => setBranches(e.target.value)}
				/>
			</div>
			<div className="flex flex-col gap-1">
				<Label htmlFor="git-auto-authors">
					{t("agents.detail.git.triggers.filters.authors")}
				</Label>
				<Input
					id="git-auto-authors"
					value={authors}
					disabled={!canWrite}
					placeholder="alice, dependabot[bot]"
					onChange={(e) => setAuthors(e.target.value)}
				/>
			</div>
			<p className="text-xs text-muted-foreground sm:col-span-2">
				{t("agents.detail.git.triggers.filters.help")}
			</p>
			{canWrite ? (
				<div className="sm:col-span-2">
					<Button type="submit" size="sm" variant="outline" disabled={pending}>
						{pending ? <Spinner aria-hidden="true" /> : null}
						{t("agents.detail.git.triggers.filters.save")}
					</Button>
				</div>
			) : null}
		</form>
	);
}
