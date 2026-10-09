import { useQuery } from "@tanstack/react-query";
import { Edit2, Key, Lock, Plus, Shield, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { activePermissions } from "@/components/admin/global-roles/utils";
import { DeleteProjectRoleDialog } from "@/components/projects/roles/DeleteProjectRoleDialog";
import { ProjectRoleFormDialog } from "@/components/projects/roles/ProjectRoleFormDialog";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import { useProjectPermissions } from "@/hooks/use-project-permissions";
import { isForbiddenError } from "@/lib/api-error";
import { formatDate as formatDateLocale } from "@/lib/format-date";
import { projectRolesQueryOptions, type Role } from "@/lib/role-api";

function formatDate(iso: string) {
	return formatDateLocale(iso, {
		year: "numeric",
		month: "short",
		day: "numeric",
	});
}

function RolesTableSkeleton() {
	return (
		<div className="rounded-xl border overflow-hidden">
			<div className="border-b bg-muted/40 px-5 py-3">
				<div className="flex gap-4">
					<Skeleton className="h-3.5 w-16" />
					<Skeleton className="h-3.5 w-24" />
					<Skeleton className="ml-auto h-3.5 w-14" />
				</div>
			</div>
			{["row-1", "row-2", "row-3"].map((rowKey) => (
				<div
					key={rowKey}
					className="flex items-center gap-4 border-b px-5 py-4 last:border-0"
				>
					<Skeleton className="h-5 w-36 rounded-md" />
					<div className="flex-1">
						<Skeleton className="h-4 w-3/5 max-w-72" />
					</div>
					<Skeleton className="h-4 w-20" />
					<div className="flex gap-1.5">
						<Skeleton className="size-7 rounded-md" />
						<Skeleton className="size-7 rounded-md" />
					</div>
				</div>
			))}
		</div>
	);
}

interface RoleRowProps {
	role: Role;
	canManageRoles: boolean;
	onEdit: (role: Role) => void;
	onDelete: (role: Role) => void;
}

function RoleTableRow({
	role,
	canManageRoles,
	onEdit,
	onDelete,
}: RoleRowProps) {
	const { t } = useTranslation("projects");
	// A workspace role shown here is shared: it is edited with the workspace's
	// roles. The project's own roles, built-in ones included, can be edited; a
	// built-in role cannot be deleted.
	const shared = !role.project_id;

	return (
		<TableRow className="group">
			<TableCell className="px-5">
				<div className="flex items-center gap-2">
					{shared || role.is_system ? (
						<Lock className="size-3.5 shrink-0 text-muted-foreground/40" />
					) : null}
					<span className="font-mono text-sm font-medium">{role.name}</span>
				</div>
			</TableCell>
			<TableCell className="max-w-0 px-5">
				{role.description ? (
					<span
						className="block truncate text-sm text-muted-foreground"
						title={role.description}
					>
						{role.description}
					</span>
				) : (
					<span className="text-xs italic text-muted-foreground/60">
						{t("settings.roles.noDescription")}
					</span>
				)}
			</TableCell>
			<TableCell className="px-5 text-sm text-muted-foreground">
				{formatDate(role.created_at)}
			</TableCell>
			<TableCell className="px-5">
				{!shared && canManageRoles ? (
					<div className="flex items-center justify-end gap-0.5 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100">
						<Button
							variant="ghost"
							size="icon-sm"
							onClick={() => onEdit(role)}
							title={t("settings.roles.editRole")}
						>
							<Edit2 className="size-3.5" />
						</Button>
						{role.is_system ? null : (
							<Button
								variant="ghost"
								size="icon-sm"
								className="text-destructive hover:text-destructive hover:bg-destructive/10"
								onClick={() => onDelete(role)}
								title={t("settings.roles.deleteRole")}
							>
								<Trash2 className="size-3.5" />
							</Button>
						)}
					</div>
				) : null}
			</TableCell>
		</TableRow>
	);
}

export function RolesSettings({
	projectId,
	canManageRoles,
}: {
	projectId: string;
	canManageRoles: boolean;
}) {
	const { t } = useTranslation("projects");
	const { hasProjectPermission, isLoading: isPermissionsLoading } =
		useProjectPermissions(projectId);
	const canReadRoles = hasProjectPermission("roles:read");
	const {
		data: roles,
		isLoading: isDataLoading,
		isError,
		error,
	} = useQuery({
		...projectRolesQueryOptions(projectId),
		enabled: canReadRoles,
	});
	// While permissions are still loading, canReadRoles defaults to false
	// same as a confirmed denial — guard on isPermissionsLoading (and fold
	// it into isLoading) so the section shows the skeleton instead of
	// flashing NoPermissionState first.
	const isLoading = isPermissionsLoading || isDataLoading;
	const noPermission =
		!isPermissionsLoading &&
		(!canReadRoles || (isError && isForbiddenError(error)));

	const [createOpen, setCreateOpen] = useState(false);
	const [editRole, setEditRole] = useState<Role | null>(null);
	const [deleteRole, setDeleteRole] = useState<Role | null>(null);

	const systemRoles = roles?.filter((r) => !r.project_id) ?? [];

	return (
		<div className="rounded-xl border border-border/60 bg-card p-6">
			{/* Header */}
			<div className="flex items-center justify-between mb-1">
				<div>
					<h3 className="font-[Syne] text-base font-semibold">
						{t("settings.roles.title")}
					</h3>
					<p className="text-xs text-muted-foreground mt-0.5">
						{t("settings.roles.description")}
					</p>
				</div>
				{canManageRoles ? (
					<Button
						size="sm"
						variant="outline"
						className="gap-1.5 border-border/60 shrink-0"
						onClick={() => setCreateOpen(true)}
					>
						<Plus className="size-3.5" />
						{t("settings.roles.newRole")}
					</Button>
				) : null}
			</div>

			{/* Stats strip */}
			{!isLoading && roles && roles.length > 0 ? (
				<div className="flex items-center gap-5 rounded-xl border bg-muted/20 px-5 py-3 mt-4">
					<div className="flex items-center gap-2">
						<Shield className="size-4 text-primary" />
						<span className="text-sm">
							<span className="font-semibold tabular-nums">{roles.length}</span>
							<span className="ml-1.5 text-muted-foreground">
								{t("settings.roles.rolesDefined", { count: roles.length })}
							</span>
						</span>
					</div>
					<div className="h-4 w-px bg-border" />
					<div className="flex items-center gap-2">
						<Key className="size-4 text-muted-foreground" />
						<span className="text-sm">
							<span className="font-semibold tabular-nums">
								{roles.reduce(
									(sum, r) => sum + activePermissions(r.policy).length,
									0,
								)}
							</span>
							<span className="ml-1.5 text-muted-foreground">
								{t("settings.roles.permissionGrants")}
							</span>
						</span>
					</div>
				</div>
			) : null}

			{/* Table */}
			{noPermission ? (
				<NoPermissionState
					icon={Shield}
					title={t("settings.roles.noPermission.title")}
					description={t("settings.roles.noPermission.description")}
				/>
			) : isLoading ? (
				<RolesTableSkeleton />
			) : !roles?.length ? (
				<div className="flex flex-col items-center gap-4 rounded-xl border border-dashed bg-muted/20 py-16 text-center mt-4">
					<div className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground/60">
						<Shield className="size-6" />
					</div>
					<div>
						<p className="text-sm font-medium">
							{t("settings.roles.empty.title")}
						</p>
						<p className="mt-1 text-xs text-muted-foreground">
							{t("settings.roles.empty.description")}
						</p>
					</div>
					{canManageRoles ? (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-4" />
							{t("settings.roles.empty.createRole")}
						</Button>
					) : null}
				</div>
			) : (
				<div className="overflow-x-auto rounded-xl border mt-4">
					<Table>
						<TableHeader>
							<TableRow className="bg-muted/40 hover:bg-muted/40">
								<TableHead className="w-44 px-5 text-xs font-semibold uppercase tracking-wide">
									{t("settings.roles.table.name")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("settings.roles.table.description")}
								</TableHead>
								<TableHead className="w-32 px-5 text-xs font-semibold uppercase tracking-wide">
									{t("settings.roles.table.created")}
								</TableHead>
								<TableHead className="w-20 px-5 text-xs font-semibold uppercase tracking-wide" />
							</TableRow>
						</TableHeader>
						<TableBody>
							{roles.map((role) => (
								<RoleTableRow
									key={role.id}
									role={role}
									canManageRoles={canManageRoles}
									onEdit={setEditRole}
									onDelete={setDeleteRole}
								/>
							))}
						</TableBody>
					</Table>
				</div>
			)}

			{/* System roles note */}
			{systemRoles.length > 0 ? (
				<p className="text-xs text-muted-foreground/60 mt-3 flex items-center gap-1">
					<Lock className="size-3 shrink-0" />
					{t("settings.roles.systemRolesNote")}
				</p>
			) : null}

			{/* Dialogs */}
			<ProjectRoleFormDialog
				projectId={projectId}
				open={createOpen}
				onOpenChange={setCreateOpen}
			/>

			{editRole ? (
				<ProjectRoleFormDialog
					projectId={projectId}
					role={editRole}
					open={!!editRole}
					onOpenChange={(open) => {
						if (!open) setEditRole(null);
					}}
				/>
			) : null}

			{deleteRole ? (
				<DeleteProjectRoleDialog
					projectId={projectId}
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
