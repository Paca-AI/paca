import { Edit2, Lock, Star, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
	activePermissions,
	permissionBadgeClass,
} from "@/components/admin/global-roles/utils";
import { Button } from "@/components/ui/button";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import type { GlobalRole } from "@/lib/admin-api";

interface GlobalRolesTableProps {
	roles: GlobalRole[];
	canWrite: boolean;
	onEdit: (role: GlobalRole) => void;
	onDelete: (role: GlobalRole) => void;
	/** Opens the confirmation for making `role` the default. */
	onSetDefault: (role: GlobalRole) => void;
}

export function GlobalRolesTable({
	roles,
	canWrite,
	onEdit,
	onDelete,
	onSetDefault,
}: GlobalRolesTableProps) {
	const { t } = useTranslation("admin");

	return (
		<div className="overflow-x-auto rounded-xl border">
			<Table>
				<TableHeader>
					<TableRow className="bg-muted/40 hover:bg-muted/40">
						<TableHead className="w-44 px-5 text-xs font-semibold uppercase tracking-wide">
							{t("globalRoles.table.columnName")}
						</TableHead>
						<TableHead className="px-5 text-xs font-semibold uppercase tracking-wide">
							{t("globalRoles.table.columnPermissions")}
						</TableHead>
						<TableHead className="w-32 px-5 text-xs font-semibold uppercase tracking-wide">
							{t("globalRoles.table.columnDefault")}
						</TableHead>
						{canWrite ? (
							<TableHead className="w-28 px-5 text-xs font-semibold uppercase tracking-wide" />
						) : null}
					</TableRow>
				</TableHeader>
				<TableBody>
					{roles.map((role) => {
						const active = activePermissions(role.permissions);
						return (
							<TableRow key={role.id} className="group">
								<TableCell className="px-5">
									<div className="flex items-center gap-2">
										<Lock className="size-3.5 shrink-0 text-muted-foreground/40" />
										<span className="font-mono text-sm font-medium">
											{role.name}
										</span>
									</div>
								</TableCell>
								<TableCell className="px-5">
									{active.length === 0 ? (
										<span className="text-xs italic text-muted-foreground/60">
											{t("globalRoles.table.noPermissionsAssigned")}
										</span>
									) : (
										<div className="flex flex-wrap gap-1">
											{active.map((permission) => (
												<span
													key={permission}
													className={`inline-flex items-center rounded-full border px-2 py-0.5 font-mono text-xs font-medium leading-none ${permissionBadgeClass(permission)}`}
												>
													{permission}
												</span>
											))}
										</div>
									)}
								</TableCell>
								<TableCell className="px-5">
									{role.is_default ? (
										<span className="inline-flex items-center gap-1 rounded-full bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
											<Star className="size-3 fill-current" />
											{t("globalRoles.table.default")}
										</span>
									) : null}
								</TableCell>
								{canWrite ? (
									<TableCell className="px-5">
										<div className="flex items-center justify-end gap-0.5 opacity-100 transition-opacity sm:opacity-0 sm:group-hover:opacity-100">
											{!role.is_default ? (
												<Button
													variant="ghost"
													size="icon-sm"
													onClick={() => onSetDefault(role)}
													title={t("globalRoles.table.setDefaultAction")}
													aria-label={t("globalRoles.table.setDefaultAction")}
												>
													<Star className="size-3.5" />
												</Button>
											) : null}
											<Button
												variant="ghost"
												size="icon-sm"
												onClick={() => onEdit(role)}
												title={t("globalRoles.table.editAction")}
											>
												<Edit2 className="size-3.5" />
											</Button>
											{role.is_default ? (
												// New users and agents start with the default role, so it can't
												// go; the span carries the explanation a disabled button can't.
												<span
													title={t("globalRoles.table.deleteDefaultDisabled")}
												>
													<Button
														variant="ghost"
														size="icon-sm"
														className="text-destructive"
														disabled
														aria-label={t("globalRoles.table.deleteAction")}
													>
														<Trash2 className="size-3.5" />
													</Button>
												</span>
											) : (
												<Button
													variant="ghost"
													size="icon-sm"
													className="text-destructive hover:text-destructive hover:bg-destructive/10"
													onClick={() => onDelete(role)}
													title={t("globalRoles.table.deleteAction")}
												>
													<Trash2 className="size-3.5" />
												</Button>
											)}
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
