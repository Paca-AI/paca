import { useQuery } from "@tanstack/react-query";

import {
	RoleOptionList,
	type RoleOptionListProps,
	RolesQueryBoundary,
} from "@/components/admin/global-roles/role-option-list";
import { projectPermissionBadgeClass } from "@/components/projects/roles/utils";
import { projectRolesQueryOptions, type Role } from "@/lib/role-api";

type ProjectRolePickerProps = Omit<
	RoleOptionListProps<Role>,
	"roles" | "badgeClass"
> & {
	projectId: string;
};

/**
 * Lists the roles that can be given inside a project (its own and the
 * workspace roles attachable there) to choose from, each with a glance at
 * what it grants (see RoleOptionList). Loads them itself, which needs
 * `roles:read`.
 */
export function ProjectRolePicker({
	projectId,
	...props
}: ProjectRolePickerProps) {
	const query = useQuery(projectRolesQueryOptions(projectId));
	return (
		<RolesQueryBoundary query={query}>
			{(roles) => (
				<RoleOptionList
					roles={roles}
					badgeClass={projectPermissionBadgeClass}
					{...props}
				/>
			)}
		</RolesQueryBoundary>
	);
}
