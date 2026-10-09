import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";

import { RoleFormDialog } from "@/components/roles/RoleFormDialog";
import {
	collectPluginCustomPermissions,
	type Plugin,
	pluginsQueryOptions,
} from "@/lib/plugin-api";
import { projectRolesQueryOptions, type Role } from "@/lib/role-api";

import {
	type KnownPermission,
	PROJECT_KNOWN_PERMISSIONS,
	PROJECT_PERMISSION_GROUPS,
	toPluginKnownPermissions,
} from "./permissions";

// Stable reference so `permissions` doesn't change identity on every render
// while the plugins query has no data yet.
const EMPTY_PLUGINS: Plugin[] = [];

interface ProjectRoleFormDialogProps {
	projectId: string;
	role?: Role;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

/** Create or edit a role that belongs to one project. */
export function ProjectRoleFormDialog({
	projectId,
	role,
	open,
	onOpenChange,
}: ProjectRoleFormDialogProps) {
	const { t } = useTranslation("projects");
	const { data: plugins = EMPTY_PLUGINS } = useQuery(pluginsQueryOptions);
	const permissions = useMemo<KnownPermission[]>(
		() => [
			...PROJECT_KNOWN_PERMISSIONS,
			...toPluginKnownPermissions(
				collectPluginCustomPermissions(plugins, "project"),
			),
		],
		[plugins],
	);

	return (
		<RoleFormDialog
			scope="project"
			projectId={projectId}
			role={role}
			open={open}
			onOpenChange={onOpenChange}
			permissions={permissions}
			groups={PROJECT_PERMISSION_GROUPS}
			tScope={(key) => t(key as never)}
			invalidateKey={projectRolesQueryOptions(projectId).queryKey}
			labels={{
				editTitle: t("roles.formDialog.editTitle"),
				createTitle: t("roles.formDialog.createTitle"),
				editDescription: t("roles.formDialog.editDescription"),
				createDescription: t("roles.formDialog.createDescription"),
				nameLabel: t("roles.formDialog.roleNameLabel"),
				namePlaceholder: t("roles.formDialog.roleNamePlaceholder"),
				permissionsLabel: t("roles.formDialog.permissionsLabel"),
				enabledCount: (count) => t("roles.formDialog.enabledCount", { count }),
				cancel: t("roles.formDialog.cancel"),
				saveChanges: t("roles.formDialog.saveChanges"),
				createRole: t("roles.formDialog.createRole"),
			}}
		/>
	);
}
