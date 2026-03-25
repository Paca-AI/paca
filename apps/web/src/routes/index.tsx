import { createFileRoute } from "@tanstack/react-router";
import {
	Eye,
	EyeOff,
	GitBranch,
	Github,
	Info,
	ShieldCheck,
	Users,
} from "lucide-react";
import { useState } from "react";

import ThemeToggle from "@/components/ThemeToggle";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useIsDark } from "@/hooks/use-is-dark";
import { useLoginForm } from "@/hooks/use-login-form";
import { markLoginSubmit, setUsernamePreview } from "@/lib/login-store";

export const Route = createFileRoute("/")({ component: App });

function FieldError({
	isTouched,
	error,
}: {
	isTouched: boolean;
	error: string | undefined;
}) {
	if (!isTouched || !error) return null;
	return (
		<p role="alert" className="mt-1 text-xs text-red-600 dark:text-red-400">
			{error}
		</p>
	);
}

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

function BrandPanel() {
	return (
		<div className="relative hidden flex-col justify-between overflow-hidden rounded-l-3xl p-10 lg:flex">
			{/* Background gradient */}
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
					className="inline-flex items-center gap-2 rounded-lg border border-white/15 bg-white/8 px-3.5 py-2 text-xs font-medium !text-white transition-colors hover:bg-white/14 hover:!text-white/80"
				>
					<Github className="size-3.5" />
					View on GitHub
				</a>
				<p className="mt-3 text-[11px] text-white/35">
					Apache-2.0 License · Open Source
				</p>
			</div>
		</div>
	);
}

