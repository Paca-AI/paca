import { useMutation, useQueryClient } from "@tanstack/react-query";
import { GitBranch, Link2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Accordion,
	AccordionContent,
	AccordionItem,
	AccordionTrigger,
} from "@/components/ui/accordion";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	NativeSelect,
	NativeSelectOption,
} from "@/components/ui/native-select";
import { Spinner } from "@/components/ui/spinner";
import {
	createTaskBranch,
	type GitRepository,
	getApiErrorCodeSafe,
	linkTaskBranch,
	linkTaskChangeRequest,
	taskGitKey,
} from "@/lib/git-api";

// ── Parsing what the user pastes ─────────────────────────────────────────────

/** "https://host/owner/name/pull/42" (or merge_requests) → repository path + number. */
export function parseChangeRequestUrl(
	raw: string,
): { host: string; fullPath: string; number: string } | null {
	try {
		const u = new URL(raw.trim());
		const m = /^\/(.+?)\/(?:-\/)?(?:pull|pulls|merge_requests)\/(\d+)\/?$/.exec(
			u.pathname,
		);
		return m ? { host: u.hostname, fullPath: m[1], number: m[2] } : null;
	} catch {
		return null;
	}
}

/** "https://host/owner/name/tree/some/branch" → repository path + branch. */
export function parseBranchUrl(
	raw: string,
): { host: string; fullPath: string; branch: string } | null {
	try {
		const u = new URL(raw.trim());
		const m = /^\/(.+?)\/(?:-\/)?tree\/(.+)$/.exec(u.pathname);
		return m
			? { host: u.hostname, fullPath: m[1], branch: decodeURIComponent(m[2]) }
			: null;
	} catch {
		return null;
	}
}

function repoOf(
	repos: GitRepository[],
	host: string,
	fullPath: string,
): GitRepository | undefined {
	return repos.find(
		(r) =>
			r.host.toLowerCase() === host.toLowerCase() &&
			r.full_path.toLowerCase() === fullPath.toLowerCase(),
	);
}

const ERROR_KEYS: Record<string, string> = {
	GIT_NOT_FOUND: "notFound",
	GIT_PROVIDER_REJECTED: "rejected",
	GIT_PROVIDER_UNAVAILABLE: "unavailable",
	GIT_NO_IDENTITY: "noIdentity",
	FORBIDDEN: "forbidden",
};

function errorKey(err: unknown): string {
	const code = getApiErrorCodeSafe(err);
	return (code && ERROR_KEYS[code]) || "generic";
}

function RepoSelect({
	id,
	repos,
	value,
	disabled,
	onChange,
}: {
	id: string;
	repos: GitRepository[];
	value: string;
	disabled?: boolean;
	onChange: (v: string) => void;
}) {
	const { t } = useTranslation("projects");
	return (
		<NativeSelect
			id={id}
			className="w-full"
			value={value}
			disabled={disabled}
			onChange={(e) => onChange(e.target.value)}
		>
			<NativeSelectOption value="">
				{t("taskDetail.git.forms.selectRepository")}
			</NativeSelectOption>
			{repos.map((r) => (
				<NativeSelectOption key={r.id} value={r.id}>
					{r.full_path}
				</NativeSelectOption>
			))}
		</NativeSelect>
	);
}

interface FormProps {
	projectId: string;
	taskId: string;
	repos: GitRepository[];
	onDone: () => void;
}

function FormActions({
	submitLabel,
	pending,
	canSubmit,
	onCancel,
	icon,
}: {
	submitLabel: string;
	pending: boolean;
	canSubmit: boolean;
	onCancel: () => void;
	icon: React.ReactNode;
}) {
	const { t } = useTranslation("projects");
	return (
		<div className="flex gap-2">
			<Button type="submit" size="sm" disabled={!canSubmit || pending}>
				{pending ? <Spinner aria-hidden="true" /> : icon}
				{submitLabel}
			</Button>
			<Button
				type="button"
				size="sm"
				variant="ghost"
				disabled={pending}
				onClick={onCancel}
			>
				{t("taskDetail.git.forms.cancel")}
			</Button>
		</div>
	);
}

const FORM_CLASS =
	"flex flex-col gap-3 rounded-xl border border-border/25 bg-card/50 p-4";

// ── Link a pull request ──────────────────────────────────────────────────────

