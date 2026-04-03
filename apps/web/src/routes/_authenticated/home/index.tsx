import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
	ArrowRight,
	Bot,
	Calendar,
	FolderKanban,
	GitMerge,
	Layers,
	Loader2,
	Plus,
	Users,
	Zap,
} from "lucide-react";
import { type ComponentType, useState } from "react";

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
import { Textarea } from "@/components/ui/textarea";
import { usePermissions } from "@/hooks/use-permissions";
import { ApiErrorCode, getApiErrorCode } from "@/lib/api-error";
import { currentUserQueryOptions } from "@/lib/auth-api";
import {
	createProject,
	type Project,
	projectsQueryOptions,
} from "@/lib/project-api";

export const Route = createFileRoute("/_authenticated/home/")({
	loader: async ({ context: { queryClient } }) => {
		await queryClient.ensureQueryData(projectsQueryOptions());
	},
	component: HomePage,
});

// ── Create Project Dialog ─────────────────────────────────────────────────────

function CreateProjectDialog({
	open,
	onOpenChange,
}: {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}) {
	const queryClient = useQueryClient();
	const [name, setName] = useState("");
	const [description, setDescription] = useState("");
	const [nameError, setNameError] = useState<string | null>(null);
	const [error, setError] = useState<string | null>(null);

	const reset = () => {
		setName("");
		setDescription("");
		setNameError(null);
		setError(null);
	};

	const mutation = useMutation({
		mutationFn: () => {
			if (!name.trim()) throw new Error("name_required");
			return createProject({
				name: name.trim(),
				description: description.trim() || undefined,
			});
		},
		onSuccess: async () => {
			await queryClient.invalidateQueries({ queryKey: ["projects"] });
			onOpenChange(false);
			reset();
		},
		onError: (err: unknown) => {
			setNameError(null);
			setError(null);
			if ((err as Error).message === "name_required") {
				setNameError("Project name is required.");
				return;
			}
			const code = getApiErrorCode(err);
			if (code === ApiErrorCode.ProjectNameTaken) {
				setNameError("A project with this name already exists.");
				return;
			}
			if (code === ApiErrorCode.ProjectNameInvalid) {
				setNameError("Project name is empty or invalid.");
				return;
			}
			setError("Something went wrong. Please try again.");
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
					<DialogTitle className="font-[Syne]">New project</DialogTitle>
					<DialogDescription>
						Create a scrumban workspace for your team. You can invite members
						and configure settings after creation.
					</DialogDescription>
				</DialogHeader>
				<div className="space-y-4">
					<div className="space-y-1.5">
						<Label htmlFor="project-name">Project name *</Label>
						<Input
							id="project-name"
							value={name}
							onChange={(e) => {
								setName(e.target.value);
								setNameError(null);
							}}
							placeholder="e.g. Platform v3"
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
						<Label htmlFor="project-description">
							Description{" "}
							<span className="text-muted-foreground font-normal">
								(optional)
							</span>
						</Label>
						<Textarea
							id="project-description"
							value={description}
							onChange={(e) => setDescription(e.target.value)}
							placeholder="What is this project about?"
							rows={3}
							className="resize-none"
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
						Cancel
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
						Create project
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

// ── Project Card ──────────────────────────────────────────────────────────────

function ProjectCard({ project }: { project: Project }) {
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
			className="group relative flex flex-col rounded-2xl border border-border/60 bg-card p-5 transition-all duration-200 hover:border-primary/40 hover:shadow-md hover:shadow-primary/5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
		>
			<div className="absolute inset-x-0 top-0 h-px rounded-t-2xl bg-linear-to-r from-transparent via-primary/30 to-transparent opacity-0 transition-opacity duration-200 group-hover:opacity-100" />
			<div className="flex items-start gap-3.5 mb-4">
				<div className="flex size-10 shrink-0 items-center justify-center rounded-xl bg-linear-to-br from-primary/20 to-primary/5 border border-primary/15 font-[Syne] text-sm font-bold text-primary">
					{initials || <FolderKanban className="size-4" />}
				</div>
				<div className="min-w-0 flex-1">
					<p className="font-[Syne] text-sm font-bold truncate leading-snug">
						{project.name}
					</p>
					{project.description ? (
						<p className="mt-0.5 text-xs text-muted-foreground line-clamp-2 leading-relaxed">
							{project.description}
						</p>
					) : (
						<p className="mt-0.5 text-xs text-muted-foreground/40 italic">
							No description
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
			<div className="absolute inset-x-0 top-0 h-0.5 bg-linear-to-r from-transparent via-primary/50 to-transparent" />
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
		title: "Create your first project",
		description: "Set up a scrumban board to manage your team's work.",
	},
	{
		step: 2,
		title: "Invite your team",
		description: "Add human collaborators and assign roles.",
	},
	{
		step: 3,
		title: "Configure an AI agent",
		description: "Connect an AI to handle tasks autonomously.",
	},
	{
		step: 4,
		title: "Run your first sprint",
		description: "Move tasks through the Plan → Act → Check → Adapt cycle.",
	},
] as const;

// ── Home Page ─────────────────────────────────────────────────────────────────

function HomePage() {
	const { data: user } = useQuery(currentUserQueryOptions);
	const { data: projectsResult } = useQuery(projectsQueryOptions());
	const { hasPermission } = usePermissions();
	const [createOpen, setCreateOpen] = useState(false);

	const canCreate = hasPermission("projects.create");

	const projects = projectsResult?.items ?? [];
	const projectCount = projectsResult?.total ?? 0;

	const greeting = (() => {
		const hour = new Date().getHours();
		if (hour < 12) return "Good morning";
		if (hour < 18) return "Good afternoon";
		return "Good evening";
	})();

	const displayName = user?.full_name || user?.username || "there";

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
								<span className="size-1.5 rounded-full bg-secondary inline-block" />
								Scrumban workspace
							</Badge>
						</div>
						<h1 className="font-[Syne] text-[2rem] font-bold tracking-tight leading-tight">
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
								? `You have ${projectCount} active ${projectCount === 1 ? "project" : "projects"}. Pick up where you left off.`
								: "Your workspace is ready. Create a project, invite your team, and start shipping."}
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
								New Project
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
						label="Projects"
						value={projectCount}
						sub={
							projectCount === 0 ? "No projects yet" : `${projectCount} active`
						}
						iconClass="bg-primary/10 text-primary"
					/>
					<StatCard
						icon={Layers}
						label="Open Tasks"
						value={0}
						sub="Across all projects"
						iconClass="bg-primary/10 text-primary"
					/>
					<StatCard
						icon={Users}
						label="Team Members"
						value={1}
						sub="Including you"
						iconClass="bg-secondary/15 text-secondary"
					/>
					<StatCard
						icon={Bot}
						label="AI Agents"
						value={0}
						sub="None configured"
						iconClass="bg-secondary/15 text-secondary"
					/>
				</div>

				{/* Projects section or empty state */}
				{projectCount > 0 ? (
					<div>
						<div className="flex items-center justify-between mb-4">
							<div>
								<h2 className="font-[Syne] text-base font-bold tracking-tight">
									Projects
								</h2>
								<p className="text-xs text-muted-foreground mt-0.5">
									{projectCount} {projectCount === 1 ? "project" : "projects"}{" "}
									in your workspace
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
									New
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
									className="group flex min-h-30 flex-col items-center justify-center gap-2 rounded-2xl border border-dashed border-border/60 bg-muted/10 p-5 text-muted-foreground/50 transition-all hover:border-primary/40 hover:bg-primary/3 hover:text-primary/70"
								>
									<div className="flex size-9 items-center justify-center rounded-xl border border-dashed border-current transition-colors">
										<Plus className="size-4" />
									</div>
									<span className="text-xs font-medium">New project</span>
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
										Get started
									</CardTitle>
									<Badge
										variant="outline"
										className="text-xs font-mono tabular-nums border-border/70"
									>
										0 / 4
									</Badge>
								</div>
								<p className="text-xs text-muted-foreground">
									Complete these steps to unlock the full power of Paca.
								</p>
							</CardHeader>
							<CardContent className="pt-3">
								<ol className="space-y-1">
									{GETTING_STARTED.map(({ step, title, description }) => (
										<li key={step}>
											<div className="flex items-start gap-3.5 rounded-xl border border-border/40 bg-muted/20 px-4 py-3.5 transition-all hover:bg-muted/50 hover:border-primary/20 group">
												<div className="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full bg-linear-to-br from-primary/20 to-primary/5 border border-primary/20 font-mono text-[11px] font-bold text-primary tabular-nums">
													{step}
												</div>
												<div className="min-w-0 flex-1">
													<p className="text-sm font-medium">{title}</p>
													<p className="mt-0.5 text-xs text-muted-foreground">
														{description}
													</p>
												</div>
												<ArrowRight className="mt-1 size-3.5 shrink-0 text-muted-foreground/30 transition-all group-hover:translate-x-0.5 group-hover:text-primary/50" />
											</div>
											{step < 4 && (
												<div className="my-0.5 ml-7 h-1.5 w-px bg-linear-to-b from-border/60 to-transparent" />
											)}
										</li>
									))}
								</ol>
							</CardContent>
						</Card>

						{/* Right column: Quick actions + About */}
						<div className="flex flex-col gap-6">
							<Card className="border-border/60">
								<CardHeader className="pb-2">
									<CardTitle className="font-[Syne] text-base font-semibold">
										Quick actions
									</CardTitle>
								</CardHeader>
								<CardContent className="pt-0">
									<div className="space-y-2">
										{[
											...(canCreate
												? [
														{
															icon: FolderKanban,
															label: "New Project",
															description: "Create a scrumban board",
															onClick: () => setCreateOpen(true),
														},
													]
												: []),
											{
												icon: Users,
												label: "Invite Team",
												description: "Add members or agents",
												onClick: undefined,
											},
											{
												icon: Bot,
												label: "Add AI Agent",
												description: "Configure automation",
												onClick: undefined,
											},
										].map(({ icon: Icon, label, description, onClick }) => (
											<button
												key={label}
												type="button"
												onClick={onClick}
												className="group flex w-full cursor-pointer items-center gap-3 rounded-xl border border-border/50 bg-background/50 px-3.5 py-3 text-left transition-all hover:border-primary/30 hover:bg-primary/5 hover:shadow-sm hover:shadow-primary/5"
											>
												<div className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-linear-to-br from-primary/15 to-primary/5 text-primary transition-colors group-hover:from-primary/25 group-hover:to-primary/10">
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
												<ArrowRight className="size-3.5 shrink-0 text-muted-foreground/30 transition-all group-hover:translate-x-0.5 group-hover:text-primary/60" />
											</button>
										))}
									</div>
								</CardContent>
							</Card>

							<Card className="relative overflow-hidden border-border/60">
								<div className="absolute inset-x-0 top-0 h-0.5 bg-linear-to-r from-transparent via-secondary/60 to-transparent" />
								<div className="pointer-events-none absolute -bottom-8 -right-8 size-32 rounded-full bg-secondary/10 blur-2xl" />
								<CardContent className="relative p-5">
									<div className="mb-2 flex items-center gap-2">
										<Zap className="size-3.5 text-secondary" />
										<p className="font-[Syne] text-xs font-bold uppercase tracking-widest text-secondary">
											How Paca works
										</p>
									</div>
									<p className="text-sm leading-relaxed text-foreground/80">
										Combine human creativity with AI speed on a shared scrumban
										board. Tasks flow through{" "}
										<span className="font-semibold text-foreground">
											Plan → Act → Check → Adapt
										</span>{" "}
										with full transparency over who — human or AI — did what.
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
											Open source · Apache-2.0
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
