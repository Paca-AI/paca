import type { ReactNode } from "react";

import LanguageToggle from "@/components/LanguageToggle";
import ThemeToggle from "@/components/ThemeToggle";

import { BrandPanel } from "./BrandPanel";
import { LoginFooter } from "./LoginFooter";

/** The page frame shared by every pre-sign-in screen: brand panel beside
 * whatever step the person is on. */
export function LoginShell({ children }: { children: ReactNode }) {
	return (
		<div className="flex min-h-screen flex-col">
			<header className="flex items-center justify-end gap-2 px-5 py-4 sm:px-8">
				<LanguageToggle />
				<ThemeToggle />
			</header>

			<main className="flex flex-1 items-center justify-center px-4 py-6">
				<div className="island-shell rise-in w-full max-w-4xl overflow-hidden rounded-xl">
					<div className="grid lg:grid-cols-[1fr_400px]">
						<BrandPanel />
						{children}
					</div>
				</div>
			</main>

			<LoginFooter />
		</div>
	);
}
