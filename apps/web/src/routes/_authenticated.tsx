import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";
import { useEffect } from "react";

import { AppSidebar } from "@/components/app-shell/app-sidebar";
import { GalaxyChatDock } from "@/components/app-shell/galaxy-dock";
import { NotificationBell } from "@/components/app-shell/notification-bell";
import {
	SidebarInset,
	SidebarProvider,
	SidebarTrigger,
} from "@/components/ui/sidebar";
import { isPasswordChangeRequired } from "@/lib/api-error";
import {
	currentUserOptionalQueryOptions,
	currentUserQueryOptions,
} from "@/lib/auth-api";
import { PluginRegistryProvider } from "@/lib/plugins/registry";
import { connectSocket, disconnectSocket } from "@/lib/socket-client";

const PROJECT_ROUTE_RE = /^\/projects\/[^/]+/;

export const Route = createFileRoute("/_authenticated")({
	beforeLoad: async ({ context: { queryClient }, location }) => {
		const isProjectRoute = PROJECT_ROUTE_RE.test(location.pathname);

		if (isProjectRoute) {
			const user = await queryClient
				.fetchQuery(currentUserOptionalQueryOptions)
				.catch((err: unknown) => {
					if (isPasswordChangeRequired(err)) {
						throw redirect({ to: "/change-password" });
					}
					return null;
				});

			if (user?.must_change_password) {
				throw redirect({ to: "/change-password" });
			}

			return { user };
		}

		const user = await queryClient
			.fetchQuery(currentUserQueryOptions)
			.catch((err: unknown) => {
				if (isPasswordChangeRequired(err)) {
					throw redirect({ to: "/change-password" });
				}
				return null;
			});

		if (!user) {
			throw redirect({ to: "/" });
		}

		if (user.must_change_password) {
			throw redirect({ to: "/change-password" });
		}

		return { user };
	},
	component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
	const queryClient = useQueryClient();
	const { data: user } = useQuery(currentUserOptionalQueryOptions);

	useEffect(() => {
		if (!user) return;

		const socket = connectSocket();

		const handleNotification = ({ type }: { type: string }) => {
			if (type === "notification.created") {
				queryClient.invalidateQueries({ queryKey: ["notifications"] });
			}
		};
		socket.on("notification", handleNotification);

		return () => {
			socket.off("notification", handleNotification);
			disconnectSocket();
		};
	}, [queryClient, user]);

	return (
		<PluginRegistryProvider>
			<SidebarProvider className="h-svh">
				<AppSidebar />
				<SidebarInset className="min-w-0 overflow-hidden">
					<header className="flex h-12 shrink-0 items-center gap-2 bg-background border-b border-border/40 px-4 sticky top-0 z-10">
						<div className="absolute inset-x-0 bottom-0 h-px bg-border/40" />
						<SidebarTrigger className="-ml-1 text-muted-foreground hover:text-foreground transition-colors" />
						<div className="w-px h-4 bg-border/60" />
						{user && (
							<div className="ml-auto">
								<NotificationBell />
							</div>
						)}
					</header>
					<div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
						<Outlet />
					</div>
				</SidebarInset>
				{/* Galaxy chat dock (ADR-038 P3.2) — only for signed-in
				    sessions; renders nothing unless /auth/config advertises
				    dock_enabled. */}
				{user && <GalaxyChatDock />}
			</SidebarProvider>
		</PluginRegistryProvider>
	);
}
