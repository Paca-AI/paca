import { useQuery } from "@tanstack/react-query";
import { Edit2, Key, Lock, Plus, Shield, Trash2 } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { DeleteProjectRoleDialog } from "@/components/projects/roles/DeleteProjectRoleDialog";
import { ProjectRoleFormDialog } from "@/components/projects/roles/ProjectRoleFormDialog";
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
import { type ProjectRole, projectRolesQueryOptions } from "@/lib/project-api";

function activePermissions(perms: Record<string, unknown>): string[] {
	return Object.entries(perms)
		.filter(([, v]) => Boolean(v))
		.map(([k]) => k);
}

function formatDate(iso: string, locale?: string) {
	return new Date(iso).toLocaleDateString(locale, {
		year: "numeric",
		month: "short",
		day: "numeric",
	});
}

function projectPermissionBadgeClass(key: string): string {
	const domain = key.split(".").slice(0, 2).join(".");
	if (domain === "projects") {
		return "bg-primary/10 text-primary border-primary/20 dark:bg-primary/20";
	}
	if (domain === "project.members") {
		return "bg-violet-50 text-violet-700 border-violet-200 dark:bg-violet-900/20 dark:text-violet-400 dark:border-violet-700/30";
	}
	if (domain === "project.roles") {
		return "bg-sky-50 text-sky-700 border-sky-200 dark:bg-sky-900/20 dark:text-sky-400 dark:border-sky-700/30";
	}
	if (domain === "tasks") {
		return "bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-900/20 dark:text-amber-400 dark:border-amber-700/30";
	}
	if (domain === "sprints") {
		return "bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-900/20 dark:text-emerald-400 dark:border-emerald-700/30";
	}
	if (domain === "docs") {
		return "bg-purple-50 text-purple-700 border-purple-200 dark:bg-purple-900/20 dark:text-purple-400 dark:border-purple-700/30";
	}
	if (domain === "agents") {
		return "bg-pink-50 text-pink-700 border-pink-200 dark:bg-pink-900/20 dark:text-pink-400 dark:border-pink-700/30";
	}
	return "bg-muted text-muted-foreground border-border";
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
					<div className="flex flex-1 gap-1.5">
						<Skeleton className="h-5 w-28 rounded-full" />
						<Skeleton className="h-5 w-24 rounded-full" />
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
	role: ProjectRole;
	canManageRoles: boolean;
	onEdit: (role: ProjectRole) => void;
	onDelete: (role: ProjectRole) => void;
}

function RoleTableRow({
	role,
	canManageRoles,
	onEdit,
	onDelete,
}: RoleRowProps) {
	const { t, i18n } = useTranslation();
	const isSystem = !role.project_id;
	const active = activePermissions(role.permissions);

	return (
		<TableRow className="group">
			<TableCell className="px-5">
				<div className="flex items-center gap-2">
					<Lock className="size-3.5 shrink-0 text-muted-foreground/40" />
					<span className="font-mono text-sm font-medium">
						{role.role_name}
					</span>
				</div>
			</TableCell>
			<TableCell className="px-5">
				{active.length === 0 ? (
					<span className="text-xs italic text-muted-foreground/60">
						{t("project.settings.roles.noPermsAssigned")}
					</span>
				) : (
					<div className="flex flex-wrap gap-1">
						{active.map((permission) => (
							<span
								key={permission}
								className={`inline-flex items-center rounded-full border px-2 py-0.5 font-mono text-xs font-medium leading-none ${projectPermissionBadgeClass(permission)}`}
							>
								{permission}
							</span>
						))}
					</div>
				)}
			</TableCell>
			<TableCell className="px-5 text-sm text-muted-foreground">
				{formatDate(role.created_at, i18n.language)}
			</TableCell>
			<TableCell className="px-5">
				{!isSystem && canManageRoles ? (
					<div className="flex items-center justify-end gap-0.5 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100">
						<Button
							variant="ghost"
							size="icon-sm"
							onClick={() => onEdit(role)}
							title={t("project.settings.roles.editRole")}
						>
							<Edit2 className="size-3.5" />
						</Button>
						<Button
							variant="ghost"
							size="icon-sm"
							className="text-destructive hover:text-destructive hover:bg-destructive/10"
							onClick={() => onDelete(role)}
							title={t("project.settings.roles.deleteRole")}
						>
							<Trash2 className="size-3.5" />
						</Button>
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
	const { t } = useTranslation();
	const { data: roles, isLoading } = useQuery(
		projectRolesQueryOptions(projectId),
	);

	const [createOpen, setCreateOpen] = useState(false);
	const [editRole, setEditRole] = useState<ProjectRole | null>(null);
	const [deleteRole, setDeleteRole] = useState<ProjectRole | null>(null);

	const systemRoles = roles?.filter((r) => !r.project_id) ?? [];

	return (
		<div className="rounded-xl border border-border/60 bg-card p-6">
			{/* Header */}
			<div className="flex items-center justify-between mb-1">
				<div>
					<h3 className="font-[Syne] text-base font-semibold">
						{t("project.settings.roles.title")}
					</h3>
					<p className="text-xs text-muted-foreground mt-0.5">
						{t("project.settings.roles.subtitle")}
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
						{t("project.settings.roles.newRole")}
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
								{t("project.settings.roles.rolesDefinedLabel", {
									count: roles.length,
								})}
							</span>
						</span>
					</div>
					<div className="h-4 w-px bg-border" />
					<div className="flex items-center gap-2">
						<Key className="size-4 text-muted-foreground" />
						<span className="text-sm">
							<span className="font-semibold tabular-nums">
								{roles.reduce(
									(sum, r) => sum + activePermissions(r.permissions).length,
									0,
								)}
							</span>
							<span className="ml-1.5 text-muted-foreground">
								{t("project.settings.roles.permissionGrants")}
							</span>
						</span>
					</div>
				</div>
			) : null}

			{/* Table */}
			{isLoading ? (
				<RolesTableSkeleton />
			) : !roles?.length ? (
				<div className="flex flex-col items-center gap-4 rounded-xl border border-dashed bg-muted/20 py-16 text-center mt-4">
					<div className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground/60">
						<Shield className="size-6" />
					</div>
					<div>
						<p className="text-sm font-medium">
							{t("project.settings.roles.emptyTitle")}
						</p>
						<p className="mt-1 text-xs text-muted-foreground">
							{t("project.settings.roles.emptyDesc")}
						</p>
					</div>
					{canManageRoles ? (
						<Button
							size="sm"
							variant="outline"
							onClick={() => setCreateOpen(true)}
						>
							<Plus className="size-4" />
							{t("project.settings.roles.createRole")}
						</Button>
					) : null}
				</div>
			) : (
				<div className="overflow-x-auto rounded-xl border mt-4">
					<Table>
						<TableHeader>
							<TableRow className="bg-muted/40 hover:bg-muted/40">
								<TableHead className="w-44 px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.roles.colName")}
								</TableHead>
								<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.roles.colPermissions")}
								</TableHead>
								<TableHead className="w-32 px-5 text-xs font-semibold uppercase tracking-wide">
									{t("project.settings.roles.colCreated")}
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
					{t("project.settings.roles.systemRolesNote")}
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
