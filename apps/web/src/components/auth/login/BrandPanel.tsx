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
			{/* Base gradient — deep navy */}
			<div className="pointer-events-none absolute inset-0 bg-[#091830]" />
			<div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_80%_60%_at_10%_0%,#1b3d6e,transparent)] opacity-80" />
			<div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_60%_50%_at_90%_100%,#0b2040,transparent)]" />

			{/* Subtle grid texture */}
			<div
				className="pointer-events-none absolute inset-0 opacity-[0.055]"
				style={{
					backgroundImage:
						"linear-gradient(rgba(255,255,255,0.6) 1px, transparent 1px), linear-gradient(90deg, rgba(255,255,255,0.6) 1px, transparent 1px)",
					backgroundSize: "36px 36px",
				}}
			/>

			{/* Green ambient glow — top left */}
			<div className="pointer-events-none absolute -left-24 -top-24 h-72 w-72 rounded-full bg-[radial-gradient(circle,rgba(50,205,50,0.16),transparent_60%)]" />

			{/* Blue depth glow — bottom right */}
			<div className="pointer-events-none absolute -bottom-20 -right-10 h-80 w-80 rounded-full bg-[radial-gradient(circle,rgba(46,73,128,0.55),transparent_55%)]" />

			{/* Decorative concentric rings — right side */}
			<div className="pointer-events-none absolute right-0 top-1/2 h-105 w-105 -translate-y-1/2 translate-x-[42%] rounded-full border border-white/6" />
			<div className="pointer-events-none absolute right-0 top-1/2 h-70 w-70 -translate-y-1/2 translate-x-[42%] rounded-full border border-white/8" />

			<div className="relative">
				{/* Logo + brand */}
				<div className="mb-8 flex items-center gap-3">
					<div className="flex size-9 shrink-0 items-center justify-center rounded-xl border border-white/15 bg-white/8 shadow-lg shadow-black/20 backdrop-blur-sm">
						<img
							src="/paca-logo-dark.svg"
							alt="Paca logo"
							width={127}
							height={175}
							className="h-auto w-5 brightness-0 invert"
						/>
					</div>
					<span className="text-xl font-bold tracking-tight text-white">
						paca
					</span>
					<span className="rounded-full border border-white/20 bg-white/8 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-widest text-white/60">
						OSS
					</span>
				</div>

				<h2 className="display-title mb-3 text-[1.85rem] font-bold leading-tight text-balance text-white">
					One team, one board,{" "}
					<span
						className="bg-clip-text text-transparent"
						style={{
							backgroundImage:
								"linear-gradient(90deg, #32cd32 0%, #7de87d 100%)",
						}}
					>
						human and AI.
					</span>
				</h2>
				<p className="mb-8 text-sm leading-relaxed text-white/55">
					Paca is the open-source collaborative task management engine where
					human creativity and AI efficiency work together in a shared Scrumban
					workflow.
				</p>

				{/* Feature cards */}
				<ul className="space-y-2.5">
					{FEATURES.map(({ icon: Icon, title, desc }) => (
						<li
							key={title}
							className="flex items-start gap-3.5 rounded-xl border border-white/8 bg-white/4 px-4 py-3.5 transition-colors hover:border-white/[0.14] hover:bg-white/[0.07]"
						>
							<div className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-lg bg-[rgba(50,205,50,0.14)] ring-1 ring-[rgba(50,205,50,0.22)]">
								<Icon className="size-3.5 text-(--palm)" />
							</div>
							<div>
								<p className="text-sm font-semibold text-white/90">{title}</p>
								<p className="mt-0.5 text-xs leading-relaxed text-white/50">
									{desc}
								</p>
							</div>
						</li>
					))}
				</ul>
			</div>

			{/* Footer */}
			<div className="relative mt-8 pt-6">
				{/* Gradient separator */}
				<div className="absolute inset-x-0 top-0 h-px bg-[linear-gradient(90deg,transparent,rgba(255,255,255,0.18),transparent)]" />

				<div className="flex items-center justify-between">
					<a
						href="https://github.com/Paca-AI/paca"
						target="_blank"
						rel="noopener noreferrer"
						className="inline-flex items-center gap-2 rounded-lg border border-white/15 bg-white/6 px-3.5 py-2 text-xs font-medium text-white! transition-all hover:border-white/25 hover:bg-white/12 hover:text-white!"
					>
						<GitHubIcon className="size-3.5" />
						View on GitHub
					</a>
					<p className="text-[11px] text-white/30">Apache-2.0 · Open Source</p>
				</div>
			</div>
		</div>
	);
}
