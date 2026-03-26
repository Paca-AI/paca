import { GitBranch, ShieldCheck, Users } from "lucide-react";

const FEATURES = [
	{
		icon: Users,
		title: "Fluid Roles",
		desc: "From PO and BA to Dev and QA, work can move between humans and AI agents seamlessly.",
	},
	{
		icon: GitBranch,
		title: "Contextual Assignment",
		desc: "Tasks are assigned by strengths: AI for speed and precision, humans for judgment and creativity.",
	},
	{
		icon: ShieldCheck,
		title: "Human-in-Control",
		desc: "Every AI contribution stays transparent and supervised so teams remain the final decision-makers.",
	},
] as const;

const GitHubIcon = (props: React.SVGProps<SVGSVGElement>) => (
	<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg" {...props}>
		<title>GitHub</title>
		<path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
	</svg>
);

export function BrandPanel() {
	return (
		<div className="relative hidden flex-col justify-between overflow-hidden rounded-l-3xl p-10 lg:flex">
			<div className="pointer-events-none absolute inset-0 bg-linear-to-br from-(--lagoon) via-[#1e3f6e] to-[#0d2240] dark:from-[#0d1f3c] dark:via-[#0f2447] dark:to-[#050f22]" />
			<div className="pointer-events-none absolute -left-20 -top-20 h-64 w-64 rounded-full bg-[radial-gradient(circle,rgba(50,205,50,0.18),transparent_65%)]" />
			<div className="pointer-events-none absolute -bottom-24 -right-16 h-72 w-72 rounded-full bg-[radial-gradient(circle,rgba(49,95,133,0.35),transparent_60%)]" />

			<div className="relative">
				<div className="mb-8 flex items-center gap-3">
					<img
						src="/paca-logo-dark.svg"
						alt="Paca logo"
						width={127}
						height={175}
						className="h-auto w-9 brightness-0 invert"
					/>
					<span className="text-xl font-bold tracking-tight text-white">
						paca
					</span>
					<span className="rounded-full border border-white/20 bg-white/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-widest text-white/70">
						OSS
					</span>
				</div>

				<h2 className="display-title mb-3 text-3xl font-bold leading-tight text-white">
					One team, one board,{" "}
					<span className="text-(--palm)">human and AI.</span>
				</h2>
				<p className="mb-10 text-sm leading-relaxed text-white/65">
					Paca is the open-source collaborative task management engine where
					human creativity and AI efficiency work together in a shared Scrumban
					workflow.
				</p>

				<ul className="space-y-4">
					{FEATURES.map(({ icon: Icon, title, desc }) => (
						<li key={title} className="flex items-start gap-3">
							<div className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md bg-white/10">
								<Icon className="size-3.5 text-white/80" />
							</div>
							<div>
								<p className="text-sm font-semibold text-white/90">{title}</p>
								<p className="text-xs leading-relaxed text-white/55">{desc}</p>
							</div>
						</li>
					))}
				</ul>
			</div>

			<div className="relative mt-8 border-t border-white/10 pt-6">
				<a
					href="https://github.com/Paca-AI/paca"
					target="_blank"
					rel="noopener noreferrer"
					className="inline-flex items-center gap-2 rounded-lg border border-white/15 bg-white/8 px-3.5 py-2 text-xs font-medium text-white! transition-colors hover:bg-white/14 hover:text-white/80!"
				>
					<GitHubIcon className="size-3.5" />
					View on GitHub
				</a>
				<p className="mt-3 text-[11px] text-white/35">
					Apache-2.0 License · Open Source
				</p>
			</div>
		</div>
	);
}
