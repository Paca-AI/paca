import { ChevronDown, Edit2, KeyRound, Lock, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

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

function formatDate(iso: string): string {
	return formatDateLocale(iso, {
		year: "numeric",
		month: "short",
		day: "numeric",
	});
}

interface UsersTableProps {
	users: User[];
	canWrite: boolean;
	/** Whether the viewer may change a user's role. That is its own permission,
	 * apart from `canWrite`, so it is shown (and hidden) independently. */
	canAssignRole: boolean;
	currentUserId?: string;
	onEdit: (user: User) => void;
	onDelete: (user: User) => void;
	onResetPassword: (user: User) => void;
	onChangeRole: (user: User) => void;
}

const ROLE_PILL =
	"inline-flex items-center rounded-full border px-2 py-0.5 font-mono text-xs font-medium leading-none text-foreground/80";

export function UsersTable({
	users,
	canWrite,
	canAssignRole,
	currentUserId,
	onEdit,
	onDelete,
	onResetPassword,
	onChangeRole,
}: UsersTableProps) {
	const { t } = useTranslation("admin");

	return (
		<div className="overflow-x-auto rounded-xl border">
			<Table>
				<TableHeader>
					<TableRow className="bg-muted/40 hover:bg-muted/40">
						<TableHead className="w-44 px-5 text-xs font-semibold uppercase tracking-wide">
							{t("users.table.columnUsername")}
						</TableHead>
						<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
							{t("users.table.columnFullName")}
						</TableHead>
						<TableHead className="w-36 px-5 text-xs font-semibold uppercase tracking-wide">
							{t("users.table.columnRole")}
						</TableHead>
						<TableHead className="w-32 px-5 text-xs font-semibold uppercase tracking-wide">
							{t("users.table.columnCreated")}
						</TableHead>
						{canWrite ? (
							<TableHead className="w-28 px-5 text-xs font-semibold uppercase tracking-wide" />
						) : null}
					</TableRow>
				</TableHeader>
				<TableBody>
					{users.map((user) => {
						const isSelf = user.id === currentUserId;
						return (
							<TableRow key={user.id} className="group">
								<TableCell className="px-5">
									<div className="flex items-center gap-2">
										<Lock className="size-3.5 shrink-0 text-muted-foreground/40" />
										<span className="font-mono text-sm font-medium">
											{user.username}
										</span>
										{user.must_change_password ? (
											<span className="inline-flex items-center rounded-full bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
												{t("users.table.pwdResetBadge")}
											</span>
										) : null}
										{isSelf ? (
											<span className="inline-flex items-center rounded-full bg-primary/10 px-1.5 py-0.5 text-xs font-medium text-primary">
												{t("users.table.youBadge")}
											</span>
										) : null}
									</div>
								</TableCell>
								<TableCell className="px-5 text-sm">
									{user.full_name || (
										<span className="italic text-muted-foreground/60">
											{t("users.table.noFullName")}
										</span>
									)}
								</TableCell>
								<TableCell className="px-5">
									{canAssignRole ? (
										<button
											type="button"
											onClick={() => onChangeRole(user)}
											title={t("users.table.changeRoleAction")}
											className={`${ROLE_PILL} gap-1 transition-colors hover:border-primary/40 hover:bg-primary/5 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/30`}
										>
											{user.role}
											<ChevronDown
												className="size-3 text-muted-foreground"
												aria-hidden="true"
											/>
										</button>
									) : (
										<span className={ROLE_PILL}>{user.role}</span>
									)}
								</TableCell>
								<TableCell className="px-5 text-sm text-muted-foreground">
									{formatDate(user.created_at)}
								</TableCell>
								{canWrite ? (
									<TableCell className="px-5">
										<div className="flex items-center justify-end gap-0.5 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100">
											<Button
												variant="ghost"
												size="icon-sm"
												onClick={() => onResetPassword(user)}
												title={t("users.table.resetPasswordAction")}
											>
												<KeyRound className="size-3.5" />
											</Button>
											<Button
												variant="ghost"
												size="icon-sm"
												onClick={() => onEdit(user)}
												title={t("users.table.editAction")}
											>
												<Edit2 className="size-3.5" />
											</Button>
											{!isSelf ? (
												<Button
													variant="ghost"
													size="icon-sm"
													className="text-destructive hover:text-destructive hover:bg-destructive/10"
													onClick={() => onDelete(user)}
													title={t("users.table.deleteAction")}
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
