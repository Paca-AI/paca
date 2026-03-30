import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import {
	ArrowRight,
	Bot,
	FolderKanban,
	GitMerge,
	Layers,
	Plus,
	Users,
	Zap,
} from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { currentUserQueryOptions } from "@/lib/auth-api";

export const Route = createFileRoute("/_authenticated/home/")({
	component: HomePage,
});

const QUICK_ACTIONS = [
	{
		icon: FolderKanban,
		label: "New Project",
		description: "Create a scrumban board",
	},
	{
		icon: Users,
		label: "Invite Team",
		description: "Add members or agents",
	},
	{
		icon: Bot,
		label: "Add AI Agent",
		description: "Configure automation",
	},
] as const;

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

function StatCard({
	icon: Icon,
	label,
	value,
	sub,
	iconClass,
}: {
	icon: React.ComponentType<{ className?: string }>;
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

function HomePage() {
	const { data: user } = useQuery(currentUserQueryOptions);

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
				{/* dot grid */}
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
				{/* glow orbs */}
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
							Your workspace is ready. Create a project, invite your team, and
							start the Plan → Act → Check → Adapt cycle.
						</p>
					</div>
					<div className="flex shrink-0 items-center gap-2">
						<Button
							size="sm"
							variant="outline"
							className="gap-1.5 border-border/70"
						>
							<Users className="size-3.5" />
							Invite
						</Button>
						<Button size="sm" className="gap-1.5 shadow-sm shadow-primary/20">
							<Plus className="size-3.5" />
							New Project
						</Button>
					</div>
				</div>
			</div>

			<div className="flex flex-col gap-6 p-6">
				{/* Stats row */}
				<div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
					<StatCard
						icon={FolderKanban}
						label="Projects"
						value={0}
						sub="No projects yet"
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

					{/* Right column */}
					<div className="flex flex-col gap-6">
						{/* Quick actions */}
						<Card className="border-border/60">
							<CardHeader className="pb-2">
								<CardTitle className="font-[Syne] text-base font-semibold">
									Quick actions
								</CardTitle>
							</CardHeader>
							<CardContent className="pt-0">
								<div className="space-y-2">
									{QUICK_ACTIONS.map(({ icon: Icon, label, description }) => (
										<button
											key={label}
											type="button"
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

						{/* About Paca */}
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
			</div>
		</div>
	);
}
