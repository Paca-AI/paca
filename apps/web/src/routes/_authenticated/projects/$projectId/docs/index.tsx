import { createFileRoute } from "@tanstack/react-router";
import { BookOpen, FileSearch, GitBranch, Sparkles } from "lucide-react";

import { Badge } from "@/components/ui/badge";

export const Route = createFileRoute(
	"/_authenticated/projects/$projectId/docs/",
)({
	component: DocsPage,
});

const FEATURES = [
	{
		icon: BookOpen,
		title: "Living Documentation",
		description:
			"Write and organize docs that live alongside your code and evolve with your project.",
		color: "bg-violet-500/10 text-violet-500",
	},
	{
		icon: Sparkles,
		title: "AI-Generated Drafts",
		description:
			"Auto-generate technical specs, ADRs, and release notes from sprint data.",
		color: "bg-amber-500/10 text-amber-500",
	},
	{
		icon: GitBranch,
		title: "Version History",
		description:
			"Full version history for every document with diff views and rollback support.",
		color: "bg-blue-500/10 text-blue-500",
	},
	{
		icon: FileSearch,
		title: "Semantic Search",
		description:
			"Find any doc, decision, or requirement instantly with AI-powered semantic search.",
		color: "bg-emerald-500/10 text-emerald-500",
	},
] as const;

function DocsPage() {
	return (
		<div className="flex flex-col">
			<div className="relative overflow-hidden border-b border-border/50">
				<div
					className="pointer-events-none absolute inset-0 opacity-50"
					style={{
						backgroundImage:
							"radial-gradient(circle, color-mix(in oklch, var(--color-primary) 12%, transparent) 1px, transparent 1px)",
						backgroundSize: "20px 20px",
						maskImage:
							"radial-gradient(ellipse 70% 100% at 0% 0%, black 20%, transparent 70%)",
					}}
				/>
				<div className="relative px-6 py-8">
					<div className="mb-2 flex items-center gap-2">
						<Badge
							variant="secondary"
							className="gap-1.5 px-2.5 py-0.5 text-xs font-semibold border border-border/60"
						>
							<span className="size-1.5 rounded-full bg-secondary inline-block" />
							Coming Soon
						</Badge>
					</div>
					<h1 className="font-[Syne] text-2xl font-bold tracking-tight">
						Docs
					</h1>
					<p className="mt-1.5 max-w-lg text-sm text-muted-foreground">
						Collaborative documentation that grows with your project — specs,
						ADRs, runbooks, and more.
					</p>
				</div>
			</div>

			<div className="p-6">
				<div className="rounded-2xl border border-dashed border-border/60 bg-muted/10 p-10 text-center mb-6">
					<div className="mx-auto flex size-14 items-center justify-center rounded-2xl bg-violet-500/10 mb-4">
						<BookOpen className="size-7 text-violet-500" />
					</div>
					<h2 className="font-[Syne] text-lg font-bold tracking-tight">
						Documentation Hub
					</h2>
					<p className="mt-2 max-w-sm mx-auto text-sm text-muted-foreground leading-relaxed">
						A living knowledge base for your project — AI-assisted,
						version-controlled, and always up to date.
					</p>
				</div>

				<div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
					{FEATURES.map(({ icon: Icon, title, description, color }) => (
						<div
							key={title}
							className="rounded-xl border border-border/50 bg-card p-4 transition-colors hover:bg-muted/30"
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
					))}
				</div>
			</div>
		</div>
	);
}
