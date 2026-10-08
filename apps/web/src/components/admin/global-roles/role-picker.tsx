import { useQuery } from "@tanstack/react-query";

import {
	RoleOptionList,
	type RoleOptionListProps,
	RolesQueryBoundary,
} from "@/components/admin/global-roles/role-option-list";
import { isFullAccessRole } from "@/components/admin/global-roles/utils";
import { isProjectTemplate } from "@/lib/policy";
import { platformRolesQueryOptions, type Role } from "@/lib/role-api";

export { isFullAccessRole };

type RolePickerProps = Omit<RoleOptionListProps<Role>, "roles" | "badgeClass">;

/**
 * Lists the global roles to choose from (see RoleOptionList). Loads them
 * itself, which needs `roles:read`.
 */
export function RolePicker(props: RolePickerProps) {
	const query = useQuery(platformRolesQueryOptions);
	return (
		<RolesQueryBoundary query={query}>
			{(roles) => (
				// A project template (project/*) only works attached per project;
				// given platform-wide it would reach every project.
				<RoleOptionList
					roles={roles.filter((r) => !isProjectTemplate(r.policy))}
					{...props}
				/>
			)}
		</RolesQueryBoundary>
	);
}
