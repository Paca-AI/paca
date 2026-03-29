import { createFileRoute, Outlet, redirect } from "@tanstack/react-router";

import { myPermissionsQueryOptions } from "@/lib/admin-api";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_authenticated/admin")({
	beforeLoad: async ({ context: { queryClient } }) => {
		const permissions = await queryClient
			.fetchQuery(myPermissionsQueryOptions)
			.catch(() => [] as string[]);

		const canReadGlobalRoles = hasPermission(permissions, "global_roles.read");

		if (!canReadGlobalRoles) {
			throw redirect({ to: "/home" });
		}
	},
	component: () => <Outlet />,
});
