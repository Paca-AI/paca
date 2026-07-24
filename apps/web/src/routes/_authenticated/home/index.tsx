import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
	ArrowRight,
	Bot,
	Calendar,
	FolderKanban,
	GitMerge,
	Globe,
	Layers,
	Loader2,
	Lock,
	Plus,
	Users,
	Zap,
} from "lucide-react";
import { type ComponentType, useState } from "react";
import { useTranslation } from "react-i18next";

import { AssignedTasksList } from "@/components/home/assigned-tasks-list";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { usePermissions } from "@/hooks/use-permissions";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { assignedTasksQueryOptions } from "@/lib/interaction-api";
import {
	createProject,
	type Project,
	projectsQueryOptions,
	workspaceStatsQueryOptions,
} from "@/lib/project-api";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/_authenticated/home/")({
	loader: async ({ context: { queryClient } }) => {
		await Promise.all([
			queryClient.ensureQueryData(projectsQueryOptions()),
			queryClient.ensureQueryData(workspaceStatsQueryOptions()),
			queryClient.ensureInfiniteQueryData(assignedTasksQueryOptions()),
		]);
	},
	component: HomePage,
});

// ── Create Project Dialog ─────────────────────────────────────────────────────

/** Mirrors the backend suggestPrefix logic: first letter of each word (up to 4)
 *  or first 4 chars of a single word, uppercased. */
function suggestPrefix(name: string): string {
	const clean = name.replace(/[^a-zA-Z0-9 ]/g, " ").trim();
	const words = clean.split(/\s+/).filter(Boolean);
	if (words.length === 0) return "";
	if (words.length === 1) return words[0].slice(0, 4).toUpperCase();
	return words
		.slice(0, 4)
		.map((w) => w[0])
		.join("")
		.toUpperCase();
}

