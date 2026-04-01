import { useMutation, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, redirect, useRouter } from "@tanstack/react-router";
import { KeyRound, LogOut } from "lucide-react";

import { ChangePasswordForm } from "@/components/profile/ChangePasswordForm";
import ThemeToggle from "@/components/ThemeToggle";
import { Button } from "@/components/ui/button";
import { isPasswordChangeRequired } from "@/lib/api-error";
import { currentUserQueryOptions, logout } from "@/lib/auth-api";

export const Route = createFileRoute("/change-password")({
	beforeLoad: async ({ context: { queryClient } }) => {
		let passwordChangeRequired = false;

		const user = await queryClient
			.fetchQuery(currentUserQueryOptions)
			.catch((err: unknown) => {
				// 403 AUTH_PASSWORD_CHANGE_REQUIRED: the user IS authenticated but
				// the backend blocks /me. Allow the page to render — this is exactly
				// where the user should be.
				if (isPasswordChangeRequired(err)) {
					passwordChangeRequired = true;
					return null;
				}
				// Any other error (401, network) → not authenticated.
				return null;
			});

		// User must change password (either from the 200 response or the 403) → stay.
		if (passwordChangeRequired || user?.must_change_password) {
			return;
		}

		// Not logged in → go to login page.
		if (!user) {
			throw redirect({ to: "/" });
		}

		// Password is already fine → go to the app.
		throw redirect({ to: "/home" });
	},
	component: ChangePasswordPage,
});

function ChangePasswordPage() {
	const router = useRouter();
	const queryClient = useQueryClient();

	const logoutMutation = useMutation({
		mutationFn: logout,
		onSettled: () => {
			queryClient.clear();
			void router.navigate({ to: "/" });
		},
	});

	return (
		<div className="flex min-h-screen flex-col bg-(--bg-base)">
			{/* Top bar */}
			<header className="flex items-center justify-end px-5 py-4 sm:px-8">
				<div className="flex items-center gap-2">
					<ThemeToggle />
					<Button
						variant="ghost"
						size="sm"
						className="gap-1.5 text-(--sea-ink-soft) hover:text-(--sea-ink)"
						disabled={logoutMutation.isPending}
						onClick={() => logoutMutation.mutate()}
					>
						<LogOut className="size-3.5" />
						{logoutMutation.isPending ? "Signing out…" : "Sign out"}
					</Button>
				</div>
			</header>

			{/* Main content */}
			<main className="flex flex-1 items-center justify-center px-4 py-6">
				<div className="island-shell rise-in w-full max-w-4xl overflow-hidden rounded-3xl">
					<div className="grid lg:grid-cols-[1fr_420px]">
						{/* Brand / context panel */}
						<div className="relative hidden flex-col justify-between overflow-hidden rounded-l-3xl p-10 lg:flex">
							{/* Backgrounds */}
							<div className="pointer-events-none absolute inset-0 bg-[#091830]" />
							<div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_80%_60%_at_10%_0%,#1b3d6e,transparent)] opacity-80" />
							<div className="pointer-events-none absolute inset-0 bg-[radial-gradient(ellipse_60%_50%_at_90%_100%,#0b2040,transparent)]" />
							{/* Grid texture */}
							<div
								className="pointer-events-none absolute inset-0 opacity-[0.055]"
								style={{
									backgroundImage:
										"linear-gradient(rgba(255,255,255,0.6) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,0.6) 1px,transparent 1px)",
									backgroundSize: "36px 36px",
								}}
							/>
							{/* Amber warning glow */}
							<div className="pointer-events-none absolute -left-20 -top-20 h-72 w-72 rounded-full bg-[radial-gradient(circle,rgba(251,191,36,0.18),transparent_60%)]" />
							<div className="pointer-events-none absolute -bottom-20 -right-10 h-80 w-80 rounded-full bg-[radial-gradient(circle,rgba(46,73,128,0.55),transparent_55%)]" />
							{/* Concentric rings */}
							<div className="pointer-events-none absolute right-0 top-1/2 h-96 w-96 -translate-y-1/2 translate-x-[42%] rounded-full border border-white/6" />
							<div className="pointer-events-none absolute right-0 top-1/2 h-64 w-64 -translate-y-1/2 translate-x-[42%] rounded-full border border-white/8" />

							<div className="relative">
								<div className="mb-10 flex items-center gap-3">
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
								</div>

								<div className="mb-3 inline-flex items-center gap-2 rounded-full border border-amber-400/30 bg-amber-400/10 px-3 py-1">
									<span className="size-1.5 rounded-full bg-amber-400" />
									<span className="text-xs font-medium text-amber-300">
										Action required
									</span>
								</div>
								<h2 className="display-title mb-3 text-2xl font-bold text-white sm:text-3xl">
									Secure your account
								</h2>
								<p className="text-sm leading-relaxed text-white/55">
									Your administrator has assigned you a temporary password. You
									must set a new personal password before you can access the
									workspace.
								</p>
							</div>

							<div className="relative space-y-4">
								{[
									{
										step: "01",
										label: "Enter the temporary password you received",
									},
									{ step: "02", label: "Choose a strong new password" },
									{ step: "03", label: "Sign in again with your new password" },
								].map(({ step, label }) => (
									<div key={step} className="flex items-center gap-3">
										<div className="flex size-7 shrink-0 items-center justify-center rounded-full border border-white/15 bg-white/8 text-xs font-bold text-white/60">
											{step}
										</div>
										<p className="text-sm text-white/55">{label}</p>
									</div>
								))}
							</div>
						</div>

						{/* Form panel */}
						<div className="relative flex flex-col justify-center px-8 py-10 sm:px-10">
							{/* Mobile top accent */}
							<div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-[linear-gradient(90deg,transparent,rgba(251,191,36,0.45),transparent)] lg:hidden" />

							<div className="relative">
								{/* Mobile logo */}
								<div className="mb-7 flex items-center gap-2.5 lg:hidden">
									<div className="flex size-7 shrink-0 items-center justify-center rounded-lg border border-(--chip-line) bg-(--chip-bg)">
										<KeyRound className="size-3.5 text-(--lagoon)" />
									</div>
									<span className="text-sm font-bold tracking-tight text-(--sea-ink)">
										paca
									</span>
								</div>

								{/* Heading */}
								<div className="mb-1 flex items-center gap-2">
									<div className="size-2 rounded-full bg-amber-400" />
									<span className="text-xs font-semibold uppercase tracking-widest text-amber-600 dark:text-amber-400">
										Password change required
									</span>
								</div>
								<h1 className="display-title mb-1 text-2xl font-bold text-(--sea-ink) sm:text-[1.75rem]">
									Set new password
								</h1>
								<p className="mb-8 text-sm text-(--sea-ink-soft)">
									Enter your temporary password and choose a new one to unlock
									your account.
								</p>

								<ChangePasswordForm onSuccess={() => logoutMutation.mutate()} />
							</div>
						</div>
					</div>
				</div>
			</main>

			<footer className="py-4 text-center text-xs text-(--sea-ink-soft) opacity-60">
				© {new Date().getFullYear()} Paca. All rights reserved.
			</footer>
		</div>
	);
}
