import { createRouter } from "@tanstack/react-router";
import { queryClient } from "./integrations/react-query/query-client";
import { routeTree } from "./routeTree.gen";

export const router = createRouter({
	routeTree,
	scrollRestoration: true,
	defaultPreload: "intent",
	defaultPreloadStaleTime: 0,
	context: { queryClient },
});

declare module "@tanstack/react-router" {
	interface Register {
		router: typeof router;
	}
}
