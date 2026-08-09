import { BookOpen, Bot, Puzzle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { GitHubIcon } from "@/components/icons/github-icon";
import { useBranding } from "@/hooks/use-branding";

const FEATURES = [
	{
		icon: Bot,
		titleKey: "brand.features.sandboxedAgents.title",
		descKey: "brand.features.sandboxedAgents.desc",
	},
	{
		icon: BookOpen,
		titleKey: "brand.features.bddSddHub.title",
		descKey: "brand.features.bddSddHub.desc",
	},
	{
		icon: Puzzle,
		titleKey: "brand.features.pluginMarketplace.title",
		descKey: "brand.features.pluginMarketplace.desc",
	},
] as const;

export function BrandPanel() {
	const { t } = useTranslation("auth");
	const branding = useBranding();
	const logoUrl = branding?.logo_thumb_url ?? branding?.logo_url;
	const brandName = branding?.brand_name;

	return (
		<div className="relative hidden flex-col justify-between overflow-hidden rounded-l-xl bg-[#0a0a0a] p-10 lg:flex">
			{/* Ambient glow — top, tinted with the brand color */}
			<div className="pointer-events-none absolute -left-24 -top-24 h-72 w-72 rounded-full bg-[radial-gradient(circle,color-mix(in_oklab,var(--palm)_8%,transparent),transparent_60%)]" />

			{/* Decorative concentric rings — right side */}
			<div className="pointer-events-none absolute right-0 top-1/2 h-105 w-105 -translate-y-1/2 translate-x-[42%] rounded-full border border-white/5" />
			<div className="pointer-events-none absolute right-0 top-1/2 h-70 w-70 -translate-y-1/2 translate-x-[42%] rounded-full border border-white/7" />

			<div className="relative">
				{/* Logo + brand */}
				<div className="mb-8 flex items-center gap-3">
					<div className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-white/10 bg-white/6 shadow-sm shadow-black/40">
						<img
							src={logoUrl ?? "/paca-logo-dark.svg"}
							alt={t("brand.logoAlt")}
							width={127}
							height={175}
							className={
								logoUrl
									? "h-auto w-5 object-contain"
									: "h-auto w-5 brightness-0 invert"
							}
						/>
					</div>
					<span className="text-xl font-bold tracking-tight text-white">
						{brandName ?? "paca"}
					</span>
					{brandName ? null : (
						<span className="rounded-full border border-white/20 bg-white/8 px-2 py-0.5 text-xs font-semibold uppercase tracking-widest text-white/60">
							{t("brand.ossBadge")}
						</span>
					)}
				</div>

				<h2 className="display-title mb-3 text-3xl font-bold leading-tight text-balance text-white">
					{t("brand.headingPrefix")}{" "}
					<span className="text-(--palm)">{t("brand.headingHighlight")}</span>
				</h2>
				<p className="mb-8 text-sm leading-relaxed text-white/55">
					{t("brand.tagline")}
				</p>

				{/* Feature cards */}
				<ul className="space-y-2.5">
					{FEATURES.map(({ icon: Icon, titleKey, descKey }) => (
						<li
							key={titleKey}
							className="flex items-start gap-3.5 rounded-xl border border-white/8 bg-white/4 px-4 py-3.5 transition-colors hover:border-white/[0.14] hover:bg-white/[0.07]"
						>
							<div className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-lg bg-[color-mix(in_oklab,var(--palm)_12%,transparent)] ring-1 ring-[color-mix(in_oklab,var(--palm)_20%,transparent)]">
								<Icon className="size-3.5 text-(--palm)" />
							</div>
							<div>
								<p className="text-sm font-semibold text-white/90">
									{t(titleKey)}
								</p>
								<p className="mt-0.5 text-xs leading-relaxed text-white/50">
									{t(descKey)}
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
						{t("brand.viewOnGitHub")}
					</a>
					<p className="text-xs text-white/30">{t("brand.licenseLine")}</p>
				</div>
			</div>
		</div>
	);
}
