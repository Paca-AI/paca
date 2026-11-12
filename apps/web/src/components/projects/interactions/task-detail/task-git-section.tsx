import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	ArrowRight,
	Check,
	CircleAlert,
	Clock,
	Copy,
	GitBranch,
	GitMerge,
	GitPullRequest,
	GitPullRequestClosed,
	GitPullRequestDraft,
	Link2,
	Plus,
	Trash2,
} from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { usePermissions } from "@/hooks/use-permissions";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import {
	type GitCISummary,
	type GitTaskBranch,
	type GitTaskChangeRequest,
	gitProjectProvidersQueryOptions,
	gitRepositoriesQueryOptions,
	taskGitKey,
	taskGitQueryOptions,
	unlinkTaskBranch,
	unlinkTaskChangeRequest,
} from "@/lib/git-api";
import {
	CreateBranchForm,
	LinkBranchForm,
	LinkChangeRequestForm,
} from "./task-git-forms";

interface TaskGitSectionProps {
	projectId: string;
	taskId: string;
	taskIdPrefix?: string;
	taskNumber?: number;
	taskTitle?: string;
	canEdit?: boolean;
}

const PILL =
	"flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-semibold transition-all duration-150";
const PILL_PRIMARY = `${PILL} bg-primary/8 text-primary/80 hover:bg-primary/15 hover:text-primary`;
const PILL_QUIET = `${PILL} text-muted-foreground/70 hover:bg-muted/50 hover:text-foreground`;
const ROWS =
	"rounded-xl border border-border/25 bg-card/50 divide-y divide-border/15 overflow-hidden";

function SectionHeader({
	title,
	children,
}: {
	title: string;
	children?: React.ReactNode;
}) {
	return (
		<div className="flex items-center justify-between">
			<h3 className="text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground/70 flex items-center gap-2">
				<span>{title}</span>
				<div className="flex-1 h-px bg-linear-to-r from-border/40 to-transparent" />
			</h3>
			<div className="flex items-center gap-1">{children}</div>
		</div>
	);
}

function Empty({
	icon,
	children,
}: {
	icon: React.ReactNode;
	children: React.ReactNode;
}) {
	return (
		<div className="flex items-center gap-2 rounded-xl border border-dashed border-border/40 px-4 py-3 text-sm text-muted-foreground">
			{icon}
			{children}
		</div>
	);
}

// ── Pull requests ────────────────────────────────────────────────────────────

function StateIcon({ cr }: { cr: GitTaskChangeRequest }) {
	const cls = "size-3.5 shrink-0";
	if (cr.state === "merged")
		return <GitMerge className={`${cls} text-violet-500`} aria-hidden="true" />;
	if (cr.state === "closed")
		return (
			<GitPullRequestClosed
				className={`${cls} text-destructive`}
				aria-hidden="true"
			/>
		);
	if (cr.draft)
		return (
			<GitPullRequestDraft
				className={`${cls} text-muted-foreground`}
				aria-hidden="true"
			/>
		);
	return (
		<GitPullRequest className={`${cls} text-success`} aria-hidden="true" />
	);
}

const CI_STYLE: Record<
	GitCISummary["state"],
	{ cls: string; Icon: typeof Check }
> = {
	passing: { cls: "bg-success/10 text-success", Icon: Check },
	failing: { cls: "bg-destructive/10 text-destructive", Icon: CircleAlert },
	pending: { cls: "bg-warning/10 text-warning", Icon: Clock },
};

/** The state of a pull request's checks, with the counts in its tooltip. */
export function CIBadge({ ci }: { ci: GitCISummary }) {
	const { t } = useTranslation("projects");
	const { cls, Icon } = CI_STYLE[ci.state];
	const detail =
		ci.state === "failing" && ci.failing.length > 0
			? t("taskDetail.git.ci.failingNames", { names: ci.failing.join(", ") })
			: t("taskDetail.git.ci.counts", { passed: ci.passed, total: ci.total });
	return (
		<span
			className={`inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${cls}`}
			title={detail}
			data-testid="ci-badge"
		>
			<Icon className="size-3" aria-hidden="true" />
			{t(`taskDetail.git.ci.${ci.state}` as never)}
		</span>
	);
}

