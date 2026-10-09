import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";

import { myPermissionsQueryOptions } from "@/lib/admin-api";
import { actionsToPolicy } from "@/lib/policy";
import { platformRolesQueryOptions, type Role } from "@/lib/role-api";

/** A workspace role granting the actions in `permissions` (those set to true). */
export function makeRole(
	id: string,
	name: string,
	permissions: Record<string, boolean> = {},
	{ isDefault = false }: { isDefault?: boolean } = {},
): Role {
	return {
		id,
		name,
		description: "",
		policy: actionsToPolicy(
			Object.keys(permissions).filter((k) => permissions[k]),
			"platform",
		),
		project_id: null,
		is_system: false,
		attachment_count: 0,
		is_default: isDefault,
		created_at: "2026-01-01T00:00:00.000Z",
		updated_at: "2026-01-01T00:00:00.000Z",
	};
}

interface RenderWithQueriesOptions {
	/** The signed-in person's global permissions (what usePermissions reads). */
	permissions?: string[];
	/** The global roles list; `null` leaves it unloaded so a test can drive it. */
	roles?: Role[] | null;
}

/**
 * Renders inside a real QueryClient whose permissions and global roles are
 * already in the cache. Nothing is fetched (`staleTime: Infinity`), so a
 * component reads them exactly as it would in the app while the test stays
 * free of network mocks; API writes are mocked by the test itself.
 */
export function renderWithQueries(
	ui: ReactElement,
	{ permissions = [], roles = [] }: RenderWithQueriesOptions = {},
) {
	const client = new QueryClient({
		defaultOptions: {
			queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
			mutations: { retry: false },
		},
	});
	client.setQueryData(myPermissionsQueryOptions.queryKey, permissions);
	if (roles) client.setQueryData(platformRolesQueryOptions.queryKey, roles);

	const wrap = (node: ReactElement) => (
		<QueryClientProvider client={client}>{node}</QueryClientProvider>
	);
	const view = render(wrap(ui));

	return {
		client,
		...view,
		// Keep the provider when a test re-renders with new props.
		rerender: (next: ReactElement) => view.rerender(wrap(next)),
	};
}
