import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import {
	BookOpen,
	FileText,
	LayoutDashboard,
	Settings,
	Sparkles,
	Users,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { projectQueryOptions } from "@/lib/project-api";

export const Route = createFileRoute("/_authenticated/projects/$projectId/")({
	component: ProjectDashboardPage,
});

const COMING_SOON_FEATURES = [
	{
		icon: LayoutDashboard,
		title: "Sprint Dashboard",
		description:
			"Track velocity, burndown, and cycle time across all your active sprints in real time.",
		color: "bg-violet-500/10 text-violet-500",
	},
	{
		icon: Sparkles,
		title: "AI Agent Activity",
		description:
			"Watch AI agents triage tickets, write code, and comment on pull requests — all from one view.",
		color: "bg-amber-500/10 text-amber-500",
	},
	{
		icon: FileText,
		title: "Release Notes",
		description:
			"Auto-generated changelogs and deployment timeline, updated with every sprint close.",
		color: "bg-emerald-500/10 text-emerald-500",
	},
] as const;

const PROJECT_PAGES = [
	{
		to: "/projects/$projectId/integrations",
		icon: BookOpen,
		label: "Integrations",
		description: "Product backlog & sprints",
	},
	{
		to: "/projects/$projectId/docs",
		icon: FileText,
		label: "Docs",
		description: "Project documentation",
	},
	{
		to: "/projects/$projectId/team",
		icon: Users,
		label: "Team",
		description: "Manage project members",
	},
	{
		to: "/projects/$projectId/settings",
		icon: Settings,
		label: "Settings",
		description: "Project settings & roles",
	},
] as const;

function ProjectDashboardPage() {
	const { projectId } = Route.useParams();
	const { data: project } = useQuery(projectQueryOptions(projectId));

	return (
		<div className="flex flex-col">
			{/* Hero */}
			<div className="relative overflow-hidden border-b border-border/50">
				<div
					className="pointer-events-none absolute inset-0"
					style={{
						backgroundImage:
							"radial-gradient(circle, color-mix(in oklch, var(--color-primary) 15%, transparent) 1px, transparent 1px)",
						backgroundSize: "24px 24px",
						maskImage:
							"radial-gradient(ellipse 80% 100% at 5% 0%, black 20%, transparent 75%)",
					}}
				/>
				<div className="pointer-events-none absolute -top-12 left-0 size-64 rounded-full bg-primary/8 blur-3xl" />
				<div className="relative px-6 py-10">
					<div className="mb-3 flex items-center gap-2">
						<Badge
							variant="secondary"
							className="gap-1.5 px-2.5 py-0.5 text-xs font-semibold border border-border/60"
						>
							<span className="size-1.5 rounded-full bg-primary inline-block animate-pulse" />
							Dashboard · Coming Soon
						</Badge>
					</div>
					<h1 className="font-[Syne] text-[2rem] font-bold tracking-tight leading-tight">
						{project?.name ?? "Project"}{" "}
						<span
							className="bg-clip-text text-transparent"
							style={{
								backgroundImage:
									"linear-gradient(135deg, var(--color-primary) 0%, color-mix(in oklch, var(--color-primary) 60%, var(--color-secondary)) 100%)",
							}}
						>
							Dashboard
						</span>
					</h1>
					{project?.description ? (
						<p className="mt-2 max-w-lg text-sm text-muted-foreground leading-relaxed">
							{project.description}
						</p>
					) : null}
				</div>
			</div>

			<div className="flex flex-col gap-8 p-6">
				{/* Coming soon callout */}
				<div className="rounded-2xl border border-dashed border-primary/30 bg-primary/3 p-8 text-center">
					<div className="mx-auto flex size-14 items-center justify-center rounded-2xl bg-primary/10">
						<LayoutDashboard className="size-7 text-primary" />
					</div>
					<h2 className="mt-4 font-[Syne] text-xl font-bold tracking-tight">
						Dashboard is on the way
					</h2>
					<p className="mt-2 max-w-md mx-auto text-sm text-muted-foreground leading-relaxed">
						We're building a real-time sprint overview with burndown charts, AI
						agent activity, and team velocity. Stay tuned for the launch.
					</p>
				</div>

				{/* Feature previews */}
				<div>
					<h3 className="mb-3 text-xs font-semibold uppercase tracking-widest text-muted-foreground">
						What's coming
					</h3>
					<div className="grid gap-3 sm:grid-cols-3">
						{COMING_SOON_FEATURES.map(
							({ icon: Icon, title, description, color }) => (
								<div
									key={title}
									className="rounded-xl border border-border/50 bg-muted/20 p-4 transition-colors hover:bg-muted/40"
								>
									<div
										className={`flex size-9 items-center justify-center rounded-lg ${color} mb-3`}
									>
										<Icon className="size-4" />
									</div>
									<p className="text-sm font-semibold">{title}</p>
									<p className="mt-1 text-xs text-muted-foreground leading-relaxed">
										{description}
									</p>
								</div>
							),
						)}
					</div>
				</div>

				{/* Quick nav to other project pages */}
				<div>
					<h3 className="mb-3 text-xs font-semibold uppercase tracking-widest text-muted-foreground">
						Explore this project
					</h3>
					<div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
						{PROJECT_PAGES.map(({ to, icon: Icon, label, description }) => (
							<Button
								key={to}
								variant="outline"
								className="h-auto w-full flex-col items-start gap-1.5 p-4 text-left border-border/60 hover:border-primary/40 hover:bg-primary/5"
								render={<Link to={to} params={{ projectId }} />}
							>
								<Icon className="size-4 text-muted-foreground" />
								<span className="text-sm font-medium">{label}</span>
								<span className="text-xs text-muted-foreground">
									{description}
								</span>
							</Button>
						))}
					</div>
				</div>
			</div>
		</div>
	);
}