export function LinkChangeRequestForm({
	projectId,
	taskId,
	repos,
	onDone,
}: FormProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [value, setValue] = useState("");
	const [picked, setPicked] = useState(repos.length === 1 ? repos[0].id : "");
	const [error, setError] = useState<string | null>(null);

	const parsed = parseChangeRequestUrl(value);
	const urlRepo = parsed
		? repoOf(repos, parsed.host, parsed.fullPath)
		: undefined;
	const number = parsed ? parsed.number : value.trim().replace(/^#/, "");

	const link = useMutation({
		mutationFn: () =>
			linkTaskChangeRequest(projectId, taskId, {
				repository_id: parsed ? (urlRepo?.id ?? "") : picked,
				number,
			}),
		onSuccess: async () => {
			await qc.invalidateQueries({ queryKey: taskGitKey(projectId, taskId) });
			onDone();
		},
		onError: (err: unknown) => setError(errorKey(err)),
	});

	function submit() {
		if (parsed && !urlRepo) return setError("repoNotLinked");
		if (!parsed && !picked) return setError("selectRepository");
		if (!/^\d+$/.test(number)) return setError("invalidNumber");
		setError(null);
		link.mutate();
	}

	return (
		<form
			className={FORM_CLASS}
			onSubmit={(e) => {
				e.preventDefault();
				submit();
			}}
			onKeyDown={(e) => e.key === "Escape" && onDone()}
		>
			<div className="flex flex-col gap-1.5">
				<Label htmlFor="git-link-cr">
					{t("taskDetail.git.forms.pullRequest")}
				</Label>
				<Input
					id="git-link-cr"
					value={value}
					placeholder="https://github.com/owner/repo/pull/42"
					disabled={link.isPending}
					aria-invalid={!!error}
					autoComplete="off"
					// biome-ignore lint/a11y/noAutofocus: opened on demand by the user
					autoFocus
					onChange={(e) => {
						setValue(e.target.value);
						setError(null);
					}}
				/>
				<p className="text-xs text-muted-foreground">
					{parsed && urlRepo
						? t("taskDetail.git.forms.willLink", {
								what: `#${parsed.number}`,
								repository: urlRepo.full_path,
							})
						: t("taskDetail.git.forms.pullRequestHint")}
				</p>
			</div>
			{!parsed && repos.length > 1 ? (
				<div className="flex flex-col gap-1.5">
					<Label htmlFor="git-link-cr-repo">
						{t("taskDetail.git.forms.repository")}
					</Label>
					<RepoSelect
						id="git-link-cr-repo"
						repos={repos}
						value={picked}
						disabled={link.isPending}
						onChange={(v) => {
							setPicked(v);
							setError(null);
						}}
					/>
				</div>
			) : null}
			{error ? (
				<p role="alert" className="text-xs text-destructive">
					{t(`taskDetail.git.errors.${error}` as never, {
						repository: parsed?.fullPath ?? "",
						number,
					})}
				</p>
			) : null}
			<FormActions
				submitLabel={t("taskDetail.git.forms.linkPullRequest")}
				pending={link.isPending}
				canSubmit={!!value.trim()}
				onCancel={onDone}
				icon={<Link2 className="size-4" />}
			/>
		</form>
	);
}

// ── Link an existing branch ──────────────────────────────────────────────────

export function LinkBranchForm({
	projectId,
	taskId,
	repos,
	onDone,
}: FormProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const [value, setValue] = useState("");
	const [picked, setPicked] = useState(repos.length === 1 ? repos[0].id : "");
	const [error, setError] = useState<string | null>(null);

	const parsed = parseBranchUrl(value);
	const urlRepo = parsed
		? repoOf(repos, parsed.host, parsed.fullPath)
		: undefined;
	const branch = parsed ? parsed.branch : value.trim();

	const link = useMutation({
		mutationFn: () =>
			linkTaskBranch(projectId, taskId, {
				repository_id: parsed ? (urlRepo?.id ?? "") : picked,
				branch,
			}),
		onSuccess: async () => {
			await qc.invalidateQueries({ queryKey: taskGitKey(projectId, taskId) });
			onDone();
		},
		onError: (err: unknown) => setError(errorKey(err)),
	});

	function submit() {
		if (parsed && !urlRepo) return setError("repoNotLinked");
		if (!parsed && !picked) return setError("selectRepository");
		if (!branch) return setError("branchRequired");
		setError(null);
		link.mutate();
	}

	return (
		<form
			className={FORM_CLASS}
			onSubmit={(e) => {
				e.preventDefault();
				submit();
			}}
			onKeyDown={(e) => e.key === "Escape" && onDone()}
		>
			<div className="flex flex-col gap-1.5">
				<Label htmlFor="git-link-branch">
					{t("taskDetail.git.forms.existingBranch")}
				</Label>
				<Input
					id="git-link-branch"
					className="font-mono"
					value={value}
					placeholder="feature/foo"
					spellCheck={false}
					disabled={link.isPending}
					aria-invalid={!!error}
					autoComplete="off"
					// biome-ignore lint/a11y/noAutofocus: opened on demand by the user
					autoFocus
					onChange={(e) => {
						setValue(e.target.value);
						setError(null);
					}}
				/>
				<p className="text-xs text-muted-foreground">
					{parsed && urlRepo
						? t("taskDetail.git.forms.willLink", {
								what: parsed.branch,
								repository: urlRepo.full_path,
							})
						: t("taskDetail.git.forms.branchHint")}
				</p>
			</div>
			{!parsed && repos.length > 1 ? (
				<div className="flex flex-col gap-1.5">
					<Label htmlFor="git-link-branch-repo">
						{t("taskDetail.git.forms.repository")}
					</Label>
					<RepoSelect
						id="git-link-branch-repo"
						repos={repos}
						value={picked}
						disabled={link.isPending}
						onChange={(v) => {
							setPicked(v);
							setError(null);
						}}
					/>
				</div>
			) : null}
			{error ? (
				<p role="alert" className="text-xs text-destructive">
					{t(`taskDetail.git.errors.${error}` as never, {
						repository: parsed?.fullPath ?? "",
						branch,
					})}
				</p>
			) : null}
			<FormActions
				submitLabel={t("taskDetail.git.forms.linkBranch")}
				pending={link.isPending}
				canSubmit={!!value.trim()}
				onCancel={onDone}
				icon={<Link2 className="size-4" />}
			/>
		</form>
	);
}

// ── Create a branch ──────────────────────────────────────────────────────────

const BRANCH_TYPES = [
	"feat",
	"fix",
	"chore",
	"docs",
	"test",
	"refactor",
] as const;
type BranchType = (typeof BRANCH_TYPES)[number];

interface CreateBranchFormProps extends FormProps {
	taskIdPrefix: string;
	taskNumber: number;
	taskTitle?: string;
	/** The provider can create the branch itself (and the user may write). */
	canCreateOnPlatform: boolean;
}

/** "feat/PACA-12-fix-login-bug": type, the task key, and a slug of its title. */
export function suggestBranchName(
	type: BranchType,
	taskRef: string,
	title?: string,
): string {
	const slug = title
		? `-${title
				.toLowerCase()
				.replace(/[^a-z0-9]+/g, "-")
				.replace(/^-|-$/g, "")
				.slice(0, 30)
				.replace(/-$/, "")}`
		: "";
	return `${type}/${taskRef}${slug === "-" ? "" : slug}`;
}

export function CreateBranchForm({
	projectId,
	taskId,
	repos,
	onDone,
	taskIdPrefix,
	taskNumber,
	taskTitle,
	canCreateOnPlatform,
}: CreateBranchFormProps) {
	const { t } = useTranslation("projects");
	const qc = useQueryClient();
	const taskRef = taskIdPrefix
		? `${taskIdPrefix.toUpperCase()}-${taskNumber}`
		: `${taskNumber}`;
	const [type, setType] = useState<BranchType>("feat");
	const [name, setName] = useState(
		suggestBranchName("feat", taskRef, taskTitle),
	);
	const [picked, setPicked] = useState(repos.length === 1 ? repos[0].id : "");
	const [from, setFrom] = useState("");
	const [error, setError] = useState<string | null>(null);
	const [copied, setCopied] = useState(false);

	function changeType(next: BranchType) {
		setType(next);
		setName((prev) => {
			const slash = prev.indexOf("/");
			return `${next}${slash >= 0 ? prev.slice(slash) : `/${taskRef}`}`;
		});
	}

	const create = useMutation({
		mutationFn: () =>
			createTaskBranch(projectId, taskId, {
				repository_id: picked,
				branch: name.trim(),
				from: from.trim() || undefined,
			}),
		onSuccess: async () => {
			await qc.invalidateQueries({ queryKey: taskGitKey(projectId, taskId) });
			onDone();
		},
		onError: (err: unknown) => {
			const code = getApiErrorCodeSafe(err);
			setError(code === "BAD_REQUEST" ? "createRefused" : errorKey(err));
		},
	});

	function submit() {
		if (!name.trim()) return setError("branchRequired");
		if (!picked) return setError("selectRepository");
		setError(null);
		create.mutate();
	}

	const local = `git checkout -b ${name.trim()} && git push -u origin ${name.trim()}`;

	return (
		<form
			className={FORM_CLASS}
			onSubmit={(e) => {
				e.preventDefault();
				submit();
			}}
			onKeyDown={(e) => e.key === "Escape" && onDone()}
		>
			<fieldset className="flex flex-col gap-1.5">
				<legend className="mb-1.5 text-sm font-medium">
					{t("taskDetail.git.forms.branchType")}
				</legend>
				<div className="flex flex-wrap gap-1.5">
					{BRANCH_TYPES.map((b) => (
						<Button
							key={b}
							type="button"
							size="sm"
							variant={b === type ? "default" : "outline"}
							aria-pressed={b === type}
							onClick={() => changeType(b)}
						>
							{b}
						</Button>
					))}
				</div>
			</fieldset>
			<div className="flex flex-col gap-1.5">
				<Label htmlFor="git-new-branch">
					{t("taskDetail.git.forms.branchName")}
				</Label>
				<Input
					id="git-new-branch"
					className="font-mono"
					value={name}
					spellCheck={false}
					autoComplete="off"
					onChange={(e) => {
						setName(e.target.value);
						setError(null);
					}}
				/>
			</div>
			{repos.length > 1 ? (
				<div className="flex flex-col gap-1.5">
					<Label htmlFor="git-new-branch-repo">
						{t("taskDetail.git.forms.repository")}
					</Label>
					<RepoSelect
						id="git-new-branch-repo"
						repos={repos}
						value={picked}
						onChange={(v) => {
							setPicked(v);
							setError(null);
						}}
					/>
				</div>
			) : null}
			{canCreateOnPlatform ? (
				<div className="flex flex-col gap-1.5">
					<Label htmlFor="git-new-branch-from">
						{t("taskDetail.git.forms.branchFrom")}
					</Label>
					<Input
						id="git-new-branch-from"
						className="font-mono"
						value={from}
						placeholder="main"
						spellCheck={false}
						autoComplete="off"
						onChange={(e) => setFrom(e.target.value)}
					/>
					<p className="text-xs text-muted-foreground">
						{t("taskDetail.git.forms.branchFromHint")}
					</p>
				</div>
			) : null}
			{error ? (
				<p role="alert" className="text-xs text-destructive">
					{t(`taskDetail.git.errors.${error}` as never, {
						branch: name.trim(),
					})}
				</p>
			) : null}
			{canCreateOnPlatform ? (
				<FormActions
					submitLabel={t("taskDetail.git.forms.createBranch")}
					pending={create.isPending}
					canSubmit={!!name.trim()}
					onCancel={onDone}
					icon={<GitBranch className="size-4" />}
				/>
			) : (
				<div>
					<Button type="button" size="sm" variant="ghost" onClick={onDone}>
						{t("taskDetail.git.forms.cancel")}
					</Button>
				</div>
			)}
			<Accordion className="text-sm">
				<AccordionItem value="item" className="border-b-0">
					<AccordionTrigger className="py-0 text-muted-foreground">
						{t("taskDetail.git.forms.orLocally")}
					</AccordionTrigger>
					<AccordionContent keepMounted className="pb-0 pt-0">
						<div className="mt-2 flex items-center gap-2 rounded-md bg-muted px-3 py-2">
							<code className="min-w-0 flex-1 break-all text-xs">{local}</code>
							<Button
								type="button"
								size="sm"
								variant="outline"
								onClick={() => {
									void navigator.clipboard?.writeText(local);
									setCopied(true);
								}}
							>
								{copied ? t("taskDetail.git.copied") : t("taskDetail.git.copy")}
							</Button>
						</div>
					</AccordionContent>
				</AccordionItem>
			</Accordion>
		</form>
	);
}
