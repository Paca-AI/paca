import { useQuery } from "@tanstack/react-query";

import {
	type RoleOption,
	RoleOptionList,
	type RoleOptionListProps,
	RolesQueryBoundary,
} from "@/components/admin/global-roles/role-option-list";
import { projectPermissionBadgeClass } from "@/components/projects/roles/utils";
import { type ProjectRole, projectRolesQueryOptions } from "@/lib/project-api";

const toRoleOptions = (roles: ProjectRole[]): RoleOption[] =>
	roles.map((role) => ({
		id: role.id,
		name: role.role_name,
		permissions: role.permissions,
	}));

type ProjectRolePickerProps = Omit<
	RoleOptionListProps<RoleOption>,
	"roles" | "badgeClass" | "none"
> & {
	projectId: string;
};

/**
 * Lists a project's roles to choose from, each with a glance at what it
 * grants in that project (see RoleOptionList). Loads them itself, which needs
 * `project.roles.read`.
 */
export function ProjectRolePicker({
	projectId,
	...props
}: ProjectRolePickerProps) {
	const query = useQuery({
		...projectRolesQueryOptions(projectId),
		select: toRoleOptions,
	});
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
