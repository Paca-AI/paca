import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

import { AppSidebar } from "@/components/app-shell/app-sidebar";
import {
	SidebarInset,
	SidebarProvider,
	SidebarTrigger,
} from "@/components/ui/sidebar";
import { currentUserQueryOptions } from "@/lib/auth-api";

/**
 * Pathless layout route that guards every route nested under it.
 *
 * Any route placed inside `routes/_authenticated/` automatically requires the
 * user to be signed in.  Unauthenticated visitors are redirected to the login
 * page (`/`).  The check reuses the cached React Query result so it only hits
 * the network when the cache is stale.
 */
export const Route = createFileRoute("/_authenticated")({
	beforeLoad: async ({ context: { queryClient } }) => {
		const user = await queryClient
			.fetchQuery(currentUserQueryOptions)
			.catch(() => null);

		if (!user) {
			throw redirect({ to: "/" });
		}
	},
	component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
	return (
		<SidebarProvider>
			<AppSidebar />
			<SidebarInset>
				<header className="flex h-12 shrink-0 items-center gap-2 bg-background/85 backdrop-blur-xl px-4 sticky top-0 z-10">
					<div className="absolute inset-x-0 bottom-0 h-px bg-linear-to-r from-transparent via-border to-transparent" />
					<SidebarTrigger className="-ml-1 text-muted-foreground hover:text-foreground transition-colors" />
					<div className="w-px h-4 bg-border/60" />
				</header>
				<div className="flex flex-1 flex-col">
					<Outlet />
				</div>
			</SidebarInset>
		</SidebarProvider>
	);
}
