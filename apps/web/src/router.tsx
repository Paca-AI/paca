import { createRouter } from "@tanstack/react-router";
import { queryClient } from "./integrations/react-query/query-client";
import { setNavigate } from "./lib/api-client";
import { routeTree } from "./routeTree.gen";

export const router = createRouter({
	routeTree,
	scrollRestoration: true,
	defaultPreload: "intent",
	defaultPreloadStaleTime: 0,
	context: { queryClient },
});

// Inject SPA navigation into the API client so 403 PASSWORD_CHANGE_REQUIRED
// errors trigger a proper router navigation instead of a hard reload.
setNavigate((to) => void router.navigate({ to: to as "/" }));

declare module "@tanstack/react-router" {
	interface Register {
		router: typeof router;
	}
}
