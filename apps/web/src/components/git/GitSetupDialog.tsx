import { useMutation, useQueryClient } from "@tanstack/react-query";
import { GitBranch, Loader2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
	type BeginGitSetupPayload,
	beginGitSetup,
	beginProjectGitSetup,
	completeGitSetup,
	completeProjectGitSetup,
	connectionKindsOf,
	followGitStep,
	type GitConnectionKind,
	type GitProvider,
	type GitSetupStep,
	gitErrorKey,
	gitQueriesKey,
	projectGitSettingsPath,
	rememberGitFlow,
} from "@/lib/git-api";
import { ChoiceCard } from "./git-ui";

/** Who may use a connection: one project, or every project. */
type Visibility = "project" | "workspace";

interface GitSetupDialogProps {
	provider: GitProvider;
	host: string;
	open: boolean;
	onOpenChange: (open: boolean) => void;
	/**
	 * Starts an agent's own identity; takes the agent's endpoint. Leave it out
	 * and pass `project` to add a connection from a project's settings.
	 */
	begin?: (payload: BeginGitSetupPayload) => Promise<GitSetupStep>;
	/**
	 * Adds a connection from a project's settings. The user chooses whether it
	 * serves this project only or every project, among what they may do.
	 */
	project?: {
		id: string;
		/** git.connections:manage on the project. */
		canProject: boolean;
		/** git.identities:manage, the workspace-level action. */
		canWorkspace: boolean;
	};
	/** Where the browser returns when setup is finished (a same-origin path). */
	returnPath?: string;
	/** Query cache entry to refresh when a token form finishes in place. */
	invalidateKey?: readonly unknown[];
}

/**
 * Starts creating a connection to one provider host. The provider decides what
 * happens next: a redirect or a form post sends the browser to the platform
 * (which returns to /git/setup/callback), a token form is answered right here.
 * The optional name and organization are passed through as provider parameters;
 * a provider that has no use for them ignores them.
 */