function App() {
	const form = useLoginForm();
	const [showPassword, setShowPassword] = useState(false);
	const isDark = useIsDark();
	const logoSrc = isDark ? "/paca-logo-dark.svg" : "/paca-logo.svg";

	return (
		<div className="flex min-h-screen flex-col">
			{/* Top bar */}
			<header className="flex items-center justify-between px-5 py-4 sm:px-8">
				<ThemeToggle />
			</header>

			{/* Main content */}
			<main className="flex flex-1 items-center justify-center px-4 py-8">
				<div className="island-shell rise-in w-full max-w-4xl overflow-hidden rounded-3xl">
					<div className="grid lg:grid-cols-[1fr_420px]">
						{/* Left branding panel */}
						<BrandPanel />

						{/* Right form panel */}
						<div className="relative flex flex-col justify-center px-7 py-10 sm:px-10">
							<div className="pointer-events-none absolute -right-12 -top-12 h-32 w-32 rounded-full bg-[radial-gradient(circle,rgba(50,205,50,0.18),transparent_65%)] lg:hidden" />

							<div className="relative">
								{/* Mobile logo */}
								<div className="mb-6 flex items-center gap-2.5 lg:hidden">
									<img
										src={logoSrc}
										alt="Paca logo"
										width={127}
										height={175}
										className="h-auto w-8"
									/>
									<span className="text-base font-bold tracking-tight text-(--sea-ink)">
										paca
									</span>
								</div>

								<h1 className="display-title mb-1 text-2xl font-bold text-(--sea-ink) sm:text-3xl">
									Welcome back
								</h1>
								<p className="mb-7 text-sm text-(--sea-ink-soft)">
									Sign in to your Paca account to continue.
								</p>

								<form
									onSubmit={(event) => {
										event.preventDefault();
										event.stopPropagation();
										markLoginSubmit();
										form.handleSubmit();
									}}
									className="space-y-4"
								>
									<form.Field
										name="username"
										validators={{
											onBlur: ({ value }) => {
												if (!value.trim()) {
													return "Username is required";
												}
												if (value.trim().length < 3) {
													return "Username must be at least 3 characters";
												}
												return undefined;
											},
										}}
									>
										{(field) => (
											<div className="space-y-1.5">
												<Label htmlFor={field.name}>Username</Label>
												<Input
													id={field.name}
													name={field.name}
													type="text"
													autoComplete="username"
													placeholder="Username"
													value={field.state.value}
													onBlur={field.handleBlur}
													onChange={(event) => {
														field.handleChange(event.target.value);
														setUsernamePreview(event.target.value);
													}}
												/>
												<FieldError
													isTouched={field.state.meta.isTouched}
													error={field.state.meta.errors[0]}
												/>
											</div>
										)}
									</form.Field>

									<form.Field
										name="password"
										validators={{
											onBlur: ({ value }) => {
												if (!value.trim()) {
													return "Password is required";
												}
												if (value.length < 8) {
													return "Password must be at least 8 characters";
												}
												return undefined;
											},
										}}
									>
										{(field) => (
											<div className="space-y-1.5">
												<Label htmlFor={field.name}>Password</Label>
												<div className="relative">
													<Input
														id={field.name}
														name={field.name}
														type={showPassword ? "text" : "password"}
														autoComplete="current-password"
														placeholder="••••••••"
														value={field.state.value}
														onBlur={field.handleBlur}
														onChange={(event) =>
															field.handleChange(event.target.value)
														}
														className="pr-10"
													/>
													<button
														type="button"
														onClick={() =>
															setShowPassword((current) => !current)
														}
														className="absolute right-2.5 top-1/2 -translate-y-1/2 text-(--sea-ink-soft) hover:text-(--sea-ink)"
														aria-label={
															showPassword ? "Hide password" : "Show password"
														}
													>
														{showPassword ? (
															<EyeOff className="size-4" />
														) : (
															<Eye className="size-4" />
														)}
													</button>
												</div>
												<FieldError
													isTouched={field.state.meta.isTouched}
													error={field.state.meta.errors[0]}
												/>
											</div>
										)}
									</form.Field>

									<form.Field name="rememberMe">
										{(field) => (
											<div className="flex items-center justify-between rounded-lg border border-(--line) bg-(--chip-bg) px-3 py-2.5">
												<Label
													htmlFor={field.name}
													className="cursor-pointer text-sm"
												>
													Remember me
												</Label>
												<Switch
													id={field.name}
													checked={field.state.value}
													onCheckedChange={field.handleChange}
												/>
											</div>
										)}
									</form.Field>

									<form.Subscribe
										selector={(state) => [state.canSubmit, state.isSubmitting]}
									>
										{([canSubmit, isSubmitting]) => (
											<Button
												type="submit"
												className="w-full"
												disabled={!canSubmit}
											>
												{isSubmitting ? "Signing in..." : "Sign in"}
											</Button>
										)}
									</form.Subscribe>
								</form>

								<div className="mt-5 flex items-start gap-2 rounded-lg border border-(--line) bg-(--chip-bg) px-3.5 py-3">
									<Info className="mt-px size-3.5 shrink-0 text-(--sea-ink-soft)" />
									<p className="text-xs leading-relaxed text-(--sea-ink-soft)">
										Account access and password resets are managed by your
										administrator.
									</p>
								</div>
							</div>
						</div>
					</div>
				</div>
			</main>

			{/* Footer */}
			<footer className="flex flex-wrap items-center justify-center gap-x-5 gap-y-1.5 px-5 py-4 text-xs text-(--sea-ink-soft)">
				<span>© {new Date().getFullYear()} Paca</span>
				<span className="hidden sm:inline opacity-30">·</span>
				<a
					href="https://github.com/Paca-AI/paca"
					target="_blank"
					rel="noopener noreferrer"
					className="hover:text-(--sea-ink)"
				>
					GitHub
				</a>
				<span className="opacity-30">·</span>
				<a
					href="https://github.com/Paca-AI/paca/tree/master/docs"
					target="_blank"
					rel="noopener noreferrer"
					className="hover:text-(--sea-ink)"
				>
					Docs
				</a>
				<span className="opacity-30">·</span>
				<a
					href="https://github.com/Paca-AI/paca/blob/HEAD/LICENSE"
					target="_blank"
					rel="noopener noreferrer"
					className="hover:text-(--sea-ink)"
				>
					Apache-2.0
				</a>
			</footer>
		</div>
	);
}
