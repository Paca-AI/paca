import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

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
	component: () => <Outlet />,
});
