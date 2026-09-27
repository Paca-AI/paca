import { createFileRoute, redirect } from "@tanstack/react-router";

import {
	BrandPanel,
	LoginFooter,
	LoginFormPanel,
} from "@/components/auth/login/index";
import LanguageToggle from "@/components/LanguageToggle";
import ThemeToggle from "@/components/ThemeToggle";
import { currentUserQueryOptions } from "@/lib/auth-api";

export const Route = createFileRoute("/")({
	// Set by the API when an SSO sign-in fails (see SsoErrorNotice).
	validateSearch: (search: Record<string, unknown>): { sso_error?: string } =>
		typeof search.sso_error === "string" ? { sso_error: search.sso_error } : {},
	beforeLoad: async ({ context: { queryClient } }) => {
		const user = await queryClient
			.fetchQuery(currentUserQueryOptions)
			.catch(() => null);
		if (user) throw redirect({ to: "/home" });
	},
	component: LoginPage,
});

function LoginPage() {
	const { sso_error: ssoError } = Route.useSearch();
	return (
		<div className="flex min-h-screen flex-col">
			{/* Top bar */}
			<header className="flex items-center justify-end gap-2 px-5 py-4 sm:px-8">
				<LanguageToggle />
				<ThemeToggle />
			</header>

			{/* Main content */}
			<main className="flex flex-1 items-center justify-center px-4 py-6">
				<div className="island-shell rise-in w-full max-w-4xl overflow-hidden rounded-xl">
					<div className="grid lg:grid-cols-[1fr_400px]">
						<BrandPanel />
						<LoginFormPanel ssoError={ssoError} />
					</div>
				</div>
			</main>

			<LoginFooter />
		</div>
	);
}
