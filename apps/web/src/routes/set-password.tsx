import { createFileRoute, Link } from "@tanstack/react-router";
import { PartyPopper, Sparkles } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { SetPasswordForm } from "@/components/auth/SetPasswordForm";
import LanguageToggle from "@/components/LanguageToggle";
import ThemeToggle from "@/components/ThemeToggle";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export const Route = createFileRoute("/set-password")({
	validateSearch: (search: Record<string, unknown>) => ({
		token: typeof search.token === "string" ? search.token : "",
	}),
	component: SetPasswordPage,
});

function SetPasswordPage() {
	const { t } = useTranslation("auth");
	const { token } = Route.useSearch();
	const [state, setState] = useState<"form" | "success" | "invalid">(
		token ? "form" : "invalid",
	);

	return (
		<div className="flex min-h-screen flex-col bg-(--bg-base)">
			{/* Top bar */}
			<header className="flex items-center justify-end px-5 py-4 sm:px-8">
				<div className="flex items-center gap-2">
					<LanguageToggle />
					<ThemeToggle />
				</div>
			</header>

			{/* Main content */}
			<main className="flex flex-1 items-center justify-center px-4 py-6">
				<div className="island-shell rise-in w-full max-w-4xl overflow-hidden rounded-xl">
					<div className="grid lg:grid-cols-[1fr_420px]">
						{/* Brand / context panel */}
						<div className="relative hidden flex-col justify-between overflow-hidden rounded-l-xl bg-[#0a0a0a] p-10 lg:flex">
							<div className="pointer-events-none absolute -left-20 -top-20 h-72 w-72 rounded-full bg-[radial-gradient(circle,color-mix(in_oklab,var(--palm)_7%,transparent),transparent_60%)]" />
							<div className="pointer-events-none absolute right-0 top-1/2 h-96 w-96 -translate-y-1/2 translate-x-[42%] rounded-full border border-white/5" />
							<div className="pointer-events-none absolute right-0 top-1/2 h-64 w-64 -translate-y-1/2 translate-x-[42%] rounded-full border border-white/7" />
							<div className="relative">
								<div className="mb-10 flex items-center gap-3">
									<div className="flex size-9 shrink-0 items-center justify-center rounded-lg border border-white/10 bg-white/6 shadow-sm shadow-black/40">
										<img
											src="/paca-logo-dark.svg"
											alt={t("brand.logoAlt")}
											width={127}
											height={175}
											className="h-auto w-5 brightness-0 invert"
										/>
									</div>
									<span className="text-xl font-bold tracking-tight text-white">
										paca
									</span>
								</div>

								<div className="mb-3 inline-flex items-center gap-2 rounded-full border border-emerald-400/30 bg-emerald-400/10 px-3 py-1">
									<Sparkles className="size-3 text-emerald-300" />
									<span className="text-xs font-medium text-emerald-300">
										{t("setPassword.welcomeBadge")}
									</span>
								</div>
								<h2 className="display-title mb-3 text-2xl font-bold text-white sm:text-3xl">
									{t("setPassword.secureAccountTitle")}
								</h2>
								<p className="text-sm leading-relaxed text-white/55">
									{t("setPassword.secureAccountDesc")}
								</p>
							</div>

							<div />
						</div>

						{/* Form panel */}
						<div className="relative flex flex-col justify-center px-8 py-10 sm:px-10">
							<div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-border/40 lg:hidden" />

							<div className="relative">
								<div className="mb-7 flex items-center gap-2.5 lg:hidden">
									<div className="flex size-7 shrink-0 items-center justify-center rounded-lg border border-(--chip-line) bg-(--chip-bg)">
										<Sparkles className="size-3.5 text-(--lagoon)" />
									</div>
									<span className="text-sm font-bold tracking-tight text-(--sea-ink)">
										paca
									</span>
								</div>

								{state === "invalid" ? (
									<>
										<h1 className="display-title mb-2 text-2xl font-bold text-(--sea-ink) sm:text-3xl">
											{t("setPassword.invalidTokenTitle")}
										</h1>
										<p className="mb-8 text-sm text-(--sea-ink-soft)">
											{t("setPassword.invalidTokenDesc")}
										</p>
										<Link
											to="/"
											className={cn(
												buttonVariants({ size: "lg" }),
												"h-11 w-full font-semibold tracking-wide",
											)}
										>
											{t("setPassword.backToLogin")}
										</Link>
									</>
								) : state === "success" ? (
									<>
										<div className="mb-3 flex items-center gap-2 text-primary">
											<PartyPopper className="size-5" />
										</div>
										<h1 className="display-title mb-2 text-2xl font-bold text-(--sea-ink) sm:text-3xl">
											{t("setPassword.title")}
										</h1>
										<p className="mb-8 text-sm text-(--sea-ink-soft)">
											{t("setPassword.success")}
										</p>
										<Link
											to="/"
											className={cn(
												buttonVariants({ size: "lg" }),
												"h-11 w-full font-semibold tracking-wide",
											)}
										>
											{t("setPassword.actions.goToLogin")}
										</Link>
									</>
								) : (
									<>
										<h1 className="display-title mb-1 text-2xl font-bold text-(--sea-ink) sm:text-3xl">
											{t("setPassword.title")}
										</h1>
										<p className="mb-8 text-sm text-(--sea-ink-soft)">
											{t("setPassword.subtitle")}
										</p>
										<SetPasswordForm
											token={token}
											onSuccess={() => setState("success")}
											onInvalidToken={() => setState("invalid")}
										/>
									</>
								)}
							</div>
						</div>
					</div>
				</div>
			</main>

			<footer className="py-4 text-center text-xs text-(--sea-ink-soft) opacity-60">
				{t("changePassword.footerCopyright", {
					year: new Date().getFullYear(),
				})}
			</footer>
		</div>
	);
}
