import { Edit2, KeyRound, ShieldCheck, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
	enrichRoles,
	RoleBadgeList,
	type RoleLike,
} from "@/components/shared/role-badge";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import type { User } from "@/lib/admin-api";
import { formatDate as formatDateLocale } from "@/lib/format-date";
import { getInitials } from "@/lib/initials";

function formatDate(iso: string): string {
	return formatDateLocale(iso, {
		year: "numeric",
		month: "short",
		day: "numeric",
	});
}

interface UsersTableProps {
	users: User[];
	/** Whether the viewer may edit a profile and reset a password (`users:write`). */
	canWrite: boolean;
	/** Whether the viewer may delete a user. `users:delete` is its own
	 * permission, apart from `canWrite` — a role that can edit users is not
	 * necessarily allowed to remove them — so it is shown (and hidden)
	 * independently, the same way `canAssignRole` already is below. */
	canDelete: boolean;
	/** Whether the viewer may change a user's role. That is its own permission,
	 * apart from `canWrite`, so it is shown (and hidden) independently. */
	canAssignRole: boolean;
	currentUserId?: string;
	/** The roles list, when the viewer can read it: lets role badges show
	 * descriptions and the kind of role. Without it they show names only. */
	roleDirectory?: RoleLike[];
	onEdit: (user: User) => void;
	onDelete: (user: User) => void;
	onResetPassword: (user: User) => void;
	onChangeRole: (user: User) => void;
}

const HEAD = "px-4 text-xs font-semibold uppercase tracking-wide sm:px-5";

export function UsersTable({
	users,
	canWrite,
	canDelete,
	canAssignRole,
	currentUserId,
	roleDirectory,
	onEdit,
	onDelete,
	onResetPassword,
	onChangeRole,
}: UsersTableProps) {
	const { t } = useTranslation("admin");
	// The actions column exists if either action it can hold is available —
	// mirrors each button's own gate below, so the column never shows empty
	// and never hides a button that should be there.
	const hasActionsColumn = canWrite || canDelete;

	return (
		<div className="overflow-x-auto rounded-xl border">
			<Table>
				<TableHeader>
					<TableRow className="bg-muted/40 hover:bg-muted/40">
						<TableHead className={HEAD}>
							{t("users.table.columnUser")}
						</TableHead>
						<TableHead className={`${HEAD} w-56 sm:w-64`}>
							{t("users.table.columnRole")}
						</TableHead>
						<TableHead className={`${HEAD} hidden w-32 md:table-cell`}>
							{t("users.table.columnCreated")}
						</TableHead>
						{hasActionsColumn ? (
							<TableHead className={`${HEAD} w-24 sm:w-28`}>
								<span className="sr-only">
									{t("users.table.columnActions")}
								</span>
							</TableHead>
						) : null}
					</TableRow>
				</TableHeader>
				<TableBody>
					{users.map((user) => {
						const isSelf = user.id === currentUserId;
						const display = user.full_name || user.username;
						const secondary = user.full_name
							? null
							: (user.email ?? t("users.table.noFullName"));
						const roles = enrichRoles(user.roles, roleDirectory);
						return (
							<TableRow key={user.id} className="group">
								<TableCell className="px-4 sm:px-5">
									<div className="flex min-w-0 items-center gap-3">
										<Avatar className="size-8 shrink-0">
											{user.avatar_thumb_url ? (
												<AvatarImage src={user.avatar_thumb_url} alt="" />
											) : null}
											<AvatarFallback className="bg-primary/10 text-xs font-semibold text-primary">
												{getInitials(display)}
											</AvatarFallback>
										</Avatar>
										<div className="min-w-0">
											<div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
												<span className="truncate text-sm font-medium">
													{display}
												</span>
												{isSelf ? (
													<span className="inline-flex items-center rounded-full bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
														{t("users.table.youBadge")}
													</span>
												) : null}
												{user.must_change_password ? (
													<span
														title={t("users.table.pwdResetHint")}
														className="inline-flex items-center gap-1 rounded-full bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400"
													>
														<KeyRound className="size-3" aria-hidden="true" />
														{t("users.table.pwdResetBadge")}
													</span>
												) : null}
											</div>
											<p className="truncate text-xs text-muted-foreground">
												{user.full_name ? (
													<>
														<span aria-hidden="true">@</span>
														<span className="font-mono">{user.username}</span>
													</>
												) : (
													secondary
												)}
											</p>
										</div>
									</div>
								</TableCell>
								<TableCell className="px-4 sm:px-5">
									<div className="flex min-w-0 items-center gap-1">
										<RoleBadgeList roles={roles} max={2} />
										{canAssignRole ? (
											<Button
												variant="ghost"
												size="icon-sm"
												onClick={() => onChangeRole(user)}
												title={t("users.table.changeRoleAction")}
												aria-label={t("users.table.changeRoleAction")}
												className="shrink-0 text-muted-foreground"
											>
												<ShieldCheck className="size-3.5" />
											</Button>
										) : null}
									</div>
								</TableCell>
								<TableCell className="hidden px-4 text-sm text-muted-foreground sm:px-5 md:table-cell">
									{formatDate(user.created_at)}
								</TableCell>
								{hasActionsColumn ? (
									<TableCell className="px-4 sm:px-5">
										<div className="flex items-center justify-end gap-0.5 opacity-100 transition-opacity focus-within:opacity-100 sm:opacity-0 sm:group-hover:opacity-100">
											{canWrite ? (
												<Button
													variant="ghost"
													size="icon-sm"
													onClick={() => onResetPassword(user)}
													title={t("users.table.resetPasswordAction")}
													aria-label={t("users.table.resetPasswordAction")}
												>
													<KeyRound className="size-3.5" />
												</Button>
											) : null}
											{canWrite ? (
												<Button
													variant="ghost"
													size="icon-sm"
													onClick={() => onEdit(user)}
													title={t("users.table.editAction")}
													aria-label={t("users.table.editAction")}
												>
													<Edit2 className="size-3.5" />
												</Button>
											) : null}
											{canDelete && !isSelf ? (
												<Button
													variant="ghost"
													size="icon-sm"
													className="text-destructive hover:text-destructive hover:bg-destructive/10"
													onClick={() => onDelete(user)}
													title={t("users.table.deleteAction")}
													aria-label={t("users.table.deleteAction")}
												>
													<Trash2 className="size-3.5" />
												</Button>
											) : null}
										</div>
									</TableCell>
								) : null}
							</TableRow>
						);
					})}
				</TableBody>
			</Table>
		</div>
	);
}