function ChangeRequestRow({
	cr,
	projectId,
	taskId,
	canEdit,
}: {
	cr: GitTaskChangeRequest;
	projectId: string;
	taskId: string;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const label = `${cr.repository} #${cr.number}`;
	const unlink = useMutation({
		mutationFn: () =>
			unlinkTaskChangeRequest(projectId, taskId, cr.repository_id, cr.number),
		onSuccess: () =>
			qc.invalidateQueries({ queryKey: taskGitKey(projectId, taskId) }),
	});
	const title = cr.title || label;
	const state = cr.draft && cr.state === "open" ? "draft" : cr.state;

	return (
		<div className="group flex items-center gap-3 px-4 py-2.5">
			<StateIcon cr={cr} />
			<span className="shrink-0 font-mono text-xs text-muted-foreground/60">
				#{cr.number}
			</span>
			<div className="min-w-0 flex-1">
				{cr.url ? (
					<a
						href={cr.url}
						target="_blank"
						rel="noopener noreferrer"
						title={title}
						className="block truncate text-sm hover:text-primary"
					>
						{title}
					</a>
				) : (
					<span className="block truncate text-sm" title={title}>
						{title}
					</span>
				)}
				<div className="flex items-center gap-1.5 overflow-hidden whitespace-nowrap text-xs text-muted-foreground/70">
					<span className="truncate">{cr.repository}</span>
					{cr.head_ref ? (
						<span className="flex shrink-0 items-center gap-1">
							{cr.head_ref}
							{cr.base_ref ? (
								<>
									<ArrowRight className="size-2.5" aria-hidden="true" />
									{cr.base_ref}
								</>
							) : null}
						</span>
					) : null}
					{cr.author ? (
						<span className="truncate">
							{t("taskDetail.git.by", { author: cr.author })}
						</span>
					) : null}
				</div>
			</div>
			{cr.ci ? <CIBadge ci={cr.ci} /> : null}
			<span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
				{t(`taskDetail.git.state.${state}` as never)}
			</span>
			{canEdit ? (
				<button
					type="button"
					aria-label={t("taskDetail.git.unlinkPullRequest", { name: label })}
					title={t("taskDetail.git.unlink")}
					disabled={unlink.isPending}
					onClick={() => unlink.mutate()}
					className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100 focus-visible:opacity-100"
				>
					<Trash2 className="size-3.5" />
				</button>
			) : null}
		</div>
	);
}

// ── Branches ─────────────────────────────────────────────────────────────────

function BranchRow({
	branch,
	projectId,
	taskId,
	canEdit,
}: {
	branch: GitTaskBranch;
	projectId: string;
	taskId: string;
	canEdit: boolean;
}) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [copied, setCopied] = useState(false);
	const unlink = useMutation({
		mutationFn: () =>
			unlinkTaskBranch(projectId, taskId, branch.repository_id, branch.branch),
		onSuccess: () =>
			qc.invalidateQueries({ queryKey: taskGitKey(projectId, taskId) }),
	});
	const checkout = `git fetch origin && git checkout ${branch.branch}`;

	return (
		<div className="group flex items-center gap-3 px-4 py-2.5">
			<GitBranch
				className="size-3.5 shrink-0 text-muted-foreground/60"
				aria-hidden="true"
			/>
			<div className="min-w-0 flex-1">
				<span
					className="block truncate font-mono text-[13px]"
					title={branch.branch}
				>
					{branch.branch}
				</span>
				<span className="block truncate text-xs text-muted-foreground/70">
					{branch.repository}
				</span>
			</div>
			<button
				type="button"
				aria-label={t("taskDetail.git.copyCheckout")}
				title={t("taskDetail.git.copyCheckout")}
				onClick={() => {
					void navigator.clipboard?.writeText(checkout);
					setCopied(true);
				}}
				className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:text-foreground group-hover:opacity-100 focus-visible:opacity-100"
			>
				{copied ? (
					<Check className="size-3.5" />
				) : (
					<Copy className="size-3.5" />
				)}
			</button>
			{canEdit ? (
				<button
					type="button"
					aria-label={t("taskDetail.git.unlinkBranch", { name: branch.branch })}
					title={t("taskDetail.git.unlink")}
					disabled={unlink.isPending}
					onClick={() => unlink.mutate()}
					className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100 focus-visible:opacity-100"
				>
					<Trash2 className="size-3.5" />
				</button>
			) : null}
		</div>
	);
}

// ── The sections ─────────────────────────────────────────────────────────────

