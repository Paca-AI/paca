import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
	CatchBoundary,
	createFileRoute,
	Outlet,
	redirect,
	useRouterState,
} from "@tanstack/react-router";
import { type CSSProperties, lazy, Suspense, useEffect } from "react";

import { AppSidebar } from "@/components/app-shell/app-sidebar";
import { NotificationBell } from "@/components/app-shell/notification-bell";
import { RouteErrorComponent } from "@/components/route-error-boundary";
import {
	SidebarInset,
	SidebarProvider,
	SidebarTrigger,
} from "@/components/ui/sidebar";
import { onTokenRefreshed } from "@/lib/api-client";
import { isPasswordChangeRequired } from "@/lib/api-error";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { playNotificationSound } from "@/lib/notification-sound";
import { SIDEBAR_PANE, usePaneWidth } from "@/lib/pane-width";
import { PluginRegistryProvider } from "@/lib/plugins/registry";
import { ShortcutProvider } from "@/lib/shortcuts/provider";
import {
	connectSocket,
	disconnectSocket,
	getSocket,
} from "@/lib/socket-client";
import { cn } from "@/lib/utils";

// Lazy: pulls in @assistant-ui/react and the agent/conversation stack, which
// is otherwise dead weight on every authenticated page (dashboard, admin,
// settings, ...) since the panel starts closed and most loads never open it.
const GlobalAIChatFloat = lazy(() =>
	import("@/components/projects/ai-chat-float-global").then((m) => ({
		default: m.GlobalAIChatFloat,
	})),
);

const PROJECT_ROUTE_RE = /^\/projects\/[^/]+/;
// The global Conversations page (and its nested $conversationId route) has
// its own dedicated "new conversation" entry point (see
// routes/_authenticated/conversations/index.tsx's NewConversationThread) —
// same reasoning as the project-scoped conversations page hiding its own
// AIChatFloat in routes/_authenticated/projects/$projectId.tsx, just via a
// pathname check here instead of useMatches()/routeId since this layout
// sits above the project/non-project split rather than inside one project.
const CONVERSATIONS_ROUTE_RE = /^\/conversations(\/|$)/;

export const Route = createFileRoute("/_authenticated")({
	beforeLoad: async ({ context: { queryClient }, location }) => {
		const user = await queryClient
			.fetchQuery(currentUserQueryOptions)
			.catch((err: unknown) => {
				if (isPasswordChangeRequired(err)) {
					throw redirect({ to: "/change-password" });
				}
				return null;
			});

		// Every authenticated route, project routes included, needs a signed-in
		// user: there are no public projects.
		if (!user) {
			// Carry the page the user was heading to through sign-in, so a
			// deep link — notably a plugin callback such as a GitHub App
			// installation return, whose one-time parameters can't be
			// reproduced — survives a fully expired session.
			throw redirect({
				to: "/",
				search:
					location.pathname === "/home" ? {} : { redirect: location.href },
			});
		}

		if (user?.must_change_password) {
			throw redirect({ to: "/change-password" });
		}

		return { user };
	},
	component: AuthenticatedLayout,
});

function AuthenticatedLayout() {
	const queryClient = useQueryClient();
	const { data: user } = useQuery(currentUserQueryOptions);
	const pathname = useRouterState({ select: (s) => s.location.pathname });
	const sidebarPane = usePaneWidth(SIDEBAR_PANE);
	// Global agent chat is available everywhere except inside a project
	// (project pages already mount their own project-scoped AIChatFloat, see
	// routes/_authenticated/projects/$projectId.tsx) or on the global
	// Conversations page itself (see CONVERSATIONS_ROUTE_RE above).
	const showGlobalChat =
		!!user &&
		!PROJECT_ROUTE_RE.test(pathname) &&
		!CONVERSATIONS_ROUTE_RE.test(pathname);

	useEffect(() => {
		if (!user) return;

		const socket = connectSocket();

		const handleNotification = ({ type }: { type: string }) => {
			if (type === "notification.created") {
				queryClient.invalidateQueries({ queryKey: ["notifications"] });
				playNotificationSound();
			}
		};
		socket.on("notification", handleNotification);

		// The realtime service's socket auth reads the access_token cookie only
		// once, at connect time (see services/realtime/src/server.ts's io.use
		// middleware), and reuses that same token for every subsequent "join"
		// call for as long as the WebSocket connection stays up — which, unlike
		// a plain HTTP request, has no natural expiry of its own. Once the
		// token expires, every "join" from then on (e.g. a project route
		// re-mounting) silently 401s, leaving the socket in zero rooms.
		// api-client already refreshes the cookie whenever a real HTTP call
		// 401s (e.g. the chat heartbeat ping while a conversation is open) —
		// piggyback on that same event to reconnect the socket, so its next
		// auth handshake picks up the freshly refreshed cookie.
		// useProjectRealtime's "connect" handler then re-joins the current
		// project automatically, exactly as it already does for a real
		// network drop.
		const unsubscribe = onTokenRefreshed(() => {
			const current = getSocket();
			if (current?.connected) {
				current.disconnect().connect();
			}
		});

		return () => {
			unsubscribe();
			socket.off("notification", handleNotification);
			disconnectSocket();
		};
	}, [queryClient, user]);

	return (
		<PluginRegistryProvider>
			<ShortcutProvider>
				<SidebarProvider
					className={cn(
						"h-svh",
						// Follow the pointer 1:1 while dragging the sidebar edge.
						sidebarPane.dragging &&
							"[&_[data-slot=sidebar-container]]:transition-none [&_[data-slot=sidebar-gap]]:transition-none",
					)}
					style={
						{ "--sidebar-width": `${sidebarPane.width}px` } as CSSProperties
					}
				>
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
							{/* Wraps only the routed content, not AppSidebar/header above —
							    a plain `errorComponent` on this route (or root's own, which
							    this falls back to today) would catch a descendant's failure
							    by replacing this route's *entire* rendered output, sidebar
							    included, since a route's errorComponent boundary wraps its
							    own component, not just its own <Outlet/>. Reset on pathname
							    change so navigating to a different page — including via the
							    sidebar itself, still rendered outside this boundary — gets a
							    fresh attempt instead of showing the previous page's error. */}
							<CatchBoundary
								getResetKey={() => pathname}
								errorComponent={RouteErrorComponent}
							>
								<Outlet />
							</CatchBoundary>
						</div>
					</SidebarInset>
				</SidebarProvider>
				{showGlobalChat && (
					<Suspense fallback={null}>
						<GlobalAIChatFloat />
					</Suspense>
				)}
			</ShortcutProvider>
		</PluginRegistryProvider>
	);
}
