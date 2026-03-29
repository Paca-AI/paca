import { createFileRoute, redirect } from "@tanstack/react-router";

import {
	BrandPanel,
	LoginFooter,
	LoginFormPanel,
} from "@/components/auth/login/index";
import ThemeToggle from "@/components/ThemeToggle";
import { currentUserQueryOptions } from "@/lib/auth-api";

export const Route = createFileRoute("/")({
	beforeLoad: async ({ context: { queryClient } }) => {
		const user = await queryClient
			.fetchQuery(currentUserQueryOptions)
			.catch(() => null);
		if (user) throw redirect({ to: "/home" });
	},
	component: LoginPage,
});

function LoginPage() {
	return (
		<div className="flex min-h-screen flex-col">
			{/* Top bar */}
			<header className="flex items-center justify-end px-5 py-4 sm:px-8">
				<ThemeToggle />
			</header>

			{/* Main content */}
			<main className="flex flex-1 items-center justify-center px-4 py-6">
				<div className="island-shell rise-in w-full max-w-4xl overflow-hidden rounded-3xl">
					<div className="grid lg:grid-cols-[1fr_400px]">
						<BrandPanel />
						<LoginFormPanel />
					</div>
				</div>
			</main>

			<LoginFooter />
		</div>
	);
}