/**
 * A task's branches and pull/merge requests, provider-neutral. Shown once the
 * project has a linked repository: a request or branch is tied to the task by
 * its key in the branch or title (for example PACA-12), by an agent that opened
 * it for the task, or by hand here. Each open request shows the state of its CI.
 */
export function TaskGitSection({
	projectId,
	taskId,
	taskIdPrefix = "",
	taskNumber = 0,
	taskTitle,
	canEdit = true,
}: TaskGitSectionProps) {
	const { t } = useTranslation("projects");
	const repos = useQuery(gitRepositoriesQueryOptions(projectId));
	const links = useQuery(taskGitQueryOptions(projectId, taskId));
	const providers = useQuery(gitProjectProvidersQueryOptions(projectId));
	const { hasPermission } = usePermissions();
	const { hasProjectPermission } = useProjectPermissions(projectId);
	const [crMode, setCrMode] = useState<"link" | null>(null);
	const [branchMode, setBranchMode] = useState<"link" | "create" | null>(null);

	const linked = repos.data ?? [];
	// Nothing to show until the project has a repository (or the user may not read Git).
	if (repos.isError || linked.length === 0) return null;

	const crs = links.data?.change_requests ?? [];
	const branches = links.data?.branches ?? [];
	const canWriteGit =
		hasPermission("git:write") || hasProjectPermission("git:write");
	// Branch creation writes to the repository: it needs git:write and a
	// provider that can do it for at least one linked repository.
	const canCreateOnPlatform =
		canWriteGit &&
		linked.some(
			(r) =>
				providers.data?.find((p) => p.id === r.provider_id)?.capabilities
					.branches,
		);

	return (
		<>
			<div className="space-y-3">
				<SectionHeader title={t("taskDetail.git.branches")}>
					{canEdit && branchMode === null ? (
						<>
							<button
								type="button"
								className={PILL_QUIET}
								onClick={() => setBranchMode("link")}
							>
								<Link2 className="size-3" />
								{t("taskDetail.git.linkBranch")}
							</button>
							<button
								type="button"
								className={PILL_PRIMARY}
								onClick={() => setBranchMode("create")}
							>
								<Plus className="size-3" />
								{t("taskDetail.git.newBranch")}
							</button>
						</>
					) : null}
				</SectionHeader>
				{branches.length > 0 ? (
					<div className={ROWS}>
						{branches.map((b) => (
							<BranchRow
								key={`${b.repository_id}/${b.branch}`}
								branch={b}
								projectId={projectId}
								taskId={taskId}
								canEdit={canEdit}
							/>
						))}
					</div>
				) : branchMode === null ? (
					<Empty icon={<GitBranch className="size-4" aria-hidden="true" />}>
						{t("taskDetail.git.noBranches")}
					</Empty>
				) : null}
				{branchMode === "link" ? (
					<LinkBranchForm
						projectId={projectId}
						taskId={taskId}
						repos={linked}
						onDone={() => setBranchMode(null)}
					/>
				) : null}
				{branchMode === "create" ? (
					<CreateBranchForm
						projectId={projectId}
						taskId={taskId}
						repos={linked}
						taskIdPrefix={taskIdPrefix}
						taskNumber={taskNumber}
						taskTitle={taskTitle}
						canCreateOnPlatform={canCreateOnPlatform}
						onDone={() => setBranchMode(null)}
					/>
				) : null}
			</div>

			<div className="space-y-3">
				<SectionHeader title={t("taskDetail.git.pullRequests")}>
					{canEdit && crMode === null ? (
						<button
							type="button"
							className={PILL_PRIMARY}
							onClick={() => setCrMode("link")}
						>
							<Plus className="size-3" />
							{t("taskDetail.git.linkPullRequest")}
						</button>
					) : null}
				</SectionHeader>
				{crs.length > 0 ? (
					<div className={ROWS}>
						{crs.map((cr) => (
							<ChangeRequestRow
								key={`${cr.repository_id}/${cr.number}`}
								cr={cr}
								projectId={projectId}
								taskId={taskId}
								canEdit={canEdit}
							/>
						))}
					</div>
				) : crMode === null ? (
					<Empty
						icon={<GitPullRequest className="size-4" aria-hidden="true" />}
					>
						{t("taskDetail.git.noPullRequests")}
					</Empty>
				) : null}
				{crMode === "link" ? (
					<LinkChangeRequestForm
						projectId={projectId}
						taskId={taskId}
						repos={linked}
						onDone={() => setCrMode(null)}
					/>
				) : null}
			</div>
		</>
	);
}
