import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
	PROJECT_KNOWN_PERMISSIONS,
	PROJECT_PERMISSION_GROUPS,
} from "@/components/projects/roles/permissions";
import { RoleFormDialog as SharedRoleFormDialog } from "@/components/roles/RoleFormDialog";
import {
	collectPluginCustomPermissions,
	type Plugin,
	pluginsQueryOptions,
} from "@/lib/plugin-api";
import { isProjectTemplate } from "@/lib/policy";
import { platformRolesQueryOptions, type Role } from "@/lib/role-api";

import {
	KNOWN_PERMISSIONS,
	type KnownPermission,
	PERMISSION_GROUPS,
	toPluginKnownPermissions,
} from "./permissions";

// Stable reference so `permissions` doesn't change identity on every render
// while the plugins query has no data yet.
const EMPTY_PLUGINS: Plugin[] = [];

interface RoleFormDialogProps {
	role?: Role;
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

/** Create or edit a workspace-level role. */
export function RoleFormDialog({
	role,
	open,
	onOpenChange,
}: RoleFormDialogProps) {
	const { t } = useTranslation("admin");
	const { t: tProjects } = useTranslation("projects");
	const { data: plugins = EMPTY_PLUGINS } = useQuery(pluginsQueryOptions);
	// A role that names only project resources (project/*) is a project-role
	// template, such as a converted project role. It has no owning project, so
	// it is listed here, but its permissions are the project ones.
	const template = !!role && isProjectTemplate(role.policy);
	const permissions = useMemo<KnownPermission[]>(
		() =>
			template
				? [
						...PROJECT_KNOWN_PERMISSIONS,
						...toPluginKnownPermissions(
							collectPluginCustomPermissions(plugins, "project"),
						),
					]
				: [
						...KNOWN_PERMISSIONS,
						...toPluginKnownPermissions(
							collectPluginCustomPermissions(plugins, "global"),
						),
					],
		[plugins, template],
	);

	return (
		<SharedRoleFormDialog
			scope={template ? "project" : "platform"}
			role={role}
			open={open}
			onOpenChange={onOpenChange}
			permissions={permissions}
			groups={template ? PROJECT_PERMISSION_GROUPS : PERMISSION_GROUPS}
			tScope={(key) => (template ? tProjects(key as never) : t(key as never))}
			invalidateKey={platformRolesQueryOptions.queryKey}
			labels={{
				editTitle: t("globalRoles.formDialog.editTitle"),
				createTitle: t("globalRoles.formDialog.createTitle"),
				editDescription: t("globalRoles.formDialog.editDescription"),
				createDescription: t("globalRoles.formDialog.createDescription"),
				nameLabel: t("globalRoles.formDialog.roleNameLabel"),
				namePlaceholder: t("globalRoles.formDialog.roleNamePlaceholder"),
				permissionsLabel: t("globalRoles.formDialog.permissionsLabel"),
				enabledCount: (count) =>
					t("globalRoles.formDialog.enabledCount", { count }),
				cancel: t("globalRoles.formDialog.cancel"),
				saveChanges: t("globalRoles.formDialog.saveChanges"),
				createRole: t("globalRoles.formDialog.createRole"),
			}}
		/>
	);
}
