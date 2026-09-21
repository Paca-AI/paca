import { useQuery } from "@tanstack/react-query";

import {
	RoleOptionList,
	type RoleOptionListProps,
	RolesQueryBoundary,
} from "@/components/admin/global-roles/role-option-list";
import { activePermissions } from "@/components/admin/global-roles/utils";
import { type GlobalRole, globalRolesQueryOptions } from "@/lib/admin-api";

/** A role holding the `*` wildcard can do everything. */
export function isFullAccessRole(role: {
	permissions: Record<string, unknown>;
}): boolean {
	return activePermissions(role.permissions).includes("*");
}

type RolePickerProps = Omit<
	RoleOptionListProps<GlobalRole>,
	"roles" | "badgeClass"
>;

/**
 * Lists the global roles to choose from (see RoleOptionList). Loads them
 * itself, which needs `global_roles.read`.
 */
export function RolePicker(props: RolePickerProps) {
	const query = useQuery(globalRolesQueryOptions);
	return (
		<RolesQueryBoundary query={query}>
			{(roles) => <RoleOptionList roles={roles} {...props} />}
		</RolesQueryBoundary>
	);
}
