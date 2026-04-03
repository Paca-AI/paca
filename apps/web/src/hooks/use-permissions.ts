import { useQuery } from "@tanstack/react-query";

import { myPermissionsQueryOptions } from "@/lib/admin-api";
import {
	hasAnyPermission as checkAnyPermission,
	hasPermission as checkPermission,
} from "@/lib/permissions";

export function usePermissions() {
	const { data: permissions = [], isLoading } = useQuery(
		myPermissionsQueryOptions,
	);

	const hasPermission = (permission: string) => {
		return checkPermission(permissions, permission);
	};

	const hasAnyPermission = (perms: string[]) => {
		return checkAnyPermission(permissions, perms);
	};

	return { permissions, hasPermission, hasAnyPermission, isLoading };
}
