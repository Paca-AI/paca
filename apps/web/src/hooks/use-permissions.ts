import { useQuery } from "@tanstack/react-query";

import { myPermissionsQueryOptions } from "@/lib/admin-api";
import { hasPermission as checkPermission } from "@/lib/permissions";

export function usePermissions() {
	const { data: permissions = [], isLoading } = useQuery(
		myPermissionsQueryOptions,
	);

	const hasPermission = (permission: string) => {
		return checkPermission(permissions, permission);
	};

	return { permissions, hasPermission, isLoading };
}
