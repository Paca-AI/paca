import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";

import {
	type GlobalRole,
	globalRolesQueryOptions,
	myPermissionsQueryOptions,
} from "@/lib/admin-api";

export function makeRole(
	id: string,
	name: string,
	permissions: Record<string, boolean> = {},
): GlobalRole {
	return {
		id,
		name,
		permissions,
		created_at: "2026-01-01T00:00:00.000Z",
		updated_at: "2026-01-01T00:00:00.000Z",
	};
}

interface RenderWithQueriesOptions {
	/** The signed-in person's global permissions (what usePermissions reads). */
	permissions?: string[];
	/** The global roles list; `null` leaves it unloaded so a test can drive it. */
	roles?: GlobalRole[] | null;
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
	if (roles) client.setQueryData(globalRolesQueryOptions.queryKey, roles);

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
