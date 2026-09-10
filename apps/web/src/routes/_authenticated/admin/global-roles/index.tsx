import { useQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { Shield } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { DeleteRoleDialog } from "@/components/admin/global-roles/DeleteRoleDialog";
import { GlobalRolesHeader } from "@/components/admin/global-roles/GlobalRolesHeader";
import {
	EmptyRolesState,
	GlobalRolesErrorState,
} from "@/components/admin/global-roles/GlobalRolesStates";
import { GlobalRolesStats } from "@/components/admin/global-roles/GlobalRolesStats";
import { GlobalRolesTable } from "@/components/admin/global-roles/GlobalRolesTable";
import { RoleFormDialog } from "@/components/admin/global-roles/RoleFormDialog";
import { RolesTableSkeleton } from "@/components/admin/global-roles/RolesTableSkeleton";
import { activePermissions } from "@/components/admin/global-roles/utils";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { usePermissions } from "@/hooks/use-permissions";
import {
	type GlobalRole,
	globalRolesQueryOptions,
	myPermissionsQueryOptions,
} from "@/lib/admin-api";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_authenticated/admin/global-roles/")({
	beforeLoad: async ({ context: { queryClient } }) => {
		const permissions = await queryClient
			.fetchQuery(myPermissionsQueryOptions)
			.catch(() => [] as string[]);

		const canAccess =
			hasPermission(permissions, "global_roles.read") ||
			hasPermission(permissions, "global_roles.write") ||
			hasPermission(permissions, "global_roles.assign");

		if (!canAccess) {
			throw redirect({ to: "/home" });
		}
	},
	component: GlobalRolesPage,
});

function GlobalRolesPage() {
	const { t } = useTranslation("admin");
	const { hasPermission, isLoading: isPermissionsLoading } = usePermissions();
	const canRead = hasPermission("global_roles.read");
	const canWrite = hasPermission("global_roles.write");

	const {
		data: roles = [],
		isLoading: isDataLoading,
		isError,
	} = useQuery({ ...globalRolesQueryOptions, enabled: canRead });
	// While permissions are still loading, canRead defaults to false same as
	// a confirmed denial — fold isPermissionsLoading into isLoading (and
	// guard the noPermission check below) so the page shows the skeleton
	// instead of flashing NoPermissionState first.
	const isLoading = isPermissionsLoading || isDataLoading;

	const [createOpen, setCreateOpen] = useState(false);
	const [editRole, setEditRole] = useState<GlobalRole | null>(null);
	const [deleteRole, setDeleteRole] = useState<GlobalRole | null>(null);

	const totalGranted = roles.reduce(
		(sum, r) => sum + activePermissions(r.permissions).length,
		0,
	);

	return (
		<div className="flex flex-col gap-6 p-6 max-w-5xl w-full mx-auto">
			<GlobalRolesHeader
				canWrite={canWrite}
				onCreate={() => setCreateOpen(true)}
			/>

			{canWrite ? (
				<RoleFormDialog open={createOpen} onOpenChange={setCreateOpen} />
			) : null}

			{canRead && !isLoading && !isError && (
				<GlobalRolesStats
					rolesCount={roles.length}
					totalGranted={totalGranted}
				/>
			)}

			{!isPermissionsLoading && !canRead ? (
				<NoPermissionState
					icon={Shield}
					title={t("globalRoles.noPermission.title")}
					description={t("globalRoles.noPermission.description")}
				/>
			) : isLoading ? (
				<RolesTableSkeleton />
			) : isError ? (
				<GlobalRolesErrorState />
			) : roles.length === 0 ? (
				<EmptyRolesState
					canWrite={canWrite}
					onCreate={() => setCreateOpen(true)}
				/>
			) : (
				<GlobalRolesTable
					roles={roles}
					canWrite={canWrite}
					onEdit={setEditRole}
					onDelete={setDeleteRole}
				/>
			)}

			{editRole ? (
				<RoleFormDialog
					role={editRole}
					open={!!editRole}
					onOpenChange={(open) => {
						if (!open) setEditRole(null);
					}}
				/>
			) : null}

			{deleteRole ? (
				<DeleteRoleDialog
					role={deleteRole}
					open={!!deleteRole}
					onOpenChange={(open) => {
						if (!open) setDeleteRole(null);
					}}
				/>
			) : null}
		</div>
	);
}