function CreateProjectDialog({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const { t } = useTranslation("shared");
	const queryClient = useQueryClient();
	const [name, setName] = useState("");
	const [description, setDescription] = useState("");
	const [prefix, setPrefix] = useState("");
	const [isPublic, setIsPublic] = useState(false);
	const [prefixTouched, setPrefixTouched] = useState(false);
	const [nameError, setNameError] = useState<string | null>(null);
	const [prefixError, setPrefixError] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);

	const reset = () => {
		setName("");
		setDescription("");
		setPrefix("");
		setIsPublic(false);
		setPrefixTouched(false);
		setNameError(null);
		setPrefixError(null);
		setError(null);
	};

	const mutation = useMutation({
		mutationFn: () => {
			if (!name.trim()) throw new Error("name_required");
			return createProject({
				name: name.trim(),
				description: description.trim() || undefined,
				task_id_prefix: prefix.trim() || undefined,
				is_public: isPublic,
			});
		},
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: ["projects"] });
			onOpenChange(false);
			reset();
		},
		onError: (err: unknown) => {
			setNameError(null);
			setPrefixError(null);
			setError(null);
			if ((err as Error).message === "name_required") {
				setNameError(t("home.createDialog.errors.nameRequired"));
				return;
			}
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.ProjectNameTaken) {
				setNameError(t("home.createDialog.errors.nameTaken"));
				return;
			}
			if (code === ApiErrorCode.ProjectNameInvalid) {
				setNameError(t("home.createDialog.errors.nameInvalid"));
				return;
			}
			if (code === ApiErrorCode.ProjectPrefixInvalid) {
				setPrefixError(t("home.createDialog.errors.prefixInvalid"));
				return;
			}
			setError(t("home.createDialog.errors.generic"));
		},
	});

	return (
		<Dialog
			open={open}
			onOpenChange={(o) => {
				onOpenChange(o);
				if (!o) reset();
			}}
		>
			<DialogContent className="sm:max-w-md">
				<DialogHeader>
					<div className="flex size-10 items-center justify-center rounded-xl bg-primary/10 mb-2">
						<FolderKanban className="size-5 text-primary" />
					</div>
					<DialogTitle className="font-[Syne]">
						{t("home.createDialog.title")}
					</DialogTitle>
					<DialogDescription>
						{t("home.createDialog.description")}
					</DialogDescription>
				</DialogHeader>
				<div className="space-y-4">
					<div className="space-y-1.5">
						<Label htmlFor="project-name">
							{t("home.createDialog.nameLabel")}
						</Label>
						<Input
							id="project-name"
							value={name}
							onChange={(e) => {
								const val = e.target.value;
								setName(val);
								setNameError(null);
								if (!prefixTouched) {
									setPrefix(suggestPrefix(val));
								}
							}}
							placeholder={t("home.createDialog.namePlaceholder")}
							autoFocus
							className={
								nameError
									? "border-destructive focus-visible:ring-destructive/30"
									: ""
							}
							onKeyDown={(e) => {
								if (e.key === "Enter") mutation.mutate();
							}}
						/>
						{nameError ? (
							<p className="text-xs text-destructive">{nameError}</p>
						) : null}
					</div>
					<div className="space-y-1.5">
						<Label htmlFor="project-prefix">
							{t("home.createDialog.prefixLabel")}{" "}
							<span className="text-muted-foreground font-normal">
								{t("home.createDialog.prefixHint")}
							</span>
						</Label>
						<div className="flex items-center gap-2">
							<Input
								id="project-prefix"
								value={prefix}
								onChange={(e) => {
									setPrefix(
										e.target.value
											.toUpperCase()
											.replace(/[^A-Z0-9]/g, "")
											.slice(0, 10),
									);
									setPrefixTouched(true);
									setPrefixError(null);
								}}
								placeholder={t("home.createDialog.prefixPlaceholder")}
								className={cn(
									"font-[JetBrains_Mono,monospace] uppercase w-32",
									prefixError
										? "border-destructive focus-visible:ring-destructive/30"
										: "",
								)}
								maxLength={10}
							/>
							{prefix ? (
								<span className="text-xs text-muted-foreground">
									{t("home.createDialog.prefixExampleLead")}{" "}
									<span className="font-[JetBrains_Mono,monospace] font-semibold text-foreground">
										{prefix}-1
									</span>
									,{" "}
									<span className="font-[JetBrains_Mono,monospace] font-semibold text-foreground">
										{prefix}-2
									</span>
									…
								</span>
							) : null}
						</div>
						{prefixError ? (
							<p className="text-xs text-destructive">{prefixError}</p>
						) : null}
					</div>
					<div className="space-y-1.5">
						<Label htmlFor="project-description">
							{t("home.createDialog.descriptionLabel")}{" "}
							<span className="text-muted-foreground font-normal">
								{t("home.createDialog.optionalHint")}
							</span>
						</Label>
						<Textarea
							id="project-description"
							value={description}
							onChange={(e) => setDescription(e.target.value)}
							placeholder={t("home.createDialog.descriptionPlaceholder")}
							rows={3}
							className="resize-none"
						/>
					</div>
					<div className="flex items-center justify-between rounded-lg border border-border/60 bg-muted/20 px-4 py-3">
						<div className="flex items-start gap-3">
							<div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 border border-primary/15">
								{isPublic ? (
									<Globe className="size-4 text-primary" />
								) : (
									<Lock className="size-4 text-primary" />
								)}
							</div>
							<div>
								<Label
									htmlFor="is-public"
									className="font-medium cursor-pointer"
								>
									{t("home.createDialog.publicProject.label")}
								</Label>
								<p className="text-xs text-muted-foreground mt-0.5 leading-relaxed">
									{isPublic
										? t("home.createDialog.publicProject.publicHint")
										: t("home.createDialog.publicProject.privateHint")}
								</p>
							</div>
						</div>
						<Switch
							id="is-public"
							checked={isPublic}
							onCheckedChange={setIsPublic}
						/>
					</div>
					{error ? (
						<p className="text-xs text-destructive bg-destructive/10 rounded-lg px-3 py-2">
							{error}
						</p>
					) : null}
				</div>
				<DialogFooter>
					<DialogClose
						render={
							<Button
								variant="outline"
								size="sm"
								disabled={mutation.isPending}
							/>
						}
					>
						{t("home.createDialog.cancel")}
					</DialogClose>
					<Button
						size="sm"
						disabled={mutation.isPending || !name.trim()}
						onClick={() => mutation.mutate()}
						className="gap-1.5 shadow-sm shadow-primary/20"
					>
						{mutation.isPending ? (
							<Loader2 className="size-3.5 animate-spin" />
						) : (
							<Plus className="size-3.5" />
						)}
						{t("home.createDialog.submit")}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Project Card ──────────────────────────────────────────────────────────────

function ProjectCard({ project }: { project: Project }) {
	const { t } = useTranslation("shared");
	const initials = project.name
		.split(/\s+/)
		.filter(Boolean)
		.slice(0, 2)
		.map((w) => w[0].toUpperCase())
		.join("");

	const formattedDate = new Date(project.created_at).toLocaleDateString(
		"en-US",
		{ month: "short", day: "numeric", year: "numeric" },
	);

	return (
		<Link
			to="/projects/$projectId"
			params={{ projectId: project.id }}
			className="group relative flex flex-col rounded-xl border border-border/60 bg-card p-5 transition-all duration-200 hover:border-primary/40 hover:shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
		>
			<div className="absolute inset-x-0 top-0 h-px rounded-t-xl bg-primary/40 opacity-0 transition-opacity duration-200 group-hover:opacity-100" />
			<div className="flex items-start gap-3.5 mb-4">
				<div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 border border-primary/15 font-[Syne] text-sm font-bold text-primary">
					{initials || <FolderKanban className="size-4" />}
				</div>
				<div className="min-w-0 flex-1">
					<div className="flex items-center gap-2 mb-0.5">
						<p className="font-[Syne] text-sm font-bold truncate leading-snug">
							{project.name}
						</p>
						{project.is_public && (
							<Badge
								variant="secondary"
								className="gap-1 px-1.5 py-0 text-xs font-medium border border-border/60 shrink-0"
							>
								<Globe className="size-3" />
								{t("home.projectCard.public")}
							</Badge>
						)}
					</div>
					{project.description ? (
						<p className="mt-0.5 text-xs text-muted-foreground line-clamp-2 leading-relaxed">
							{project.description}
						</p>
					) : (
						<p className="mt-0.5 text-xs text-muted-foreground/40 italic">
							{t("home.projectCard.noDescription")}
						</p>
					)}
				</div>
			</div>
			<div className="mt-auto flex items-center gap-2 text-xs text-muted-foreground">
				<Calendar className="size-3 shrink-0" />
				<span>{formattedDate}</span>
				<ArrowRight className="size-3 ml-auto opacity-0 transition-all duration-150 group-hover:opacity-60 group-hover:translate-x-0.5" />
			</div>
		</Link>
	);
}

// ── Stat Card ─────────────────────────────────────────────────────────────────

function StatCard({
	icon: Icon,
	label,
	value,
	sub,
	iconClass,
}: {
	icon: ComponentType<{ className?: string }>;
	label: string;
	value: string | number;
	sub: string;
	iconClass: string;
}) {
	return (
		<Card className="relative overflow-hidden border-border/60">
			<div className="absolute inset-x-0 top-0 h-px bg-primary/40" />
			<CardContent className="p-5">
				<div
					className={`flex size-9 items-center justify-center rounded-[10px] ${iconClass}`}
				>
					<Icon className="size-4" />
				</div>
				<div className="mt-4">
					<p className="font-mono text-4xl font-semibold tracking-tight tabular-nums">
						{value}
					</p>
					<p className="mt-1 text-sm font-medium text-foreground/80">{label}</p>
					<p className="mt-0.5 text-xs text-muted-foreground">{sub}</p>
				</div>
			</CardContent>
		</Card>
	);
}

const GETTING_STARTED = [
	{
		step: 1,
		titleKey: "home.gettingStarted.steps.createProject.title",
		descriptionKey: "home.gettingStarted.steps.createProject.description",
	},
	{
		step: 2,
		titleKey: "home.gettingStarted.steps.inviteTeam.title",
		descriptionKey: "home.gettingStarted.steps.inviteTeam.description",
	},
	{
		step: 3,
		titleKey: "home.gettingStarted.steps.configureAgent.title",
		descriptionKey: "home.gettingStarted.steps.configureAgent.description",
	},
	{
		step: 4,
		titleKey: "home.gettingStarted.steps.runSprint.title",
		descriptionKey: "home.gettingStarted.steps.runSprint.description",
	},
] as const;

// ── Home Page ─────────────────────────────────────────────────────────────────

function HomePage() {
	const { t } = useTranslation("shared");
	const { data: user } = useQuery(currentUserQueryOptions);
	const { data: projectsResult } = useQuery(projectsQueryOptions());
	const { data: workspaceStats } = useQuery(workspaceStatsQueryOptions());
	const { hasPermission } = usePermissions();
	const [createOpen, setCreateOpen] = useState(false);

	const canCreate = hasPermission("projects.create");

	const projects = projectsResult?.items ?? [];
	const projectCount = projectsResult?.total ?? 0;
	const openTaskCount = workspaceStats?.open_task_count ?? 0;
	const teamMemberCount = workspaceStats?.team_member_count ?? 0;
	const aiAgentCount = workspaceStats?.ai_agent_count ?? 0;

	const greeting = (() => {
		const hour = new Date().getHours();
		if (hour < 12) return t("home.greeting.morning");
		if (hour < 18) return t("home.greeting.afternoon");
		return t("home.greeting.evening");
	})();

	const displayName =
		user?.full_name || user?.username || t("home.greeting.fallbackName");

	return (
		<div className="flex flex-col">
			{/* Hero banner */}
			<div className="relative overflow-hidden border-b border-border/50">
				<div
					className="pointer-events-none absolute inset-0"
					style={{
						backgroundImage:
							"radial-gradient(circle, color-mix(in oklch, var(--color-primary) 18%, transparent) 1px, transparent 1px)",
						backgroundSize: "22px 22px",
						maskImage:
							"radial-gradient(ellipse 90% 100% at 0% 0%, black 30%, transparent 80%)",
					}}
				/>
				<div className="pointer-events-none absolute -top-16 -left-16 size-72 rounded-full bg-primary/10 blur-3xl" />
				<div className="pointer-events-none absolute -bottom-8 right-8 size-52 rounded-full bg-secondary/10 blur-3xl" />
				<div className="relative flex flex-col gap-4 px-6 py-10 sm:flex-row sm:items-end sm:justify-between">
					<div>
						<div className="mb-3 flex items-center gap-2">
							<Badge
								variant="secondary"
								className="gap-1.5 px-2.5 py-0.5 text-xs font-semibold border border-border/60"
							>
								{t("home.hero.badge")}
							</Badge>
						</div>
						<h1 className="font-[Syne] text-3xl font-bold tracking-tight leading-tight">
							{greeting},{" "}
							<span
								className="bg-clip-text text-transparent"
								style={{
									backgroundImage:
										"linear-gradient(135deg, var(--color-primary) 0%, color-mix(in oklch, var(--color-primary) 70%, var(--color-secondary)) 100%)",
								}}
							>
								{displayName}
							</span>
						</h1>
						<p className="mt-2 max-w-lg text-sm text-muted-foreground leading-relaxed">
							{projectCount > 0
								? t("home.hero.subtitleWithProjects", { count: projectCount })
								: t("home.hero.subtitleEmpty")}
						</p>
					</div>
					<div className="flex shrink-0 items-center gap-2">
						{canCreate ? (
							<Button
								size="sm"
								className="gap-1.5 shadow-sm shadow-primary/20"
								onClick={() => setCreateOpen(true)}
							>
								<Plus className="size-3.5" />
								{t("home.hero.newProject")}
							</Button>
						) : null}
					</div>
				</div>
			</div>

			<div className="flex flex-col gap-6 p-6">
				{/* Stats row */}
				<div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
					<StatCard
						icon={FolderKanban}
						label={t("home.stats.projects.label")}
						value={projectCount}
						sub={
							projectCount === 0
								? t("home.stats.projects.subEmpty")
								: t("home.stats.projects.subActive", { count: projectCount })
						}
						iconClass="bg-primary/10 text-primary"
					/>
					<StatCard
						icon={Layers}
						label={t("home.stats.openTasks.label")}
						value={openTaskCount}
						sub={t("home.stats.openTasks.sub")}
						iconClass="bg-primary/10 text-primary"
					/>
					<StatCard
						icon={Users}
						label={t("home.stats.teamMembers.label")}
						value={teamMemberCount}
						sub={t("home.stats.teamMembers.sub")}
						iconClass="bg-muted text-muted-foreground"
					/>
					<StatCard
						icon={Bot}
						label={t("home.stats.aiAgents.label")}
						value={aiAgentCount}
						sub={
							aiAgentCount === 0
								? t("home.stats.aiAgents.sub")
								: t("home.stats.aiAgents.subActive", { count: aiAgentCount })
						}
						iconClass="bg-muted text-muted-foreground"
					/>
				</div>

				{/* My Tasks — assigned to the current user across every project */}
				{projectCount > 0 ? (
					<div>
						<div className="mb-4">
							<h2 className="font-[Syne] text-base font-bold tracking-tight">
								{t("home.myTasks.title")}
							</h2>
							<p className="text-xs text-muted-foreground mt-0.5">
								{t("home.myTasks.subtitle")}
							</p>
						</div>
						<AssignedTasksList />
					</div>
				) : null}

				{/* Projects section or empty state */}
				{projectCount > 0 ? (
					<div>
						<div className="flex items-center justify-between mb-4">
							<div>
								<h2 className="font-[Syne] text-base font-bold tracking-tight">
									{t("home.projectsSection.title")}
								</h2>
								<p className="text-xs text-muted-foreground mt-0.5">
									{t("home.projectsSection.countInWorkspace", {
										count: projectCount,
									})}
								</p>
							</div>
							{canCreate ? (
								<Button
									size="sm"
									variant="outline"
									className="gap-1.5 border-border/60"
									onClick={() => setCreateOpen(true)}
								>
									<Plus className="size-3.5" />
									{t("home.projectsSection.newShort")}
								</Button>
							) : null}
						</div>
						<div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
							{projects.map((project) => (
								<ProjectCard key={project.id} project={project} />
							))}
							{/* Add project card */}
							{canCreate ? (
								<button
									type="button"
									onClick={() => setCreateOpen(true)}
									className="group flex min-h-30 flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-border/60 bg-muted/10 p-5 text-muted-foreground/50 transition-all hover:border-primary/40 hover:bg-primary/5 hover:text-primary/70"
								>
									<div className="flex size-9 items-center justify-center rounded-xl border border-dashed border-current transition-colors">
										<Plus className="size-4" />
									</div>
									<span className="text-xs font-medium">
										{t("home.projectsSection.newProjectCard")}
									</span>
								</button>
							) : null}
						</div>
					</div>
				) : (
					<div className="grid gap-6 lg:grid-cols-[1fr_300px]">
						{/* Getting started checklist */}
						<Card className="border-border/60">
							<CardHeader className="pb-1">
								<div className="flex items-center justify-between">
									<CardTitle className="font-[Syne] text-base font-semibold">
										{t("home.gettingStarted.title")}
									</CardTitle>
									<Badge
										variant="outline"
										className="text-xs font-mono tabular-nums border-border/70"
									>
										{t("home.gettingStarted.progress", {
											completed: 0,
											total: 4,
										})}
									</Badge>
								</div>
								<p className="text-xs text-muted-foreground">
									{t("home.gettingStarted.subtitle")}
								</p>
							</CardHeader>
							<CardContent className="pt-3">
								<ol className="space-y-1">
									{GETTING_STARTED.map(({ step, titleKey, descriptionKey }) => {
										const isActionable = step === 1 && canCreate;
										const isLocked = step > 1;
										const inner = (
											<>
												<div className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full bg-primary/10 border border-primary/20 font-mono text-xs font-bold text-primary tabular-nums">
													{step}
												</div>
												<div className="min-w-0 flex-1">
													<p className="text-sm font-medium">{t(titleKey)}</p>
													<p className="mt-0.5 text-xs text-muted-foreground">
														{t(descriptionKey)}
													</p>
												</div>
												<ArrowRight
													className={cn(
														"mt-1 size-3.5 shrink-0 text-muted-foreground/30 transition-all",
														isActionable &&
															"group-hover:translate-x-0.5 group-hover:text-primary/50",
													)}
												/>
											</>
										);
										return (
											<li key={step}>
												{isActionable ? (
													<button
														type="button"
														onClick={() => setCreateOpen(true)}
														className="group flex w-full items-start gap-3.5 rounded-xl border border-border/40 bg-muted/20 px-4 py-3.5 text-left transition-all hover:bg-muted/50 hover:border-primary/20"
													>
														{inner}
													</button>
												) : (
													<div
														className={cn(
															"flex items-start gap-3.5 rounded-xl border border-border/40 bg-muted/20 px-4 py-3.5",
															isLocked && "opacity-50",
														)}
													>
														{inner}
													</div>
												)}
												{step < 4 && (
													<div className="my-0.5 ml-7 h-1.5 w-px bg-linear-to-b from-border/60 to-transparent" />
												)}
											</li>
										);
									})}
								</ol>
							</CardContent>
						</Card>

						{/* Right column: Quick actions + About */}
						<div className="flex flex-col gap-6">
							<Card className="border-border/60">
								<CardHeader className="pb-2">
									<CardTitle className="font-[Syne] text-base font-semibold">
										{t("home.quickActions.title")}
									</CardTitle>
								</CardHeader>
								<CardContent className="pt-0">
									<div className="space-y-2">
										{[
											...(canCreate
												? [
														{
															icon: FolderKanban,
															label: t("home.quickActions.newProject.label"),
															description: t(
																"home.quickActions.newProject.description",
															),
															onClick: () => setCreateOpen(true),
														},
													]
												: []),
											{
												icon: Users,
												label: t("home.quickActions.inviteTeam.label"),
												description: t(
													"home.quickActions.inviteTeam.description",
												),
												onClick: undefined,
											},
											{
												icon: Bot,
												label: t("home.quickActions.addAiAgent.label"),
												description: t(
													"home.quickActions.addAiAgent.description",
												),
												onClick: undefined,
											},
										].map(({ icon: Icon, label, description, onClick }) => (
											<button
												key={label}
												type="button"
												onClick={onClick}
												disabled={!onClick}
												className={cn(
													"group flex w-full items-center gap-3 rounded-xl border border-border/50 bg-background/50 px-3.5 py-3 text-left transition-all",
													onClick
														? "cursor-pointer hover:border-primary/30 hover:bg-primary/5 hover:shadow-sm hover:shadow-primary/5"
														: "cursor-default opacity-50",
												)}
											>
												<div
													className={cn(
														"flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary transition-colors",
														onClick && "group-hover:bg-primary/20",
													)}
												>
													<Icon className="size-4" />
												</div>
												<div className="min-w-0 flex-1">
													<p className="text-sm font-medium leading-none">
														{label}
													</p>
													<p className="mt-1 text-xs text-muted-foreground">
														{description}
													</p>
												</div>
												<ArrowRight
													className={cn(
														"size-3.5 shrink-0 text-muted-foreground/30 transition-all",
														onClick &&
															"group-hover:translate-x-0.5 group-hover:text-primary/60",
													)}
												/>
											</button>
										))}
									</div>
								</CardContent>
							</Card>

							<Card className="relative overflow-hidden border-border/60">
								<div className="absolute inset-x-0 top-0 h-px bg-primary/30" />
								<div className="pointer-events-none absolute -bottom-8 -right-8 size-32 rounded-full bg-primary/5" />
								<CardContent className="relative p-5">
									<div className="mb-2 flex items-center gap-2">
										<Zap className="size-3.5 text-primary" />
										<p className="font-[Syne] text-xs font-bold uppercase tracking-widest text-primary">
											{t("home.about.title")}
										</p>
									</div>
									<p className="text-sm leading-relaxed text-foreground/80">
										{t("home.about.descriptionLead")}{" "}
										<span className="font-semibold text-foreground">
											{t("home.about.workflow")}
										</span>{" "}
										{t("home.about.descriptionTrail")}
									</p>
									<Separator className="my-3 opacity-50" />
									<div className="flex items-center gap-2">
										<GitMerge className="size-3.5 text-muted-foreground" />
										<a
											href="https://github.com/Paca-AI/paca"
											target="_blank"
											rel="noopener noreferrer"
											className="text-xs text-muted-foreground transition-colors hover:text-foreground"
										>
											{t("home.about.openSource")}
										</a>
									</div>
								</CardContent>
							</Card>
						</div>
					</div>
				)}
			</div>

			<CreateProjectDialog open={createOpen} onOpenChange={setCreateOpen} />
		</div>
	);
}
