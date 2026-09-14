import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { createFileRoute, redirect } from "@tanstack/react-router";
import { Users } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

import { DeleteUserDialog } from "@/components/admin/users/DeleteUserDialog";
import { ResetPasswordDialog } from "@/components/admin/users/ResetPasswordDialog";
import { UserFormDialog } from "@/components/admin/users/UserFormDialog";
import { UsersHeader } from "@/components/admin/users/UsersHeader";
import {
	EmptyUsersState,
	UsersErrorState,
} from "@/components/admin/users/UsersStates";
import { UsersStats } from "@/components/admin/users/UsersStats";
import { UsersTable } from "@/components/admin/users/UsersTable";
import { UsersTableSkeleton } from "@/components/admin/users/UsersTableSkeleton";
import { NoPermissionState } from "@/components/shared/no-permission-state";
import { Pagination } from "@/components/ui/pagination";
import { usePermissions } from "@/hooks/use-permissions";
import {
	myPermissionsQueryOptions,
	type User,
	usersQueryOptions,
} from "@/lib/admin-api";
import { currentUserQueryOptions } from "@/lib/auth-api";
import { hasPermission } from "@/lib/permissions";

export const Route = createFileRoute("/_authenticated/admin/users/")({
	beforeLoad: async ({ context: { queryClient } }) => {
		const permissions = await queryClient
			.fetchQuery(myPermissionsQueryOptions)
			.catch(() => [] as string[]);

		const canAccess =
			hasPermission(permissions, "users.read") ||
			hasPermission(permissions, "users.write") ||
			hasPermission(permissions, "users.delete");

		if (!canAccess) {
			throw redirect({ to: "/home" });
		}
	},
	component: UsersManagementPage,
});

function UsersManagementPage() {
	const { t } = useTranslation("admin");
	const { hasPermission, isLoading: isPermissionsLoading } = usePermissions();
	const canRead = hasPermission("users.read");
	const canWrite = hasPermission("users.write");

	const [page, setPage] = useState(1);
	const pageSize = 20;

	const {
		data: pagedUsers,
		isLoading: isDataLoading,
		isError,
	} = useQuery({
		...usersQueryOptions(page, pageSize),
		enabled: canRead,
		placeholderData: keepPreviousData,
	});
	// While permissions are still loading, canRead defaults to false same as
	// a confirmed denial — fold isPermissionsLoading into isLoading (and
	// guard the noPermission check below) so the page shows the skeleton
	// instead of flashing NoPermissionState first.
	const isLoading = isPermissionsLoading || isDataLoading;

	const { data: currentUser } = useQuery(currentUserQueryOptions);

	const [createOpen, setCreateOpen] = useState(false);
	const [editUser, setEditUser] = useState<User | null>(null);
	const [deleteUser, setDeleteUser] = useState<User | null>(null);
	const [resetPasswordUser, setResetPasswordUser] = useState<User | null>(null);

	const users = pagedUsers?.items ?? [];
	const total = pagedUsers?.total ?? 0;
	const totalPages = Math.max(1, Math.ceil(total / pageSize));
	const mustChangePasswordCount = pagedUsers?.must_change_password_count ?? 0;

	// A delete (or a stale page number from a shrinking result set) can leave
	// `page` pointing past the last page — e.g. deleting the last user on the
	// last page. Snap back so the table never renders an empty page while
	// users still exist on an earlier one.
	useEffect(() => {
		if (!isLoading && !isError && page > totalPages) {
			setPage(totalPages);
		}
	}, [isLoading, isError, page, totalPages]);

	return (
		<div className="flex flex-col gap-6 p-6 max-w-5xl w-full mx-auto">
			<UsersHeader canWrite={canWrite} onCreate={() => setCreateOpen(true)} />

			{canWrite ? (
				<UserFormDialog open={createOpen} onOpenChange={setCreateOpen} />
			) : null}

			{canRead && !isLoading && !isError && (
				<UsersStats
					total={total}
					mustChangePasswordCount={mustChangePasswordCount}
				/>
			)}

			{!isPermissionsLoading && !canRead ? (
				<NoPermissionState
					icon={Users}
					title={t("users.noPermission.title")}
					description={t("users.noPermission.description")}
				/>
			) : isLoading ? (
				<UsersTableSkeleton />
			) : isError ? (
				<UsersErrorState />
			) : users.length === 0 ? (
				<EmptyUsersState
					canWrite={canWrite}
					onCreate={() => setCreateOpen(true)}
				/>
			) : (
				<div className="flex flex-col gap-4">
					<UsersTable
						users={users}
						canWrite={canWrite}
						currentUserId={currentUser?.id}
						onEdit={setEditUser}
						onDelete={setDeleteUser}
						onResetPassword={setResetPasswordUser}
					/>
					<Pagination
						page={page}
						totalPages={totalPages}
						onPageChange={setPage}
					/>
				</div>
			)}

			{editUser ? (
				<UserFormDialog
					user={editUser}
					open={!!editUser}
					onOpenChange={(open) => {
						if (!open) setEditUser(null);
					}}
				/>
			) : null}

			{deleteUser ? (
				<DeleteUserDialog
					user={deleteUser}
					open={!!deleteUser}
					onOpenChange={(open) => {
						if (!open) setDeleteUser(null);
					}}
				/>
			) : null}

			{resetPasswordUser ? (
				<ResetPasswordDialog
					user={resetPasswordUser}
					open={!!resetPasswordUser}
					onOpenChange={(open) => {
						if (!open) setResetPasswordUser(null);
					}}
				/>
			) : null}
		</div>
	);
}