export function GitSetupDialog({
	provider,
	host: defaultHost,
	open,
	onOpenChange,
	begin,
	project,
	returnPath = "/home",
	invalidateKey = gitQueriesKey,
}: GitSetupDialogProps) {
	const { t } = useTranslation("admin");
	const queryClient = useQueryClient();
	const kinds = connectionKindsOf(provider);
	const [kind, setKind] = useState<GitConnectionKind>(kinds[0]);
	const [host, setHost] = useState(defaultHost);
	const [visibility, setVisibility] = useState<Visibility>(
		project && !project.canProject && project.canWorkspace
			? "workspace"
			: "project",
	);
	const [name, setName] = useState("");
	const [org, setOrg] = useState("");
	const [isPublic, setIsPublic] = useState(false);
	const [error, setError] = useState<string | null>(null);
	const [tokenStep, setTokenStep] = useState<GitSetupStep | null>(null);
	const [tokenValues, setTokenValues] = useState<Record<string, string>>({});

	const forProject = project && visibility === "project" ? project : null;
	const target = forProject
		? { kind: "project-identity" as const, projectId: forProject.id }
		: { kind: "identity" as const };
	const back = forProject ? projectGitSettingsPath(forProject.id) : returnPath;

	const start = useMutation({
		mutationFn: () => {
			const params: Record<string, string> = {};
			if (kinds.length > 1) params.kind = kind;
			if (kind === "app") {
				if (name.trim()) params.name = name.trim();
				if (org.trim()) params.owner_org = org.trim();
				if (isPublic) params.public = "true";
			}
			const payload = {
				provider_id: provider.id,
				host: host.trim() || defaultHost,
				return_url: `${window.location.origin}${back}`,
				params,
			};
			if (begin) return begin(payload);
			return forProject
				? beginProjectGitSetup(forProject.id, payload)
				: beginGitSetup(payload);
		},
		onSuccess: (step) => {
			if (step.kind === "token_form") {
				setTokenStep(step);
				return;
			}
			if (step.kind === "done") {
				void queryClient.invalidateQueries({ queryKey: invalidateKey });
				onOpenChange(false);
				return;
			}
			rememberGitFlow(step.state, target);
			// Leaves the page; the platform returns to /git/setup/callback.
			if (!followGitStep(step)) setError(t("git.errors.generic"));
		},
		onError: (err: unknown) => {
			setError(t(`git.errors.${gitErrorKey(err)}` as never));
		},
	});

	const submitToken = useMutation({
		mutationFn: () => {
			const state = tokenStep?.state ?? "";
			return forProject
				? completeProjectGitSetup(forProject.id, state, tokenValues)
				: completeGitSetup(state, tokenValues);
		},
		onSuccess: () => {
			void queryClient.invalidateQueries({ queryKey: invalidateKey });
			onOpenChange(false);
		},
		onError: (err: unknown) => {
			setError(t(`git.errors.${gitErrorKey(err)}` as never));
		},
	});

	const busy = start.isPending || submitToken.isPending;
	const canStart = !project || project.canProject || project.canWorkspace;
	const title = t("git.setup.dialogTitle", {
		provider: provider.display_name,
		host,
	});
	const description =
		tokenStep?.prompt ??
		(kind === "app"
			? t("git.setup.dialogDescription", {
					host,
					changeRequests: provider.terms.change_request_plural.toLowerCase(),
				})
			: t("git.setup.tokenDescription", { host }));

	return (
		<Dialog
			open={open}
			onOpenChange={(next) => {
				if (!next) {
					setError(null);
					setTokenStep(null);
					setTokenValues({});
				}
				onOpenChange(next);
			}}
		>
			<DialogContent className="sm:max-w-md">
				<form
					className="flex min-w-0 flex-col gap-4"
					onSubmit={(e) => {
						e.preventDefault();
						setError(null);
						if (tokenStep) submitToken.mutate();
						else start.mutate();
					}}
				>
					<DialogHeader>
						<div className="mb-1 flex size-9 items-center justify-center rounded-lg bg-primary/10">
							<GitBranch className="size-4 text-primary" />
						</div>
						<DialogTitle>{title}</DialogTitle>
						<DialogDescription className="mt-1 wrap-anywhere">
							{description}
						</DialogDescription>
					</DialogHeader>

					{tokenStep ? (
						(tokenStep.form ?? []).map((f) => (
							<div key={f.name} className="flex flex-col gap-1.5">
								<Label htmlFor={`git-setup-${f.name}`}>{f.label}</Label>
								<Input
									id={`git-setup-${f.name}`}
									type={f.secret ? "password" : "text"}
									autoComplete="off"
									required={f.required}
									value={tokenValues[f.name] ?? ""}
									onChange={(e) =>
										setTokenValues((v) => ({ ...v, [f.name]: e.target.value }))
									}
								/>
								{f.help ? (
									<p className="text-xs text-muted-foreground">{f.help}</p>
								) : null}
							</div>
						))
					) : (
						<>
							{provider.self_hosted_hosts ? (
								<div className="flex flex-col gap-1.5">
									<Label htmlFor="git-setup-host">{t("git.setup.host")}</Label>
									<Input
										id="git-setup-host"
										value={host}
										autoComplete="off"
										onChange={(e) => setHost(e.target.value)}
									/>
									<p className="text-xs text-muted-foreground">
										{t("git.setup.hostHelp")}
									</p>
								</div>
							) : null}

							{kinds.length > 1 ? (
								<fieldset className="flex flex-col gap-2">
									<legend className="mb-1 text-sm font-medium">
										{t("git.setup.kind.label")}
									</legend>
									{kinds.map((k) => (
										<ChoiceCard
											key={k}
											name="git-setup-kind"
											value={k}
											checked={kind === k}
											onSelect={() => setKind(k)}
											title={t(`git.setup.kind.${k}.title` as never)}
											description={t(`git.setup.kind.${k}.help` as never, {
												provider: provider.display_name,
											})}
										/>
									))}
								</fieldset>
							) : null}

							{project ? (
								<fieldset className="flex flex-col gap-2">
									<legend className="mb-1 text-sm font-medium">
										{t("git.setup.scope.label")}
									</legend>
									<ChoiceCard
										name="git-setup-scope"
										value="project"
										checked={visibility === "project"}
										disabled={!project.canProject}
										onSelect={() => setVisibility("project")}
										title={t("git.setup.scope.project.title")}
										description={t("git.setup.scope.project.help")}
									/>
									<ChoiceCard
										name="git-setup-scope"
										value="workspace"
										checked={visibility === "workspace"}
										disabled={!project.canWorkspace}
										onSelect={() => setVisibility("workspace")}
										title={t("git.setup.scope.workspace.title")}
										description={
											project.canWorkspace
												? t("git.setup.scope.workspace.help")
												: t("git.setup.scope.workspace.noPermission")
										}
									/>
									{!canStart ? (
										<p className="text-xs text-destructive">
											{t("git.setup.scope.none")}
										</p>
									) : null}
								</fieldset>
							) : null}

							{kind === "app" ? (
								<>
									<div className="flex flex-col gap-1.5">
										<Label htmlFor="git-setup-name">
											{t("git.setup.name")}
										</Label>
										<Input
											id="git-setup-name"
											value={name}
											maxLength={34}
											placeholder="Paca"
											onChange={(e) => setName(e.target.value)}
										/>
										<p className="text-xs text-muted-foreground">
											{t("git.setup.nameHelp")}
										</p>
									</div>
									<div className="flex flex-col gap-1.5">
										<Label htmlFor="git-setup-org">{t("git.setup.org")}</Label>
										<Input
											id="git-setup-org"
											value={org}
											onChange={(e) => setOrg(e.target.value)}
										/>
										<p className="text-xs text-muted-foreground">
											{t("git.setup.orgHelp")}
										</p>
									</div>
									<div className="flex items-start gap-3">
										<Switch
											id="git-setup-public"
											checked={isPublic}
											onCheckedChange={setIsPublic}
										/>
										<div className="flex flex-col gap-1">
											<Label htmlFor="git-setup-public">
												{t("git.setup.public")}
											</Label>
											<p className="text-xs text-muted-foreground">
												{t("git.setup.publicHelp")}
											</p>
										</div>
									</div>
								</>
							) : null}
						</>
					)}

					{error ? (
						<div
							role="alert"
							className="flex items-center gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive"
						>
							<span className="shrink-0">⚠</span>
							<span>{error}</span>
						</div>
					) : null}

					<DialogFooter>
						<DialogClose
							render={
								<Button type="button" variant="outline" disabled={busy} />
							}
						>
							{t("git.cancel")}
						</DialogClose>
						<Button type="submit" disabled={busy || !canStart}>
							{busy ? <Loader2 className="size-4 animate-spin" /> : null}
							{tokenStep
								? t("git.setup.submitToken")
								: kind === "app"
									? t("git.setup.start", { host })
									: t("git.setup.next")}
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}
